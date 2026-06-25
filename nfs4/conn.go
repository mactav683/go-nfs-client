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
	"crypto/rand"
	"fmt"
	"os"
	"sync"
)

// CompoundCaller executes a single COMPOUND request and decodes its results.
// Abstracting the transport at the compound level lets the NFSv4.1 SEQUENCE op
// be injected as a prefix later without changing the operation logic, and lets
// tests substitute a fake caller.
type CompoundCaller interface {
	CallCompound(ctx context.Context, c *Compound, results []Res) (*CompoundResult, error)
}

// ConnConfig configures a Conn's bootstrap behavior.
type ConnConfig struct {
	// ClientName is the client identity string sent in SETCLIENTID. If empty, a
	// name is derived from the hostname.
	ClientName string
	// MinorVersion selects the NFSv4 minor version (0 for v4.0).
	MinorVersion uint32
}

// Conn is a logical NFSv4 connection: it owns the confirmed clientid, the root
// filehandle, and issues COMPOUND requests through a CompoundCaller.
type Conn struct {
	caller CompoundCaller
	cfg    ConnConfig

	// rpc, when non-nil, is the owned RPC client closed by Close. It is nil
	// for connections driven by a fake caller in tests.
	rpc closer

	mu       sync.RWMutex
	clientID uint64
	confirm  [8]byte
	rootFH   FileHandle

	// session is the negotiated v4.1 session, nil for v4.0 connections.
	session *Session
	// eirSequence is the sequence id returned by EXCHANGE_ID, used as the
	// csa_sequence in CREATE_SESSION (v4.1).
	eirSequence uint32

	// openSeqid is the monotonically increasing open-owner seqid (v4.0).
	openSeqid uint32
}

// MinorVersion returns the NFSv4 minor version this connection negotiated.
func (c *Conn) MinorVersion() uint32 {
	return c.cfg.MinorVersion
}

// Session returns the negotiated v4.1 session, or nil for v4.0 connections.
func (c *Conn) Session() *Session {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.session
}

// closer is the subset of the RPC client Conn needs for lifecycle management.
type closer interface {
	Close() error
}

// Close tears down session/client state (v4.1) and releases the underlying RPC
// connection, if any. Session teardown is best-effort: its errors are not
// returned, only the transport close error is.
func (c *Conn) Close() error {
	c.teardownSession()
	if c.rpc != nil {
		return c.rpc.Close()
	}
	return nil
}

// teardownSession issues DESTROY_SESSION and DESTROY_CLIENTID for v4.1
// connections. DESTROY_SESSION must not carry SEQUENCE, so it is sent through
// the base caller beneath the sessionCaller.
func (c *Conn) teardownSession() {
	c.mu.RLock()
	sess := c.session
	caller := c.caller
	clientID := c.clientID
	c.mu.RUnlock()
	if sess == nil {
		return
	}
	base := caller
	if sc, ok := caller.(*sessionCaller); ok {
		base = sc.base
	}
	ctx := context.Background()

	var dsRes DestroySessionRes
	dsComp := c.compound().Add(DestroySessionArgs{SessionID: sess.ID()})
	_, _ = base.CallCompound(ctx, dsComp, []Res{&dsRes})

	var dcRes DestroyClientIDRes
	dcComp := c.compound().Add(DestroyClientIDArgs{ClientID: clientID})
	_, _ = base.CallCompound(ctx, dcComp, []Res{&dcRes})
}

// NewConn returns a Conn that issues compounds through caller.
func NewConn(caller CompoundCaller, cfg ConnConfig) *Conn {
	return &Conn{caller: caller, cfg: cfg}
}

// ClientID returns the confirmed clientid established by Mount.
func (c *Conn) ClientID() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clientID
}

// RootFH returns the export root filehandle obtained by Mount.
func (c *Conn) RootFH() FileHandle {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rootFH
}

// compound returns a new Compound carrying this connection's minor version.
func (c *Conn) compound() *Compound {
	cc := NewCompound("")
	cc.MinorVersion = c.cfg.MinorVersion
	return cc
}

