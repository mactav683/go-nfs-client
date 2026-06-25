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
	"context"
	"fmt"
	"sync"
)

// DefaultSlotCount is the number of concurrent SEQUENCE slots requested for the
// fore channel. Each slot carries its own monotonically increasing sequence id.
const DefaultSlotCount = 16

// slot is one fore-channel SEQUENCE slot. seq is the next sequence id to send
// for this slot; the server increments its expected value on each successful
// use, and a retried request reuses the same (slot, seq) pair.
type slot struct {
	seq   uint32
	inUse bool
}

// Session holds the negotiated v4.1 session state and a fixed-size slot table.
// It is safe for concurrent use: AcquireSlot blocks until a slot is free.
//
// Free slots are tracked with a buffered channel acting as a counting
// semaphore, which makes context-cancellable waiting straightforward.
type Session struct {
	id SessionID

	mu      sync.Mutex
	slots   []slot
	highest uint32        // highest slot id ever handed out (sa_highest_slotid)
	free    chan struct{} // one token per currently-free slot
}

// NewSession returns a Session with the given id and slotCount slots, each
// starting at sequence id 1.
func NewSession(id SessionID, slotCount int) *Session {
	if slotCount < 1 {
		slotCount = 1
	}
	s := &Session{
		id:    id,
		slots: make([]slot, slotCount),
		free:  make(chan struct{}, slotCount),
	}
	for i := range s.slots {
		s.slots[i].seq = 1
		s.free <- struct{}{}
	}
	return s
}

// ID returns the session id.
func (s *Session) ID() SessionID { return s.id }

// SlotCount returns the number of slots in the table.
func (s *Session) SlotCount() int { return len(s.slots) }

// slotLease is a held slot reservation. Exactly one of Done/Retry must be called
// to release it (Retry leaves the sequence id unchanged so the same request can
// be safely re-sent; Done advances it on success).
type slotLease struct {
	s        *Session
	slotID   uint32
	seq      uint32
	highest  uint32
	released bool
}

// AcquireSlot blocks until a fore-channel slot is free, returning a lease with
// the slot id, the sequence id to send, and the highest in-use slot id. The
// returned lease must be released with Done (success) or Retry (retriable
// failure). It honors ctx cancellation while waiting for a free slot.
func (s *Session) AcquireSlot(ctx context.Context) (*slotLease, error) {
	// Wait for a free-slot token, cancellable via ctx.
	select {
	case <-s.free:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.slots {
		if !s.slots[i].inUse {
			s.slots[i].inUse = true
			if uint32(i) > s.highest {
				s.highest = uint32(i)
			}
			return &slotLease{
				s:       s,
				slotID:  uint32(i),
				seq:     s.slots[i].seq,
				highest: s.highest,
			}, nil
		}
	}
	// Token/slot accounting is consistent, so this is unreachable; return the
	// token to keep the semaphore balanced if it ever happens.
	s.free <- struct{}{}
	return nil, context.Canceled
}

// SlotID returns the leased slot id.
func (l *slotLease) SlotID() uint32 { return l.slotID }

// Seq returns the sequence id to send for the leased slot.
func (l *slotLease) Seq() uint32 { return l.seq }

// Highest returns the highest in-use slot id (sa_highest_slotid).
func (l *slotLease) Highest() uint32 { return l.highest }

// Done releases the slot after a successful request, advancing its sequence id.
func (l *slotLease) Done() {
	l.release(true)
}

// Retry releases the slot after a retriable failure without advancing the
// sequence id, so the same (slot, seq) can be re-sent.
func (l *slotLease) Retry() {
	l.release(false)
}

func (l *slotLease) release(advance bool) {
	if l.released {
		return
	}
	l.released = true
	l.s.mu.Lock()
	if advance {
		l.s.slots[l.slotID].seq++
	}
	l.s.slots[l.slotID].inUse = false
	l.s.mu.Unlock()
	// Return the free-slot token, unblocking a waiting AcquireSlot.
	l.s.free <- struct{}{}
}

// sessionCaller wraps a base CompoundCaller and prepends a SEQUENCE operation to
// every compound, managing slot acquisition and sequence-id advancement. This
// keeps the v4.0 operation logic in Conn unchanged: the SEQUENCE op is injected
// transparently at the transport boundary for v4.1.
type sessionCaller struct {
	base    CompoundCaller
	session *Session
}

// newSessionCaller returns a CompoundCaller that adds SEQUENCE to each compound.
func newSessionCaller(base CompoundCaller, session *Session) *sessionCaller {
	return &sessionCaller{base: base, session: session}
}

// CallCompound prepends SEQUENCE to c, decodes the leading SEQUENCE result, and
// forwards the remaining results to the wrapped decoders. The slot's sequence id
// advances on success; on a retriable transport/decoding failure the slot is
// released for reuse without advancing.
func (sc *sessionCaller) CallCompound(ctx context.Context, c *Compound, results []Res) (*CompoundResult, error) {
	lease, err := sc.session.AcquireSlot(ctx)
	if err != nil {
		return nil, err
	}
	advanced := false
	defer func() {
		if !advanced {
			lease.Retry()
		}
	}()

	seqArgs := SequenceArgs{
		SessionID:     sc.session.ID(),
		SequenceID:    lease.Seq(),
		SlotID:        lease.SlotID(),
		HighestSlotID: lease.Highest(),
		CacheThis:     false,
	}

	// Build a new compound with SEQUENCE first, preserving the caller's ops.
	wrapped := NewCompound(c.Tag)
	wrapped.MinorVersion = c.MinorVersion
	wrapped.Add(seqArgs)
	for _, op := range c.Ops() {
		wrapped.Add(op)
	}

	var seqRes SequenceRes
	wrappedResults := make([]Res, 0, len(results)+1)
	wrappedResults = append(wrappedResults, &seqRes)
	wrappedResults = append(wrappedResults, results...)

	r, err := sc.base.CallCompound(ctx, wrapped, wrappedResults)
	if err != nil {
		return nil, err
	}

	// SEQUENCE is the first resop; a non-OK SEQUENCE status fails the compound.
	if seqRes.Status != NFS4_OK {
		// SEQUENCE itself failed; advance unless it's a slot/seq replay error
		// (BAD_SESSION etc. are handled by higher layers). Advance to avoid
		// wedging the slot on a definite-position error.
		advanced = true
		lease.Done()
		return nil, fmt.Errorf("nfs4: SEQUENCE: %w", seqRes.Status.Err())
	}

	advanced = true
	lease.Done()

	// Adjust the result for the caller: the SEQUENCE resop is consumed here, so
	// the reported NumResults excludes it.
	if r != nil && r.NumResults > 0 {
		r.NumResults--
	}
	return r, nil
}
