package app

import (
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// softwareStepTerminalFailure reports whether a Task status means the install cannot proceed, so the
// assignment should be marked failed. Canceled is included because a canceled install left nothing
// installed to record.
func softwareStepTerminalFailure(status workflowdomain.TaskStatus) bool {
	switch status {
	case workflowdomain.TaskFailed, workflowdomain.TaskRequiresAttention, workflowdomain.TaskCanceled:
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
