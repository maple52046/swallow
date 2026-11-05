package temporalworkflow

import (
	"context"
	"errors"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// ErrActivityWorkerStopping distinguishes local worker drain from a user-requested
// Workflow cancellation. Executors must leave external work running for this cause so the
// next activity attempt can resume it by idempotency key.
var ErrActivityWorkerStopping = errors.New("activity worker is stopping")

// IsActivityWorkerStopping reports whether an executor context ended for a local worker drain.
func IsActivityWorkerStopping(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), ErrActivityWorkerStopping)
}

// LeavesExternalWorkRunning reports whether an executor context ended for a cause after which
// another run resumes the external work by idempotency key: a local worker drain, or a later
// dispatch of the Workflow fencing this run (workflowdomain.ErrDispatchFenced). An executor
// must not abort the external work for either; only a cancellation the user asked for does.
func LeavesExternalWorkRunning(ctx context.Context) bool {
	cause := context.Cause(ctx)
	return errors.Is(cause, ErrActivityWorkerStopping) || errors.Is(cause, workflowdomain.ErrDispatchFenced)
}
