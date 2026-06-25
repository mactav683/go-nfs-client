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

// Share access/deny constants (nfs4.x:959).
const (
	OpenShareAccessRead  uint32 = 0x00000001
	OpenShareAccessWrite uint32 = 0x00000002
	OpenShareAccessBoth  uint32 = 0x00000003

	OpenShareDenyNone  uint32 = 0x00000000
	OpenShareDenyRead  uint32 = 0x00000001
	OpenShareDenyWrite uint32 = 0x00000002
	OpenShareDenyBoth  uint32 = 0x00000003
)

// opentype4 (nfs4.x:923).
const (
	Open4NoCreate uint32 = 0
	Open4Create   uint32 = 1
)

// createmode4 (nfs4.x:909).
const (
	CreateUnchecked uint32 = 0
	CreateGuarded   uint32 = 1
	CreateExclusive uint32 = 2
)

// open_claim_type4 (nfs4.x:974).
const (
	ClaimNull uint32 = 0
)

// OPEN result flags (nfs4.x:1104).
const (
	OpenResultConfirm uint32 = 0x00000002
)

// stable_how4 (nfs4.x:1452).
type StableHow uint32

const (
	Unstable4 StableHow = 0
	DataSync4 StableHow = 1
	FileSync4 StableHow = 2
)

// --- OPEN ---

// OpenArgs opens (and optionally creates) a file. The current filehandle must
// be the containing directory (CLAIM_NULL). Only the UNCHECKED create mode with
// settable attributes is supported here; EXCLUSIVE create uses CreateVerf.
type OpenArgs struct {
	Seqid       uint32
	ShareAccess uint32
	ShareDeny   uint32
	ClientID    uint64
	Owner       []byte

	OpenType uint32 // Open4NoCreate or Open4Create

	// Create parameters (used when OpenType == Open4Create).
	CreateMode     uint32 // CreateUnchecked/Guarded/Exclusive
	CreateAttrMask Bitmap
	CreateAttrVals []byte
	CreateVerf     [8]byte // used when CreateMode == CreateExclusive

	// CLAIM_NULL file name within the current directory.
	Name string
}

func (OpenArgs) Op() Opnum { return OpOpen }
func (a OpenArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpOpen))
	e.Uint32(a.Seqid)
	e.Uint32(a.ShareAccess)
	e.Uint32(a.ShareDeny)
	// open_owner4
	e.Uint64(a.ClientID)
	e.Opaque(a.Owner)
	// openflag4
	e.Uint32(a.OpenType)
	if a.OpenType == Open4Create {
		// createhow4
		e.Uint32(a.CreateMode)
		switch a.CreateMode {
		case CreateExclusive:
			e.FixedOpaque(a.CreateVerf[:])
		default: // UNCHECKED / GUARDED: fattr4 createattrs
			a.CreateAttrMask.Encode(e)
			e.Opaque(a.CreateAttrVals)
		}
	}
	// open_claim4: CLAIM_NULL
	e.Uint32(ClaimNull)
	e.String(a.Name)
}

// OpenRes is the OPEN result. It exposes the open stateid, result flags, and the
// attribute set established on create. Delegation info is decoded but only the
// NONE case is retained for now.
type OpenRes struct {
	Status  Status
	Stateid Stateid
	RFlags  uint32
	AttrSet Bitmap
}

func (r *OpenRes) Op() Opnum { return OpOpen }
func (r *OpenRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status != NFS4_OK {
		return
	}
	r.Stateid = DecodeStateid(d)
	// change_info4
	_ = d.Bool()   // atomic
	_ = d.Uint64() // before
	_ = d.Uint64() // after
	r.RFlags = d.Uint32()
	r.AttrSet = DecodeBitmap(d)
	// open_delegation4
	delegType := d.Uint32()
	switch delegType {
	case 0: // OPEN_DELEGATE_NONE
	case 1: // OPEN_DELEGATE_READ: stateid + recall + nfsace4
		_ = DecodeStateid(d)
		_ = d.Bool()
		decodeNfsace4(d)
	case 2: // OPEN_DELEGATE_WRITE: stateid + recall + space_limit + nfsace4
		_ = DecodeStateid(d)
		_ = d.Bool()
		decodeSpaceLimit(d)
		decodeNfsace4(d)
	}
}

