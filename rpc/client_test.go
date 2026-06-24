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
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/mactav683/go-nfs-client/xdr"
)

// fakeConn is an in-memory net.Conn-like pipe used to drive Client without a
// real network. It captures bytes written by the client and serves canned
// reply records back. It echoes the request xid into the reply so xid
// correlation can be exercised.
type fakeConn struct {
	net.Conn
	out      bytes.Buffer // bytes the client writes
	in       bytes.Buffer // bytes served back to the client
	replyGen func(reqRecord []byte) []byte
}

func (c *fakeConn) Write(p []byte) (int, error) {
	n, _ := c.out.Write(p)
	// On each complete record written, generate a reply and queue it.
	if c.replyGen != nil {
		rec, err := ReadRecord(bytes.NewReader(c.out.Bytes()))
		if err == nil {
			reply := c.replyGen(rec)
			_ = WriteRecord(&c.in, reply)
			c.replyGen = nil // one-shot
		}
	}
	return n, nil
}

func (c *fakeConn) Read(p []byte) (int, error) {
	if c.in.Len() == 0 {
		return 0, io.EOF
	}
	return c.in.Read(p)
}

func (c *fakeConn) Close() error { return nil }

// xidOf extracts the xid (first 4 bytes) of an RPC record payload.
func xidOf(record []byte) uint32 {
	return binary.BigEndian.Uint32(record[:4])
}

// TestCallCorrelatesXID issues a Call and verifies the reply for the matching
// xid is returned along with the trailing result bytes.
func TestCallCorrelatesXID(t *testing.T) {
	fc := &fakeConn{}
	fc.replyGen = func(req []byte) []byte {
		xid := xidOf(req)
		var b bytes.Buffer
		// accepted SUCCESS reply for this xid
		b.Write([]byte{byte(xid >> 24), byte(xid >> 16), byte(xid >> 8), byte(xid)})
		b.Write([]byte{0, 0, 0, 1})             // REPLY
		b.Write([]byte{0, 0, 0, 0})             // MSG_ACCEPTED
		b.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0}) // verf
		b.Write([]byte{0, 0, 0, 0})             // SUCCESS
		b.Write([]byte{0xAB, 0xCD, 0xEF, 0x12}) // result payload
		return b.Bytes()
	}

	cli := NewClient(fc, ClientOptions{Cred: AuthNull(), Verf: AuthNull()})

	var resultPayload []byte
	encodeArgs := func(e *xdr.Encoder) {}
	decodeRes := func(d *xdr.Decoder) {
		resultPayload = d.FixedOpaque(4)
	}
	if err := cli.Call(context.Background(), 100003, 4, 1, encodeArgs, decodeRes); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !bytes.Equal(resultPayload, []byte{0xAB, 0xCD, 0xEF, 0x12}) {
		t.Fatalf("result payload = % x", resultPayload)
	}

	// The written request must be a valid record whose xid is non-zero.
	rec, err := ReadRecord(bytes.NewReader(fc.out.Bytes()))
	if err != nil {
		t.Fatalf("request not a valid record: %v", err)
	}
	if xidOf(rec) == 0 {
		t.Fatalf("expected non-zero xid in request")
	}
}

// TestCallContextCancelled ensures a cancelled context aborts the call.
func TestCallContextCancelled(t *testing.T) {
	fc := &fakeConn{} // never produces a reply
	cli := NewClient(fc, ClientOptions{Cred: AuthNull(), Verf: AuthNull()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := cli.Call(ctx, 100003, 4, 1, func(e *xdr.Encoder) {}, func(d *xdr.Decoder) {})
	if err == nil {
		t.Fatalf("expected error from cancelled context")
	}
}
