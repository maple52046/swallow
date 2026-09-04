// Package temporalworkflow owns deterministic orchestration definitions and typed
// activities. Workflows coordinate only; all I/O is delegated to activities.
package temporalworkflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
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
	Kind              operationdomain.OperationKind
	PlatformID        string
	SiteID            string
	Definition        string
	DefinitionVersion int
	Steps             []operationdomain.OperationStep
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
	Status      operationdomain.OrchestrationStatus
	Reason      string
	StartedAt   *time.Time
	FinishedAt  *time.Time
}

// StepUpdate is one projection write.
type StepUpdate struct {
	OperationID string
	Step        operationdomain.OperationStep
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
	Kind          operationdomain.OperationKind
	PlatformID    string
	SiteID        string
	Step          operationdomain.OperationStep
	Leases        []operationdomain.ResourceLease
	LeaseDuration time.Duration
}

// StepExecutionResult is normalized before it reaches workflow history.
type StepExecutionResult struct {
	Status            operationdomain.StepStatus
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
	ObserveStep(ctx context.Context, operationID string, step operationdomain.OperationStep) error
}

// Activities contains only narrow repositories and typed executors.
type Activities struct {
	operations operationdomain.OrchestrationRepository
	leases     operationdomain.ResourceLeaseRepository
	executors  map[operationdomain.StepExecutor]StepLifecycleExecutor
	observers  []StepProjectionObserver
}

func NewActivities(operations operationdomain.OrchestrationRepository, leases operationdomain.ResourceLeaseRepository, executors map[operationdomain.StepExecutor]StepLifecycleExecutor, observers ...StepProjectionObserver) *Activities {
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
	return a.operations.AppendEvent(ctx, operationdomain.TimelineEvent{
		ID: uuid.NewString(), OperationID: input.OperationID, Type: "operation_status",
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
	return a.operations.AppendEvent(ctx, operationdomain.TimelineEvent{
		ID: uuid.NewString(), OperationID: input.OperationID, StepID: input.Step.ID,
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
		return StepExecutionResult{Status: operationdomain.StepFailed, Error: &operationdomain.NormalizedError{
			Code: "executor_unavailable", Message: fmt.Sprintf("No %s executor is configured.", input.Step.Executor), Retryable: false,
		}}, nil
	}
	waiting := input.Step
	waiting.Status = operationdomain.StepWaitingExternal
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
		result.Status = operationdomain.StepSucceeded
	}
	return result, nil
}

// NoopExecutor is the side-effect-free foundation workflow executor.
type NoopExecutor struct{}

func (NoopExecutor) Execute(_ context.Context, _ StepExecutionInput) StepExecutionResult {
	return StepExecutionResult{Status: operationdomain.StepSucceeded, Progress: 100}
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
		OperationID: input.OperationID, Status: operationdomain.OrchestrationWaitingDependency,
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
		OperationID: input.OperationID, Status: operationdomain.OrchestrationRunning,
		StartedAt: &now,
	}).Get(projectionCtx, nil)

	steps := append([]operationdomain.OperationStep(nil), input.Steps...)
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
					OperationID: input.OperationID, Status: operationdomain.OrchestrationSucceeded, FinishedAt: &finished,
				}).Get(projectionCtx, nil)
			}
			failedIndex := retryableFailure(steps)
			if failedIndex < 0 {
				finished := workflow.Now(ctx).UTC()
				status := operationdomain.OrchestrationFailed
				if anySucceeded(steps) {
					status = operationdomain.OrchestrationPartiallySucceeded
				}
				return workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
					OperationID: input.OperationID, Status: status, Reason: firstFailure(steps), FinishedAt: &finished,
				}).Get(projectionCtx, nil)
			}
			_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
				OperationID: input.OperationID, Status: operationdomain.OrchestrationRequiresAttention,
				Reason: "A failed Step requires operator attention.",
			}).Get(projectionCtx, nil)
			command, err := awaitRetryStep(ctx, retryChannel, leases, input.LeaseDuration)
			if err != nil {
				return finishCanceled(ctx, input.OperationID, steps)
			}
			for index := range steps {
				if steps[index].ID == command.StepID && (steps[index].Status == operationdomain.StepFailed || steps[index].Status == operationdomain.StepRequiresAttention) && steps[index].Error != nil && steps[index].Error.Retryable {
					steps[index].Attempt++
					steps[index].Status = operationdomain.StepPending
					steps[index].Error = nil
					steps[index].FinishedAt = nil
					steps[index].Progress = 0
					_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateStep, StepUpdate{OperationID: input.OperationID, Step: steps[index]}).Get(projectionCtx, nil)
				}
			}
			continue
		}

		_ = workflow.ExecuteActivity(projectionCtx, ActivityUpdateState, StateUpdate{
			OperationID: input.OperationID, Status: operationdomain.OrchestrationWaitingExternal,
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
				steps[index].Status = operationdomain.StepRunning
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
						steps[index].Status = operationdomain.StepCanceled
					} else {
						// A non-retryable activity failure (for example a lost lease
						// fencing token) is terminal: marking it retryable would let the
						// operator retry into the same guaranteed failure and never
						// converge. Only genuinely uncertain infrastructure failures pause
						// for operator attention.
						code, message, retryable := normalizeActivityError(err)
						status := operationdomain.StepRequiresAttention
						if !retryable {
							status = operationdomain.StepFailed
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
				OperationID: input.OperationID, Status: operationdomain.OrchestrationRunning,
			}).Get(projectionCtx, nil)
		}
	}
}

