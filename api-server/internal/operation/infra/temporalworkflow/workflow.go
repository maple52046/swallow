// Package temporalworkflow owns deterministic orchestration definitions and typed
// activities. Workflows coordinate only; all I/O is delegated to activities.
package temporalworkflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

const (
	// WorkflowNameV1 is immutable because persisted histories identify it by name.
	WorkflowNameV1  = "swallow.operation.v1"
	TaskQueue       = "swallow-operations"
	RetryStepSignal = "retry-operation-step"

	ActivityAcquireLeases = "swallow.operation.acquire-leases"
	ActivityRenewLeases   = "swallow.operation.renew-leases"
	ActivityReleaseLeases = "swallow.operation.release-leases"
	ActivityUpdateState   = "swallow.operation.update-state"
	ActivityUpdateStep    = "swallow.operation.update-step"
	ActivityExecuteStep   = "swallow.operation.execute-step"
)

// operationWorkflowVersion is the current logic version recorded via workflow.GetVersion.
// Histories written before versioning was introduced replay as workflow.DefaultVersion;
// future incompatible changes must add a branch keyed on a higher value here rather than
// editing existing decision paths, so completed and in-flight histories stay replayable.
const operationWorkflowVersion = 1

// WorkflowInput is a secret-free immutable snapshot. Workflows carry opaque references,
// never credential, cloud-init, or automation secret values.
type WorkflowInput struct {
	OperationID       string
	Kind              operationdomain.WorkflowKind
	PlatformID        string
	SiteID            string
	Definition        string
	DefinitionVersion int
	Steps             []operationdomain.Task
	ResourceKeys      []string
	LeaseDuration     time.Duration
	MaxParallelism    int
}

// RetryStepCommand requests another attempt of one failed, retryable Step.
type RetryStepCommand struct {
	StepID string
}

// StateUpdate is written by an activity so workflows never access Mongo directly.
type StateUpdate struct {
	OperationID string
	Status      operationdomain.WorkflowStatus
	Reason      string
	StartedAt   *time.Time
	FinishedAt  *time.Time
}

// StepUpdate is one projection write.
type StepUpdate struct {
	OperationID string
	Step        operationdomain.Task
}

// LeaseRequest asks the persistence adapter for all resources atomically from the
// workflow's perspective. The repository orders them and releases partial acquisition.
type LeaseRequest struct {
	ResourceKeys []string
	Owner        string
	Duration     time.Duration
}

// LeaseRenewal extends leases while a long provider or Ansible Step is observed.
type LeaseRenewal struct {
	Leases   []operationdomain.ResourceLease
	Duration time.Duration
}

// StepExecutionInput is passed to exactly one typed lifecycle adapter.
type StepExecutionInput struct {
	OperationID   string
	Kind          operationdomain.WorkflowKind
	PlatformID    string
	SiteID        string
	Step          operationdomain.Task
	Leases        []operationdomain.ResourceLease
	LeaseDuration time.Duration
}

// StepExecutionResult is normalized before it reaches workflow history.
type StepExecutionResult struct {
	Status            operationdomain.TaskStatus
	Progress          int
	WaitingReason     string
	Error             *operationdomain.NormalizedError
	ExternalExecution *operationdomain.ExternalExecutionReference
	Artifacts         []operationdomain.ArtifactMetadata
}

// StepLifecycleExecutor implements Start/Observe/Cancel semantics behind one activity.
// Implementations must use Operation, Step, target, and attempt as their idempotency key.
type StepLifecycleExecutor interface {
	Execute(ctx context.Context, input StepExecutionInput) StepExecutionResult
}

// StepProjectionObserver materializes public read models from durable Step transitions.
type StepProjectionObserver interface {
	ObserveStep(ctx context.Context, operationID string, step operationdomain.Task) error
}

// Activities contains only narrow repositories and typed executors.
type Activities struct {
	operations operationdomain.WorkflowRepository
	leases     operationdomain.ResourceLeaseRepository
	executors  map[operationdomain.RunnerKind]StepLifecycleExecutor
	observers  []StepProjectionObserver
}

func NewActivities(operations operationdomain.WorkflowRepository, leases operationdomain.ResourceLeaseRepository, executors map[operationdomain.RunnerKind]StepLifecycleExecutor, observers ...StepProjectionObserver) *Activities {
	return &Activities{operations: operations, leases: leases, executors: executors, observers: observers}
}

func (a *Activities) AcquireLeases(ctx context.Context, input LeaseRequest) ([]operationdomain.ResourceLease, error) {
	return a.leases.Acquire(ctx, input.ResourceKeys, input.Owner, time.Now().UTC().Add(input.Duration))
}

