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
	"sync"
	"testing"
	"time"

	"github.com/mactav683/go-nfs-client/nfs4"
)

// fakeClock is a manually-advanced clock for deterministic lease tests. It
// supports a single pending timer (sufficient for the lease loop).
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	fires   chan time.Time
	dur     time.Duration
	started bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(0, 0), fires: make(chan time.Time, 1)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After returns a channel that fires when the test advances past d.
func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dur = d
	c.started = true
	return c.fires
}

// Advance moves the clock forward and fires the pending timer.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
	select {
	case c.fires <- c.now:
	default:
	}
}

// TestRenewLoopFiresWithinLeasePeriod verifies the RENEW loop calls renew when
// the timer fires (within the lease period).
func TestRenewLoopFiresWithinLeasePeriod(t *testing.T) {
	clock := newFakeClock()
	renewed := make(chan struct{}, 4)

	lm := NewLeaseManager(LeaseConfig{
		LeasePeriod: 90 * time.Second,
		Clock:       clock,
		Renew: func(ctx context.Context) error {
			renewed <- struct{}{}
			return nil
		},
		Recover: func(ctx context.Context) error { return nil },
	})
	lm.Start()
	defer lm.Stop()

	// Advance to trigger the renew timer.
	clock.Advance(60 * time.Second)
	select {
	case <-renewed:
	case <-time.After(2 * time.Second):
		t.Fatalf("RENEW loop did not fire within deadline")
	}
}

// TestStaleClientIDTriggersRecovery verifies that a RENEW returning
// STALE_CLIENTID triggers the recover hook.
func TestStaleClientIDTriggersRecovery(t *testing.T) {
	clock := newFakeClock()
	recovered := make(chan struct{}, 1)
	var renewCalls int
	var mu sync.Mutex

	lm := NewLeaseManager(LeaseConfig{
		LeasePeriod: 90 * time.Second,
		Clock:       clock,
		Renew: func(ctx context.Context) error {
			mu.Lock()
			renewCalls++
			mu.Unlock()
			return &nfs4.StatusError{Status: nfs4.NFS4ERR_STALE_CLIENTID}
		},
		Recover: func(ctx context.Context) error {
			recovered <- struct{}{}
			return nil
		},
	})
	lm.Start()
	defer lm.Stop()

	clock.Advance(60 * time.Second)
	select {
	case <-recovered:
	case <-time.After(2 * time.Second):
		t.Fatalf("recovery was not triggered by STALE_CLIENTID")
	}
}

// TestWithRetryRecoversAndRetries verifies the transparent-retry wrapper: a call
// that first returns STALE_CLIENTID triggers recovery and a successful retry.
func TestWithRetryRecoversAndRetries(t *testing.T) {
	clock := newFakeClock()
	var recovered bool
	lm := NewLeaseManager(LeaseConfig{
		LeasePeriod: 90 * time.Second,
		Clock:       clock,
		Renew:       func(ctx context.Context) error { return nil },
		Recover: func(ctx context.Context) error {
			recovered = true
			return nil
		},
	})

	calls := 0
	err := lm.WithRetry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return &nfs4.StatusError{Status: nfs4.NFS4ERR_STALE_CLIENTID}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithRetry: %v", err)
	}
	if !recovered {
		t.Fatalf("expected recovery to be invoked")
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
}

// TestStateidTracking verifies tracked stateids can be registered, listed, and
// reissued (updated) after recovery.
func TestStateidTracking(t *testing.T) {
	lm := NewLeaseManager(LeaseConfig{
		LeasePeriod: 90 * time.Second,
		Clock:       newFakeClock(),
		Renew:       func(ctx context.Context) error { return nil },
		Recover:     func(ctx context.Context) error { return nil },
	})

	old := nfs4.Stateid{Seqid: 1, Other: [12]byte{1}}
	h := lm.TrackStateid("file-key", old)
	if got := lm.Stateid(h); got != old {
		t.Fatalf("tracked stateid = %+v, want %+v", got, old)
	}

	updated := nfs4.Stateid{Seqid: 2, Other: [12]byte{2}}
	lm.ReissueStateid(h, updated)
	if got := lm.Stateid(h); got != updated {
		t.Fatalf("reissued stateid = %+v, want %+v", got, updated)
	}

	keys := lm.TrackedKeys()
	if len(keys) != 1 || keys[0] != "file-key" {
		t.Fatalf("tracked keys = %v", keys)
	}
}

// TestNonRecoverableErrorNotRetried verifies a plain error is returned without
// recovery or retry.
func TestNonRecoverableErrorNotRetried(t *testing.T) {
	lm := NewLeaseManager(LeaseConfig{
		LeasePeriod: 90 * time.Second,
		Clock:       newFakeClock(),
		Renew:       func(ctx context.Context) error { return nil },
		Recover:     func(ctx context.Context) error { t.Fatalf("recover should not be called"); return nil },
	})
	calls := 0
	err := lm.WithRetry(context.Background(), func(ctx context.Context) error {
		calls++
		return &nfs4.StatusError{Status: nfs4.NFS4ERR_NOENT}
	})
	if err == nil {
		t.Fatalf("expected error to propagate")
	}
	if calls != 1 {
		t.Fatalf("expected single attempt, got %d", calls)
	}
}
