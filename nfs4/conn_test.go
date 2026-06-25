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

// fakeCaller is a CompoundCaller that returns canned COMPOUND reply bodies in
// sequence, recording the requests it received for assertion.
type fakeCaller struct {
	replies  [][]byte
	calls    int
	requests []*Compound
}

func (f *fakeCaller) CallCompound(ctx context.Context, c *Compound, results []Res) (*CompoundResult, error) {
	f.requests = append(f.requests, c)
	if f.calls >= len(f.replies) {
		return nil, errors.New("fakeCaller: no more canned replies")
	}
	reply := f.replies[f.calls]
	f.calls++
	return DecodeCompound(xdr.NewDecoder(bytes.NewReader(reply)), results)
}

// buildCompoundRes assembles a COMPOUND4res body: status, tag, resarray.
func buildCompoundRes(status Status, resops ...[]byte) []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Uint32(uint32(status))
	e.String("")
	e.Uint32(uint32(len(resops)))
	for _, r := range resops {
		buf.Write(r)
	}
	return buf.Bytes()
}

// resop prefixes a result body with its opnum discriminant.
func resop(op Opnum, body ...byte) []byte {
	out := []byte{byte(op >> 24), byte(op >> 16), byte(op >> 8), byte(op)}
	return append(out, body...)
}

func statusOK() []byte { return []byte{0, 0, 0, 0} }

// fhResop builds a GETFH resop body: status OK + opaque fh.
func fhResop(fh []byte) []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Uint32(0) // status OK
	e.Opaque(fh)
	return resop(OpGetfh, buf.Bytes()...)
}

// setclientidResop builds a SETCLIENTID OK resop: status + clientid + confirm.
func setclientidResop(clientID uint64, confirm [8]byte) []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Uint32(0)
	e.Uint64(clientID)
	e.FixedOpaque(confirm[:])
	return resop(OpSetclientid, buf.Bytes()...)
}

// TestMountBootstrap drives the v4.0 mount flow against a fake caller:
// SETCLIENTID -> SETCLIENTID_CONFIRM -> PUTROOTFH/GETFH, then resolves a path
// via LOOKUP/GETFH.
func TestMountBootstrap(t *testing.T) {
	rootFH := []byte{0xF0, 0x00, 0x00, 0x01}
	etcFH := []byte{0xF0, 0x00, 0x00, 0x02}
	confirm := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}

	fc := &fakeCaller{
		replies: [][]byte{
			// Call 1: SETCLIENTID
			buildCompoundRes(NFS4_OK, setclientidResop(0xABCDEF, confirm)),
			// Call 2: SETCLIENTID_CONFIRM
			buildCompoundRes(NFS4_OK, resop(OpSetclientidConfirm, statusOK()...)),
			// Call 3: PUTROOTFH + GETFH
			buildCompoundRes(NFS4_OK,
				resop(OpPutrootfh, statusOK()...),
				fhResop(rootFH),
			),
			// Call 4: PUTFH(root) + LOOKUP(etc) + GETFH
			buildCompoundRes(NFS4_OK,
				resop(OpPutfh, statusOK()...),
				resop(OpLookup, statusOK()...),
				fhResop(etcFH),
			),
		},
	}

	conn := NewConn(fc, ConnConfig{ClientName: "test-client"})
	ctx := context.Background()

	if err := conn.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if conn.ClientID() != 0xABCDEF {
		t.Fatalf("clientid = %#x, want 0xABCDEF", conn.ClientID())
	}
	if !bytes.Equal(conn.RootFH(), rootFH) {
		t.Fatalf("root fh = % x, want % x", conn.RootFH(), rootFH)
	}

	fh, err := conn.Lookup(ctx, rootFH, "etc")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !bytes.Equal(fh, etcFH) {
		t.Fatalf("looked-up fh = % x, want % x", fh, etcFH)
	}
}

// TestMountNonexistentExportError verifies a failed LOOKUP path resolution
// returns a mapped NFS error rather than panicking.
func TestLookupNotFound(t *testing.T) {
	fc := &fakeCaller{
		replies: [][]byte{
			// PUTFH OK, LOOKUP NOENT (early termination, GETFH omitted)
			buildCompoundRes(NFS4ERR_NOENT,
				resop(OpPutfh, statusOK()...),
				resop(OpLookup, []byte{0, 0, 0, 2}...), // NFS4ERR_NOENT
			),
		},
	}
	conn := NewConn(fc, ConnConfig{ClientName: "test"})
	_, err := conn.Lookup(context.Background(), []byte{0x01}, "missing")
	if err == nil {
		t.Fatalf("expected error for missing path")
	}
	var se *StatusError
	if !errors.As(err, &se) || se.Status != NFS4ERR_NOENT {
		t.Fatalf("err = %v, want StatusError(NOENT)", err)
	}
}
