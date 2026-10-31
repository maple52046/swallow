package temporalworkflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// DriverActivities are the state transitions a run calls. Each is a thin call into the core
// engine, the only writer of execution state.
type DriverActivities struct {
	engine *workflowapp.Engine
}

// NewDriverActivities returns the transition activities.
func NewDriverActivities(engine *workflowapp.Engine) *DriverActivities {
	return &DriverActivities{engine: engine}
}

// Advance implements ActivityAdvance.
func (a *DriverActivities) Advance(ctx context.Context, request workflowapp.AdvanceRequest) (workflowapp.AdvanceResult, error) {
	return a.engine.Advance(ctx, request)
}

// RecordFailure implements ActivityRecordFailure: an execution that ended without recording an
// outcome is recorded requires_attention (retryable) or failed (not retryable). A Task that is no
// longer running at the attempt, or a superseded generation, is left as it is.
func (a *DriverActivities) RecordFailure(ctx context.Context, input FailureInput) error {
	outcome := workflowapp.Outcome{Status: workflowdomain.TaskRequiresAttention, Error: &workflowdomain.NormalizedError{
		Code: input.Code, Message: "The step activity failed before returning a normalized result.", Retryable: true,
	}}
	switch {
	case input.OutcomeUnknown:
		outcome.Error = &workflowdomain.NormalizedError{
			Code:      "executor_outcome_unknown",
			Message:   "The executor stopped before recording the Task's outcome; retry the Task to run it again.",
			Retryable: true,
		}
	case input.NonRetryable:
		outcome.Status = workflowdomain.TaskFailed
		outcome.Error.Retryable = false
		if input.Code == "executor_unavailable" {
			outcome.Error.Message = "No executor is configured for this step."
		}
	}
	err := a.engine.Record(ctx, taskRef(input.Task), outcome)
	if errors.Is(err, workflowdomain.ErrTaskNotRunning) || errors.Is(err, workflowdomain.ErrDispatchFenced) {
		return nil
	}
	return err
}

// BeginCancel implements ActivityBeginCancel.
func (a *DriverActivities) BeginCancel(ctx context.Context, input GenerationInput) ([]workflowapp.Compensation, error) {
	compensations, err := a.engine.BeginCancel(ctx, input.WorkflowID, input.Generation)
	return compensations, fenced(err)
}

// FinishCanceled implements ActivityFinishCanceled.
func (a *DriverActivities) FinishCanceled(ctx context.Context, input GenerationInput) error {
	return fenced(a.engine.FinishCanceled(ctx, input.WorkflowID, input.Generation))
}

// CompensationExecutor runs one registered cancel compensation for a Task and returns the timeline
// event that describes its outcome. It must be best-effort: a failure is described in the event,
// never returned, so the cancel completes.
type CompensationExecutor interface {
	Compensate(ctx context.Context, input StepExecutionInput) (eventType, message string)
}

// TaskActivities execute Task attempts with a process's Runners. The worker registers it with the
// provider and internal Runners; the Ansible executor with the Ansible Runner.
type TaskActivities struct {
	engine        *workflowapp.Engine
	runners       map[workflowdomain.RunnerKind]StepLifecycleExecutor
	compensations map[string]CompensationExecutor
	fenceInterval time.Duration
}

// heartbeatInterval is how often a running Task's activity heartbeats. A cancel reaches the
// activity only in a heartbeat response, and the SDK sends at most one heartbeat per 80% of the
// heartbeat timeout, so taskHeartbeatTimeout bounds how late a cancel arrives.
const heartbeatInterval = 3 * time.Second

// runnerStopTimeout bounds how long a canceled or superseded activity waits for its Runner to
// stop. It exceeds the Ansible Runner's own stop grace, after which ansible-runner is killed.
const runnerStopTimeout = 45 * time.Second

// NewTaskActivities returns the Task activities for runners and compensations.
func NewTaskActivities(engine *workflowapp.Engine, runners map[workflowdomain.RunnerKind]StepLifecycleExecutor,
	compensations map[string]CompensationExecutor) *TaskActivities {
	return &TaskActivities{engine: engine, runners: runners, compensations: compensations, fenceInterval: 15 * time.Second}
}

