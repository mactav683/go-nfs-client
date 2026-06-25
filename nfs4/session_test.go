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
	"testing"
	"time"

	"github.com/mactav683/go-nfs-client/xdr"
)

// --- v4.1 op round-trips ---

func TestExchangeIDRoundTrip(t *testing.T) {
	args := ExchangeIDArgs{
		Verifier: [VerifierSize]byte{1, 2, 3, 4, 5, 6, 7, 8},
		OwnerID:  []byte("owner-id"),
		Flags:    ExchgIDFlagUseNonPNFS,
	}
	var buf bytes.Buffer
	args.EncodeArg(xdr.NewEncoder(&buf))

	d := xdr.NewDecoder(bytes.NewReader(buf.Bytes()))
	if op := Opnum(d.Uint32()); op != OpExchangeID {
		t.Fatalf("opnum = %d, want %d", op, OpExchangeID)
	}
	gotVerf := d.FixedOpaque(VerifierSize)
	if !bytes.Equal(gotVerf, args.Verifier[:]) {
		t.Fatalf("verifier = % x, want % x", gotVerf, args.Verifier[:])
	}
	if got := string(d.Opaque()); got != "owner-id" {
		t.Fatalf("owner = %q, want owner-id", got)
	}
	if got := d.Uint32(); got != ExchgIDFlagUseNonPNFS {
		t.Fatalf("flags = %#x, want %#x", got, ExchgIDFlagUseNonPNFS)
	}
	if got := d.Uint32(); got != spNone {
		t.Fatalf("state_protect = %d, want SP4_NONE", got)
	}
	if got := d.Uint32(); got != 0 {
		t.Fatalf("impl_id array len = %d, want 0", got)
	}
	if err := d.Err(); err != nil {
		t.Fatalf("decode err: %v", err)
	}
}

func TestSequenceRoundTrip(t *testing.T) {
	sid := SessionID{}
	copy(sid[:], []byte("0123456789abcdef"))
	args := SequenceArgs{
		SessionID:     sid,
		SequenceID:    7,
		SlotID:        3,
		HighestSlotID: 5,
		CacheThis:     true,
	}
	var buf bytes.Buffer
	args.EncodeArg(xdr.NewEncoder(&buf))

	d := xdr.NewDecoder(bytes.NewReader(buf.Bytes()))
	if op := Opnum(d.Uint32()); op != OpSequence {
		t.Fatalf("opnum = %d, want %d", op, OpSequence)
	}
	if got := d.FixedOpaque(SessionIDLen); !bytes.Equal(got, sid[:]) {
		t.Fatalf("sessionid = % x, want % x", got, sid[:])
	}
	if got := d.Uint32(); got != 7 {
		t.Fatalf("seqid = %d, want 7", got)
	}
	if got := d.Uint32(); got != 3 {
		t.Fatalf("slotid = %d, want 3", got)
	}
	if got := d.Uint32(); got != 5 {
		t.Fatalf("highest = %d, want 5", got)
	}
	if got := d.Bool(); got != true {
		t.Fatalf("cachethis = %v, want true", got)
	}
}

// --- slot table ---

func TestSlotSequenceAdvancesOnDone(t *testing.T) {
	s := NewSession(SessionID{}, 1)
	ctx := context.Background()

	l1, err := s.AcquireSlot(ctx)
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if l1.SlotID() != 0 || l1.Seq() != 1 {
		t.Fatalf("lease 1 = slot %d seq %d, want slot 0 seq 1", l1.SlotID(), l1.Seq())
	}
	l1.Done()

	l2, err := s.AcquireSlot(ctx)
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	if l2.Seq() != 2 {
		t.Fatalf("lease 2 seq = %d, want 2 (advanced)", l2.Seq())
	}
	l2.Done()
}

func TestSlotSequenceUnchangedOnRetry(t *testing.T) {
	s := NewSession(SessionID{}, 1)
	ctx := context.Background()

	l1, _ := s.AcquireSlot(ctx)
	seq1 := l1.Seq()
	l1.Retry()

	l2, _ := s.AcquireSlot(ctx)
	if l2.Seq() != seq1 {
		t.Fatalf("after retry seq = %d, want unchanged %d", l2.Seq(), seq1)
	}
	l2.Done()
}