// NeedsConfirm reports whether the server requires OPEN_CONFIRM.
func (r *OpenRes) NeedsConfirm() bool {
	return r.RFlags&OpenResultConfirm != 0
}

// decodeNfsace4 consumes an nfsace4 { type, flag, access_mask, who<> }.
func decodeNfsace4(d *xdr.Decoder) {
	_ = d.Uint32()
	_ = d.Uint32()
	_ = d.Uint32()
	_ = d.String()
}

// decodeSpaceLimit consumes an nfs_space_limit4 union.
func decodeSpaceLimit(d *xdr.Decoder) {
	limitBy := d.Uint32()
	switch limitBy {
	case 1: // NFS_LIMIT_SIZE
		_ = d.Uint64()
	case 2: // NFS_LIMIT_BLOCKS
		_ = d.Uint32()
		_ = d.Uint32()
	}
}

// --- OPEN_CONFIRM ---

// OpenConfirmArgs confirms an open with the open stateid and a fresh seqid.
type OpenConfirmArgs struct {
	Stateid Stateid
	Seqid   uint32
}

func (OpenConfirmArgs) Op() Opnum { return OpOpenConfirm }
func (a OpenConfirmArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpOpenConfirm))
	a.Stateid.Encode(e)
	e.Uint32(a.Seqid)
}

// OpenConfirmRes carries the confirmed open stateid.
type OpenConfirmRes struct {
	Status  Status
	Stateid Stateid
}

func (r *OpenConfirmRes) Op() Opnum { return OpOpenConfirm }
func (r *OpenConfirmRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		r.Stateid = DecodeStateid(d)
	}
}

// --- WRITE ---

// WriteArgs writes Data at Offset using the given open stateid and stability
// level.
type WriteArgs struct {
	Stateid Stateid
	Offset  uint64
	Stable  StableHow
	Data    []byte
}

func (WriteArgs) Op() Opnum { return OpWrite }
func (a WriteArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpWrite))
	a.Stateid.Encode(e)
	e.Uint64(a.Offset)
	e.Uint32(uint32(a.Stable))
	e.Opaque(a.Data)
}

// WriteRes is the WRITE result: bytes written, the achieved stability, and the
// write verifier.
type WriteRes struct {
	Status    Status
	Count     uint32
	Committed StableHow
	Verf      [8]byte
}

func (r *WriteRes) Op() Opnum { return OpWrite }
func (r *WriteRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		r.Count = d.Uint32()
		r.Committed = StableHow(d.Uint32())
		copy(r.Verf[:], d.FixedOpaque(8))
	}
}

// --- COMMIT ---

// CommitArgs commits cached writes in [Offset, Offset+Count) to stable storage.
// A Count of 0 commits the whole file.
type CommitArgs struct {
	Offset uint64
	Count  uint32
}

func (CommitArgs) Op() Opnum { return OpCommit }
func (a CommitArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpCommit))
	e.Uint64(a.Offset)
	e.Uint32(a.Count)
}

// CommitRes carries the write verifier.
type CommitRes struct {
	Status Status
	Verf   [8]byte
}

func (r *CommitRes) Op() Opnum { return OpCommit }
func (r *CommitRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		copy(r.Verf[:], d.FixedOpaque(8))
	}
}

// --- CLOSE ---

// CloseArgs closes an open file, releasing its share reservation.
type CloseArgs struct {
	Seqid   uint32
	Stateid Stateid
}

func (CloseArgs) Op() Opnum { return OpClose }
func (a CloseArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpClose))
	e.Uint32(a.Seqid)
	a.Stateid.Encode(e)
}

// CloseRes carries the final open stateid.
type CloseRes struct {
	Status  Status
	Stateid Stateid
}

func (r *CloseRes) Op() Opnum { return OpClose }
func (r *CloseRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		r.Stateid = DecodeStateid(d)
	}
}

// --- CREATE (non-regular files: dir, symlink) ---

// CreateArgs creates a non-regular file (directory, symlink, etc.) named
// ObjName in the current directory.
type CreateArgs struct {
	Type     uint32 // nfs_ftype4 (e.g. 2=DIR, 5=LNK)
	LinkData string // for symlinks (NF4LNK)
	ObjName  string
	AttrMask Bitmap
	AttrVals []byte
}

