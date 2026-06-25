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

import "github.com/mactav683/go-nfs-client/xdr"

// FileHandle is an nfs_fh4: an opaque server-assigned handle (opaque<128>).
type FileHandle []byte

// Verifier is an NFS4_VERIFIER_SIZE-byte opaque verifier (verifier4).
type Verifier [VerifierSize]byte

// ClientID is the server-assigned clientid4.
type ClientID uint64

// Bitmap is a bitmap4: a variable-length array of 32-bit words used for
// attribute masks. Word 0 holds attributes 0-31, word 1 holds 32-63, etc.
type Bitmap []uint32

// Encode writes a bitmap4 (count-prefixed array of uint32).
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
	b := make(Bitmap, n)
	for i := range b {
		b[i] = d.Uint32()
	}
	return b
}

// Stateid is the NFSv4 stateid4: a sequence id plus a 12-byte opaque "other".
type Stateid struct {
	Seqid uint32
	Other [12]byte
}

// Encode writes a stateid4.
func (s Stateid) Encode(e *xdr.Encoder) {
	e.Uint32(s.Seqid)
	e.FixedOpaque(s.Other[:])
}

// DecodeStateid reads a stateid4 from d.
func DecodeStateid(d *xdr.Decoder) Stateid {
	var s Stateid
	s.Seqid = d.Uint32()
	copy(s.Other[:], d.FixedOpaque(12))
	return s
}