// TestSlotReuseUnderExhaustion confirms that with a single slot, a second
// acquisition blocks until the first is released, then reuses the same slot id
// with an advanced sequence id (proves correct slot reuse).
func TestSlotReuseUnderExhaustion(t *testing.T) {
	s := NewSession(SessionID{}, 1)
	ctx := context.Background()

	l1, _ := s.AcquireSlot(ctx)

	acquired := make(chan *slotLease, 1)
	go func() {
		l2, err := s.AcquireSlot(ctx)
		if err != nil {
			t.Errorf("concurrent acquire: %v", err)
			close(acquired)
			return
		}
		acquired <- l2
	}()

	// The goroutine must block: no slot is free yet.
	select {
	case <-acquired:
		t.Fatalf("second acquire succeeded while slot was held")
	case <-time.After(50 * time.Millisecond):
	}

	l1.Done()

	select {
	case l2 := <-acquired:
		if l2 == nil {
			t.Fatalf("concurrent acquire failed")
		}
		if l2.SlotID() != 0 {
			t.Fatalf("reused slot id = %d, want 0", l2.SlotID())
		}
		if l2.Seq() != 2 {
			t.Fatalf("reused slot seq = %d, want 2", l2.Seq())
		}
		l2.Done()
	case <-time.After(time.Second):
		t.Fatalf("second acquire did not unblock after release")
	}
}

func TestAcquireSlotRespectsContext(t *testing.T) {
	s := NewSession(SessionID{}, 1)
	l1, _ := s.AcquireSlot(context.Background())
	defer l1.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.AcquireSlot(ctx); err == nil {
		t.Fatalf("expected context error when no slot available")
	}
}

// --- sessionCaller SEQUENCE prepend ---

// seqResop builds a SEQUENCE OK resop body.
func seqResop(sid SessionID, seq, slot uint32) []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Uint32(0) // status OK
	e.FixedOpaque(sid[:])
	e.Uint32(seq)
	e.Uint32(slot)
	e.Uint32(0) // highest
	e.Uint32(0) // target highest
	e.Uint32(0) // status flags
	return resop(OpSequence, buf.Bytes()...)
}

// TestSessionCallerPrependsSequence verifies the wrapped caller injects a
// SEQUENCE op first and hides its resop from the caller's result decoders.
func TestSessionCallerPrependsSequence(t *testing.T) {
	var sid SessionID
	copy(sid[:], []byte("session-id-16byt"))
	rootFH := []byte{0xF0, 0x00, 0x00, 0x01}

	fc := &fakeCaller{
		replies: [][]byte{
			buildCompoundRes(NFS4_OK,
				seqResop(sid, 1, 0),
				resop(OpPutrootfh, statusOK()...),
				fhResop(rootFH),
			),
		},
	}
	sess := NewSession(sid, 4)
	sc := newSessionCaller(fc, sess)

	var putRes StatusRes
	var getRes GetfhRes
	comp := NewCompound("")
	comp.MinorVersion = MinorV41
	comp.Add(PutrootfhArgs{}).Add(GetfhArgs{})

	r, err := sc.CallCompound(context.Background(), comp, []Res{&putRes, &getRes})
	if err != nil {
		t.Fatalf("CallCompound: %v", err)
	}
	if r.NumResults != 2 {
		t.Fatalf("NumResults = %d, want 2 (SEQUENCE hidden)", r.NumResults)
	}
	if !bytes.Equal(getRes.FH, rootFH) {
		t.Fatalf("fh = % x, want % x", getRes.FH, rootFH)
	}

	// The request the base caller saw must lead with SEQUENCE.
	if len(fc.requests) != 1 {
		t.Fatalf("base calls = %d, want 1", len(fc.requests))
	}
	ops := fc.requests[0].Ops()
	if len(ops) != 3 || ops[0].Op() != OpSequence {
		t.Fatalf("wrapped ops = %v, want [SEQUENCE PUTROOTFH GETFH]", ops)
	}
}