func (CreateArgs) Op() Opnum { return OpCreate }
func (a CreateArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpCreate))
	// createtype4 union switched on nfs_ftype4.
	e.Uint32(a.Type)
	switch a.Type {
	case 5: // NF4LNK
		e.String(a.LinkData)
	case 3, 4: // NF4BLK, NF4CHR -> specdata4
		e.Uint32(0)
		e.Uint32(0)
	}
	e.String(a.ObjName)
	a.AttrMask.Encode(e)
	e.Opaque(a.AttrVals)
}

// CreateRes carries the directory change info and the attribute set.
type CreateRes struct {
	Status  Status
	AttrSet Bitmap
}

func (r *CreateRes) Op() Opnum { return OpCreate }
func (r *CreateRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		// change_info4
		_ = d.Bool()
		_ = d.Uint64()
		_ = d.Uint64()
		r.AttrSet = DecodeBitmap(d)
	}
}

// --- REMOVE ---

// RemoveArgs removes the entry Name from the current directory.
type RemoveArgs struct {
	Name string
}

func (RemoveArgs) Op() Opnum { return OpRemove }
func (a RemoveArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpRemove))
	e.String(a.Name)
}

// RemoveRes carries the directory change info.
type RemoveRes struct {
	Status Status
}

func (r *RemoveRes) Op() Opnum { return OpRemove }
func (r *RemoveRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		_ = d.Bool()
		_ = d.Uint64()
		_ = d.Uint64()
	}
}

// --- RENAME ---

// RenameArgs renames OldName in the saved directory to NewName in the current
// directory (set with SAVEFH/PUTFH).
type RenameArgs struct {
	OldName string
	NewName string
}

func (RenameArgs) Op() Opnum { return OpRename }
func (a RenameArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpRename))
	e.String(a.OldName)
	e.String(a.NewName)
}

// RenameRes carries source/target change info.
type RenameRes struct {
	Status Status
}

func (r *RenameRes) Op() Opnum { return OpRename }
func (r *RenameRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		// source_cinfo
		_ = d.Bool()
		_ = d.Uint64()
		_ = d.Uint64()
		// target_cinfo
		_ = d.Bool()
		_ = d.Uint64()
		_ = d.Uint64()
	}
}

// --- LINK ---

// LinkArgs creates a hard link named NewName in the current directory pointing
// at the saved filehandle.
type LinkArgs struct {
	NewName string
}

func (LinkArgs) Op() Opnum { return OpLink }
func (a LinkArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpLink))
	e.String(a.NewName)
}

// LinkRes carries the directory change info.
type LinkRes struct {
	Status Status
}

func (r *LinkRes) Op() Opnum { return OpLink }
func (r *LinkRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		_ = d.Bool()
		_ = d.Uint64()
		_ = d.Uint64()
	}
}

// --- SETATTR ---

// SetattrArgs sets attributes on the current filehandle. The all-zero stateid
// is used for non-stateful attribute changes; a SIZE change (truncate) requires
// a valid open stateid.
type SetattrArgs struct {
	Stateid  Stateid
	AttrMask Bitmap
	AttrVals []byte
}

func (SetattrArgs) Op() Opnum { return OpSetattr }
func (a SetattrArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpSetattr))
	a.Stateid.Encode(e)
	a.AttrMask.Encode(e)
	e.Opaque(a.AttrVals)
}

// SetattrRes carries the set of attributes the server actually changed.
type SetattrRes struct {
	Status  Status
	AttrSet Bitmap
}

func (r *SetattrRes) Op() Opnum { return OpSetattr }
func (r *SetattrRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	r.AttrSet = DecodeBitmap(d)
}

// --- SAVEFH / RESTOREFH (void args, status-only results) ---

// SavefhArgs saves the current filehandle for use by RENAME/LINK.
type SavefhArgs struct{}

func (SavefhArgs) Op() Opnum { return OpSavefh }
func (SavefhArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpSavefh))
}

// RestorefhArgs restores the saved filehandle as current.
type RestorefhArgs struct{}

func (RestorefhArgs) Op() Opnum { return OpRestorefh }
func (RestorefhArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpRestorefh))
}
