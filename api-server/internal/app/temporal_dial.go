package app

import (
	"context"
	"log/slog"
	"time"

	"go.temporal.io/sdk/client"
)

// temporalDialTimeout bounds how long the worker waits for Temporal at startup. After a host
// reboot every container restarts at once and Docker does not apply Compose's depends_on health
// gates, so the worker can start before the Temporal frontend serves; the auto-setup image can
// take a minute or more to get there on a cold start. A worker that gave up at the first attempt
// stayed down under a supervisor that does not restart it (Air in the dev stack), leaving every
// Workflow pending. The bound still turns an address that never answers into a startup error.
const temporalDialTimeout = 3 * time.Minute

// temporalDialMaxBackoff caps the exponential backoff between connection attempts.
const temporalDialMaxBackoff = 30 * time.Second

// dialTemporal connects to Temporal, retrying with exponential backoff from one second up to
// temporalDialMaxBackoff until timeout leaves no room for another attempt, and then returns the
// last Dial error. Every Dial error is retried: while the frontend starts, Dial's eager health
// check reports refused connections, names that do not resolve yet, or deadlines waiting for a
// ready connection, in shapes that depend on where the attempt was when it gave up, so the
// timeout rather than error classification is what makes a persistent failure fatal. Canceling
// ctx (SIGINT/SIGTERM) stops the wait at once. The caller owns the returned client and must
// Close it.
func dialTemporal(ctx context.Context, options client.Options, timeout time.Duration) (client.Client, error) {
	deadline := time.Now().Add(timeout)
	backoff := time.Second
	for {
		temporalClient, err := client.DialContext(ctx, options)
		if err == nil {
			return temporalClient, nil
		}
		if ctx.Err() != nil || time.Now().Add(backoff).After(deadline) {
			return nil, err
		}
		slog.Warn("Temporal is not reachable yet; retrying",
			"address", options.HostPort, "retryIn", backoff.String(), "error", err)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, err
		case <-timer.C:
		}
		backoff = min(2*backoff, temporalDialMaxBackoff)
	}
}