// Mount performs the bootstrap appropriate to the configured minor version.
//
// For v4.0 the flow mirrors libnfs's nfs4_mount_async (SETCLIENTID ->
// SETCLIENTID_CONFIRM -> PUTROOTFH/GETFH). For v4.1 it runs EXCHANGE_ID ->
// CREATE_SESSION, installs the SEQUENCE-prepending caller, issues
// RECLAIM_COMPLETE, then PUTROOTFH/GETFH.
func (c *Conn) Mount(ctx context.Context) error {
	if c.cfg.MinorVersion >= MinorV41 {
		return c.mountV41(ctx)
	}
	if err := c.setClientID(ctx); err != nil {
		return err
	}
	if err := c.confirmClientID(ctx); err != nil {
		return err
	}
	return c.fetchRootFH(ctx)
}

// mountV41 establishes a v4.1 session and fetches the root filehandle. The
// EXCHANGE_ID and CREATE_SESSION compounds are sent through the base caller
// (they must not carry SEQUENCE); once the session exists, c.caller is replaced
// with a sessionCaller so every subsequent compound is SEQUENCE-prefixed.
func (c *Conn) mountV41(ctx context.Context) error {
	if err := c.exchangeID(ctx); err != nil {
		return err
	}
	if err := c.createSession(ctx); err != nil {
		return err
	}
	// Best-effort reclaim-complete; servers require it before normal ops but a
	// non-OK status here is surfaced.
	if err := c.reclaimComplete(ctx); err != nil {
		return err
	}
	return c.fetchRootFH(ctx)
}

// exchangeID issues EXCHANGE_ID and stores the returned clientid and sequence.
func (c *Conn) exchangeID(ctx context.Context) error {
	var verifier [VerifierSize]byte
	if _, err := rand.Read(verifier[:]); err != nil {
		return fmt.Errorf("nfs4: generating exchange-id verifier: %w", err)
	}
	args := ExchangeIDArgs{
		Verifier: verifier,
		OwnerID:  []byte(c.clientName()),
		Flags:    ExchgIDFlagUseNonPNFS,
	}
	var res ExchangeIDRes
	comp := c.compound().Add(args)
	if _, err := c.caller.CallCompound(ctx, comp, []Res{&res}); err != nil {
		return fmt.Errorf("nfs4: EXCHANGE_ID: %w", err)
	}
	if err := res.Status.Err(); err != nil {
		return fmt.Errorf("nfs4: EXCHANGE_ID rejected: %w", err)
	}
	c.mu.Lock()
	c.clientID = res.ClientID
	c.eirSequence = res.SequenceID
	c.mu.Unlock()
	return nil
}

// createSession issues CREATE_SESSION, builds the slot table, and swaps in the
// SEQUENCE-prepending caller.
func (c *Conn) createSession(ctx context.Context) error {
	c.mu.RLock()
	clientID := c.clientID
	seq := c.eirSequence
	c.mu.RUnlock()

	chan_ := ChannelAttrs{
		MaxRequestSize:      1 << 20,
		MaxResponseSize:     1 << 20,
		MaxResponseSizeCach: 1 << 16,
		MaxOperations:       16,
		MaxRequests:         DefaultSlotCount,
	}
	args := CreateSessionArgs{
		ClientID:   clientID,
		SequenceID: seq,
		ForeChan:   chan_,
		BackChan:   chan_,
	}
	var res CreateSessionRes
	comp := c.compound().Add(args)
	if _, err := c.caller.CallCompound(ctx, comp, []Res{&res}); err != nil {
		return fmt.Errorf("nfs4: CREATE_SESSION: %w", err)
	}
	if err := res.Status.Err(); err != nil {
		return fmt.Errorf("nfs4: CREATE_SESSION rejected: %w", err)
	}

	slotCount := int(res.ForeChan.MaxRequests)
	if slotCount < 1 {
		slotCount = 1
	}
	sess := NewSession(res.SessionID, slotCount)

	c.mu.Lock()
	c.session = sess
	c.caller = newSessionCaller(c.caller, sess)
	c.mu.Unlock()
	return nil
}

// reclaimComplete issues RECLAIM_COMPLETE (global form) through the session.
func (c *Conn) reclaimComplete(ctx context.Context) error {
	var res ReclaimCompleteRes
	comp := c.compound().Add(ReclaimCompleteArgs{OneFS: false})
	if _, err := c.caller.CallCompound(ctx, comp, []Res{&res}); err != nil {
		return fmt.Errorf("nfs4: RECLAIM_COMPLETE: %w", err)
	}
	// NFS4ERR_COMPLETE_ALREADY is benign (already reclaimed).
	if res.Status != NFS4_OK && res.Status != NFS4ERR_COMPLETE_ALREADY {
		return fmt.Errorf("nfs4: RECLAIM_COMPLETE rejected: %w", res.Status.Err())
	}
	return nil
}

