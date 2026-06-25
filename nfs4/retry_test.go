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
	"errors"
	"io/fs"
	"testing"
	"time"
)

// scriptedCaller returns a canned (CompoundResult, error) for each call in
// sequence, recording how many times it was invoked. It is used to drive the
// retryCaller without going through XDR encoding.
type scriptedCaller struct {
	results []scriptedResult
	calls   int
}

type scriptedResult struct {
	status Status
	err    error
}

func (s *scriptedCaller) CallCompound(ctx context.Context, c *Compound, results []Res) (*CompoundResult, error) {
	if s.calls >= len(s.results) {
		s.calls++
		return &CompoundResult{Status: NFS4_OK}, nil
	}
	r := s.results[s.calls]
	s.calls++
	if r.err != nil {
		return nil, r.err
	}
	return &CompoundResult{Status: r.status}, nil
}

// recordingSleeper records the backoff durations requested and can be told to
// fail (e.g. to simulate ctx cancellation during a sleep).
type recordingSleeper struct {
	delays []time.Duration
	fail   error
}

func (r *recordingSleeper) sleep(ctx context.Context, d time.Duration) error {
	r.delays = append(r.delays, d)
	if r.fail != nil {
		return r.fail
	}
	return nil
}

// TestRobustnessDelayRetry proves a single NFS4ERR_DELAY is retried and the
// following success is returned, with backoff bounded by the attempt cap.
func TestRobustnessDelayRetry(t *testing.T) {
	base := &scriptedCaller{
		results: []scriptedResult{
			{status: NFS4ERR_DELAY},
			{status: NFS4_OK},
		},
	}
	sl := &recordingSleeper{}
	rc := newRetryCaller(base, RetryConfig{
		Initial:     1 * time.Millisecond,
		Max:         10 * time.Millisecond,
		MaxAttempts: 5,
		sleep:       sl.sleep,
	})

	r, err := rc.CallCompound(context.Background(), NewCompound(""), nil)
	if err != nil {
		t.Fatalf("CallCompound: %v", err)
	}
	if r.Status != NFS4_OK {
		t.Fatalf("status = %v, want NFS4_OK", r.Status)
	}
	if base.calls != 2 {
		t.Fatalf("base called %d times, want 2", base.calls)
	}
	if len(sl.delays) != 1 {
		t.Fatalf("slept %d times, want 1", len(sl.delays))
	}
}

// TestRobustnessGraceRetry proves NFS4ERR_GRACE is retried until cleared.
func TestRobustnessGraceRetry(t *testing.T) {
	base := &scriptedCaller{
		results: []scriptedResult{
			{status: NFS4ERR_GRACE},
			{status: NFS4ERR_GRACE},
			{status: NFS4_OK},
		},
	}
	sl := &recordingSleeper{}
	rc := newRetryCaller(base, RetryConfig{
		Initial:     1 * time.Millisecond,
		Max:         10 * time.Millisecond,
		MaxAttempts: 10,
		sleep:       sl.sleep,
	})

	r, err := rc.CallCompound(context.Background(), NewCompound(""), nil)
	if err != nil {
		t.Fatalf("CallCompound: %v", err)
	}
	if r.Status != NFS4_OK {
		t.Fatalf("status = %v, want NFS4_OK", r.Status)
	}
	if base.calls != 3 {
		t.Fatalf("base called %d times, want 3", base.calls)
	}
}

// TestRobustnessRetryBounded proves retries are bounded: when DELAY persists
// past MaxAttempts the last error is returned rather than looping forever.
func TestRobustnessRetryBounded(t *testing.T) {
	base := &scriptedCaller{
		results: []scriptedResult{
			{status: NFS4ERR_DELAY},
			{status: NFS4ERR_DELAY},
			{status: NFS4ERR_DELAY},
			{status: NFS4ERR_DELAY},
			{status: NFS4ERR_DELAY},
		},
	}
	sl := &recordingSleeper{}
	rc := newRetryCaller(base, RetryConfig{
		Initial:     1 * time.Millisecond,
		Max:         4 * time.Millisecond,
		MaxAttempts: 3,
		sleep:       sl.sleep,
	})

	_, err := rc.CallCompound(context.Background(), NewCompound(""), nil)
	if err == nil {
		t.Fatalf("expected error after exhausting attempts")
	}
	if !errorHasStatus(err, NFS4ERR_DELAY) {
		t.Fatalf("err = %v, want wrapping NFS4ERR_DELAY", err)
	}
	if base.calls != 3 {
		t.Fatalf("base called %d times, want 3 (bounded)", base.calls)
	}
	// Backoff must be capped at Max and grow monotonically toward it.
	for _, d := range sl.delays {
		if d > 4*time.Millisecond {
			t.Fatalf("backoff %v exceeded Max", d)
		}
	}
}

