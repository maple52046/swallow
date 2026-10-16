package app

import (
	"context"
	"testing"
	"time"

	"go.temporal.io/sdk/client"
)

// unreachableTemporal is a loopback address nothing listens on, so Dial fails without leaving
// the host.
const unreachableTemporal = "127.0.0.1:1"

// SIGTERM during startup must not hold the worker for the whole retry window.
func TestDialTemporalStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := dialTemporal(ctx, client.Options{HostPort: unreachableTemporal}, time.Hour)
	if err == nil {
		t.Fatal("dialTemporal() with a canceled context: error = nil, want the Dial error")
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("dialTemporal() with a canceled context returned after %v, want it not to wait out the timeout", elapsed)
	}
}

// An address that does not answer yet is retried after the backoff instead of failing at the first
// attempt, and once the timeout leaves no room for another attempt the Dial error is returned, so
// a misconfigured worker still exits instead of hanging. With a 2.5s timeout the attempts are at
// 0s and 1s; the next backoff (2s) would pass the deadline.
func TestDialTemporalRetriesUntilTimeoutExpires(t *testing.T) {
	const timeout = 2500 * time.Millisecond
	start := time.Now()
	_, err := dialTemporal(context.Background(), client.Options{HostPort: unreachableTemporal}, timeout)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("dialTemporal() to an unreachable address: error = nil, want the Dial error")
	}
	if elapsed < time.Second {
		t.Errorf("dialTemporal() returned after %v, want at least one 1s backoff before giving up", elapsed)
	}
	if elapsed > timeout+5*time.Second {
		t.Errorf("dialTemporal() returned after %v, want it to stop near the %v timeout", elapsed, timeout)
	}
}
