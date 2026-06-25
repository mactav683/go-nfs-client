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
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/mactav683/go-nfs-client/xdr"
)

// openResop builds an OPEN OK resop body with the given stateid and rflags.
func openResop(seqid uint32, rflags uint32) []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Uint32(0) // status OK
	// stateid
	e.Uint32(seqid)
	e.FixedOpaque(make([]byte, 12))
	// change_info4
	e.Bool(true)
	e.Uint64(0)
	e.Uint64(1)
	e.Uint32(rflags)
	e.Uint32(0) // attrset bitmap empty
	e.Uint32(0) // delegation NONE
	return resop(OpOpen, buf.Bytes()...)
}

func stateidResop(op Opnum, seqid uint32) []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Uint32(0)
	e.Uint32(seqid)
	e.FixedOpaque(make([]byte, 12))
	return resop(op, buf.Bytes()...)
}

func writeResop(count uint32) []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Uint32(0)
	e.Uint32(count)
	e.Uint32(uint32(FileSync4))
	e.FixedOpaque(make([]byte, 8))
	return resop(OpWrite, buf.Bytes()...)
}

// TestOpenWithConfirmThenWrite drives OPEN (needing confirm) + OPEN_CONFIRM, then
// a WRITE, over the fake caller.
func TestOpenWithConfirmThenWrite(t *testing.T) {
	fileFH := []byte{0xF1, 0x00, 0x00, 0x01}
	fc := &fakeCaller{
		replies: [][]byte{
			// OPEN: PUTFH + OPEN(confirm) + GETFH
			buildCompoundRes(NFS4_OK,
				resop(OpPutfh, statusOK()...),
				openResop(7, OpenResultConfirm),
				fhResop(fileFH),
			),
			// OPEN_CONFIRM: PUTFH + OPEN_CONFIRM
			buildCompoundRes(NFS4_OK,
				resop(OpPutfh, statusOK()...),
				stateidResop(OpOpenConfirm, 8),
			),
			// WRITE: PUTFH + WRITE
			buildCompoundRes(NFS4_OK,
				resop(OpPutfh, statusOK()...),
				writeResop(5),
			),
		},
	}
	conn := NewConn(fc, ConnConfig{ClientName: "t"})
	ctx := context.Background()

	res, err := conn.Open(ctx, []byte{0x01}, "f.txt", OpenShareAccessWrite, nil, nil, false, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(res.FH, fileFH) {
		t.Fatalf("opened fh = % x", res.FH)
	}
	if res.Stateid.Seqid != 8 {
		t.Fatalf("confirmed stateid seqid = %d, want 8", res.Stateid.Seqid)
	}

	n, _, err := conn.Write(ctx, res.FH, res.Stateid, 0, []byte("hello"), FileSync4)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Fatalf("wrote %d bytes, want 5", n)
	}
}

// TestRemove drives a REMOVE.
func TestRemove(t *testing.T) {
	fc := &fakeCaller{
		replies: [][]byte{
			buildCompoundRes(NFS4_OK,
				resop(OpPutfh, statusOK()...),
				resop(OpRemove, append(statusOK(), encodeChangeInfo()...)...),
			),
		},
	}
	conn := NewConn(fc, ConnConfig{ClientName: "t"})
	if err := conn.Remove(context.Background(), []byte{0x01}, "gone"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

// TestOpenExclExistMapsToErrExist verifies O_EXCL create of an existing file
// surfaces NFS4ERR_EXIST (which maps to fs.ErrExist via StatusError.Is).
func TestOpenExclExistMapsToErrExist(t *testing.T) {
	fc := &fakeCaller{
		replies: [][]byte{
			// PUTFH OK, OPEN fails EXIST (early termination, GETFH omitted)
			buildCompoundRes(NFS4ERR_EXIST,
				resop(OpPutfh, statusOK()...),
				resop(OpOpen, []byte{0, 0, 0, 17}...), // NFS4ERR_EXIST
			),
		},
	}
	conn := NewConn(fc, ConnConfig{ClientName: "t"})
	_, err := conn.Open(context.Background(), []byte{0x01}, "exists", OpenShareAccessWrite, nil, nil, true, true)
	if err == nil {
		t.Fatalf("expected error for O_EXCL create of existing file")
	}
	var se *StatusError
	if !errors.As(err, &se) || se.Status != NFS4ERR_EXIST {
		t.Fatalf("err = %v, want StatusError(EXIST)", err)
	}
}

// encodeChangeInfo encodes a change_info4 { atomic, before, after }.
func encodeChangeInfo() []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Bool(true)
	e.Uint64(0)
	e.Uint64(1)
	return buf.Bytes()
}
