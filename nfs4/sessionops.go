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

// SessionID is the fixed-length sessionid4 (nfs4.x:151).
type SessionID [SessionIDLen]byte

// EXCHANGE_ID flags (nfs4.x). Only the flags the client sets/inspects.
const (
	ExchgIDFlagSuppMovedRefer uint32 = 0x00000001
	ExchgIDFlagUsePNFSMDS     uint32 = 0x00010000
	ExchgIDFlagUsePNFSDS      uint32 = 0x00020000
	ExchgIDFlagUseNonPNFS     uint32 = 0x00040000
	ExchgIDFlagConfirmedR     uint32 = 0x80000000
)

// state_protect_how4 (nfs4.x:1586). Only SP4_NONE is supported.
const spNone uint32 = 0

// --- EXCHANGE_ID ---

// ExchangeIDArgs establishes (or looks up) a client record for v4.1. Only the
// SP4_NONE state-protection variant is supported; no implementation id is sent.
type ExchangeIDArgs struct {
	Verifier [VerifierSize]byte
	OwnerID  []byte
	Flags    uint32
}

func (ExchangeIDArgs) Op() Opnum { return OpExchangeID }
func (a ExchangeIDArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpExchangeID))
	// client_owner4: co_verifier (verifier4) + co_ownerid (opaque<>).
	e.FixedOpaque(a.Verifier[:])
	e.Opaque(a.OwnerID)
	// eia_flags.
	e.Uint32(a.Flags)
	// eia_state_protect: SP4_NONE -> void.
	e.Uint32(spNone)
	// eia_client_impl_id<1>: empty array.
	e.Uint32(0)
}

// ExchangeIDRes is the EXCHANGE_ID result. Only the fields the client needs are
// retained; the server_owner, scope, and impl id are decoded and discarded.
type ExchangeIDRes struct {
	Status     Status
	ClientID   uint64
	SequenceID uint32
	Flags      uint32
}

func (r *ExchangeIDRes) Op() Opnum { return OpExchangeID }
func (r *ExchangeIDRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status != NFS4_OK {
		return
	}
	r.ClientID = d.Uint64()
	r.SequenceID = d.Uint32()
	r.Flags = d.Uint32()
	// eir_state_protect: discriminant + (SP4_NONE -> void).
	_ = d.Uint32()
	// eir_server_owner: so_minor_id (uint64) + so_major_id (opaque<>).
	_ = d.Uint64()
	_ = d.Opaque()
	// eir_server_scope (opaque<>).
	_ = d.Opaque()
	// eir_server_impl_id<1>: array of nfs_impl_id4.
	n := d.Uint32()
	for i := uint32(0); i < n; i++ {
		// nfs_impl_id4: nii_domain (utf8), nii_name (utf8), nii_date (nfstime4).
		_ = d.String()
		_ = d.String()
		_ = d.Int64()  // seconds
		_ = d.Uint32() // nseconds
	}
}

// --- CREATE_SESSION ---

// ChannelAttrs is the channel_attrs4 fore/back channel parameters.
type ChannelAttrs struct {
	HeaderPadSize       uint32
	MaxRequestSize      uint32
	MaxResponseSize     uint32
	MaxResponseSizeCach uint32
	MaxOperations       uint32
	MaxRequests         uint32
}

func (c ChannelAttrs) encode(e *xdr.Encoder) {
	e.Uint32(c.HeaderPadSize)
	e.Uint32(c.MaxRequestSize)
	e.Uint32(c.MaxResponseSize)
	e.Uint32(c.MaxResponseSizeCach)
	e.Uint32(c.MaxOperations)
	e.Uint32(c.MaxRequests)
	e.Uint32(0) // ca_rdma_ird<1>: empty array.
}

func decodeChannelAttrs(d *xdr.Decoder) ChannelAttrs {
	var c ChannelAttrs
	c.HeaderPadSize = d.Uint32()
	c.MaxRequestSize = d.Uint32()
	c.MaxResponseSize = d.Uint32()
	c.MaxResponseSizeCach = d.Uint32()
	c.MaxOperations = d.Uint32()
	c.MaxRequests = d.Uint32()
	n := d.Uint32() // ca_rdma_ird<1>
	for i := uint32(0); i < n; i++ {
		_ = d.Uint32()
	}
	return c
}

// CreateSessionArgs creates a session bound to the exchanged clientid. Only the
// AUTH_SYS/AUTH_NULL fore-channel is needed, so no callback security parms are
// sent and the back channel is requested with minimal attributes.
type CreateSessionArgs struct {
	ClientID   uint64
	SequenceID uint32
	Flags      uint32
	ForeChan   ChannelAttrs
	BackChan   ChannelAttrs
	CBProgram  uint32
}

