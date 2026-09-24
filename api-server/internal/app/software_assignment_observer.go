package app

import (
	"context"
	"errors"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// softwareAssignmentObserver marks a Software Assignment failed when a software install's ansible
// Task ends in a terminal failure and the record step therefore never runs. The record step owns
// the success path (installed); this observer owns only the failure path, keyed off the assignment
// identity the launcher stamps on the install Task's parameters. It acts only on install-mode
// Tasks: an uninstall failure leaves the assignment in `uninstalling` for the operator to retry.
type softwareAssignmentObserver struct {
	assignments softwaredomain.AssignmentRepository
}

// ObserveStep implements temporalworkflow.StepProjectionObserver. It ignores every Task that is not
// a software install ansible Task in a terminal failure state.
func (o softwareAssignmentObserver) ObserveStep(ctx context.Context, _ string, step operationdomain.Task) error {
	if step.Kind != "ansible-playbook" || step.Parameters == nil {
		return nil
	}
	if mode, _ := step.Parameters["softwareMode"].(string); mode != "install" {
		return nil
	}
	if !softwareStepTerminalFailure(step.Status) {
		return nil
	}
	kind, ok := step.Parameters["softwareKind"].(string)
	if !ok || kind == "" {
		return nil
	}
	for _, serverID := range coerceStringSlice(step.Parameters["serverIds"]) {
		err := o.assignments.SetState(ctx, serverID, softwaredomain.Kind(kind), softwaredomain.StateFailed, "", nil)
		// A not-found assignment is tolerated: the pending record is written just after the
		// Workflow is accepted, and a very early failure can race ahead of it. The failure is
		// still visible on the Workflow itself, so this observer must not fail the activity.
		if err != nil && !errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
			return err
		}
	}
	return nil
}

// softwareStepTerminalFailure reports whether a Task status means the install cannot proceed, so the
// assignment should be marked failed. Canceled is included because a canceled install left nothing
// installed to record.
func softwareStepTerminalFailure(status operationdomain.TaskStatus) bool {
	switch status {
	case operationdomain.TaskFailed, operationdomain.TaskRequiresAttention, operationdomain.TaskCanceled:
		return true
	default:
		return false
	}
}

// coerceStringSlice normalizes a parameter that may decode from Mongo/Temporal as []any or []string
// into a []string.
func coerceStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}