func readySteps(steps []operationdomain.OperationStep) []int {
	succeeded := map[string]bool{}
	for _, step := range steps {
		if step.Status == operationdomain.StepSucceeded || step.Status == operationdomain.StepSkipped {
			succeeded[step.ID] = true
		}
	}
	ready := []int{}
	for index, step := range steps {
		if step.Status != operationdomain.StepPending && step.Status != operationdomain.StepWaitingDependency {
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

func allSucceeded(steps []operationdomain.OperationStep) bool {
	for _, step := range steps {
		if step.Status != operationdomain.StepSucceeded && step.Status != operationdomain.StepSkipped {
			return false
		}
	}
	return true
}

func anySucceeded(steps []operationdomain.OperationStep) bool {
	for _, step := range steps {
		if step.Status == operationdomain.StepSucceeded {
			return true
		}
	}
	return false
}

func retryableFailure(steps []operationdomain.OperationStep) int {
	for index, step := range steps {
		if (step.Status == operationdomain.StepFailed || step.Status == operationdomain.StepRequiresAttention) && step.Error != nil && step.Error.Retryable {
			return index
		}
	}
	return -1
}

func firstFailure(steps []operationdomain.OperationStep) string {
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

func finishCanceled(ctx workflow.Context, operationID string, steps []operationdomain.OperationStep) error {
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	disconnected = workflow.WithActivityOptions(disconnected, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, MaximumAttempts: 0},
	})
	finished := workflow.Now(disconnected).UTC()
	_ = workflow.ExecuteActivity(disconnected, ActivityUpdateState, StateUpdate{
		OperationID: operationID, Status: operationdomain.OrchestrationCanceling,
		Reason: "Canceling active work.",
	}).Get(disconnected, nil)
	// Reflect every non-terminal step as canceled, not just the ones that never
	// started. A step left running or waiting-external in the projection after the
	// workflow has stopped would misreport the Operation as still working.
	for index := range steps {
		switch steps[index].Status {
		case operationdomain.StepPending, operationdomain.StepWaitingDependency,
			operationdomain.StepWaitingExternal, operationdomain.StepRunning:
			steps[index].Status = operationdomain.StepCanceled
			steps[index].FinishedAt = &finished
			_ = workflow.ExecuteActivity(disconnected, ActivityUpdateStep, StepUpdate{OperationID: operationID, Step: steps[index]}).Get(disconnected, nil)
		}
	}
	_ = workflow.ExecuteActivity(disconnected, ActivityUpdateState, StateUpdate{
		OperationID: operationID, Status: operationdomain.OrchestrationCanceled,
		Reason: "Canceled by operator.", FinishedAt: &finished,
	}).Get(disconnected, nil)
	return nil
}

// Controller sends controls to an existing Temporal workflow.
type Controller struct{ client client.Client }

func NewController(temporalClient client.Client) *Controller {
	return &Controller{client: temporalClient}
}

func (c *Controller) Cancel(ctx context.Context, operation *operationdomain.OperationV3) error {
	return c.client.CancelWorkflow(ctx, operation.Temporal.WorkflowID, operation.Temporal.RunID)
}

func (c *Controller) RetryStep(ctx context.Context, operation *operationdomain.OperationV3, stepID string) error {
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
	operations     operationdomain.OrchestrationRepository
	taskQueue      string
	interval       time.Duration
	leaseDuration  time.Duration
	maxParallelism int
}

func NewStarter(temporalClient client.Client, operations operationdomain.OrchestrationRepository, taskQueue string, interval, leaseDuration time.Duration, maxParallelism int) *Starter {
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

var _ operationapp.OrchestrationController = (*Controller)(nil)

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
