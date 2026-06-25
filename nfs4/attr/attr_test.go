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

package attr

import (
	"bytes"
	"io/fs"
	"testing"
	"time"

	"github.com/mactav683/go-nfs-client/xdr"
)

// TestModuleCompiles is a baseline test ensuring the package compiles.
func TestModuleCompiles(t *testing.T) {}

// TestBitmapForAttrs builds a bitmap from attribute numbers and asserts the
// multi-word layout: attr 1 (TYPE) and 4 (SIZE) go in word 0; attr 33 (MODE)
// goes in word 1 at bit (33-32)=1.
func TestBitmapForAttrs(t *testing.T) {
	bm := BitmapFor(AttrType, AttrSize, AttrMode)
	if len(bm) != 2 {
		t.Fatalf("bitmap words = %d, want 2", len(bm))
	}
	wantWord0 := uint32(1<<AttrType | 1<<AttrSize)
	if bm[0] != wantWord0 {
		t.Fatalf("word0 = %#x, want %#x", bm[0], wantWord0)
	}
	wantWord1 := uint32(1 << (AttrMode - 32))
	if bm[1] != wantWord1 {
		t.Fatalf("word1 = %#x, want %#x", bm[1], wantWord1)
	}
}

// TestBitmapHasAttr verifies membership testing across words.
func TestBitmapHasAttr(t *testing.T) {
	bm := BitmapFor(AttrType, AttrMode)
	if !bm.Has(AttrType) {
		t.Errorf("expected Has(TYPE)")
	}
	if !bm.Has(AttrMode) {
		t.Errorf("expected Has(MODE)")
	}
	if bm.Has(AttrSize) {
		t.Errorf("did not expect Has(SIZE)")
	}
}

