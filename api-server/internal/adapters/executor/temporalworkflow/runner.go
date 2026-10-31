// Package temporalworkflow is the Temporal adapter of the Workflow executor port (decision 072):
// the driver workflow that runs a Workflow from the store, the activities it calls, and the
// executor the dispatcher drives. Workflows coordinate only; all I/O is delegated to activities,
// and every state change goes through the core engine.
package temporalworkflow

import (
	"context"
	"errors"
	"strings"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/temporal"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// TaskQueue is the default task queue of driver runs and of the worker's Runners.
const TaskQueue = "swallow-operations"

// ReturnCanceledDeploymentKind is the provider Runner's step kind that returns a deployment a
// cancel interrupted to Ready (decision 060); the return-canceled-deployment compensation runs it.
const ReturnCanceledDeploymentKind = "return-canceled-deployment"

// StepExecutionInput is what a Runner gets for one Task attempt: the Workflow's identity and the
// Task as stored. It is built inside an activity from the store and never enters workflow history.
type StepExecutionInput struct {
	OperationID string
	Kind        workflowdomain.WorkflowKind
	PlatformID  string
	SiteID      string
	Step        workflowdomain.Task
	// Generation is the dispatch generation the attempt runs under.
	Generation int64
	// Resumed is true when a later dispatch continues an attempt an earlier one started: the
	// Runner observes the external work it already started instead of starting it again.
	Resumed bool
	// ActivityAttempt counts infrastructure retries of the activity; above 1 the Runner observes
	// before acting.
	ActivityAttempt int
}

// StepExecutionResult is a Runner's normalized outcome for one attempt.
type StepExecutionResult struct {
	Status            workflowdomain.TaskStatus
	Progress          int
	WaitingReason     string
	Error             *workflowdomain.NormalizedError
	ExternalExecution *workflowdomain.ExternalExecutionReference
	Artifacts         []workflowdomain.ArtifactMetadata
	// Data is the Runner's result data for the Task's result completions. It never reaches
	// workflow history or the store.
	Data map[string]any
}

// StepLifecycleExecutor is a Runner: it carries out one Task attempt and reports its outcome.
// Implementations must use the Workflow, Task, target, and attempt as their idempotency key, and
// observe before acting when the attempt is resumed or retried.
type StepLifecycleExecutor interface {
	Execute(ctx context.Context, input StepExecutionInput) StepExecutionResult
}

// normalizeActivityError maps a Temporal activity error onto a normalized code and a
// retryability decision without copying its text into history: an activity error can carry
// provider dumps, SSH details, or paths.
func normalizeActivityError(err error) (code string, message string, retryable bool) {
	code, retryable = "activity_failure", true
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		if appErr.Type() != "" {
			code = appErr.Type()
		}
		if appErr.NonRetryable() {
			retryable = false
		}
	}
	if code == "executor_unavailable" {
		return code, "No executor is configured for this step.", retryable
	}
	return code, "The step activity failed before returning a normalized result.", retryable
}

// isWorkflowExecutionGone reports whether a Temporal control failed because the target execution
// is no longer running — completed, terminated, timed out, or absent. The message match is a
// defensive fallback across SDK versions.
func isWorkflowExecutionGone(err error) bool {
	if err == nil {
		return false
	}
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "already completed") ||
		strings.Contains(message, "execution not found") ||
		strings.Contains(message, "workflow not found")
}