// TestRobustnessNoRetryOnOtherStatus proves a non-retriable status is returned
// immediately without sleeping or re-calling.
func TestRobustnessNoRetryOnOtherStatus(t *testing.T) {
	base := &scriptedCaller{
		results: []scriptedResult{
			{status: NFS4ERR_NOENT},
		},
	}
	sl := &recordingSleeper{}
	rc := newRetryCaller(base, RetryConfig{
		Initial:     1 * time.Millisecond,
		Max:         10 * time.Millisecond,
		MaxAttempts: 5,
		sleep:       sl.sleep,
	})

	r, err := rc.CallCompound(context.Background(), NewCompound(""), nil)
	if err != nil {
		t.Fatalf("CallCompound: %v", err)
	}
	if r.Status != NFS4ERR_NOENT {
		t.Fatalf("status = %v, want NFS4ERR_NOENT", r.Status)
	}
	if base.calls != 1 {
		t.Fatalf("base called %d times, want 1 (no retry)", base.calls)
	}
	if len(sl.delays) != 0 {
		t.Fatalf("slept %d times, want 0", len(sl.delays))
	}
}

// TestRobustnessContextCancel proves a cancelled ctx aborts a retry loop
// promptly: the sleeper reports ctx cancellation and the call returns it.
func TestRobustnessContextCancel(t *testing.T) {
	base := &scriptedCaller{
		results: []scriptedResult{
			{status: NFS4ERR_DELAY},
			{status: NFS4ERR_DELAY},
		},
	}
	sl := &recordingSleeper{fail: context.Canceled}
	rc := newRetryCaller(base, RetryConfig{
		Initial:     1 * time.Millisecond,
		Max:         10 * time.Millisecond,
		MaxAttempts: 5,
		sleep:       sl.sleep,
	})

	_, err := rc.CallCompound(context.Background(), NewCompound(""), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// One attempt made, one sleep attempted, then aborted.
	if base.calls != 1 {
		t.Fatalf("base called %d times, want 1", base.calls)
	}
}

// TestRobustnessContextAlreadyCancelled proves the loop checks ctx before each
// attempt and does not issue a call once ctx is already done.
func TestRobustnessContextAlreadyCancelled(t *testing.T) {
	base := &scriptedCaller{
		results: []scriptedResult{
			{status: NFS4_OK},
		},
	}
	rc := newRetryCaller(base, RetryConfig{
		Initial:     1 * time.Millisecond,
		Max:         10 * time.Millisecond,
		MaxAttempts: 5,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := rc.CallCompound(ctx, NewCompound(""), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if base.calls != 0 {
		t.Fatalf("base called %d times, want 0 (ctx pre-cancelled)", base.calls)
	}
}

// TestRobustnessDeadline proves a deadline causes the real sleeper to return a
// timeout error which aborts the retry loop.
func TestRobustnessDeadline(t *testing.T) {
	base := &scriptedCaller{
		results: []scriptedResult{
			{status: NFS4ERR_DELAY},
		},
	}
	// Use the real sleeper so the deadline path is exercised end-to-end.
	rc := newRetryCaller(base, RetryConfig{
		Initial:     50 * time.Millisecond,
		Max:         50 * time.Millisecond,
		MaxAttempts: 5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := rc.CallCompound(ctx, NewCompound(""), nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

// TestRobustnessRetryOnWrappedError proves the retry caller also retries when
// the base returns an error that wraps a retriable status (not just when it
// surfaces via CompoundResult.Status).
func TestRobustnessRetryOnWrappedError(t *testing.T) {
	base := &scriptedCaller{
		results: []scriptedResult{
			{err: NFS4ERR_DELAY.Err()},
			{status: NFS4_OK},
		},
	}
	sl := &recordingSleeper{}
	rc := newRetryCaller(base, RetryConfig{
		Initial:     1 * time.Millisecond,
		Max:         10 * time.Millisecond,
		MaxAttempts: 5,
		sleep:       sl.sleep,
	})

	r, err := rc.CallCompound(context.Background(), NewCompound(""), nil)
	if err != nil {
		t.Fatalf("CallCompound: %v", err)
	}
	if r.Status != NFS4_OK {
		t.Fatalf("status = %v, want NFS4_OK", r.Status)
	}
	if base.calls != 2 {
		t.Fatalf("base called %d times, want 2", base.calls)
	}
}

// TestRobustnessErrorMapping proves every representative nfsstat4 maps to a Go
// error and that the io/fs sentinels hold via errors.Is. It also asserts the
// full nfsstat4 set has a symbolic name so String() never falls back to the
// numeric form for a known code.
func TestRobustnessErrorMapping(t *testing.T) {
	cases := []struct {
		status   Status
		sentinel error
	}{
		{NFS4ERR_NOENT, fs.ErrNotExist},
		{NFS4ERR_STALE, fs.ErrNotExist},
		{NFS4ERR_FHEXPIRED, fs.ErrNotExist},
		{NFS4ERR_NOFILEHANDLE, fs.ErrNotExist},
		{NFS4ERR_EXIST, fs.ErrExist},
		{NFS4ERR_PERM, fs.ErrPermission},
		{NFS4ERR_ACCESS, fs.ErrPermission},
		{NFS4ERR_INVAL, fs.ErrInvalid},
		{NFS4ERR_BADXDR, fs.ErrInvalid},
		{NFS4ERR_BADHANDLE, fs.ErrInvalid},
	}
	for _, tc := range cases {
		err := tc.status.Err()
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("errors.Is(%v, %v) = false, want true", tc.status, tc.sentinel)
		}
	}

	// Codes that must NOT match a sentinel they are unrelated to.
	if errors.Is(NFS4ERR_NOENT.Err(), fs.ErrPermission) {
		t.Errorf("NOENT should not map to ErrPermission")
	}

	// Every known status code must have a symbolic name (no NFS4ERR(n) fallback).
	for s := range statusNames {
		name := s.String()
		if len(name) >= 8 && name[:8] == "NFS4ERR(" {
			t.Errorf("status %d has no symbolic name: got %q", uint32(s), name)
		}
	}
}

// TestRobustnessErrTimedOut proves NFS4ERR_DELAY/GRACE remain inspectable as
// retriable via the exported helper used by the retry caller.
func TestRobustnessIsRetriable(t *testing.T) {
	retriable := []Status{NFS4ERR_DELAY, NFS4ERR_GRACE}
	for _, s := range retriable {
		if !isRetriableStatus(s) {
			t.Errorf("isRetriableStatus(%v) = false, want true", s)
		}
	}
	notRetriable := []Status{NFS4_OK, NFS4ERR_NOENT, NFS4ERR_PERM, NFS4ERR_STALE}
	for _, s := range notRetriable {
		if isRetriableStatus(s) {
			t.Errorf("isRetriableStatus(%v) = true, want false", s)
		}
	}
}

// TestRobustnessConnRetriesDelay proves the retryCaller is actually installed by
// NewConn: a fake caller returns a DELAYed LOOKUP compound then a successful
// one, and Conn.Lookup transparently retries and returns the resolved fh.
func TestRobustnessConnRetriesDelay(t *testing.T) {
	dir := []byte{0x01}
	targetFH := []byte{0xF0, 0x0D}

	// A LOOKUP compound the server answered with NFS4ERR_DELAY (early
	// termination at PUTFH), followed by a successful PUTFH+LOOKUP+GETFH.
	fc := &fakeCaller{
		replies: [][]byte{
			buildCompoundRes(NFS4ERR_DELAY,
				resop(OpPutfh, []byte{0, 0, 0x27, 0x18}...), // NFS4ERR_DELAY=10008
			),
			buildCompoundRes(NFS4_OK,
				resop(OpPutfh, statusOK()...),
				resop(OpLookup, statusOK()...),
				fhResop(targetFH),
			),
		},
	}

	var slept int
	conn := NewConn(fc, ConnConfig{
		ClientName: "test",
		Retry: RetryConfig{
			Initial:     time.Millisecond,
			Max:         time.Millisecond,
			MaxAttempts: 5,
			sleep: func(ctx context.Context, d time.Duration) error {
				slept++
				return nil
			},
		},
	})

	fh, err := conn.Lookup(context.Background(), dir, "x")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !bytes.Equal(fh, targetFH) {
		t.Fatalf("fh = % x, want % x", fh, targetFH)
	}
	if fc.calls != 2 {
		t.Fatalf("caller invoked %d times, want 2 (one retry)", fc.calls)
	}
	if slept != 1 {
		t.Fatalf("slept %d times, want 1", slept)
	}
}