func (a *Activities) RenewLeases(ctx context.Context, input LeaseRenewal) error {
	return a.leases.Renew(ctx, input.Leases, time.Now().UTC().Add(input.Duration))
}

func (a *Activities) ReleaseLeases(ctx context.Context, leases []operationdomain.ResourceLease) error {
	return a.leases.Release(ctx, leases)
}

func (a *Activities) UpdateState(ctx context.Context, input StateUpdate) error {
	if err := a.operations.UpdateState(ctx, input.OperationID, input.Status, input.Reason, input.StartedAt, input.FinishedAt); err != nil {
		return err
	}
	message := "Operation status changed to " + string(input.Status) + "."
	// Deterministic ID per operation status so a retried projection activity upserts the
	// same event instead of appending a duplicate. Repeated visits to the same status
	// (for example running between batches) intentionally collapse to one timeline entry.
	return a.operations.AppendEvent(ctx, operationdomain.TimelineEvent{
		ID: input.OperationID + "/state/" + string(input.Status), OperationID: input.OperationID, Type: "operation_status",
		Message: message, Details: map[string]any{"status": string(input.Status), "reason": input.Reason},
		CreatedAt: time.Now().UTC(),
	})
}

func (a *Activities) UpdateStep(ctx context.Context, input StepUpdate) error {
	if err := a.operations.UpdateStep(ctx, input.OperationID, input.Step); err != nil {
		return err
	}
	for _, observer := range a.observers {
		if err := observer.ObserveStep(ctx, input.OperationID, input.Step); err != nil {
			return err
		}
	}
	// Deterministic ID per (step, status, attempt): a step reaches a given status at most
	// once per attempt, so a retried projection activity upserts rather than duplicates.
	eventID := fmt.Sprintf("%s/step/%s/%s/%d", input.OperationID, input.Step.ID, input.Step.Status, input.Step.Attempt)
	return a.operations.AppendEvent(ctx, operationdomain.TimelineEvent{
		ID: eventID, OperationID: input.OperationID, StepID: input.Step.ID,
		Type: "step_status", Message: input.Step.Name + " changed to " + string(input.Step.Status) + ".",
		Details:   map[string]any{"status": string(input.Step.Status), "attempt": input.Step.Attempt},
		CreatedAt: time.Now().UTC(),
	})
}

func (a *Activities) ExecuteStep(ctx context.Context, input StepExecutionInput) (StepExecutionResult, error) {
	for _, lease := range input.Leases {
		if err := a.leases.Validate(ctx, lease); err != nil {
			return StepExecutionResult{}, temporal.NewNonRetryableApplicationError(
				"resource lease fencing token is no longer current", "lease_fenced", err)
		}
	}
	executor := a.executors[input.Step.Executor]
	if executor == nil {
		return StepExecutionResult{Status: operationdomain.TaskFailed, Error: &operationdomain.NormalizedError{
			Code: "executor_unavailable", Message: fmt.Sprintf("No %s executor is configured.", input.Step.Executor), Retryable: false,
		}}, nil
	}
	waiting := input.Step
	waiting.Status = operationdomain.TaskWaitingExternal
	waiting.WaitingReason = "Waiting for " + string(input.Step.Executor) + " execution."
	_ = a.UpdateStep(ctx, StepUpdate{OperationID: input.OperationID, Step: waiting})
	executionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type outcome struct{ result StepExecutionResult }
	done := make(chan outcome, 1)
	go func() { done <- outcome{result: executor.Execute(executionCtx, input)} }()
	duration := input.LeaseDuration
	if duration <= 0 {
		duration = 90 * time.Second
	}
	renewInterval := duration / 4
	if renewInterval <= 0 || renewInterval > 15*time.Second {
		renewInterval = 15 * time.Second
	}
	activity.RecordHeartbeat(ctx, "Step execution started")
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()
	var result StepExecutionResult
	for {
		select {
		case completed := <-done:
			result = completed.result
			goto finished
		case <-ticker.C:
			if err := a.leases.Renew(ctx, input.Leases, time.Now().UTC().Add(duration)); err != nil {
				cancel()
				return StepExecutionResult{}, temporal.NewNonRetryableApplicationError("resource lease could not be renewed", "lease_fenced", err)
			}
			activity.RecordHeartbeat(ctx, "resource leases renewed")
		case <-ctx.Done():
			cancel()
			return StepExecutionResult{}, ctx.Err()
		}
	}
finished:
	if result.Status == "" {
		result.Status = operationdomain.TaskSucceeded
	}
	return result, nil
}