// TestDecodeStandardAttrs decodes a synthetic attrlist for the standard set and
// checks typed values and ordering. Attribute values appear in increasing
// attribute-number order.
func TestDecodeStandardAttrs(t *testing.T) {
	mask := BitmapFor(
		AttrType, AttrSize, AttrFSID, AttrFileID,
		AttrMode, AttrNumLinks, AttrOwner, AttrOwnerGroup,
		AttrRawDev, AttrSpaceUsed, AttrTimeAccess, AttrTimeMetadata, AttrTimeModify,
	)

	var vals bytes.Buffer
	e := xdr.NewEncoder(&vals)
	// Order: TYPE(1), SIZE(4), FSID(8), FILEID(20), MODE(33), NUMLINKS(35),
	// OWNER(36), OWNER_GROUP(37), RAWDEV(41), SPACE_USED(45), TIME_ACCESS(47),
	// TIME_METADATA(52), TIME_MODIFY(53).
	e.Uint32(uint32(FtypeDir)) // type = NF4DIR
	e.Uint64(4096)             // size
	e.Uint64(1)                // fsid.major
	e.Uint64(2)                // fsid.minor
	e.Uint64(42)               // fileid
	e.Uint32(0o755)            // mode
	e.Uint32(3)                // numlinks
	e.String("root")           // owner
	e.String("wheel")          // owner_group
	e.Uint32(0)                // rawdev.spec1
	e.Uint32(0)                // rawdev.spec2
	e.Uint64(8192)             // space_used
	// time_access nfstime4{seconds int64, nseconds uint32}
	e.Int64(1000)
	e.Uint32(500)
	// time_metadata
	e.Int64(1100)
	e.Uint32(0)
	// time_modify
	e.Int64(1200)
	e.Uint32(250)
	if err := e.Err(); err != nil {
		t.Fatalf("encode vals: %v", err)
	}

	attrs, err := Decode(mask, vals.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if attrs.Type != FtypeDir {
		t.Errorf("type = %d, want DIR", attrs.Type)
	}
	if attrs.Size != 4096 {
		t.Errorf("size = %d", attrs.Size)
	}
	if attrs.FSID.Major != 1 || attrs.FSID.Minor != 2 {
		t.Errorf("fsid = %+v", attrs.FSID)
	}
	if attrs.FileID != 42 {
		t.Errorf("fileid = %d", attrs.FileID)
	}
	if attrs.Mode != 0o755 {
		t.Errorf("mode = %o", attrs.Mode)
	}
	if attrs.NumLinks != 3 {
		t.Errorf("numlinks = %d", attrs.NumLinks)
	}
	if attrs.Owner != "root" || attrs.OwnerGroup != "wheel" {
		t.Errorf("owner=%q group=%q", attrs.Owner, attrs.OwnerGroup)
	}
	if attrs.SpaceUsed != 8192 {
		t.Errorf("space_used = %d", attrs.SpaceUsed)
	}
	if !attrs.TimeModify.Equal(time.Unix(1200, 250)) {
		t.Errorf("time_modify = %v, want %v", attrs.TimeModify, time.Unix(1200, 250))
	}
	if !attrs.TimeAccess.Equal(time.Unix(1000, 500)) {
		t.Errorf("time_access = %v", attrs.TimeAccess)
	}
}

// TestFileModeMapping verifies the os.FileMode mapping for type + permission
// bits.
func TestFileModeMapping(t *testing.T) {
	dir := Attributes{Type: FtypeDir, Mode: 0o755}
	if fm := dir.FileMode(); !fm.IsDir() || fm.Perm() != 0o755 {
		t.Errorf("dir mode = %v", fm)
	}
	reg := Attributes{Type: FtypeReg, Mode: 0o644}
	if fm := reg.FileMode(); fm.IsDir() || fm.Perm() != 0o644 {
		t.Errorf("reg mode = %v", fm)
	}
	lnk := Attributes{Type: FtypeLnk, Mode: 0o777}
	if fm := lnk.FileMode(); fm&fs.ModeSymlink == 0 {
		t.Errorf("symlink mode missing ModeSymlink: %v", fm)
	}
}

// TestFileInfoAdapter checks the fs.FileInfo view.
func TestFileInfoAdapter(t *testing.T) {
	a := Attributes{Type: FtypeReg, Size: 123, Mode: 0o644, TimeModify: time.Unix(5000, 0)}
	fi := a.FileInfo("hello.txt")
	if fi.Name() != "hello.txt" {
		t.Errorf("name = %q", fi.Name())
	}
	if fi.Size() != 123 {
		t.Errorf("size = %d", fi.Size())
	}
	if fi.IsDir() {
		t.Errorf("IsDir should be false")
	}
	if !fi.ModTime().Equal(time.Unix(5000, 0)) {
		t.Errorf("modtime = %v", fi.ModTime())
	}
}

// TestEncodeFattrModeSize builds a settable fattr4 (mode + size) and decodes it
// back, asserting the bitmap and encoded values round-trip.
func TestEncodeFattrModeSize(t *testing.T) {
	mode := uint32(0o600)
	size := uint64(4096)
	mask, vals := EncodeFattr(SettableAttrs{
		Mode: &mode,
		Size: &size,
	})
	// SIZE (4) precedes MODE (33) in attribute-number order.
	if !mask.Has(AttrSize) || !mask.Has(AttrMode) {
		t.Fatalf("mask missing size/mode: %v", mask)
	}
	got, err := Decode(mask, vals)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Size != size {
		t.Fatalf("size = %d, want %d", got.Size, size)
	}
	if got.Mode != mode {
		t.Fatalf("mode = %o, want %o", got.Mode, mode)
	}
}

// TestRoundTripBitmap encodes then decodes a bitmap, preserving attr ordering.
func TestRoundTripBitmap(t *testing.T) {
	bm := BitmapFor(AttrType, AttrMode, AttrTimeModify)
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	bm.Encode(e)
	if e.Err() != nil {
		t.Fatalf("encode: %v", e.Err())
	}
	d := xdr.NewDecoder(bytes.NewReader(buf.Bytes()))
	got := DecodeBitmap(d)
	if d.Err() != nil {
		t.Fatalf("decode: %v", d.Err())
	}
	if len(got) != len(bm) {
		t.Fatalf("len = %d, want %d", len(got), len(bm))
	}
	for i := range bm {
		if got[i] != bm[i] {
			t.Fatalf("word %d = %#x, want %#x", i, got[i], bm[i])
		}
	}
}
