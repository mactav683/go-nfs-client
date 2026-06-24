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
	"fmt"

	"github.com/mactav683/go-nfs-client/xdr"
)

// CallHeader is the RPC call message header (RFC 5531 §9): the xid and the
// call_body fields (rpcvers, prog, vers, proc, cred, verf). The procedure
// arguments follow the header in the message body.
type CallHeader struct {
	XID  uint32
	Prog uint32
	Vers uint32
	Proc uint32
	Cred OpaqueAuth
	Verf OpaqueAuth
}

// Encode writes the call header to e.
func (c CallHeader) Encode(e *xdr.Encoder) {
	e.Uint32(c.XID)
	e.Uint32(msgCall)
	e.Uint32(RPCVersion)
	e.Uint32(c.Prog)
	e.Uint32(c.Vers)
	e.Uint32(c.Proc)
	c.Cred.Encode(e)
	c.Verf.Encode(e)
}

// ReplyHeader holds the decoded fields of an RPC reply message header. The
// procedure results (for an accepted SUCCESS reply) follow in the message body.
type ReplyHeader struct {
	XID  uint32
	Stat uint32 // reply_stat: MsgAccepted or MsgDenied

	// Accepted-reply fields.
	Verf       OpaqueAuth
	AcceptStat uint32

	// PROG_MISMATCH range (accept_stat == AcceptProgMismatch).
	Low  uint32
	High uint32

	// Denied-reply fields.
	RejectStat uint32 // RejectRPCMismatch or RejectAuthError
	AuthStat   uint32 // when RejectStat == RejectAuthError
}

// DecodeReplyHeader decodes an RPC reply message header from d, stopping just
// before any procedure result body.
func DecodeReplyHeader(d *xdr.Decoder) (*ReplyHeader, error) {
	r := &ReplyHeader{}
	r.XID = d.Uint32()
	mtype := d.Uint32()
	if err := d.Err(); err != nil {
		return nil, err
	}
	if mtype != msgReply {
		return nil, fmt.Errorf("rpc: expected REPLY message type, got %d", mtype)
	}

	r.Stat = d.Uint32()
	switch r.Stat {
	case MsgAccepted:
		r.Verf = DecodeOpaqueAuth(d)
		r.AcceptStat = d.Uint32()
		if r.AcceptStat == AcceptProgMismatch {
			r.Low = d.Uint32()
			r.High = d.Uint32()
		}
	case MsgDenied:
		r.RejectStat = d.Uint32()
		switch r.RejectStat {
		case RejectRPCMismatch:
			r.Low = d.Uint32()
			r.High = d.Uint32()
		case RejectAuthError:
			r.AuthStat = d.Uint32()
		default:
			return nil, fmt.Errorf("rpc: unknown reject_stat %d", r.RejectStat)
		}
	default:
		return nil, fmt.Errorf("rpc: unknown reply_stat %d", r.Stat)
	}

	if err := d.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

// AuthError represents an RPC AUTH_ERROR denied reply carrying an auth_stat.
type AuthError struct {
	Stat uint32
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("rpc: authentication error (auth_stat=%d)", e.Stat)
}

// Error returns a non-nil error if the reply does not represent an accepted
// SUCCESS response. It returns nil for an accepted SUCCESS reply.
func (r *ReplyHeader) Error() error {
	switch r.Stat {
	case MsgAccepted:
		switch r.AcceptStat {
		case AcceptSuccess:
			return nil
		case AcceptProgMismatch:
			return fmt.Errorf("rpc: program version mismatch (low=%d high=%d)", r.Low, r.High)
		default:
			return fmt.Errorf("rpc: call rejected with accept_stat %d", r.AcceptStat)
		}
	case MsgDenied:
		switch r.RejectStat {
		case RejectRPCMismatch:
			return fmt.Errorf("rpc: RPC version mismatch (low=%d high=%d)", r.Low, r.High)
		case RejectAuthError:
			return &AuthError{Stat: r.AuthStat}
		default:
			return fmt.Errorf("rpc: denied with reject_stat %d", r.RejectStat)
		}
	default:
		return fmt.Errorf("rpc: unknown reply_stat %d", r.Stat)
	}
}