// NoopExecutor is the side-effect-free foundation workflow executor.
type NoopExecutor struct{}

func (NoopExecutor) Execute(_ context.Context, _ StepExecutionInput) StepExecutionResult {
	return StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
}

// OperationWorkflowV1 executes dependency layers in stable input order and pauses on a
// retryable failure. Failed-Step retry increments the attempt in the same Operation.
func OperationWorkflowV1(ctx workflow.Context, input WorkflowInput) (returnErr error) {
	if input.DefinitionVersion != 1 {
		return temporal.NewNonRetryableApplicationError("unsupported workflow definition version", "unsupported_definition", nil)
	}
	// Versioning checkpoint. Discarded today because there is only one logic version;
	// it exists so a future incompatible change can branch here without breaking the
	// replay of histories recorded before that change.
	_ = workflow.GetVersion(ctx, "operation-workflow", workflow.DefaultVersion, operationWorkflowVersion)
	if input.LeaseDuration <= 0 {
		input.LeaseDuration = 90 * time.Second
	}
	if input.MaxParallelism <= 0 {
		input.MaxParallelism = 4
	}
	projectionOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 10 * time.Second, MaximumAttempts: 0},
	}
	projectionCtx := workflow.WithActivityOptions(ctx, projectionOptions)
	now := workflow.Now(ctx).UTC()
	_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
		OperationID: input.OperationID, Status: operationdomain.WorkflowWaitingDependency,
		Reason: "Waiting for resource leases.", StartedAt: &now,
	}).Get(projectionCtx, nil)

	owner := workflow.GetInfo(ctx).WorkflowExecution.ID
	var leases []operationdomain.ResourceLease
	for {
		leaseCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		err := workflow.ExecuteActivity(leaseCtx, ActivityAcquireLeases, LeaseRequest{
			ResourceKeys: input.ResourceKeys, Owner: owner, Duration: input.LeaseDuration,
		}).Get(leaseCtx, &leases)
		if err == nil {
			break
		}
		if sleepErr := workflow.Sleep(ctx, 5*time.Second); sleepErr != nil {
			return finishCanceled(ctx, input.OperationID, input.Steps)
		}
	}
	defer func() {
		disconnected, _ := workflow.NewDisconnectedContext(ctx)
		disconnected = workflow.WithActivityOptions(disconnected, projectionOptions)
		_ = workflow.ExecuteActivity(disconnected, ActivityReleaseLeases, leases).Get(disconnected, nil)
	}()

	_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
		OperationID: input.OperationID, Status: operationdomain.WorkflowRunning,
		StartedAt: &now,
	}).Get(projectionCtx, nil)

	// Jobs path (ADR 017): when Tasks are grouped into Jobs, run each Job as a Temporal
	// child workflow in cross-Job dependency order. The proven flat path below runs
	// unchanged when no Task declares a Job, so existing histories replay identically.
	if hasJobs(input.Steps) {
		return runJobbedWorkflow(ctx, input, leases, projectionCtx)
	}

	steps := append([]operationdomain.Task(nil), input.Steps...)
	retryChannel := workflow.GetSignalChannel(ctx, RetryStepSignal)
	for {
		if ctx.Err() != nil {
			return finishCanceled(ctx, input.OperationID, steps)
		}
		ready := readySteps(steps)
		if len(ready) == 0 {
			if allSucceeded(steps) {
				finished := workflow.Now(ctx).UTC()
				return workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
					OperationID: input.OperationID, Status: operationdomain.WorkflowSucceeded, FinishedAt: &finished,
				}).Get(projectionCtx, nil)
			}
			failedIndex := retryableFailure(steps)
			if failedIndex < 0 {
				finished := workflow.Now(ctx).UTC()
				status := operationdomain.WorkflowFailed
				if anySucceeded(steps) {
					status = operationdomain.WorkflowPartiallySucceeded
				}
				return workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
					OperationID: input.OperationID, Status: status, Reason: firstFailure(steps), FinishedAt: &finished,
				}).Get(projectionCtx, nil)
			}
			_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
				OperationID: input.OperationID, Status: operationdomain.WorkflowRequiresAttention,
				Reason: "A failed Step requires operator attention.",
			}).Get(projectionCtx, nil)
			command, err := awaitRetryStep(ctx, retryChannel, leases, input.LeaseDuration)
			if err != nil {
				return finishCanceled(ctx, input.OperationID, steps)
			}
			for index := range steps {
				if steps[index].ID == command.StepID && (steps[index].Status == operationdomain.TaskFailed || steps[index].Status == operationdomain.TaskRequiresAttention) && steps[index].Error != nil && steps[index].Error.Retryable {
					steps[index].Attempt++
					steps[index].Status = operationdomain.TaskPending
					steps[index].Error = nil
					steps[index].FinishedAt = nil
					steps[index].Progress = 0
					_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateStep, StepUpdate{OperationID: input.OperationID, Step: steps[index]}).Get(projectionCtx, nil)
				}
			}
			continue
		}

		_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
			OperationID: input.OperationID, Status: operationdomain.WorkflowWaitingExternal,
			Reason: "Waiting for an external executor or provider.",
		}).Get(projectionCtx, nil)
		for start := 0; start < len(ready); start += input.MaxParallelism {
			end := start + input.MaxParallelism
			if end > len(ready) {
				end = len(ready)
			}
			batch := ready[start:end]
			futures := make([]workflow.Future, len(batch))
			for pos, index := range batch {
				started := workflow.Now(ctx).UTC()
				steps[index].Status = operationdomain.TaskRunning
				steps[index].StartedAt = &started
				steps[index].WaitingReason = ""
				_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateStep, StepUpdate{OperationID: input.OperationID, Step: steps[index]}).Get(projectionCtx, nil)
				executionCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
					StartToCloseTimeout: 7 * 24 * time.Hour, HeartbeatTimeout: 30 * time.Second,
					RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
				})
				futures[pos] = workflow.ExecuteActivity(executionCtx, ActivityExecuteStep, StepExecutionInput{
					OperationID: input.OperationID, Kind: input.Kind, PlatformID: input.PlatformID, SiteID: input.SiteID, Step: steps[index], Leases: leases, LeaseDuration: input.LeaseDuration,
				})
			}
			for pos, index := range batch {
				var result StepExecutionResult
				err := futures[pos].Get(ctx, &result)
				finished := workflow.Now(ctx).UTC()
				steps[index].FinishedAt = &finished
				if err != nil {
					if temporal.IsCanceledError(err) || ctx.Err() != nil {
						steps[index].Status = operationdomain.TaskCanceled
					} else {
						// A non-retryable activity failure (for example a lost lease
						// fencing token) is terminal: marking it retryable would let the
						// operator retry into the same guaranteed failure and never
						// converge. Only genuinely uncertain infrastructure failures pause
						// for operator attention.
						code, message, retryable := normalizeActivityError(err)
						status := operationdomain.TaskRequiresAttention
						if !retryable {
							status = operationdomain.TaskFailed
						}
						steps[index].Status = status
						steps[index].Error = &operationdomain.NormalizedError{Code: code, Message: message, Retryable: retryable}
					}
				} else {
					steps[index].Status = result.Status
					steps[index].Progress = result.Progress
					steps[index].WaitingReason = result.WaitingReason
					steps[index].Error = result.Error
					steps[index].ExternalExecution = result.ExternalExecution
					steps[index].Artifacts = result.Artifacts
				}
				_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateStep, StepUpdate{OperationID: input.OperationID, Step: steps[index]}).Get(projectionCtx, nil)
			}
			_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
				OperationID: input.OperationID, Status: operationdomain.WorkflowRunning,
			}).Get(projectionCtx, nil)
		}
	}
}

