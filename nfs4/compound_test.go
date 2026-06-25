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
	"errors"
	"testing"

	"github.com/mactav683/go-nfs-client/xdr"
)

// TestCompoundEncode asserts the COMPOUND4args byte layout: tag, minorversion,
// op count, then each op's nfs_argop4 union (opnum + body).
func TestCompoundEncode(t *testing.T) {
	c := NewCompound("").
		Add(PutrootfhArgs{}).
		Add(GetfhArgs{})

	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	c.Encode(e)
	if err := e.Err(); err != nil {
		t.Fatalf("encode: %v", err)
	}

	want := []byte{
		0, 0, 0, 0, // tag = "" (empty utf8string)
		0, 0, 0, 0, // minorversion = 0
		0, 0, 0, 2, // argarray count = 2
		0, 0, 0, 24, // OP_PUTROOTFH
		0, 0, 0, 10, // OP_GETFH
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("compound encode =\n% x\nwant\n% x", buf.Bytes(), want)
	}
}

// TestCompoundEncodeWithTagAndMinorVersion checks tag/minorversion wiring.
func TestCompoundEncodeWithTagAndMinorVersion(t *testing.T) {
	c := NewCompound("hi")
	c.MinorVersion = 1
	c.Add(GetfhArgs{})

	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	c.Encode(e)
	if err := e.Err(); err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := []byte{
		0, 0, 0, 2, 'h', 'i', 0, 0, // tag
		0, 0, 0, 1, // minorversion = 1
		0, 0, 0, 1, // count
		0, 0, 0, 10, // OP_GETFH
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("compound encode =\n% x\nwant\n% x", buf.Bytes(), want)
	}
}

// TestCompoundDecodeSuccess decodes a full success response into ordered
// results.
func TestCompoundDecodeSuccess(t *testing.T) {
	// COMPOUND4res: status, tag, resarray<>. Two ops: PUTROOTFH (status only),
	// GETFH (status + fh).
	in := []byte{
		0, 0, 0, 0, // overall status NFS4_OK
		0, 0, 0, 0, // tag = ""
		0, 0, 0, 2, // resarray count = 2
		// resop[0]: PUTROOTFH
		0, 0, 0, 24, // resop opnum
		0, 0, 0, 0, // status OK
		// resop[1]: GETFH
		0, 0, 0, 10, // resop opnum
		0, 0, 0, 0, // status OK
		0, 0, 0, 3, 0xAA, 0xBB, 0xCC, 0x00, // fh
	}

	var putrootRes StatusRes
	var getfhRes GetfhRes
	r, err := DecodeCompound(xdr.NewDecoder(bytes.NewReader(in)), []Res{&putrootRes, &getfhRes})
	if err != nil {
		t.Fatalf("DecodeCompound: %v", err)
	}
	if r.Status != NFS4_OK {
		t.Fatalf("overall status = %d", r.Status)
	}
	if r.NumResults != 2 {
		t.Fatalf("NumResults = %d, want 2", r.NumResults)
	}
	if putrootRes.Status != NFS4_OK {
		t.Fatalf("putroot status = %d", putrootRes.Status)
	}
	if getfhRes.Status != NFS4_OK || !bytes.Equal(getfhRes.FH, []byte{0xAA, 0xBB, 0xCC}) {
		t.Fatalf("getfh res = %+v", getfhRes)
	}
}

// TestCompoundDecodeEarlyTermination verifies that when an op fails mid-array,
// the server returns fewer results and decoding stops at the failing op while
// surfacing its status and the overall status.
func TestCompoundDecodeEarlyTermination(t *testing.T) {
	// PUTFH succeeds, LOOKUP fails with NFS4ERR_NOENT, GETFH never runs.
	// resarray therefore has only 2 entries and overall status is NOENT.
	in := []byte{
		0, 0, 0, 2, // overall status NFS4ERR_NOENT
		0, 0, 0, 0, // tag
		0, 0, 0, 2, // resarray count = 2 (GETFH omitted)
		// resop[0]: PUTFH OK
		0, 0, 0, 22,
		0, 0, 0, 0,
		// resop[1]: LOOKUP NOENT
		0, 0, 0, 15,
		0, 0, 0, 2,
	}

	var putfhRes StatusRes
	var lookupRes StatusRes
	var getfhRes GetfhRes
	r, err := DecodeCompound(xdr.NewDecoder(bytes.NewReader(in)),
		[]Res{&putfhRes, &lookupRes, &getfhRes})
	if err != nil {
		t.Fatalf("DecodeCompound: %v", err)
	}
	if r.Status != NFS4ERR_NOENT {
		t.Fatalf("overall status = %s, want NOENT", r.Status)
	}
	if r.NumResults != 2 {
		t.Fatalf("NumResults = %d, want 2", r.NumResults)
	}
	if putfhRes.Status != NFS4_OK {
		t.Fatalf("putfh status = %s", putfhRes.Status)
	}
	if lookupRes.Status != NFS4ERR_NOENT {
		t.Fatalf("lookup status = %s, want NOENT", lookupRes.Status)
	}
	// getfhRes must be left untouched (its op never executed).
	if getfhRes.Status != NFS4_OK || getfhRes.FH != nil {
		t.Fatalf("getfh should be zero-valued, got %+v", getfhRes)
	}
	// Err() should reflect the overall failure.
	if r.Err() == nil {
		t.Fatalf("expected non-nil Err for failed compound")
	}
	if !errors.Is(r.Err(), &StatusError{Status: NFS4ERR_NOENT}) {
		// errors.Is on a value pointer won't match; just assert it's a StatusError.
		var se *StatusError
		if !errors.As(r.Err(), &se) || se.Status != NFS4ERR_NOENT {
			t.Fatalf("Err() = %v, want StatusError(NOENT)", r.Err())
		}
	}
}

// TestCompoundResopMismatch surfaces an error if a returned resop opnum does
// not match the expected op order.
func TestCompoundResopMismatch(t *testing.T) {
	in := []byte{
		0, 0, 0, 0, // status OK
		0, 0, 0, 0, // tag
		0, 0, 0, 1, // count = 1
		0, 0, 0, 99, // unexpected opnum (expected OP_GETFH = 10)
		0, 0, 0, 0,
	}
	var res GetfhRes // GetfhRes advertises Op() == OP_GETFH
	_, err := DecodeCompound(xdr.NewDecoder(bytes.NewReader(in)), []Res{&res})
	if err == nil {
		t.Fatalf("expected error for resop opnum mismatch")
	}
}
