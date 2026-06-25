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

// TestWriteArgEncode: opnum + stateid + offset + stable + data<>.
func TestWriteArgEncode(t *testing.T) {
	args := WriteArgs{
		Stateid: Stateid{Seqid: 1, Other: [12]byte{2}},
		Offset:  16,
		Stable:  FileSync4,
		Data:    []byte{0xAA, 0xBB, 0xCC},
	}
	got := encode(t, func(e *xdr.Encoder) { args.EncodeArg(e) })

	var want bytes.Buffer
	we := xdr.NewEncoder(&want)
	we.Uint32(uint32(OpWrite))
	we.Uint32(1) // stateid seqid
	we.FixedOpaque([]byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	we.Uint64(16) // offset
	we.Uint32(uint32(FileSync4))
	we.Opaque([]byte{0xAA, 0xBB, 0xCC})
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("WRITE arg =\n% x\nwant\n% x", got, want.Bytes())
	}
}

func TestWriteResDecode(t *testing.T) {
	var in bytes.Buffer
	e := xdr.NewEncoder(&in)
	e.Uint32(0)                 // status OK
	e.Uint32(3)                 // count
	e.Uint32(uint32(FileSync4)) // committed
	e.FixedOpaque([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	var res WriteRes
	d := xdr.NewDecoder(bytes.NewReader(in.Bytes()))
	res.DecodeRes(d)
	if d.Err() != nil {
		t.Fatalf("decode: %v", d.Err())
	}
	if res.Status != NFS4_OK || res.Count != 3 || res.Committed != FileSync4 {
		t.Fatalf("res = %+v", res)
	}
}

func TestCommitArgEncode(t *testing.T) {
	got := encode(t, func(e *xdr.Encoder) { CommitArgs{Offset: 8, Count: 4}.EncodeArg(e) })
	want := []byte{
		0, 0, 0, 5, // OP_COMMIT
		0, 0, 0, 0, 0, 0, 0, 8, // offset
		0, 0, 0, 4, // count
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("COMMIT arg = % x, want % x", got, want)
	}
}

func TestCloseArgEncode(t *testing.T) {
	args := CloseArgs{Seqid: 7, Stateid: Stateid{Seqid: 3, Other: [12]byte{9}}}
	got := encode(t, func(e *xdr.Encoder) { args.EncodeArg(e) })
	var want bytes.Buffer
	we := xdr.NewEncoder(&want)
	we.Uint32(uint32(OpClose))
	we.Uint32(7) // seqid
	we.Uint32(3) // stateid seqid
	we.FixedOpaque([]byte{9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("CLOSE arg =\n% x\nwant\n% x", got, want.Bytes())
	}
}

// TestOpenArgEncodeNoCreate: a minimal OPEN for an existing file (CLAIM_NULL,
// OPEN4_NOCREATE).
func TestOpenArgEncodeNoCreate(t *testing.T) {
	args := OpenArgs{
		Seqid:       1,
		ShareAccess: OpenShareAccessRead,
		ShareDeny:   OpenShareDenyNone,
		ClientID:    0xABCD,
		Owner:       []byte("owner-1"),
		OpenType:    Open4NoCreate,
		Name:        "file.txt",
	}
	got := encode(t, func(e *xdr.Encoder) { args.EncodeArg(e) })

	var want bytes.Buffer
	we := xdr.NewEncoder(&want)
	we.Uint32(uint32(OpOpen))
	we.Uint32(1)                   // seqid
	we.Uint32(OpenShareAccessRead) // share_access
	we.Uint32(OpenShareDenyNone)   // share_deny
	// open_owner4 { clientid, owner<> }
	we.Uint64(0xABCD)
	we.Opaque([]byte("owner-1"))
	// openflag4: OPEN4_NOCREATE
	we.Uint32(Open4NoCreate)
	// open_claim4: CLAIM_NULL + component
	we.Uint32(ClaimNull)
	we.String("file.txt")
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("OPEN arg =\n% x\nwant\n% x", got, want.Bytes())
	}
}

// TestOpenResDecode decodes OPEN4resok including a NONE delegation.
func TestOpenResDecode(t *testing.T) {
	var in bytes.Buffer
	e := xdr.NewEncoder(&in)
	e.Uint32(0) // status OK
	// stateid
	e.Uint32(5)
	e.FixedOpaque([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
	// change_info4 { atomic bool, before, after }
	e.Bool(true)
	e.Uint64(100)
	e.Uint64(101)
	// rflags
	e.Uint32(OpenResultConfirm)
	// attrset bitmap (empty)
	e.Uint32(0)
	// delegation: OPEN_DELEGATE_NONE
	e.Uint32(0)

	var res OpenRes
	d := xdr.NewDecoder(bytes.NewReader(in.Bytes()))
	res.DecodeRes(d)
	if d.Err() != nil {
		t.Fatalf("decode: %v", d.Err())
	}
	if res.Status != NFS4_OK {
		t.Fatalf("status = %s", res.Status)
	}
	if res.Stateid.Seqid != 5 {
		t.Fatalf("stateid seqid = %d", res.Stateid.Seqid)
	}
	if !res.NeedsConfirm() {
		t.Fatalf("expected NeedsConfirm true with OPEN4_RESULT_CONFIRM")
	}
}

func TestRemoveArgEncode(t *testing.T) {
	got := encode(t, func(e *xdr.Encoder) { RemoveArgs{Name: "x"}.EncodeArg(e) })
	want := []byte{
		0, 0, 0, 28, // OP_REMOVE
		0, 0, 0, 1, 'x', 0, 0, 0,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("REMOVE arg = % x, want % x", got, want)
	}
}

func TestRenameArgEncode(t *testing.T) {
	got := encode(t, func(e *xdr.Encoder) { RenameArgs{OldName: "a", NewName: "b"}.EncodeArg(e) })
	want := []byte{
		0, 0, 0, 29, // OP_RENAME
		0, 0, 0, 1, 'a', 0, 0, 0,
		0, 0, 0, 1, 'b', 0, 0, 0,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("RENAME arg = % x, want % x", got, want)
	}
}

func TestSavefhRestorefhEncode(t *testing.T) {
	if got := encode(t, func(e *xdr.Encoder) { SavefhArgs{}.EncodeArg(e) }); !bytes.Equal(got, []byte{0, 0, 0, 32}) {
		t.Fatalf("SAVEFH = % x", got)
	}
	if got := encode(t, func(e *xdr.Encoder) { RestorefhArgs{}.EncodeArg(e) }); !bytes.Equal(got, []byte{0, 0, 0, 31}) {
		t.Fatalf("RESTOREFH = % x", got)
	}
}