// JobWorkflowName registers the reusable Job child workflow. It is a new name so existing
// OperationWorkflowV1 histories (which never call a child workflow) stay replay-safe.
const JobWorkflowName = "swallow.job.v1"

// JobWorkflowInput runs one Job (a reusable group of Tasks) inside a parent Operation. The
// parent owns and renews the resource leases; the child validates them before each Task, so
// only one workflow tree ever mutates a Server or Platform at a time.
type JobWorkflowInput struct {
	OperationID    string
	Kind           operationdomain.WorkflowKind
	PlatformID     string
	SiteID         string
	JobName        string
	Tasks          []operationdomain.Task
	Leases         []operationdomain.ResourceLease
	LeaseDuration  time.Duration
	MaxParallelism int
}

// JobResult is the normalized outcome a Job child workflow returns to its parent. Failure is
// returned as a value (not an error) so the parent can decide whether to pause for retry.
type JobResult struct {
	Succeeded bool
	Retryable bool
	Reason    string
}

// hasJobs reports whether any Task is assigned to a Job, which selects the child-workflow
// orchestration path.
func hasJobs(tasks []operationdomain.Task) bool {
	for _, task := range tasks {
		if task.Job != "" {
			return true
		}
	}
	return false
}

// runJobbedWorkflow orchestrates a Workflow whose Tasks are grouped into Jobs. Each Job runs
// as a child workflow in cross-Job dependency order; a retryable Job failure pauses the
// Operation and, on an operator retry, re-runs the (idempotent) Job.
func runJobbedWorkflow(ctx workflow.Context, input WorkflowInput, leases []operationdomain.ResourceLease, projectionCtx workflow.Context) error {
	// Partition Tasks into Jobs, preserving first-appearance order for determinism.
	order := []string{}
	byJob := map[string][]operationdomain.Task{}
	taskJob := map[string]string{}
	for _, task := range input.Steps {
		if _, ok := byJob[task.Job]; !ok {
			order = append(order, task.Job)
		}
		byJob[task.Job] = append(byJob[task.Job], task)
		taskJob[task.ID] = task.Job
	}
	// Derive Job-level dependencies from cross-Job Task dependencies, appended in a
	// deterministic order (no map iteration drives control flow).
	jobDeps := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, task := range input.Steps {
		for _, dep := range task.DependsOn {
			depJob := taskJob[dep]
			if depJob == "" || depJob == task.Job {
				continue
			}
			if seen[task.Job] == nil {
				seen[task.Job] = map[string]bool{}
			}
			if !seen[task.Job][depJob] {
				seen[task.Job][depJob] = true
				jobDeps[task.Job] = append(jobDeps[task.Job], depJob)
			}
		}
	}

	retryChannel := workflow.GetSignalChannel(ctx, RetryStepSignal)
	done := map[string]bool{}
	anySucceeded := false
	remaining := append([]string(nil), order...)
	for len(remaining) > 0 {
		progressed := false
		for i := 0; i < len(remaining); i++ {
			job := remaining[i]
			ready := true
			for _, dep := range jobDeps[job] {
				if !done[dep] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			result := runJobChild(ctx, input, leases, projectionCtx, retryChannel, job, byJob[job])
			if result.Succeeded {
				anySucceeded = true
				done[job] = true
				remaining = append(remaining[:i], remaining[i+1:]...)
				progressed = true
				break
			}
			if ctx.Err() != nil {
				return finishCanceled(ctx, input.OperationID, input.Steps)
			}
			finished := workflow.Now(ctx).UTC()
			status := operationdomain.WorkflowFailed
			if anySucceeded {
				status = operationdomain.WorkflowPartiallySucceeded
			}
			return workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
				OperationID: input.OperationID, Status: status, Reason: result.Reason, FinishedAt: &finished,
			}).Get(projectionCtx, nil)
		}
		if !progressed {
			finished := workflow.Now(ctx).UTC()
			return workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
				OperationID: input.OperationID, Status: operationdomain.WorkflowFailed,
				Reason: "Job dependencies could not be satisfied.", FinishedAt: &finished,
			}).Get(projectionCtx, nil)
		}
	}
	finished := workflow.Now(ctx).UTC()
	return workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
		OperationID: input.OperationID, Status: operationdomain.WorkflowSucceeded, FinishedAt: &finished,
	}).Get(projectionCtx, nil)
}

