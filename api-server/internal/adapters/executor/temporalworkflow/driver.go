package temporalworkflow

import (
	"errors"
	"sort"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
)

const (
	// DriverWorkflowName is the driver of decision 072. A change to its decisions registers a new
	// name: the dispatcher terminates runs of an earlier driver and resumes their Workflows, so no
	// version branches are kept.
	DriverWorkflowName = "swallow.workflow.v2"
	// WakeSignal tells a run that a command is waiting in the store.
	WakeSignal = "wake"

	ActivityAdvance        = "swallow.workflow.advance"
	ActivityExecuteTask    = "swallow.workflow.execute-task"
	ActivityRecordFailure  = "swallow.workflow.record-failure"
	ActivityBeginCancel    = "swallow.workflow.begin-cancel"
	ActivityCompensate     = "swallow.workflow.compensate"
	ActivityFinishCanceled = "swallow.workflow.finish-canceled"
)

// driverRounds bounds a run's history: after this many rounds with nothing executing, the run
// continues as new. The store holds the state, so this is always safe.
const driverRounds = 500

// resourcePoll is how often a run waiting for another Workflow's resources asks again.
const resourcePoll = 5 * time.Second

// RunnerRoute says where a Runner's Tasks execute: the task queue of the process that registers
// the Runner, and whether its outcome survives an activity retry (SingleAttempt is false for a
// Runner that observes before acting, true for one whose outcome cannot be proven, like Ansible).
type RunnerRoute struct {
	TaskQueue     string
	SingleAttempt bool
}

// DriverInput is the only input of a run: which Workflow, which dispatch generation, and where each
// Runner's Tasks go. It carries no plan, Task parameter, or secret.
type DriverInput struct {
	WorkflowID string
	Generation int64
	Routes     map[string]RunnerRoute
}

// TaskInput identifies the attempt an execute-task activity runs.
type TaskInput struct {
	WorkflowID string
	Generation int64
	TaskID     string
	Attempt    int
	Resumed    bool
}

// FailureInput records why an execute-task activity ended without recording an outcome.
type FailureInput struct {
	Task TaskInput
	// OutcomeUnknown is true for a single-attempt Runner: the Task may have had effects, so it
	// becomes requires_attention with executor_outcome_unknown.
	OutcomeUnknown bool
	Code           string
	NonRetryable   bool
}

// GenerationInput names a Workflow at one dispatch generation.
type GenerationInput struct {
	WorkflowID string
	Generation int64
}

// CompensationInput is one cancel compensation of a canceling Workflow.
type CompensationInput struct {
	WorkflowID string
	Generation int64
	TaskID     string
	Attempt    int
	Kind       string
	Run        bool
}

// stateActivityOptions apply to the transitions: each is idempotent and retried until it succeeds,
// so a store outage pauses the run instead of failing it.
var stateActivityOptions = workflow.ActivityOptions{
	StartToCloseTimeout: 30 * time.Second,
	RetryPolicy: &temporal.RetryPolicy{
		InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 10 * time.Second, MaximumAttempts: 0,
	},
}

// taskActivityOptions runs one Task attempt. A Runner that observes before acting is retried
// across worker loss within seven days; a single-attempt Runner runs once and ends with its
// heartbeat when its process stops. WaitForCancellation keeps the run waiting until the Runner
// stopped its external work before compensating and releasing resources.
func taskActivityOptions(route RunnerRoute) workflow.ActivityOptions {
	options := workflow.ActivityOptions{
		TaskQueue:           route.TaskQueue,
		StartToCloseTimeout: 7 * 24 * time.Hour,
		HeartbeatTimeout:    30 * time.Second,
		WaitForCancellation: true,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	}
	if !route.SingleAttempt {
		options.ScheduleToCloseTimeout = 7 * 24 * time.Hour
		options.RetryPolicy = &temporal.RetryPolicy{
			InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 30 * time.Second, MaximumAttempts: 0,
		}
	}
	return options
}

// compensationActivityOptions bounds one compensation so a slow provider cannot hold a canceled
// Workflow open; it runs once and is best-effort.
func compensationActivityOptions(route RunnerRoute) workflow.ActivityOptions {
	return workflow.ActivityOptions{
		TaskQueue:           route.TaskQueue,
		StartToCloseTimeout: 30 * time.Minute,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	}
}

type runningTask struct {
	input  TaskInput
	route  RunnerRoute
	future workflow.Future
	cancel workflow.CancelFunc
}

