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

package nfs4

import (
	"context"

	"github.com/mactav683/go-nfs-client/rpc"
	"github.com/mactav683/go-nfs-client/xdr"
)

// RPCCaller is a CompoundCaller backed by an ONC RPC client. It encodes a
// COMPOUND4args as the arguments of the NFSPROC4_COMPOUND procedure and decodes
// the COMPOUND4res from the reply.
type RPCCaller struct {
	rpc *rpc.Client
}

// NewRPCCaller wraps an rpc.Client as a CompoundCaller.
func NewRPCCaller(client *rpc.Client) *RPCCaller {
	return &RPCCaller{rpc: client}
}

// Dial connects to an NFSv4 server at address (host:port) using the given RPC
// credentials and returns a mounted Conn whose root filehandle points at the
// server's pseudo-root. The configured cfg.MinorVersion selects v4.0 or v4.1.
// The caller resolves exports beneath it with Lookup.
func Dial(ctx context.Context, address string, cred rpc.OpaqueAuth, cfg ConnConfig) (*Conn, error) {
	cli, err := rpc.Dial(ctx, address, rpc.ClientOptions{Cred: cred, Verf: rpc.AuthNull()})
	if err != nil {
		return nil, err
	}
	conn := NewConn(NewRPCCaller(cli), cfg)
	if err := conn.Mount(ctx); err != nil {
		_ = cli.Close()
		return nil, err
	}
	conn.rpc = cli
	return conn, nil
}

// DialAuto connects and negotiates the highest minor version the server
// supports: it attempts v4.1 first and falls back to v4.0 when the server
// rejects the minor version (NFS4ERR_MINOR_VERS_MISMATCH) or does not implement
// the v4.1 session operations (NFS4ERR_NOTSUPP / NFS4ERR_OP_ILLEGAL). The
// negotiated version is observable via Conn.MinorVersion.
func DialAuto(ctx context.Context, address string, cred rpc.OpaqueAuth, cfg ConnConfig) (*Conn, error) {
	v41 := cfg
	v41.MinorVersion = MinorV41
	conn, err := Dial(ctx, address, cred, v41)
	if err == nil {
		return conn, nil
	}
	if !isMinorVersionFallback(err) {
		return nil, err
	}
	v40 := cfg
	v40.MinorVersion = MinorV40
	return Dial(ctx, address, cred, v40)
}

// isMinorVersionFallback reports whether err indicates the server does not
// support the attempted (v4.1) minor version and a v4.0 retry is warranted.
func isMinorVersionFallback(err error) bool {
	for _, s := range []Status{
		NFS4ERR_MINOR_VERS_MISMATCH,
		NFS4ERR_NOTSUPP,
		NFS4ERR_OP_ILLEGAL,
	} {
		if errorHasStatus(err, s) {
			return true
		}
	}
	return false
}

// CallCompound encodes c, sends it as procedure NFSPROC4_COMPOUND, and decodes
// the result array into results.
func (t *RPCCaller) CallCompound(ctx context.Context, c *Compound, results []Res) (*CompoundResult, error) {
	var out *CompoundResult
	var decErr error
	err := t.rpc.Call(ctx, Program, Version4, ProcCompound,
		func(e *xdr.Encoder) { c.Encode(e) },
		func(d *xdr.Decoder) { out, decErr = DecodeCompound(d, results) },
	)
	if err != nil {
		return nil, err
	}
	if decErr != nil {
		return nil, decErr
	}
	return out, nil
}