// runJobChild runs one Job as a child workflow, pausing the Operation for operator retry on
// a retryable failure and then re-running the idempotent Job.
func runJobChild(ctx workflow.Context, input WorkflowInput, leases []operationdomain.ResourceLease, projectionCtx workflow.Context, retryChannel workflow.ReceiveChannel, job string, tasks []operationdomain.Task) JobResult {
	attempt := 0
	for {
		attempt++
		_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
			OperationID: input.OperationID, Status: operationdomain.WorkflowRunning,
			Reason: "Running job " + job + ".",
		}).Get(projectionCtx, nil)
		childID := fmt.Sprintf("%s/job/%s/%d", workflow.GetInfo(ctx).WorkflowExecution.ID, job, attempt)
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{WorkflowID: childID})
		var result JobResult
		err := workflow.ExecuteChildWorkflow(childCtx, JobWorkflowName, JobWorkflowInput{
			OperationID: input.OperationID, Kind: input.Kind, PlatformID: input.PlatformID, SiteID: input.SiteID,
			JobName: job, Tasks: tasks, Leases: leases, LeaseDuration: input.LeaseDuration, MaxParallelism: input.MaxParallelism,
		}).Get(ctx, &result)
		if err != nil {
			if temporal.IsCanceledError(err) || ctx.Err() != nil {
				return JobResult{Succeeded: false, Retryable: false, Reason: "canceled"}
			}
			_, message, retryable := normalizeActivityError(err)
			result = JobResult{Succeeded: false, Retryable: retryable, Reason: message}
		}
		if result.Succeeded || !result.Retryable {
			return result
		}
		_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
			OperationID: input.OperationID, Status: operationdomain.WorkflowRequiresAttention,
			Reason: "Job " + job + " requires operator attention.",
		}).Get(projectionCtx, nil)
		if _, waitErr := awaitRetryStep(ctx, retryChannel, leases, input.LeaseDuration); waitErr != nil {
			return JobResult{Succeeded: false, Retryable: false, Reason: "canceled"}
		}
		// Loop: re-run the Job as a fresh child. Idempotent ("ensure") Tasks skip work that
		// already converged, so a retry only redoes what still needs doing.
	}
}

