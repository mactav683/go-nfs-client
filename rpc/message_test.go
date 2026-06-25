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
	"errors"
	"testing"

	"github.com/mactav683/go-nfs-client/xdr"
)

// TestEncodeCallHeader asserts the RPC call message header layout (RFC 5531
// §9): xid, msg_type=CALL(0), rpcvers=2, prog, vers, proc, cred, verf.
func TestEncodeCallHeader(t *testing.T) {
	c := CallHeader{
		XID:  0xCAFEBABE,
		Prog: 100003, // NFS
		Vers: 4,
		Proc: 1,
		Cred: AuthNull(),
		Verf: AuthNull(),
	}
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	c.Encode(e)
	if err := e.Err(); err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := []byte{
		0xCA, 0xFE, 0xBA, 0xBE, // xid
		0x00, 0x00, 0x00, 0x00, // msg_type = CALL
		0x00, 0x00, 0x00, 0x02, // rpcvers = 2
		0x00, 0x01, 0x86, 0xA3, // prog = 100003
		0x00, 0x00, 0x00, 0x04, // vers = 4
		0x00, 0x00, 0x00, 0x01, // proc = 1
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // cred AUTH_NULL
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // verf AUTH_NULL
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("CallHeader.Encode =\n% x\nwant\n% x", buf.Bytes(), want)
	}
}

// replyAccepted builds an accepted-SUCCESS reply prefix for the given xid.
func replyAccepted(xid uint32) []byte {
	return []byte{
		byte(xid >> 24), byte(xid >> 16), byte(xid >> 8), byte(xid), // xid
		0x00, 0x00, 0x00, 0x01, // msg_type = REPLY
		0x00, 0x00, 0x00, 0x00, // reply_stat = MSG_ACCEPTED
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // verf AUTH_NULL
		0x00, 0x00, 0x00, 0x00, // accept_stat = SUCCESS
	}
}

func TestDecodeAcceptedSuccess(t *testing.T) {
	in := replyAccepted(0x01020304)
	d := xdr.NewDecoder(bytes.NewReader(in))
	r, err := DecodeReplyHeader(d)
	if err != nil {
		t.Fatalf("DecodeReplyHeader: %v", err)
	}
	if r.XID != 0x01020304 {
		t.Fatalf("xid = %#x", r.XID)
	}
	if r.Stat != MsgAccepted {
		t.Fatalf("reply_stat = %d, want MsgAccepted", r.Stat)
	}
	if r.AcceptStat != AcceptSuccess {
		t.Fatalf("accept_stat = %d, want SUCCESS", r.AcceptStat)
	}
	if err := r.Error(); err != nil {
		t.Fatalf("Error() = %v, want nil", err)
	}
}

func TestDecodeAcceptedProgMismatch(t *testing.T) {
	in := []byte{
		0x00, 0x00, 0x00, 0x01, // xid
		0x00, 0x00, 0x00, 0x01, // REPLY
		0x00, 0x00, 0x00, 0x00, // MSG_ACCEPTED
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // verf
		0x00, 0x00, 0x00, 0x02, // accept_stat = PROG_MISMATCH
		0x00, 0x00, 0x00, 0x04, // low
		0x00, 0x00, 0x00, 0x04, // high
	}
	d := xdr.NewDecoder(bytes.NewReader(in))
	r, err := DecodeReplyHeader(d)
	if err != nil {
		t.Fatalf("DecodeReplyHeader: %v", err)
	}
	if r.AcceptStat != AcceptProgMismatch {
		t.Fatalf("accept_stat = %d", r.AcceptStat)
	}
	if r.Error() == nil {
		t.Fatalf("expected error for PROG_MISMATCH")
	}
}

func TestDecodeDenied(t *testing.T) {
	in := []byte{
		0x00, 0x00, 0x00, 0x07, // xid
		0x00, 0x00, 0x00, 0x01, // REPLY
		0x00, 0x00, 0x00, 0x01, // reply_stat = MSG_DENIED
		0x00, 0x00, 0x00, 0x00, // reject_stat = RPC_MISMATCH
		0x00, 0x00, 0x00, 0x02, // low
		0x00, 0x00, 0x00, 0x02, // high
	}
	d := xdr.NewDecoder(bytes.NewReader(in))
	r, err := DecodeReplyHeader(d)
	if err != nil {
		t.Fatalf("DecodeReplyHeader: %v", err)
	}
	if r.Stat != MsgDenied {
		t.Fatalf("reply_stat = %d, want MsgDenied", r.Stat)
	}
	if r.RejectStat != RejectRPCMismatch {
		t.Fatalf("reject_stat = %d", r.RejectStat)
	}
	if r.Error() == nil {
		t.Fatalf("expected error for MSG_DENIED")
	}
}

func TestDecodeAuthError(t *testing.T) {
	in := []byte{
		0x00, 0x00, 0x00, 0x09, // xid
		0x00, 0x00, 0x00, 0x01, // REPLY
		0x00, 0x00, 0x00, 0x01, // MSG_DENIED
		0x00, 0x00, 0x00, 0x01, // reject_stat = AUTH_ERROR
		0x00, 0x00, 0x00, 0x01, // auth_stat = AUTH_BADCRED
	}
	d := xdr.NewDecoder(bytes.NewReader(in))
	r, err := DecodeReplyHeader(d)
	if err != nil {
		t.Fatalf("DecodeReplyHeader: %v", err)
	}
	if r.RejectStat != RejectAuthError {
		t.Fatalf("reject_stat = %d", r.RejectStat)
	}
	if r.AuthStat != 1 {
		t.Fatalf("auth_stat = %d, want 1", r.AuthStat)
	}
	var ae *AuthError
	if !errors.As(r.Error(), &ae) {
		t.Fatalf("expected *AuthError, got %T", r.Error())
	}
}
