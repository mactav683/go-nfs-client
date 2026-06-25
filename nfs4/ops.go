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

// Arg is an NFSv4 operation argument. EncodeArg writes the nfs_argop4 union: the
// opnum discriminant followed by the operation-specific body.
type Arg interface {
	EncodeArg(e *xdr.Encoder)
	Op() Opnum
}

// Res is an NFSv4 operation result. DecodeRes reads the operation-specific body
// of an nfs_resop4 (the opnum discriminant is consumed by the COMPOUND
// decoder before DecodeRes is called).
type Res interface {
	DecodeRes(d *xdr.Decoder)
}

// --- PUTROOTFH (void args) ---

// PutrootfhArgs sets the current filehandle to the export root.
type PutrootfhArgs struct{}

func (PutrootfhArgs) Op() Opnum { return OpPutrootfh }
func (PutrootfhArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpPutrootfh))
}

// --- PUTFH ---

// PutfhArgs sets the current filehandle to FH.
type PutfhArgs struct {
	FH []byte
}

func (PutfhArgs) Op() Opnum { return OpPutfh }
func (a PutfhArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpPutfh))
	e.Opaque(a.FH)
}

// --- GETFH (void args) ---

// GetfhArgs retrieves the current filehandle.
type GetfhArgs struct{}

func (GetfhArgs) Op() Opnum { return OpGetfh }
func (GetfhArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpGetfh))
}

// GetfhRes is the GETFH result: status and (on success) the filehandle.
type GetfhRes struct {
	Status Status
	FH     []byte
}

func (r *GetfhRes) Op() Opnum { return OpGetfh }
func (r *GetfhRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		r.FH = d.Opaque()
	}
}

// --- LOOKUP ---

// LookupArgs resolves Name within the current directory filehandle.
type LookupArgs struct {
	Name string
}

func (LookupArgs) Op() Opnum { return OpLookup }
func (a LookupArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpLookup))
	e.String(a.Name)
}

// --- GETATTR ---

// GetattrArgs requests the attributes named by the bitmap AttrRequest.
type GetattrArgs struct {
	AttrRequest []uint32
}

func (GetattrArgs) Op() Opnum { return OpGetattr }
func (a GetattrArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpGetattr))
	Bitmap(a.AttrRequest).Encode(e)
}

// GetattrRes is the GETATTR result: status and (on success) the raw fattr4
// (attribute bitmap + opaque attribute values). The attr package decodes the
// values.
type GetattrRes struct {
	Status   Status
	AttrMask Bitmap
	AttrVals []byte
}

func (r *GetattrRes) Op() Opnum { return OpGetattr }
func (r *GetattrRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		r.AttrMask = DecodeBitmap(d)
		r.AttrVals = d.Opaque()
	}
}

// --- SETCLIENTID ---

// SetclientidArgs establishes (or re-establishes) a client identity.
type SetclientidArgs struct {
	Verifier      [8]byte
	ID            []byte
	CallbackProg  uint32
	CallbackNetID string
	CallbackAddr  string
	CallbackIdent uint32
}

func (SetclientidArgs) Op() Opnum { return OpSetclientid }
func (a SetclientidArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpSetclientid))
	// nfs_client_id4
	e.FixedOpaque(a.Verifier[:])
	e.Opaque(a.ID)
	// cb_client4
	e.Uint32(a.CallbackProg)
	// clientaddr4
	e.String(a.CallbackNetID)
	e.String(a.CallbackAddr)
	// callback_ident
	e.Uint32(a.CallbackIdent)
}

// SetclientidRes is the SETCLIENTID result. On NFS4_OK it carries the clientid
// and a confirm verifier. On NFS4ERR_CLID_INUSE it carries the conflicting
// client's address (decoded into ClientUsingNetID/Addr).
type SetclientidRes struct {
	Status           Status
	ClientID         uint64
	Confirm          [8]byte
	ClientUsingNetID string
	ClientUsingAddr  string
}

func (r *SetclientidRes) Op() Opnum { return OpSetclientid }
func (r *SetclientidRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	switch r.Status {
	case NFS4_OK:
		r.ClientID = d.Uint64()
		copy(r.Confirm[:], d.FixedOpaque(8))
	case NFS4ERR_CLID_INUSE:
		r.ClientUsingNetID = d.String()
		r.ClientUsingAddr = d.String()
	}
}

// --- SETCLIENTID_CONFIRM ---