// JobWorkflowV1 executes one Job's Tasks in intra-Job dependency order, updating each Task's
// projection. It reports failure as a JobResult value so the parent Operation owns retry and
// final status. Cross-Job dependencies are treated as already satisfied by parent ordering.
func JobWorkflowV1(ctx workflow.Context, input JobWorkflowInput) (JobResult, error) {
	if input.LeaseDuration <= 0 {
		input.LeaseDuration = 90 * time.Second
	}
	if input.MaxParallelism <= 0 {
		input.MaxParallelism = 4
	}
	projectionOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 10 * time.Second, MaximumAttempts: 0},
	}
	projectionCtx := workflow.WithActivityOptions(ctx, projectionOptions)
	tasks := append([]operationdomain.Task(nil), input.Tasks...)
	known := map[string]bool{}
	for _, task := range tasks {
		known[task.ID] = true
	}
	for {
		if ctx.Err() != nil {
			return JobResult{Succeeded: false, Retryable: false, Reason: "canceled"}, nil
		}
		ready := readyJobTasks(tasks, known)
		if len(ready) == 0 {
			if allSucceeded(tasks) {
				return JobResult{Succeeded: true}, nil
			}
			for _, task := range tasks {
				if task.Error != nil {
					return JobResult{Succeeded: false, Retryable: task.Error.Retryable, Reason: task.Error.Message}, nil
				}
			}
			return JobResult{Succeeded: false, Retryable: false, Reason: "job did not converge"}, nil
		}
		for start := 0; start < len(ready); start += input.MaxParallelism {
			end := start + input.MaxParallelism
			if end > len(ready) {
				end = len(ready)
			}
			batch := ready[start:end]
			futures := make([]workflow.Future, len(batch))
			for pos, index := range batch {
				started := workflow.Now(ctx).UTC()
				tasks[index].Status = operationdomain.TaskRunning
				tasks[index].StartedAt = &started
				tasks[index].WaitingReason = ""
				_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateStep, StepUpdate{OperationID: input.OperationID, Step: tasks[index]}).Get(projectionCtx, nil)
				executionCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
					StartToCloseTimeout: 7 * 24 * time.Hour, HeartbeatTimeout: 30 * time.Second,
					RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
				})
				futures[pos] = workflow.ExecuteActivity(executionCtx, ActivityExecuteStep, StepExecutionInput{
					OperationID: input.OperationID, Kind: input.Kind, PlatformID: input.PlatformID, SiteID: input.SiteID, Step: tasks[index], Leases: input.Leases, LeaseDuration: input.LeaseDuration,
				})
			}
			for pos, index := range batch {
				var result StepExecutionResult
				err := futures[pos].Get(ctx, &result)
				finished := workflow.Now(ctx).UTC()
				tasks[index].FinishedAt = &finished
				if err != nil {
					if temporal.IsCanceledError(err) || ctx.Err() != nil {
						tasks[index].Status = operationdomain.TaskCanceled
					} else {
						code, message, retryable := normalizeActivityError(err)
						status := operationdomain.TaskRequiresAttention
						if !retryable {
							status = operationdomain.TaskFailed
						}
						tasks[index].Status = status
						tasks[index].Error = &operationdomain.NormalizedError{Code: code, Message: message, Retryable: retryable}
					}
				} else {
					tasks[index].Status = result.Status
					tasks[index].Progress = result.Progress
					tasks[index].WaitingReason = result.WaitingReason
					tasks[index].Error = result.Error
					tasks[index].ExternalExecution = result.ExternalExecution
					tasks[index].Artifacts = result.Artifacts
				}
				_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateStep, StepUpdate{OperationID: input.OperationID, Step: tasks[index]}).Get(projectionCtx, nil)
			}
		}
	}
}