// clientName returns the configured client name or a hostname-derived default.
func (c *Conn) clientName() string {
	if c.cfg.ClientName != "" {
		return c.cfg.ClientName
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "go-nfs-client"
	}
	return "go-nfs-client/" + host
}

// setClientID issues SETCLIENTID and stores the returned clientid and confirm
// verifier.
func (c *Conn) setClientID(ctx context.Context) error {
	var verifier [8]byte
	if _, err := rand.Read(verifier[:]); err != nil {
		return fmt.Errorf("nfs4: generating clientid verifier: %w", err)
	}

	args := SetclientidArgs{
		Verifier:      verifier,
		ID:            []byte(c.clientName()),
		CallbackProg:  0,
		CallbackNetID: "tcp",
		CallbackAddr:  "0.0.0.0.0.0",
		CallbackIdent: 0,
	}
	var res SetclientidRes
	comp := c.compound().Add(args)
	if _, err := c.caller.CallCompound(ctx, comp, []Res{&res}); err != nil {
		return fmt.Errorf("nfs4: SETCLIENTID: %w", err)
	}
	if err := res.Status.Err(); err != nil {
		return fmt.Errorf("nfs4: SETCLIENTID rejected: %w", err)
	}

	c.mu.Lock()
	c.clientID = res.ClientID
	c.confirm = res.Confirm
	c.mu.Unlock()
	return nil
}

// confirmClientID issues SETCLIENTID_CONFIRM for the stored clientid.
func (c *Conn) confirmClientID(ctx context.Context) error {
	c.mu.RLock()
	args := SetclientidConfirmArgs{ClientID: c.clientID, Verifier: c.confirm}
	c.mu.RUnlock()

	var res StatusRes
	comp := c.compound().Add(args)
	if _, err := c.caller.CallCompound(ctx, comp, []Res{&res}); err != nil {
		return fmt.Errorf("nfs4: SETCLIENTID_CONFIRM: %w", err)
	}
	if err := res.Status.Err(); err != nil {
		return fmt.Errorf("nfs4: SETCLIENTID_CONFIRM rejected: %w", err)
	}
	return nil
}

// fetchRootFH issues PUTROOTFH + GETFH and stores the root filehandle.
func (c *Conn) fetchRootFH(ctx context.Context) error {
	var putRes StatusRes
	var getRes GetfhRes
	comp := c.compound().Add(PutrootfhArgs{}).Add(GetfhArgs{})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &getRes})
	if err != nil {
		return fmt.Errorf("nfs4: PUTROOTFH/GETFH: %w", err)
	}
	if err := r.Err(); err != nil {
		return fmt.Errorf("nfs4: fetching root filehandle: %w", err)
	}
	if getRes.Status != NFS4_OK {
		return fmt.Errorf("nfs4: GETFH on root: %w", getRes.Status.Err())
	}
	c.mu.Lock()
	c.rootFH = FileHandle(getRes.FH)
	c.mu.Unlock()
	return nil
}

// Lookup resolves a single name within the directory identified by dir and
// returns the resulting filehandle. It issues PUTFH(dir) + LOOKUP(name) +
// GETFH.
func (c *Conn) Lookup(ctx context.Context, dir FileHandle, name string) (FileHandle, error) {
	var putRes, lookupRes StatusRes
	var getRes GetfhRes
	comp := c.compound().
		Add(PutfhArgs{FH: dir}).
		Add(LookupArgs{Name: name}).
		Add(GetfhArgs{})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &lookupRes, &getRes})
	if err != nil {
		return nil, fmt.Errorf("nfs4: LOOKUP %q: %w", name, err)
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	if getRes.Status != NFS4_OK {
		return nil, getRes.Status.Err()
	}
	return FileHandle(getRes.FH), nil
}