// DriverWorkflow runs a Workflow from the store (decision 072). Each round it asks the core to
// advance the Workflow and starts the Tasks the core began, each on its Runner's task queue; it
// then waits until an execution ends, a command wakes it, or — while waiting for resources — a
// poll interval passes. A cancel (Temporal's, or a stored command the core reports) stops the
// executions, runs the declared compensations, and records the Workflow canceled. A run of a
// superseded generation ends without side effects.
func DriverWorkflow(ctx workflow.Context, input DriverInput) error {
	stateCtx := workflow.WithActivityOptions(ctx, stateActivityOptions)
	wake := workflow.GetSignalChannel(ctx, WakeSignal)
	running := map[string]*runningTask{}
	idle := 0
	for {
		if ctx.Err() != nil {
			return cancelWorkflow(ctx, input, running)
		}
		ids := sortedKeys(running)
		var result workflowapp.AdvanceResult
		err := workflow.ExecuteActivity(stateCtx, ActivityAdvance, workflowapp.AdvanceRequest{
			WorkflowID: input.WorkflowID, Generation: input.Generation,
			RunID: workflow.GetInfo(ctx).WorkflowExecution.RunID, Running: ids,
		}).Get(ctx, &result)
		if err != nil {
			if ctx.Err() != nil {
				return cancelWorkflow(ctx, input, running)
			}
			return err
		}
		switch result.Action {
		case workflowapp.AdvanceFenced, workflowapp.AdvanceFinished:
			stopAll(ctx, running)
			return nil
		case workflowapp.AdvanceCancel:
			return cancelWorkflow(ctx, input, running)
		}
		for _, start := range result.Start {
			if _, ok := running[start.TaskID]; ok {
				continue
			}
			route := input.Routes[string(start.Runner)]
			taskCtx, cancel := workflow.WithCancel(ctx)
			task := TaskInput{
				WorkflowID: input.WorkflowID, Generation: input.Generation,
				TaskID: start.TaskID, Attempt: start.Attempt, Resumed: start.Resumed,
			}
			running[start.TaskID] = &runningTask{
				input: task, route: route, cancel: cancel,
				future: workflow.ExecuteActivity(workflow.WithActivityOptions(taskCtx, taskActivityOptions(route)), ActivityExecuteTask, task),
			}
		}

		var ended *runningTask
		var endedErr error
		selector := workflow.NewSelector(ctx)
		for _, id := range sortedKeys(running) {
			task := running[id]
			selector.AddFuture(task.future, func(f workflow.Future) {
				ended, endedErr = task, f.Get(ctx, nil)
			})
		}
		selector.AddReceive(wake, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil) })
		// Select does not return on cancellation by itself; a parked run must still see a cancel.
		selector.AddReceive(ctx.Done(), func(workflow.ReceiveChannel, bool) {})
		if result.Action == workflowapp.AdvanceWaitResources || (result.Action == workflowapp.AdvanceRun && len(running) == 0) {
			selector.AddFuture(workflow.NewTimer(ctx, resourcePoll), func(workflow.Future) {})
		}
		selector.Select(ctx)
		if ended != nil {
			delete(running, ended.input.TaskID)
			if endedErr != nil && ctx.Err() == nil && !isFenced(endedErr) {
				code, _, retryable := normalizeActivityError(endedErr)
				_ = workflow.ExecuteActivity(stateCtx, ActivityRecordFailure, FailureInput{
					Task: ended.input, OutcomeUnknown: ended.route.SingleAttempt, Code: code, NonRetryable: !retryable,
				}).Get(ctx, nil)
			}
		}
		if len(running) == 0 {
			idle++
			if idle >= driverRounds {
				return workflow.NewContinueAsNewError(ctx, DriverWorkflowName, input)
			}
		}
	}
}

// cancelWorkflow stops every execution, waits until each stopped, runs the compensations the core
// returns, and records the Workflow canceled. It runs on a disconnected context because the
// run's own context may already be canceled.
func cancelWorkflow(ctx workflow.Context, input DriverInput, running map[string]*runningTask) error {
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	stopAll(disconnected, running)
	stateCtx := workflow.WithActivityOptions(disconnected, stateActivityOptions)
	generation := GenerationInput{WorkflowID: input.WorkflowID, Generation: input.Generation}
	var compensations []workflowapp.Compensation
	if err := workflow.ExecuteActivity(stateCtx, ActivityBeginCancel, generation).Get(disconnected, &compensations); err != nil {
		if isFenced(err) {
			return nil
		}
		return err
	}
	futures := make([]workflow.Future, len(compensations))
	for i, compensation := range compensations {
		route := input.Routes[string(compensation.Runner)]
		futures[i] = workflow.ExecuteActivity(workflow.WithActivityOptions(disconnected, compensationActivityOptions(route)),
			ActivityCompensate, CompensationInput{
				WorkflowID: input.WorkflowID, Generation: input.Generation, TaskID: compensation.TaskID,
				Attempt: compensation.Attempt, Kind: compensation.Kind, Run: compensation.Run,
			})
	}
	for _, future := range futures {
		_ = future.Get(disconnected, nil)
	}
	if err := workflow.ExecuteActivity(stateCtx, ActivityFinishCanceled, generation).Get(disconnected, nil); err != nil && !isFenced(err) {
		return err
	}
	return nil
}

// stopAll cancels every execution and waits until each acknowledged.
func stopAll(ctx workflow.Context, running map[string]*runningTask) {
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	for _, id := range sortedKeys(running) {
		running[id].cancel()
	}
	for _, id := range sortedKeys(running) {
		_ = running[id].future.Get(disconnected, nil)
		delete(running, id)
	}
}

func sortedKeys(running map[string]*runningTask) []string {
	ids := make([]string, 0, len(running))
	for id := range running {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// fencedErrorType marks an activity that stopped because its generation was superseded.
const fencedErrorType = "dispatch_fenced"

func isFenced(err error) bool {
	var appErr *temporal.ApplicationError
	return errors.As(err, &appErr) && appErr.Type() == fencedErrorType
}
