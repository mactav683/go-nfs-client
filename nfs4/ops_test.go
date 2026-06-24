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
	"testing"

	"github.com/mactav683/go-nfs-client/xdr"
)

func encode(t *testing.T, fn func(e *xdr.Encoder)) []byte {
	t.Helper()
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	fn(e)
	if err := e.Err(); err != nil {
		t.Fatalf("encode error: %v", err)
	}
	return buf.Bytes()
}

// TestPutrootfhArgEncode: PUTROOTFH args is just the opnum (void body).
func TestPutrootfhArgEncode(t *testing.T) {
	got := encode(t, func(e *xdr.Encoder) { PutrootfhArgs{}.EncodeArg(e) })
	want := []byte{0, 0, 0, 24} // OP_PUTROOTFH
	if !bytes.Equal(got, want) {
		t.Fatalf("PUTROOTFH arg = % x, want % x", got, want)
	}
}

// TestGetfhArgEncode: GETFH args is just the opnum (void body).
func TestGetfhArgEncode(t *testing.T) {
	got := encode(t, func(e *xdr.Encoder) { GetfhArgs{}.EncodeArg(e) })
	want := []byte{0, 0, 0, 10} // OP_GETFH
	if !bytes.Equal(got, want) {
		t.Fatalf("GETFH arg = % x, want % x", got, want)
	}
}

// TestPutfhArgEncode: opnum + nfs_fh4 (opaque<128>).
func TestPutfhArgEncode(t *testing.T) {
	fh := []byte{0x01, 0x02, 0x03}
	got := encode(t, func(e *xdr.Encoder) { PutfhArgs{FH: fh}.EncodeArg(e) })
	want := []byte{
		0, 0, 0, 22, // OP_PUTFH
		0, 0, 0, 3, 0x01, 0x02, 0x03, 0x00, // fh opaque + pad
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("PUTFH arg = % x, want % x", got, want)
	}
}

// TestLookupArgEncode: opnum + component4 (utf8string = opaque).
func TestLookupArgEncode(t *testing.T) {
	got := encode(t, func(e *xdr.Encoder) { LookupArgs{Name: "etc"}.EncodeArg(e) })
	want := []byte{
		0, 0, 0, 15, // OP_LOOKUP
		0, 0, 0, 3, 'e', 't', 'c', 0, // name
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("LOOKUP arg = % x, want % x", got, want)
	}
}

// TestGetattrArgEncode: opnum + bitmap4 (variable-length array of uint32).
func TestGetattrArgEncode(t *testing.T) {
	got := encode(t, func(e *xdr.Encoder) {
		GetattrArgs{AttrRequest: []uint32{0x00000018, 0x00000030}}.EncodeArg(e)
	})
	want := []byte{
		0, 0, 0, 9, // OP_GETATTR
		0, 0, 0, 2, // bitmap length = 2 words
		0x00, 0x00, 0x00, 0x18,
		0x00, 0x00, 0x00, 0x30,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("GETATTR arg = % x, want % x", got, want)
	}
}

// TestSetclientidArgEncode: opnum + nfs_client_id4{verifier[8], id<>} +
// cb_client4{cb_program, clientaddr4{r_netid, r_addr}} + callback_ident.
func TestSetclientidArgEncode(t *testing.T) {
	args := SetclientidArgs{
		Verifier:      [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		ID:            []byte("client-x"),
		CallbackProg:  0,
		CallbackNetID: "tcp",
		CallbackAddr:  "0.0.0.0.0.0",
		CallbackIdent: 1,
	}
	got := encode(t, func(e *xdr.Encoder) { args.EncodeArg(e) })

	var want bytes.Buffer
	want.Write([]byte{0, 0, 0, 35})            // OP_SETCLIENTID
	want.Write([]byte{1, 2, 3, 4, 5, 6, 7, 8}) // verifier (fixed 8, no length)
	want.Write([]byte{0, 0, 0, 8})             // id length
	want.Write([]byte("client-x"))             // id (8 bytes, aligned)
	want.Write([]byte{0, 0, 0, 0})             // cb_program
	want.Write([]byte{0, 0, 0, 3, 't', 'c', 'p', 0})
	want.Write([]byte{0, 0, 0, 11})
	want.Write([]byte("0.0.0.0.0.0"))
	want.Write([]byte{0})          // pad 11 -> 12
	want.Write([]byte{0, 0, 0, 1}) // callback_ident
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("SETCLIENTID arg =\n% x\nwant\n% x", got, want.Bytes())
	}
}

