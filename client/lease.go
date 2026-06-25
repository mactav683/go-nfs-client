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

package client

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mactav683/go-nfs-client/nfs4"
)

// Clock abstracts time so the lease loop is deterministically testable.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// realClock is the production Clock backed by the time package.
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// StateidHandle identifies a tracked stateid within a LeaseManager.
type StateidHandle uint64

// LeaseConfig configures a LeaseManager.
type LeaseConfig struct {
	// LeasePeriod is the server's advertised lease time. RENEW fires at a
	// fraction of this period to stay comfortably within it.
	LeasePeriod time.Duration
	// Clock supplies time; defaults to a real clock if nil.
	Clock Clock
	// Renew renews the client's leases (typically Conn.Renew).
	Renew func(ctx context.Context) error
	// Recover re-establishes client state after the server loses it
	// (re-SETCLIENTID + reclaim/reopen), typically Conn.Mount.
	Recover func(ctx context.Context) error
}

// LeaseManager keeps a client's leases alive with a background RENEW loop,
// tracks open stateids, and provides transparent recovery + retry when the
// server reports it has lost the client's state.
type LeaseManager struct {
	cfg   LeaseConfig
	clock Clock

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}

	mu       sync.Mutex
	nextH    StateidHandle
	stateids map[StateidHandle]trackedStateid
}

type trackedStateid struct {
	key string
	sid nfs4.Stateid
}

// NewLeaseManager constructs a LeaseManager from cfg.
func NewLeaseManager(cfg LeaseConfig) *LeaseManager {
	clk := cfg.Clock
	if clk == nil {
		clk = realClock{}
	}
	if cfg.LeasePeriod <= 0 {
		cfg.LeasePeriod = 90 * time.Second
	}
	return &LeaseManager{
		cfg:      cfg,
		clock:    clk,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		stateids: map[StateidHandle]trackedStateid{},
	}
}

// renewInterval is the duration between RENEW attempts: two-thirds of the lease
// period, leaving headroom for retries before expiry.
func (lm *LeaseManager) renewInterval() time.Duration {
	return lm.cfg.LeasePeriod * 2 / 3
}

// Start launches the background RENEW loop. It is safe to call once.
func (lm *LeaseManager) Start() {
	go lm.loop()
}

// loop runs the periodic RENEW until Stop is called.
func (lm *LeaseManager) loop() {
	defer close(lm.doneCh)
	for {
		select {
		case <-lm.stopCh:
			return
		case <-lm.clock.After(lm.renewInterval()):
			ctx := context.Background()
			if err := lm.cfg.Renew(ctx); err != nil {
				if isClientStateLost(err) {
					_ = lm.cfg.Recover(ctx)
				}
			}
		}
	}
}

// Stop terminates the RENEW loop and waits for it to exit.
func (lm *LeaseManager) Stop() {
	lm.stopOnce.Do(func() {
		close(lm.stopCh)
	})
	select {
	case <-lm.doneCh:
	case <-time.After(time.Second):
		// Best-effort: the loop may be blocked in a fake clock without a fire.
	}
}

// WithRetry runs fn, and if it fails with a client-state-lost error, invokes
// Recover and retries fn once. Other errors propagate unchanged.
func (lm *LeaseManager) WithRetry(ctx context.Context, fn func(ctx context.Context) error) error {
	err := fn(ctx)
	if err == nil || !isClientStateLost(err) {
		return err
	}
	if rerr := lm.cfg.Recover(ctx); rerr != nil {
		return rerr
	}
	return fn(ctx)
}

// TrackStateid records a stateid under a caller-chosen key and returns a handle.
func (lm *LeaseManager) TrackStateid(key string, sid nfs4.Stateid) StateidHandle {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.nextH++
	h := lm.nextH
	lm.stateids[h] = trackedStateid{key: key, sid: sid}
	return h
}

// Stateid returns the current stateid for a handle.
func (lm *LeaseManager) Stateid(h StateidHandle) nfs4.Stateid {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.stateids[h].sid
}

// ReissueStateid updates the stateid for a handle (e.g. after reopen during
// recovery).
func (lm *LeaseManager) ReissueStateid(h StateidHandle, sid nfs4.Stateid) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if t, ok := lm.stateids[h]; ok {
		t.sid = sid
		lm.stateids[h] = t
	}
}

// ForgetStateid removes a tracked stateid (e.g. on CLOSE).
func (lm *LeaseManager) ForgetStateid(h StateidHandle) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	delete(lm.stateids, h)
}

// TrackedKeys returns the keys of all tracked stateids.
func (lm *LeaseManager) TrackedKeys() []string {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	keys := make([]string, 0, len(lm.stateids))
	for _, t := range lm.stateids {
		keys = append(keys, t.key)
	}
	return keys
}

// isClientStateLost reports whether err indicates the server has lost the
// client's clientid/stateid and recovery (re-SETCLIENTID, reopen) is required.
func isClientStateLost(err error) bool {
	var se *nfs4.StatusError
	if !errors.As(err, &se) {
		return false
	}
	switch se.Status {
	case nfs4.NFS4ERR_STALE_CLIENTID,
		nfs4.NFS4ERR_STALE_STATEID,
		nfs4.NFS4ERR_BAD_STATEID,
		nfs4.NFS4ERR_EXPIRED:
		return true
	}
	return false
}