// SetclientidConfirmArgs confirms a clientid using the confirm verifier.
type SetclientidConfirmArgs struct {
	ClientID uint64
	Verifier [8]byte
}

func (SetclientidConfirmArgs) Op() Opnum { return OpSetclientidConfirm }
func (a SetclientidConfirmArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpSetclientidConfirm))
	e.Uint64(a.ClientID)
	e.FixedOpaque(a.Verifier[:])
}

// --- READ ---

// ReadArgs reads Count bytes at Offset from the current filehandle using the
// given stateid (the all-zero "anonymous" stateid is valid for unopened
// read access).
type ReadArgs struct {
	Stateid Stateid
	Offset  uint64
	Count   uint32
}

func (ReadArgs) Op() Opnum { return OpRead }
func (a ReadArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpRead))
	a.Stateid.Encode(e)
	e.Uint64(a.Offset)
	e.Uint32(a.Count)
}

// ReadRes is the READ result: status, EOF flag, and the data read.
type ReadRes struct {
	Status Status
	EOF    bool
	Data   []byte
}

func (r *ReadRes) Op() Opnum { return OpRead }
func (r *ReadRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		r.EOF = d.Bool()
		r.Data = d.Opaque()
	}
}

// --- READDIR ---

// ReaddirArgs reads directory entries starting at Cookie/Cookieverf. Dircount
// bounds the attribute bytes; Maxcount bounds the whole reply. AttrRequest is
// the per-entry attribute bitmap.
type ReaddirArgs struct {
	Cookie      uint64
	Cookieverf  [8]byte
	Dircount    uint32
	Maxcount    uint32
	AttrRequest []uint32
}

func (ReaddirArgs) Op() Opnum { return OpReaddir }
func (a ReaddirArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpReaddir))
	e.Uint64(a.Cookie)
	e.FixedOpaque(a.Cookieverf[:])
	e.Uint32(a.Dircount)
	e.Uint32(a.Maxcount)
	Bitmap(a.AttrRequest).Encode(e)
}

// DirEntry is a decoded READDIR entry4: cookie, name, and the raw fattr4
// (attribute mask + values) for the entry.
type DirEntry struct {
	Cookie   uint64
	Name     string
	AttrMask Bitmap
	AttrVals []byte
}

// ReaddirRes is the READDIR result: status, the returned cookie verifier, the
// decoded entries, and the directory EOF flag.
type ReaddirRes struct {
	Status     Status
	Cookieverf [8]byte
	Entries    []DirEntry
	EOF        bool
}

func (r *ReaddirRes) Op() Opnum { return OpReaddir }
func (r *ReaddirRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status != NFS4_OK {
		return
	}
	copy(r.Cookieverf[:], d.FixedOpaque(8))
	// dirlist4: a "value-follows" boolean precedes each entry (XDR optional
	// linked list), terminated by a false, then the directory EOF boolean.
	for {
		more := d.Bool()
		if d.Err() != nil || !more {
			break
		}
		var e DirEntry
		e.Cookie = d.Uint64()
		e.Name = d.String()
		e.AttrMask = DecodeBitmap(d)
		e.AttrVals = d.Opaque()
		if d.Err() != nil {
			return
		}
		r.Entries = append(r.Entries, e)
	}
	r.EOF = d.Bool()
}

// --- READLINK ---

// ReadlinkArgs reads the target of the symlink at the current filehandle.
type ReadlinkArgs struct{}

func (ReadlinkArgs) Op() Opnum { return OpReadlink }
func (ReadlinkArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpReadlink))
}

// ReadlinkRes is the READLINK result: status and the link target text.
type ReadlinkRes struct {
	Status Status
	Link   string
}

func (r *ReadlinkRes) Op() Opnum { return OpReadlink }
func (r *ReadlinkRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status == NFS4_OK {
		r.Link = d.String()
	}
}

// --- RENEW ---

// RenewArgs renews all leases held by the client identified by ClientID.
type RenewArgs struct {
	ClientID uint64
}

func (RenewArgs) Op() Opnum { return OpRenew }
func (a RenewArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpRenew))
	e.Uint64(a.ClientID)
}

// --- Shared status-only result ---

// StatusRes decodes operations whose result is a bare nfsstat4: PUTFH,
// PUTROOTFH, LOOKUP, SETCLIENTID_CONFIRM, RENEW, etc.
type StatusRes struct {
	Status Status
}

func (r *StatusRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
}