// --- v4.1 mount flow ---

// exchangeIDResop builds an EXCHANGE_ID OK resok with minimal trailing fields.
func exchangeIDResop(clientID uint64, seq uint32) []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Uint32(0) // status OK
	e.Uint64(clientID)
	e.Uint32(seq)
	e.Uint32(0)         // flags
	e.Uint32(spNone)    // state protect
	e.Uint64(0)         // server_owner.minor_id
	e.Opaque([]byte{1}) // server_owner.major_id
	e.Opaque(nil)       // server_scope
	e.Uint32(0)         // impl_id array (empty)
	return resop(OpExchangeID, buf.Bytes()...)
}

// createSessionResop builds a CREATE_SESSION OK resok.
func createSessionResop(sid SessionID, seq uint32, slots uint32) []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Uint32(0) // status OK
	e.FixedOpaque(sid[:])
	e.Uint32(seq)
	e.Uint32(0) // flags
	encodeChan := func(maxReq uint32) {
		e.Uint32(0)       // headerpad
		e.Uint32(1 << 20) // maxrequestsize
		e.Uint32(1 << 20) // maxresponsesize
		e.Uint32(1 << 16) // maxresponsesize_cached
		e.Uint32(16)      // maxoperations
		e.Uint32(maxReq)  // maxrequests
		e.Uint32(0)       // rdma_ird array
	}
	encodeChan(slots) // fore
	encodeChan(slots) // back
	return resop(OpCreateSession, buf.Bytes()...)
}

func TestMountV41(t *testing.T) {
	var sid SessionID
	copy(sid[:], []byte("mount-session-16"))
	rootFH := []byte{0xF0, 0x00, 0x00, 0x09}

	fc := &fakeCaller{
		replies: [][]byte{
			// Call 1: EXCHANGE_ID (no SEQUENCE).
			buildCompoundRes(NFS4_OK, exchangeIDResop(0xC0FFEE, 1)),
			// Call 2: CREATE_SESSION (no SEQUENCE).
			buildCompoundRes(NFS4_OK, createSessionResop(sid, 1, 8)),
			// Call 3: RECLAIM_COMPLETE (with SEQUENCE prefix).
			buildCompoundRes(NFS4_OK,
				seqResop(sid, 1, 0),
				resop(OpReclaimComplete, statusOK()...),
			),
			// Call 4: PUTROOTFH + GETFH (with SEQUENCE prefix).
			buildCompoundRes(NFS4_OK,
				seqResop(sid, 2, 0),
				resop(OpPutrootfh, statusOK()...),
				fhResop(rootFH),
			),
		},
	}

	conn := NewConn(fc, ConnConfig{MinorVersion: MinorV41})
	if err := conn.Mount(context.Background()); err != nil {
		t.Fatalf("Mount v4.1: %v", err)
	}
	if conn.Session() == nil {
		t.Fatalf("session not established")
	}
	if conn.Session().SlotCount() != 8 {
		t.Fatalf("slot count = %d, want 8", conn.Session().SlotCount())
	}
	if !bytes.Equal(conn.RootFH(), rootFH) {
		t.Fatalf("root fh = % x, want % x", conn.RootFH(), rootFH)
	}

	// EXCHANGE_ID and CREATE_SESSION must NOT carry SEQUENCE; later ops must.
	if op := fc.requests[0].Ops()[0].Op(); op != OpExchangeID {
		t.Fatalf("call 1 first op = %d, want EXCHANGE_ID", op)
	}
	if op := fc.requests[1].Ops()[0].Op(); op != OpCreateSession {
		t.Fatalf("call 2 first op = %d, want CREATE_SESSION", op)
	}
	if op := fc.requests[2].Ops()[0].Op(); op != OpSequence {
		t.Fatalf("call 3 first op = %d, want SEQUENCE", op)
	}
	if op := fc.requests[3].Ops()[0].Op(); op != OpSequence {
		t.Fatalf("call 4 first op = %d, want SEQUENCE", op)
	}
}
