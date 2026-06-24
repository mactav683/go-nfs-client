// Copyright 2026 The go-nfs-client Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/mactav683/go-nfs-client/xdr"
)

// ClientOptions configures a Client.
type ClientOptions struct {
	// Cred is the credential sent with every call (e.g. AUTH_SYS).
	Cred OpaqueAuth
	// Verf is the verifier sent with every call (usually AUTH_NULL).
	Verf OpaqueAuth
}

// Client is a synchronous ONC RPC client over a single stream connection
// (typically TCP). Calls are serialized: each Call writes a request record and
// reads the matching reply before returning. context.Context cancellation
// aborts an in-flight call.
//
// The serialized model keeps the v4.0 path simple and matches NFSv4's COMPOUND
// usage where each request is a self-contained round trip.
type Client struct {
	conn io.ReadWriteCloser
	opts ClientOptions

	mu     sync.Mutex
	nextID uint32
}

// NewClient returns a Client that communicates over conn using opts.
func NewClient(conn io.ReadWriteCloser, opts ClientOptions) *Client {
	return &Client{
		conn:   conn,
		opts:   opts,
		nextID: rand.Uint32(),
	}
}

// Dial establishes a TCP connection to address (host:port) and returns a
// Client configured with opts. The provided context bounds the dial.
func Dial(ctx context.Context, address string, opts ClientOptions) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("rpc: dialing %s: %w", address, err)
	}
	// Disable Nagle's algorithm: NFS COMPOUND round trips are latency
	// sensitive and benefit from immediate flushing.
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}
	return NewClient(conn, opts), nil
}

// SetDeadline applies a read/write deadline to the underlying connection when
// it supports one. It is a no-op for non-net connections.
func (c *Client) SetDeadline(t time.Time) {
	if dc, ok := c.conn.(interface{ SetDeadline(time.Time) error }); ok {
		_ = dc.SetDeadline(t)
	}
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// allocXID returns a fresh transaction id.
func (c *Client) allocXID() uint32 {
	c.nextID++
	if c.nextID == 0 {
		c.nextID = 1
	}
	return c.nextID
}

// Call performs a synchronous RPC: it builds the call header for
// prog/vers/proc, lets encodeArgs append the procedure arguments, writes the
// record, reads the matching reply, and invokes decodeRes on the result body.
//
// Call returns an error if the context is cancelled, the transport fails, the
// reply xid does not match, or the reply is not an accepted SUCCESS response.
func (c *Client) Call(
	ctx context.Context,
	prog, vers, proc uint32,
	encodeArgs func(*xdr.Encoder),
	decodeRes func(*xdr.Decoder),
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	xid := c.allocXID()

	// Build the request payload: call header followed by procedure arguments.
	var reqBuf bytes.Buffer
	enc := xdr.NewEncoder(&reqBuf)
	hdr := CallHeader{
		XID:  xid,
		Prog: prog,
		Vers: vers,
		Proc: proc,
		Cred: c.opts.Cred,
		Verf: c.opts.Verf,
	}
	hdr.Encode(enc)
	if encodeArgs != nil {
		encodeArgs(enc)
	}
	if err := enc.Err(); err != nil {
		return fmt.Errorf("rpc: encoding request: %w", err)
	}

	// Perform the round trip in a goroutine so context cancellation can
	// preempt a blocked write or read.
	type result struct {
		reply []byte
		err   error
	}
	done := make(chan result, 1)
	go func() {
		if err := WriteRecord(c.conn, reqBuf.Bytes()); err != nil {
			done <- result{err: fmt.Errorf("rpc: writing request: %w", err)}
			return
		}
		reply, err := ReadRecord(c.conn)
		if err != nil {
			done <- result{err: fmt.Errorf("rpc: reading reply: %w", err)}
			return
		}
		done <- result{reply: reply}
	}()

	select {
	case <-ctx.Done():
		// Best-effort: close the connection so the blocked goroutine unwinds.
		// A cancelled connection is no longer reusable.
		_ = c.conn.Close()
		return ctx.Err()
	case res := <-done:
		if res.err != nil {
			return res.err
		}
		return c.decodeReply(xid, res.reply, decodeRes)
	}
}

// decodeReply parses a reply record, validates the xid and status, and invokes
// decodeRes on the remaining result body.
func (c *Client) decodeReply(xid uint32, reply []byte, decodeRes func(*xdr.Decoder)) error {
	dec := xdr.NewDecoder(bytes.NewReader(reply))
	hdr, err := DecodeReplyHeader(dec)
	if err != nil {
		return fmt.Errorf("rpc: decoding reply header: %w", err)
	}
	if hdr.XID != xid {
		return fmt.Errorf("rpc: reply xid %#x does not match request xid %#x", hdr.XID, xid)
	}
	if err := hdr.Error(); err != nil {
		return err
	}
	if decodeRes != nil {
		decodeRes(dec)
		if err := dec.Err(); err != nil {
			return fmt.Errorf("rpc: decoding result: %w", err)
		}
	}
	return nil
}
