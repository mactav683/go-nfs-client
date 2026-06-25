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
	"time"
)

// Default retry parameters used when a ConnConfig leaves them unset. They give a
// bounded exponential backoff: ~10ms, 20ms, 40ms, ... capped at 1s, for up to
// 10 attempts (roughly a few seconds total) before the last error is returned.
const (
	DefaultRetryInitial     = 10 * time.Millisecond
	DefaultRetryMax         = 1 * time.Second
	DefaultRetryMaxAttempts = 10
)

// RetryConfig tunes the backoff-and-retry behavior applied to retriable server
// responses (NFS4ERR_DELAY / NFS4ERR_GRACE). The zero value is filled in with
// the package defaults by newRetryCaller.
type RetryConfig struct {
	// Initial is the first backoff delay. Each subsequent retry doubles the
	// delay up to Max.
	Initial time.Duration
	// Max caps the backoff delay between attempts.
	Max time.Duration
	// MaxAttempts bounds the total number of attempts (the first try plus
	// retries). A value < 1 is treated as 1.
	MaxAttempts int
	// sleep waits for d honoring ctx cancellation; it is injectable so tests can
	// run deterministically. When nil, a real context-aware sleeper is used.
	sleep func(ctx context.Context, d time.Duration) error
}

// withDefaults returns a copy of cfg with any zero fields replaced by the
// package defaults and a usable sleeper.
func (cfg RetryConfig) withDefaults() RetryConfig {
	if cfg.Initial <= 0 {
		cfg.Initial = DefaultRetryInitial
	}
	if cfg.Max <= 0 {
		cfg.Max = DefaultRetryMax
	}
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = DefaultRetryMaxAttempts
	}
	if cfg.sleep == nil {
		cfg.sleep = sleepCtx
	}
	return cfg
}

// sleepCtx waits for d or until ctx is done, returning ctx.Err() if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isRetriableStatus reports whether a server status warrants a backoff-and-retry
// rather than surfacing the error to the caller. NFS4ERR_DELAY asks the client
// to retry shortly; NFS4ERR_GRACE indicates the server is in its grace/recovery
// window and the request should be retried until the window clears.
func isRetriableStatus(s Status) bool {
	return s == NFS4ERR_DELAY || s == NFS4ERR_GRACE
}

// retryCaller decorates a CompoundCaller, transparently retrying compounds that
// the server answered with a retriable status (DELAY/GRACE) using bounded
// exponential backoff. It sits above the sessionCaller so each retry re-issues a
// fresh SEQUENCE slot/seq, matching the server's expectation that a DELAY
// consumed the slot.
type retryCaller struct {
	base CompoundCaller
	cfg  RetryConfig
}

// newRetryCaller returns a CompoundCaller that retries retriable responses from
// base according to cfg (defaults applied for zero fields).
func newRetryCaller(base CompoundCaller, cfg RetryConfig) *retryCaller {
	return &retryCaller{base: base, cfg: cfg.withDefaults()}
}

// CallCompound invokes the base caller, retrying with bounded backoff while the
// response is retriable (DELAY/GRACE), honoring ctx cancellation before each
// attempt and during each backoff sleep. When attempts are exhausted the last
// result/error is returned.
func (rc *retryCaller) CallCompound(ctx context.Context, c *Compound, results []Res) (*CompoundResult, error) {
	delay := rc.cfg.Initial
	var lastRes *CompoundResult
	var lastErr error

	for attempt := 0; attempt < rc.cfg.MaxAttempts; attempt++ {
		// Abort promptly if the caller's context is already done.
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}

		r, err := rc.base.CallCompound(ctx, c, results)
		lastRes, lastErr = r, err

		if !retriable(r, err) {
			return r, err
		}

		// Retriable; back off before the next attempt unless this was the last.
		if attempt == rc.cfg.MaxAttempts-1 {
			break
		}
		if serr := rc.cfg.sleep(ctx, delay); serr != nil {
			return nil, serr
		}
		delay *= 2
		if delay > rc.cfg.Max {
			delay = rc.cfg.Max
		}
	}

	// Attempts exhausted while still retriable. Surface an error so the caller
	// sees a failure rather than a silently-still-DELAY compound result. If the
	// base reported the retriable condition only via the compound status (no
	// error), synthesize one from that status.
	if lastErr == nil && lastRes != nil {
		return nil, lastRes.Status.Err()
	}
	return nil, lastErr
}

// retriable reports whether (r, err) represents a retriable server response,
// either via the compound status or an error wrapping a retriable status.
func retriable(r *CompoundResult, err error) bool {
	if r != nil && isRetriableStatus(r.Status) {
		return true
	}
	if err != nil {
		if errorHasStatus(err, NFS4ERR_DELAY) || errorHasStatus(err, NFS4ERR_GRACE) {
			return true
		}
	}
	return false
}
