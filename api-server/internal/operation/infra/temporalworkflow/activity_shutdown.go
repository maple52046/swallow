package temporalworkflow

import (
	"context"
	"errors"
)

// ErrActivityWorkerStopping distinguishes local worker drain from an operator-requested
// Workflow cancellation. Executors must leave external work running for this cause so the
// next activity attempt can resume it by idempotency key.
var ErrActivityWorkerStopping = errors.New("activity worker is stopping")

// IsActivityWorkerStopping reports whether an executor context ended for a local worker drain.
func IsActivityWorkerStopping(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), ErrActivityWorkerStopping)
}