// TestSetclientidConfirmArgEncode: opnum + clientid4 + verifier[8].
func TestSetclientidConfirmArgEncode(t *testing.T) {
	args := SetclientidConfirmArgs{
		ClientID: 0x0102030405060708,
		Verifier: [8]byte{9, 10, 11, 12, 13, 14, 15, 16},
	}
	got := encode(t, func(e *xdr.Encoder) { args.EncodeArg(e) })
	want := []byte{
		0, 0, 0, 36, // OP_SETCLIENTID_CONFIRM
		1, 2, 3, 4, 5, 6, 7, 8, // clientid
		9, 10, 11, 12, 13, 14, 15, 16, // verifier
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("SETCLIENTID_CONFIRM arg = % x, want % x", got, want)
	}
}

// --- Result decoding ---

// TestGetfhResDecode: opnum + status(OK) + nfs_fh4.
func TestGetfhResDecode(t *testing.T) {
	in := []byte{
		0, 0, 0, 10, // OP_GETFH (resop discriminant)
		0, 0, 0, 0, // status OK
		0, 0, 0, 4, 0xAA, 0xBB, 0xCC, 0xDD, // fh
	}
	d := xdr.NewDecoder(bytes.NewReader(in))
	op := Opnum(d.Uint32())
	if op != OpGetfh {
		t.Fatalf("op = %d", op)
	}
	var res GetfhRes
	res.DecodeRes(d)
	if d.Err() != nil {
		t.Fatalf("decode: %v", d.Err())
	}
	if res.Status != NFS4_OK {
		t.Fatalf("status = %d", res.Status)
	}
	if !bytes.Equal(res.FH, []byte{0xAA, 0xBB, 0xCC, 0xDD}) {
		t.Fatalf("fh = % x", res.FH)
	}
}

// TestSetclientidResDecode (OK arm): status + clientid + verifier.
func TestSetclientidResDecode(t *testing.T) {
	in := []byte{
		0, 0, 0, 0, // status OK
		1, 2, 3, 4, 5, 6, 7, 8, // clientid
		8, 7, 6, 5, 4, 3, 2, 1, // setclientid_confirm verifier
	}
	d := xdr.NewDecoder(bytes.NewReader(in))
	var res SetclientidRes
	res.DecodeRes(d)
	if d.Err() != nil {
		t.Fatalf("decode: %v", d.Err())
	}
	if res.Status != NFS4_OK {
		t.Fatalf("status = %d", res.Status)
	}
	if res.ClientID != 0x0102030405060708 {
		t.Fatalf("clientid = %#x", res.ClientID)
	}
	if res.Confirm != [8]byte{8, 7, 6, 5, 4, 3, 2, 1} {
		t.Fatalf("confirm verifier = % x", res.Confirm)
	}
}

// TestStatusOnlyResDecode covers PUTFH/PUTROOTFH/SETCLIENTID_CONFIRM/LOOKUP
// which decode to a bare nfsstat4.
func TestStatusOnlyResDecode(t *testing.T) {
	in := []byte{0, 0, 0, 2} // NFS4ERR_NOENT
	d := xdr.NewDecoder(bytes.NewReader(in))
	var res StatusRes
	res.DecodeRes(d)
	if d.Err() != nil {
		t.Fatalf("decode: %v", d.Err())
	}
	if res.Status != NFS4ERR_NOENT {
		t.Fatalf("status = %d", res.Status)
	}
}