// ExecuteTask implements ActivityExecuteTask. It reads the attempt from the store, runs its Runner,
// and records the outcome through the core. Every fence interval it heartbeats and confirms the
// generation; a superseded run cancels the Runner and ends with a non-retryable dispatch_fenced. A
// worker drain leaves the external work running for the next activity attempt; a cancel stops the
// Runner and records nothing, because the run's cancel path records the Task.
func (a *TaskActivities) ExecuteTask(ctx context.Context, input TaskInput) error {
	ref := taskRef(input)
	execution, err := a.engine.TaskForExecution(ctx, ref)
	if errors.Is(err, workflowdomain.ErrTaskNotRunning) {
		return nil
	}
	if err != nil {
		return fenced(err)
	}
	runner := a.runners[execution.Task.Executor]
	if runner == nil {
		return temporal.NewNonRetryableApplicationError("no Runner is registered for the Task", "executor_unavailable", nil)
	}
	step := stepInput(execution, int(activity.GetInfo(ctx).Attempt), input.Resumed || execution.Task.Resumed)
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	done := make(chan StepExecutionResult, 1)
	go func() { done <- runner.Execute(runCtx, step) }()
	activity.RecordHeartbeat(ctx, "Task execution started")
	beat := time.NewTicker(heartbeatInterval)
	defer beat.Stop()
	fence := time.NewTicker(a.fenceInterval)
	defer fence.Stop()
	stopping := activity.GetWorkerStopChannel(ctx)
	for {
		select {
		case result := <-done:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fenced(ignoreNotRunning(a.engine.Complete(ctx, ref, outcomeOf(result))))
		case <-beat.C:
			activity.RecordHeartbeat(ctx, "Task executing")
		case <-fence.C:
			if err := a.engine.CheckGeneration(ctx, input.WorkflowID, input.Generation); errors.Is(err, workflowdomain.ErrDispatchFenced) {
				stopRunner(ctx, cancel, err, done)
				return fenced(err)
			}
		case <-stopping:
			cancel(ErrActivityWorkerStopping)
			return temporal.NewApplicationError("activity worker stopped before external execution completed", "worker_shutdown")
		case <-ctx.Done():
			stopRunner(ctx, cancel, ctx.Err(), done)
			return ctx.Err()
		}
	}
}

// stopRunner cancels the Runner and waits until it returned, up to runnerStopTimeout, so the
// external work it started (an ansible-runner process tree) has stopped before the activity ends
// and the run's cancel path releases the Task's resources. It keeps heartbeating meanwhile so the
// stop is not cut short by the heartbeat timeout.
func stopRunner(ctx context.Context, cancel context.CancelCauseFunc, cause error, done <-chan StepExecutionResult) {
	cancel(cause)
	limit := time.NewTimer(runnerStopTimeout)
	defer limit.Stop()
	beat := time.NewTicker(heartbeatInterval)
	defer beat.Stop()
	for {
		select {
		case <-done:
			return
		case <-limit.C:
			return
		case <-beat.C:
			activity.RecordHeartbeat(ctx, "Stopping the Task")
		}
	}
}

// Compensate implements ActivityCompensate: a registered compensation for an interrupted Task, or
// the Task itself for one that runs on cancel. It is best-effort and never fails the cancel.
func (a *TaskActivities) Compensate(ctx context.Context, input CompensationInput) error {
	execution, err := a.engine.TaskForCompensation(ctx, input.WorkflowID, input.Generation, input.TaskID)
	if err != nil {
		return fenced(err)
	}
	step := stepInput(execution, int(activity.GetInfo(ctx).Attempt), false)
	step.Step.Status = workflowdomain.TaskRunning
	if input.Run {
		if runner := a.runners[execution.Task.Executor]; runner != nil {
			_ = runner.Execute(ctx, step)
		}
		return nil
	}
	compensation := a.compensations[input.Kind]
	if compensation == nil {
		return fmt.Errorf("no compensation %q is registered", input.Kind)
	}
	eventType, message := compensation.Compensate(ctx, step)
	return a.engine.RecordCompensation(ctx, input.WorkflowID, input.TaskID, input.Kind, input.Attempt, eventType, message)
}

func stepInput(execution *workflowapp.TaskExecution, activityAttempt int, resumed bool) StepExecutionInput {
	return StepExecutionInput{
		OperationID: execution.WorkflowID, Kind: execution.Kind, PlatformID: execution.PlatformID,
		SiteID: execution.SiteID, Step: execution.Task, Generation: execution.Generation,
		Resumed: resumed, ActivityAttempt: activityAttempt,
	}
}

func outcomeOf(result StepExecutionResult) workflowapp.Outcome {
	return workflowapp.Outcome{
		Status: result.Status, Progress: result.Progress, WaitingReason: result.WaitingReason, Error: result.Error,
		ExternalExecution: result.ExternalExecution, Artifacts: result.Artifacts, Data: result.Data,
	}
}

func taskRef(input TaskInput) workflowapp.TaskRef {
	return workflowapp.TaskRef{WorkflowID: input.WorkflowID, Generation: input.Generation, TaskID: input.TaskID, Attempt: input.Attempt}
}

func ignoreNotRunning(err error) error {
	if errors.Is(err, workflowdomain.ErrTaskNotRunning) {
		return nil
	}
	return err
}

// fenced turns a superseded-generation error into the non-retryable dispatch_fenced the driver
// recognizes; other errors pass through.
func fenced(err error) error {
	if errors.Is(err, workflowdomain.ErrDispatchFenced) {
		return temporal.NewNonRetryableApplicationError("the run was superseded by a later dispatch", fencedErrorType, err)
	}
	return err
}
