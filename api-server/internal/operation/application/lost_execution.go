package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// CanceledWithoutExecutionReason is the status reason of a Workflow canceled after its Temporal
// execution was already gone, so the cancellation was recorded by swallow rather than by the
// workflow's own cancel path.
const CanceledWithoutExecutionReason = "Canceled by operator; its workflow execution had already ended."

// FinalizeCanceledProjection records a Workflow as canceled directly in the projection, for
// callers that know no workflow execution will ever write the outcome: the execution is gone, or
// a rerun supersedes it. It mirrors the workflow's own cancel projection: the Workflow becomes
// canceled with reason, every non-terminal Task (including requires_attention) becomes canceled,
// and a status event is appended to the timeline. Terminal Tasks keep their outcome.
//
// It writes the repository directly, so Step projection observers do not run; the Server
// deployment result of a canceled provision-os is settled by the deployment repair sweep once
// the Workflow is terminal. Every write is idempotent, so a caller may retry after an error.
func FinalizeCanceledProjection(
	ctx context.Context,
	operations operationdomain.WorkflowRepository,
	operation *operationdomain.Workflow,
	reason string,
	at time.Time,
) error {
	if err := operations.UpdateState(ctx, operation.ID, operationdomain.WorkflowCanceled, reason, nil, &at); err != nil {
		return fmt.Errorf("record workflow %s canceled: %w", operation.ID, err)
	}
	for _, step := range operation.Steps {
		switch step.Status {
		case operationdomain.TaskPending, operationdomain.TaskWaitingDependency,
			operationdomain.TaskWaitingExternal, operationdomain.TaskRunning,
			operationdomain.TaskRequiresAttention:
			step.Status = operationdomain.TaskCanceled
			step.FinishedAt = &at
			if err := operations.UpdateStep(ctx, operation.ID, step); err != nil &&
				!errors.Is(err, operationdomain.ErrStepTransitionIgnored) {
				return fmt.Errorf("record task %s canceled: %w", step.ID, err)
			}
		}
	}
	// The ID matches the event the workflow's own status activity writes, so a status change the
	// workflow already recorded is upserted rather than duplicated.
	return operations.AppendEvent(ctx, operationdomain.TimelineEvent{
		ID: operation.ID + "/state/" + string(operationdomain.WorkflowCanceled), OperationID: operation.ID,
		Type: "operation_status", Message: "Operation status changed to canceled.",
		Details:   map[string]any{"status": string(operationdomain.WorkflowCanceled), "reason": reason},
		CreatedAt: at,
	})
}

// ReleaseExecutionLeases frees the resource leases still held by a Workflow whose execution is
// gone. A parked execution holds its leases for up to a day between renewals, so without this a
// replacement Release or Recover on the same Server would wait for them to expire. Callers must
// first confirm the execution no longer exists (ErrWorkflowExecutionGone); a nil releaser is a
// no-op for compositions that do not wire one.
func ReleaseExecutionLeases(
	ctx context.Context,
	leases operationdomain.ResourceLeaseReleaser,
	operation *operationdomain.Workflow,
) error {
	if leases == nil || operation.Temporal.WorkflowID == "" {
		return nil
	}
	held, err := leases.FindByOwner(ctx, operation.Temporal.WorkflowID)
	if err != nil {
		return fmt.Errorf("find leases of workflow %s: %w", operation.ID, err)
	}
	if err := leases.Release(ctx, held); err != nil {
		return fmt.Errorf("release leases of workflow %s: %w", operation.ID, err)
	}
	return nil
}