// readyJobTasks returns runnable Task indices within one Job. A dependency on a Task outside
// this Job's set is treated as satisfied, because the parent runs dependency Jobs first.
func readyJobTasks(tasks []operationdomain.Task, known map[string]bool) []int {
	succeeded := map[string]bool{}
	for _, task := range tasks {
		if task.Status == operationdomain.TaskSucceeded || task.Status == operationdomain.TaskSkipped {
			succeeded[task.ID] = true
		}
	}
	ready := []int{}
	for index, task := range tasks {
		if task.Status != operationdomain.TaskPending && task.Status != operationdomain.TaskWaitingDependency {
			continue
		}
		all := true
		for _, dep := range task.DependsOn {
			if known[dep] && !succeeded[dep] {
				all = false
				break
			}
		}
		if all {
			ready = append(ready, index)
		}
	}
	sort.Ints(ready)
	return ready
}

func readySteps(steps []operationdomain.Task) []int {
	succeeded := map[string]bool{}
	for _, step := range steps {
		if step.Status == operationdomain.TaskSucceeded || step.Status == operationdomain.TaskSkipped {
			succeeded[step.ID] = true
		}
	}
	ready := []int{}
	for index, step := range steps {
		if step.Status != operationdomain.TaskPending && step.Status != operationdomain.TaskWaitingDependency {
			continue
		}
		all := true
		for _, dependency := range step.DependsOn {
			all = all && succeeded[dependency]
		}
		if all {
			ready = append(ready, index)
		}
	}
	sort.Ints(ready)
	return ready
}

func allSucceeded(steps []operationdomain.Task) bool {
	for _, step := range steps {
		if step.Status != operationdomain.TaskSucceeded && step.Status != operationdomain.TaskSkipped {
			return false
		}
	}
	return true
}

func anySucceeded(steps []operationdomain.Task) bool {
	for _, step := range steps {
		if step.Status == operationdomain.TaskSucceeded {
			return true
		}
	}
	return false
}

func retryableFailure(steps []operationdomain.Task) int {
	for index, step := range steps {
		if (step.Status == operationdomain.TaskFailed || step.Status == operationdomain.TaskRequiresAttention) && step.Error != nil && step.Error.Retryable {
			return index
		}
	}
	return -1
}

func firstFailure(steps []operationdomain.Task) string {
	for _, step := range steps {
		if step.Error != nil {
			return step.Error.Message
		}
	}
	return "An Operation Step failed."
}

// normalizeActivityError maps a raw Temporal activity error onto a normalized code,
// a bounded operator-facing message, and a retryability decision.
//
// It deliberately does not propagate the raw error text into workflow history: an
// activity error can carry provider dumps, SSH details, or file paths, and Temporal
// persists activity inputs and results. Executor-produced results already carry a
// normalized Error; this helper only covers Temporal-level failures (heartbeat
// timeout, non-retryable application errors). A non-retryable application error such as
// lease_fenced is reported as terminal so the workflow does not offer an unsafe retry.
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
	switch code {
	case "lease_fenced":
		message = "A resource lease was lost before the step could run safely."
	case "executor_unavailable":
		message = "No executor is configured for this step."
	default:
		message = "The step activity failed before returning a normalized result."
	}
	return code, message, retryable
}

func finishCanceled(ctx workflow.Context, operationID string, steps []operationdomain.Task) error {
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	disconnected = workflow.WithActivityOptions(disconnected, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, MaximumAttempts: 0},
	})
	finished := workflow.Now(disconnected).UTC()
	_ = workflow.ExecuteActivity(disconnected, ActivityUpdateState, StateUpdate{
		OperationID: operationID, Status: operationdomain.WorkflowCanceling,
		Reason: "Canceling active work.",
	}).Get(disconnected, nil)
	// Reflect every non-terminal step as canceled, not just the ones that never
	// started. A step left running or waiting-external in the projection after the
	// workflow has stopped would misreport the Operation as still working.
	for index := range steps {
		switch steps[index].Status {
		case operationdomain.TaskPending, operationdomain.TaskWaitingDependency,
			operationdomain.TaskWaitingExternal, operationdomain.TaskRunning:
			steps[index].Status = operationdomain.TaskCanceled
			steps[index].FinishedAt = &finished
			_ = workflow.ExecuteActivity(disconnected, ActivityUpdateStep, StepUpdate{OperationID: operationID, Step: steps[index]}).Get(disconnected, nil)
		}
	}
	_ = workflow.ExecuteActivity(disconnected, ActivityUpdateState, StateUpdate{
		OperationID: operationID, Status: operationdomain.WorkflowCanceled,
		Reason: "Canceled by operator.", FinishedAt: &finished,
	}).Get(disconnected, nil)
	return nil
}

// Controller sends controls to an existing Temporal workflow.
type Controller struct{ client client.Client }

func NewController(temporalClient client.Client) *Controller {
	return &Controller{client: temporalClient}
}

