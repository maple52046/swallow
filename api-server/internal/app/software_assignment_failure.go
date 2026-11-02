package app

import (
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// softwareStepTerminalFailure reports whether the status of an install's install or record Task
// means the install did not complete, so the assignment should be marked failed. Canceled is
// included because a canceled install left nothing recorded, and skipped because the Task never ran
// after an earlier Task failed. A record Task that fails after its software was installed is still
// a failed install: reinstalling converges a host that already has the software.
func softwareStepTerminalFailure(status workflowdomain.TaskStatus) bool {
	switch status {
	case workflowdomain.TaskFailed, workflowdomain.TaskRequiresAttention, workflowdomain.TaskCanceled, workflowdomain.TaskSkipped:
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