func (CreateSessionArgs) Op() Opnum { return OpCreateSession }
func (a CreateSessionArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpCreateSession))
	e.Uint64(a.ClientID)
	e.Uint32(a.SequenceID)
	e.Uint32(a.Flags)
	a.ForeChan.encode(e)
	a.BackChan.encode(e)
	e.Uint32(a.CBProgram)
	// csa_sec_parms<>: empty (no callback security).
	e.Uint32(0)
}

// CreateSessionRes is the CREATE_SESSION result.
type CreateSessionRes struct {
	Status     Status
	SessionID  SessionID
	SequenceID uint32
	Flags      uint32
	ForeChan   ChannelAttrs
	BackChan   ChannelAttrs
}

func (r *CreateSessionRes) Op() Opnum { return OpCreateSession }
func (r *CreateSessionRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status != NFS4_OK {
		return
	}
	copy(r.SessionID[:], d.FixedOpaque(SessionIDLen))
	r.SequenceID = d.Uint32()
	r.Flags = d.Uint32()
	r.ForeChan = decodeChannelAttrs(d)
	r.BackChan = decodeChannelAttrs(d)
}

// --- DESTROY_SESSION ---

// DestroySessionArgs tears down the named session.
type DestroySessionArgs struct {
	SessionID SessionID
}

func (DestroySessionArgs) Op() Opnum { return OpDestroySession }
func (a DestroySessionArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpDestroySession))
	e.FixedOpaque(a.SessionID[:])
}

// DestroySessionRes is the DESTROY_SESSION result (status only).
type DestroySessionRes struct {
	Status Status
}

func (r *DestroySessionRes) Op() Opnum { return OpDestroySession }
func (r *DestroySessionRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
}

// --- DESTROY_CLIENTID ---

// DestroyClientIDArgs releases the client record created by EXCHANGE_ID.
type DestroyClientIDArgs struct {
	ClientID uint64
}

func (DestroyClientIDArgs) Op() Opnum { return OpDestroyClientID }
func (a DestroyClientIDArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpDestroyClientID))
	e.Uint64(a.ClientID)
}

// DestroyClientIDRes is the DESTROY_CLIENTID result (status only).
type DestroyClientIDRes struct {
	Status Status
}

func (r *DestroyClientIDRes) Op() Opnum { return OpDestroyClientID }
func (r *DestroyClientIDRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
}

// --- RECLAIM_COMPLETE ---

// ReclaimCompleteArgs signals the end of reclaim after session establishment.
// OneFS requests the whole-fs (rca_one_fs = false) global form.
type ReclaimCompleteArgs struct {
	OneFS bool
}

func (ReclaimCompleteArgs) Op() Opnum { return OpReclaimComplete }
func (a ReclaimCompleteArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpReclaimComplete))
	e.Bool(a.OneFS)
}

// ReclaimCompleteRes is the RECLAIM_COMPLETE result (status only).
type ReclaimCompleteRes struct {
	Status Status
}

func (r *ReclaimCompleteRes) Op() Opnum { return OpReclaimComplete }
func (r *ReclaimCompleteRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
}

// --- SEQUENCE ---

// SequenceArgs is the per-compound SEQUENCE op carrying session slot state.
type SequenceArgs struct {
	SessionID     SessionID
	SequenceID    uint32
	SlotID        uint32
	HighestSlotID uint32
	CacheThis     bool
}

func (SequenceArgs) Op() Opnum { return OpSequence }
func (a SequenceArgs) EncodeArg(e *xdr.Encoder) {
	e.Uint32(uint32(OpSequence))
	e.FixedOpaque(a.SessionID[:])
	e.Uint32(a.SequenceID)
	e.Uint32(a.SlotID)
	e.Uint32(a.HighestSlotID)
	e.Bool(a.CacheThis)
}

// SequenceRes is the SEQUENCE result with the server's slot bookkeeping.
type SequenceRes struct {
	Status            Status
	SessionID         SessionID
	SequenceID        uint32
	SlotID            uint32
	HighestSlotID     uint32
	TargetHighestSlot uint32
	StatusFlags       uint32
}

func (r *SequenceRes) Op() Opnum { return OpSequence }
func (r *SequenceRes) DecodeRes(d *xdr.Decoder) {
	r.Status = Status(d.Uint32())
	if r.Status != NFS4_OK {
		return
	}
	copy(r.SessionID[:], d.FixedOpaque(SessionIDLen))
	r.SequenceID = d.Uint32()
	r.SlotID = d.Uint32()
	r.HighestSlotID = d.Uint32()
	r.TargetHighestSlot = d.Uint32()
	r.StatusFlags = d.Uint32()
}