// GetAttr issues PUTFH(fh) + GETATTR(mask) and returns the raw attribute mask
// and values for the object. The attr package decodes the values.
func (c *Conn) GetAttr(ctx context.Context, fh FileHandle, mask Bitmap) (Bitmap, []byte, error) {
	var putRes StatusRes
	var getRes GetattrRes
	comp := c.compound().
		Add(PutfhArgs{FH: fh}).
		Add(GetattrArgs{AttrRequest: mask})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &getRes})
	if err != nil {
		return nil, nil, fmt.Errorf("nfs4: GETATTR: %w", err)
	}
	if err := r.Err(); err != nil {
		return nil, nil, err
	}
	if getRes.Status != NFS4_OK {
		return nil, nil, getRes.Status.Err()
	}
	return getRes.AttrMask, getRes.AttrVals, nil
}

// AnonStateid is the all-zero "anonymous" stateid valid for read access to a
// file that has not been explicitly OPENed (RFC 7530 §9.1.4.3).
var AnonStateid = Stateid{}

// Read issues PUTFH(fh) + READ(offset, count) using the anonymous stateid and
// returns the data, the server EOF flag, and any error.
func (c *Conn) Read(ctx context.Context, fh FileHandle, offset uint64, count uint32) ([]byte, bool, error) {
	var putRes StatusRes
	var readRes ReadRes
	comp := c.compound().
		Add(PutfhArgs{FH: fh}).
		Add(ReadArgs{Stateid: AnonStateid, Offset: offset, Count: count})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &readRes})
	if err != nil {
		return nil, false, fmt.Errorf("nfs4: READ: %w", err)
	}
	if err := r.Err(); err != nil {
		return nil, false, err
	}
	if readRes.Status != NFS4_OK {
		return nil, false, readRes.Status.Err()
	}
	return readRes.Data, readRes.EOF, nil
}

// Readdir issues PUTFH(dir) + READDIR for one page starting at cookie/cookieverf
// and returns the decoded result (entries, next cookie verifier, EOF).
func (c *Conn) Readdir(ctx context.Context, dir FileHandle, cookie uint64, cookieverf [8]byte, attrMask Bitmap) (*ReaddirRes, error) {
	const dircount = 8192
	const maxcount = 65536
	var putRes StatusRes
	var ddRes ReaddirRes
	comp := c.compound().
		Add(PutfhArgs{FH: dir}).
		Add(ReaddirArgs{
			Cookie:      cookie,
			Cookieverf:  cookieverf,
			Dircount:    dircount,
			Maxcount:    maxcount,
			AttrRequest: attrMask,
		})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &ddRes})
	if err != nil {
		return nil, fmt.Errorf("nfs4: READDIR: %w", err)
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	if ddRes.Status != NFS4_OK {
		return nil, ddRes.Status.Err()
	}
	return &ddRes, nil
}

// Renew issues a RENEW for the connection's clientid, keeping its leases alive.
func (c *Conn) Renew(ctx context.Context) error {
	id := c.ClientID()
	var res StatusRes
	comp := c.compound().Add(RenewArgs{ClientID: id})
	if _, err := c.caller.CallCompound(ctx, comp, []Res{&res}); err != nil {
		return fmt.Errorf("nfs4: RENEW: %w", err)
	}
	return res.Status.Err()
}

// Readlink issues PUTFH(fh) + READLINK and returns the symlink target.
func (c *Conn) Readlink(ctx context.Context, fh FileHandle) (string, error) {
	var putRes StatusRes
	var rlRes ReadlinkRes
	comp := c.compound().
		Add(PutfhArgs{FH: fh}).
		Add(ReadlinkArgs{})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &rlRes})
	if err != nil {
		return "", fmt.Errorf("nfs4: READLINK: %w", err)
	}
	if err := r.Err(); err != nil {
		return "", err
	}
	if rlRes.Status != NFS4_OK {
		return "", rlRes.Status.Err()
	}
	return rlRes.Link, nil
}

// LookupPath resolves a slash-separated path relative to the root filehandle
// by issuing successive LOOKUP operations, returning the target filehandle.
func (c *Conn) LookupPath(ctx context.Context, components []string) (FileHandle, error) {
	cur := c.RootFH()
	if cur == nil {
		return nil, fmt.Errorf("nfs4: not mounted (no root filehandle)")
	}
	for _, comp := range components {
		if comp == "" || comp == "." {
			continue
		}
		fh, err := c.Lookup(ctx, cur, comp)
		if err != nil {
			return nil, err
		}
		cur = fh
	}
	return cur, nil
}
