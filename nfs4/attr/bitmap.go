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

import "github.com/mactav683/go-nfs-client/xdr"

// AttrNum is an NFSv4 fattr4 attribute number (nfs4.x FATTR4_* constants).
type AttrNum uint32

// Standard attribute numbers (nfs4.x:455). Values match the spec exactly.
const (
	AttrSupportedAttrs AttrNum = 0
	AttrType           AttrNum = 1
	AttrFHExpireType   AttrNum = 2
	AttrChange         AttrNum = 3
	AttrSize           AttrNum = 4
	AttrLinkSupport    AttrNum = 5
	AttrSymlinkSupport AttrNum = 6
	AttrNamedAttr      AttrNum = 7
	AttrFSID           AttrNum = 8
	AttrLeaseTime      AttrNum = 10
	AttrFileID         AttrNum = 20
	AttrFilesAvail     AttrNum = 21
	AttrFilesFree      AttrNum = 22
	AttrFilesTotal     AttrNum = 23
	AttrMaxName        AttrNum = 30
	AttrMaxRead        AttrNum = 31
	AttrMaxWrite       AttrNum = 32
	AttrMode           AttrNum = 33
	AttrNumLinks       AttrNum = 35
	AttrOwner          AttrNum = 36
	AttrOwnerGroup     AttrNum = 37
	AttrRawDev         AttrNum = 41
	AttrSpaceAvail     AttrNum = 42
	AttrSpaceFree      AttrNum = 43
	AttrSpaceTotal     AttrNum = 44
	AttrSpaceUsed      AttrNum = 45
	AttrTimeAccess     AttrNum = 47
	AttrTimeMetadata   AttrNum = 52
	AttrTimeModify     AttrNum = 53
)

// Bitmap is a bitmap4: a variable-length array of 32-bit words. Attribute n is
// represented by bit (n mod 32) in word (n / 32).
type Bitmap []uint32

// BitmapFor builds a Bitmap with the bits for the given attribute numbers set.
func BitmapFor(attrs ...AttrNum) Bitmap {
	var max AttrNum
	for _, a := range attrs {
		if a > max {
			max = a
		}
	}
	words := int(max/32) + 1
	bm := make(Bitmap, words)
	for _, a := range attrs {
		bm[a/32] |= 1 << (a % 32)
	}
	return bm
}

// Has reports whether attribute a is set in the bitmap.
func (b Bitmap) Has(a AttrNum) bool {
	word := int(a / 32)
	if word >= len(b) {
		return false
	}
	return b[word]&(1<<(a%32)) != 0
}

// SetAttrs returns the attribute numbers set in the bitmap, in increasing
// order. This order matches the order attribute values appear in attrlist4.
func (b Bitmap) SetAttrs() []AttrNum {
	var out []AttrNum
	for word := 0; word < len(b); word++ {
		for bit := 0; bit < 32; bit++ {
			if b[word]&(1<<bit) != 0 {
				out = append(out, AttrNum(word*32+bit))
			}
		}
	}
	return out
}

// Encode writes the bitmap4 (count-prefixed array of uint32).
func (b Bitmap) Encode(e *xdr.Encoder) {
	e.Uint32(uint32(len(b)))
	for _, w := range b {
		e.Uint32(w)
	}
}

// DecodeBitmap reads a bitmap4 from d.
func DecodeBitmap(d *xdr.Decoder) Bitmap {
	n := d.Uint32()
	if d.Err() != nil {
		return nil
	}
	bm := make(Bitmap, n)
	for i := range bm {
		bm[i] = d.Uint32()
	}
	return bm
}

// StandardMask is the bitmap requesting the standard attribute set (matching
// libnfs's standard_attributes), used for GETATTR on files and directories.
func StandardMask() Bitmap {
	return BitmapFor(
		AttrType, AttrSize, AttrFSID, AttrFileID,
		AttrMode, AttrNumLinks, AttrOwner, AttrOwnerGroup,
		AttrRawDev, AttrSpaceUsed, AttrTimeAccess, AttrTimeMetadata, AttrTimeModify,
	)
}

// StatvfsMask is the bitmap requesting filesystem-statistics attributes
// (matching libnfs's statvfs_attributes).
func StatvfsMask() Bitmap {
	return BitmapFor(
		AttrFSID, AttrFilesAvail, AttrFilesFree, AttrFilesTotal, AttrMaxName,
		AttrSpaceAvail, AttrSpaceFree, AttrSpaceTotal,
	)
}