func (c *Controller) Cancel(ctx context.Context, operation *operationdomain.Workflow) error {
	return c.client.CancelWorkflow(ctx, operation.Temporal.WorkflowID, operation.Temporal.RunID)
}

func (c *Controller) RetryStep(ctx context.Context, operation *operationdomain.Workflow, stepID string) error {
	return c.client.SignalWorkflow(ctx, operation.Temporal.WorkflowID, operation.Temporal.RunID, RetryStepSignal, RetryStepCommand{StepID: stepID})
}

// Starter closes the Mongo-persisted/Temporal-start gap with a stable Workflow ID.
//
// It is safe to run in more than one process at once (API and worker): the stable
// Workflow ID plus WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE make a duplicate start a
// no-op that is reconciled by marking the persisted record started. leaseDuration and
// maxParallelism come from configuration so operators can tune them without a code
// change; both fall back to safe defaults.
type Starter struct {
	client         client.Client
	operations     operationdomain.WorkflowRepository
	taskQueue      string
	interval       time.Duration
	leaseDuration  time.Duration
	maxParallelism int
}

func NewStarter(temporalClient client.Client, operations operationdomain.WorkflowRepository, taskQueue string, interval, leaseDuration time.Duration, maxParallelism int) *Starter {
	if taskQueue == "" {
		taskQueue = TaskQueue
	}
	if interval <= 0 {
		interval = time.Second
	}
	if leaseDuration <= 0 {
		leaseDuration = 90 * time.Second
	}
	if maxParallelism <= 0 {
		maxParallelism = 4
	}
	return &Starter{
		client: temporalClient, operations: operations, taskQueue: taskQueue,
		interval: interval, leaseDuration: leaseDuration, maxParallelism: maxParallelism,
	}
}

func (s *Starter) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		s.StartPending(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Starter) StartPending(ctx context.Context) {
	operations, err := s.operations.ListPendingStart(ctx, 100)
	if err != nil {
		return
	}
	for _, operation := range operations {
		input := WorkflowInput{
			OperationID: operation.ID, Kind: operation.Kind, PlatformID: operation.PlatformID, SiteID: operation.SiteID, Definition: operation.Definition,
			DefinitionVersion: operation.DefinitionVersion, Steps: operation.Steps,
			ResourceKeys: resourceKeys(operation.TargetResources), LeaseDuration: s.leaseDuration,
			MaxParallelism: s.maxParallelism,
		}
		run, startErr := s.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID: operation.Temporal.WorkflowID, TaskQueue: s.taskQueue,
			WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		}, WorkflowNameV1, input)
		if startErr != nil {
			var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
			if !errors.As(startErr, &alreadyStarted) {
				continue
			}
			run = s.client.GetWorkflow(ctx, operation.Temporal.WorkflowID, "")
		}
		_ = s.operations.MarkWorkflowStarted(ctx, operation.ID, run.GetRunID())
	}
}

func resourceKeys(resources []operationdomain.ResourceReference) []string {
	keys := make([]string, 0, len(resources))
	for _, resource := range resources {
		keys = append(keys, resource.Kind+":"+resource.ID)
	}
	sort.Strings(keys)
	return keys
}

var _ operationapp.WorkflowController = (*Controller)(nil)

// awaitRetryStep keeps resource leases fenced while an Operation requires operator input.
func awaitRetryStep(ctx workflow.Context, retryChannel workflow.ReceiveChannel, leases []operationdomain.ResourceLease, leaseDuration time.Duration) (RetryStepCommand, error) {
	if leaseDuration <= 0 {
		leaseDuration = 90 * time.Second
	}
	for {
		var command RetryStepCommand
		received := false
		selector := workflow.NewSelector(ctx)
		selector.AddReceive(retryChannel, func(channel workflow.ReceiveChannel, _ bool) {
			channel.Receive(ctx, &command)
			received = true
		})
		selector.AddFuture(workflow.NewTimer(ctx, leaseDuration/3), func(future workflow.Future) {
			_ = future.Get(ctx, nil)
		})
		selector.Select(ctx)
		if ctx.Err() != nil {
			return RetryStepCommand{}, ctx.Err()
		}
		if received {
			return command, nil
		}
		renewCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				InitialInterval: time.Second, MaximumInterval: 10 * time.Second, MaximumAttempts: 3,
			},
		})
		if err := workflow.ExecuteActivity(renewCtx, ActivityRenewLeases, LeaseRenewal{
			Leases: leases, Duration: leaseDuration,
		}).Get(renewCtx, nil); err != nil {
			return RetryStepCommand{}, err
		}
	}
}
