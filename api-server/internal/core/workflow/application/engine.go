package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/maple52046/swallow/internal/core/store"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// Engine is the only writer of Workflow execution state (decision 072). Every method is one
// transition, committed in one store transaction together with the timeline and the Task's
// declared projections; the store retries it when a concurrent write touched the same Workflow.
// Writes that carry a dispatch generation older than the Workflow's fail with
// workflowdomain.ErrDispatchFenced, so a superseded run can never change state.
//
// The engine knows no Workflow or Task kind: what a Task does on completion, on cancel, on a
// rerun, and on a resume comes from its declarations and the PlanCatalog.
type Engine struct {
	transactor  store.Transactor
	workflows   workflowdomain.WorkflowRepository
	ownership   workflowdomain.OwnershipRepository
	catalog     workflowdomain.PlanCatalog
	handlers    CompletionHandlers
	maxParallel int
	now         func() time.Time
	// observers see every Task transition, inside its transaction, for read models that have no
	// declaration yet.
	observers []TaskObserver
}

// ProjectionHandler applies one projection completion. It runs inside the transaction of every
// transition of a Task declaring it, so it must only write the store, must be idempotent, and is
// rolled back with the transition.
type ProjectionHandler interface {
	Project(ctx context.Context, transition TaskTransition) error
}

// ResultHandler applies one result completion after the Runner succeeded, before the outcome is
// recorded and outside any transaction. It may call other systems; an error fails the Task with a
// retryable completion_failed, so a retry runs the Task and the handler again.
type ResultHandler interface {
	Complete(ctx context.Context, result TaskResult) error
}

// TaskObserver sees every Task transition inside its transaction. It exists for read models that
// are not declared as completions yet; it must only write the store.
type TaskObserver interface {
	ObserveStep(ctx context.Context, workflowID string, task workflowdomain.Task) error
}

// TaskTransition is a Task after one transition, with the Workflow it belongs to and the
// completion being applied.
type TaskTransition struct {
	Workflow   *workflowdomain.Workflow
	Task       workflowdomain.Task
	Completion workflowdomain.Completion
}

// TaskResult is a succeeded Task with the data its Runner returned.
type TaskResult struct {
	Workflow   *workflowdomain.Workflow
	Task       workflowdomain.Task
	Completion workflowdomain.Completion
	Data       map[string]any
}

// CompletionHandlers are the handlers a process registers for the completion types of the plan
// catalog, by type.
type CompletionHandlers struct {
	Projections map[string]ProjectionHandler
	Results     map[string]ResultHandler
}

// Require fails when a completion type of one of classes has no handler, so a process that would
// silently skip a declared behavior refuses to start (decision 072).
func (h CompletionHandlers) Require(catalog workflowdomain.PlanCatalog, classes ...workflowdomain.CompletionClass) error {
	wanted := map[workflowdomain.CompletionClass]bool{}
	for _, class := range classes {
		wanted[class] = true
	}
	var missing []string
	for name, class := range catalog.Completions {
		if !wanted[class] {
			continue
		}
		switch class {
		case workflowdomain.CompletionProjection:
			if h.Projections[name] == nil {
				missing = append(missing, name)
			}
		case workflowdomain.CompletionResult:
			if h.Results[name] == nil {
				missing = append(missing, name)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("completion types without a handler: %v", missing)
	}
	return nil
}

// NewEngine builds the engine. Every projection type in catalog needs a handler, because any
// transition may apply one. Result handlers belong to the process whose Runners return data (the
// Ansible executor): a nil Results map means this process runs no such Runner and leaves result
// completions to the one that does, while a non-nil map must cover every result type.
// maxParallel bounds the sliding window (a non-positive value is 4).
func NewEngine(
	transactor store.Transactor,
	workflows workflowdomain.WorkflowRepository,
	ownership workflowdomain.OwnershipRepository,
	catalog workflowdomain.PlanCatalog,
	handlers CompletionHandlers,
	maxParallel int,
) (*Engine, error) {
	if transactor == nil || workflows == nil || ownership == nil {
		return nil, errors.New("workflow engine: transactor, workflows, and ownership are required")
	}
	classes := []workflowdomain.CompletionClass{workflowdomain.CompletionProjection}
	if handlers.Results != nil {
		classes = append(classes, workflowdomain.CompletionResult)
	}
	if err := handlers.Require(catalog, classes...); err != nil {
		return nil, fmt.Errorf("workflow engine: %w", err)
	}
	if maxParallel <= 0 {
		maxParallel = 4
	}
	return &Engine{
		transactor: transactor, workflows: workflows, ownership: ownership, catalog: catalog,
		handlers: handlers, maxParallel: maxParallel, now: func() time.Time { return time.Now().UTC() },
	}, nil
}

// AttachObservers adds observers that see every Task transition.
func (e *Engine) AttachObservers(observers ...TaskObserver) {
	e.observers = append(e.observers, observers...)
}

// TaskRef identifies one attempt of one Task as started by one dispatch generation.
type TaskRef struct {
	WorkflowID string
	Generation int64
	TaskID     string
	Attempt    int
}

// TaskStart is a Task the driver must execute now.
type TaskStart struct {
	TaskID  string
	Attempt int
	Runner  workflowdomain.RunnerKind
	// Resumed is true when an earlier dispatch started this attempt; its Runner observes first.
	Resumed bool
}

// AdvanceAction is what the driver does after an Advance.
type AdvanceAction string

const (
	// AdvanceRun means execute Start (possibly empty while other Tasks still run) and Advance
	// again when any execution ends.
	AdvanceRun AdvanceAction = "run"
	// AdvanceWaitResources means another Workflow owns a resource; Advance again shortly.
	AdvanceWaitResources AdvanceAction = "wait-resources"
	// AdvancePark means the Workflow waits in requires_attention until woken by a command.
	AdvancePark AdvanceAction = "park"
	// AdvanceCancel means a cancel was requested: stop running executions and take the cancel path.
	AdvanceCancel AdvanceAction = "cancel"
	// AdvanceFinished means the Workflow is terminal; the run ends.
	AdvanceFinished AdvanceAction = "finished"
	// AdvanceFenced means a later dispatch superseded this run; it ends without side effects.
	AdvanceFenced AdvanceAction = "fenced"
)

// AdvanceRequest is one round of a run. Running lists the Tasks the run is executing now, so a
// retried Advance hands back Tasks it began but the run never started.
type AdvanceRequest struct {
	WorkflowID string
	Generation int64
	RunID      string
	Running    []string
}

// AdvanceResult is the action for the run and the Tasks it must start.
type AdvanceResult struct {
	Action AdvanceAction
	Start  []TaskStart
}

// Outcome is what a Runner reports for one attempt. Data is the Runner's result data for result
// completions; it is never stored.
type Outcome struct {
	Status            workflowdomain.TaskStatus
	Progress          int
	WaitingReason     string
	Error             *workflowdomain.NormalizedError
	ExternalExecution *workflowdomain.ExternalExecutionReference
	Artifacts         []workflowdomain.ArtifactMetadata
	Data              map[string]any
}

// TaskExecution is what a Runner needs to execute one attempt.
type TaskExecution struct {
	WorkflowID string
	Kind       workflowdomain.WorkflowKind
	PlatformID string
	SiteID     string
	Generation int64
	Task       workflowdomain.Task
}

// Compensation is one cancel compensation to run before the Workflow is recorded canceled.
type Compensation struct {
	TaskID  string
	Attempt int
	Runner  workflowdomain.RunnerKind
	// Kind names the registered compensation; empty when Run is true.
	Kind string
	// Run means execute the Task itself (CancelBehavior.Run).
	Run bool
}

const (
	reasonWaitingForResources = "Waiting for resource leases."
	reasonWaitingExternal     = "Waiting for an external executor or provider."
	reasonParked              = "A failed Step requires operator attention."
	reasonCanceling           = "Canceling active work."
)

// Dispatch starts a new dispatch generation: the generation increases, the Workflow is marked
// started for the current driver, every stored command counts as delivered (the new run applies
// them), and the run reference is cleared until RecordRun. It returns the new generation, or
// ErrWorkflowControlConflict for a terminal Workflow.
func (e *Engine) Dispatch(ctx context.Context, id string) (int64, error) {
	var generation int64
	err := e.transactor.InTransaction(ctx, func(ctx context.Context) error {
		w, err := e.workflows.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if w.Status.Terminal() {
			return fmt.Errorf("%w: a finished Workflow is not dispatched", workflowdomain.ErrWorkflowControlConflict)
		}
		now := e.now()
		w.Dispatch.Generation++
		w.Dispatch.Driver = workflowdomain.DriverVersion
		w.Dispatch.DeliveredCommand = w.Commands.Sequence
		w.Dispatch.DispatchedAt = &now
		w.StartState = "started"
		w.Temporal.RunID = ""
		generation = w.Dispatch.Generation
		return e.workflows.Save(ctx, w)
	})
	return generation, err
}

// Advance runs one round of a Workflow for the run of request.Generation: it takes the
// Workflow's resources, applies retry commands, resumes Tasks an earlier dispatch left running,
// skips Tasks that can never run, begins Tasks the sliding window has room for, and records the
// Workflow's resulting status — finishing it, and releasing its resources, when nothing is left
// to run.
func (e *Engine) Advance(ctx context.Context, request AdvanceRequest) (AdvanceResult, error) {
	var result AdvanceResult
	err := e.transactor.InTransaction(ctx, func(ctx context.Context) error {
		result = AdvanceResult{}
		w, err := e.workflows.FindByID(ctx, request.WorkflowID)
		if err != nil {
			return err
		}
		if w.Dispatch.Generation != request.Generation {
			result.Action = AdvanceFenced
			return nil
		}
		if w.Status.Terminal() {
			result.Action = AdvanceFinished
			return nil
		}
		changed := false
		if request.RunID != "" && w.Temporal.RunID != request.RunID {
			w.Temporal.RunID = request.RunID
			changed = true
		}
		if w.Commands.Cancel != nil {
			result.Action = AdvanceCancel
			if changed {
				return e.workflows.Save(ctx, w)
			}
			return nil
		}
		now := e.now()
		if w.StartedAt == nil {
			w.StartedAt = &now
		}
		if !w.ResourcesAcquired {
			acquired, err := e.ownership.Acquire(ctx, w.ID, resourceKeys(w.TargetResources), now)
			if err != nil {
				return err
			}
			if !acquired {
				result.Action = AdvanceWaitResources
				return e.setStatus(ctx, w, workflowdomain.WorkflowWaitingDependency, reasonWaitingForResources, nil)
			}
			w.ResourcesAcquired = true
		}
		if err := e.applyRetries(ctx, w); err != nil {
			return err
		}
		starts, err := e.resume(ctx, w, request)
		if err != nil {
			return err
		}
		running := 0
		for _, task := range w.Steps {
			if task.Status == workflowdomain.TaskRunning || task.Status == workflowdomain.TaskWaitingExternal {
				running++
			}
		}
		schedule := workflowdomain.Plan(w.Steps, e.maxParallel-running)
		for _, index := range schedule.Skip {
			task := &w.Steps[index]
			task.Status = workflowdomain.TaskSkipped
			task.FinishedAt = &now
			if err := e.transition(ctx, w, index); err != nil {
				return err
			}
		}
		for _, index := range schedule.Ready {
			task := &w.Steps[index]
			task.Status = workflowdomain.TaskRunning
			task.StartedAt = &now
			task.FinishedAt = nil
			task.WaitingReason = ""
			task.Generation = request.Generation
			task.Resumed = false
			task.Live = nil
			if err := e.transition(ctx, w, index); err != nil {
				return err
			}
			starts = append(starts, TaskStart{TaskID: task.ID, Attempt: task.Attempt, Runner: task.Executor})
		}
		switch schedule.Outcome {
		case workflowdomain.OutcomeContinue:
			result.Action, result.Start = AdvanceRun, starts
			return e.setStatus(ctx, w, workflowdomain.WorkflowWaitingExternal, reasonWaitingExternal, nil)
		case workflowdomain.OutcomePark:
			result.Action = AdvancePark
			return e.setStatus(ctx, w, workflowdomain.WorkflowRequiresAttention, reasonParked, nil)
		default:
			result.Action = AdvanceFinished
			status := map[workflowdomain.ScheduleOutcome]workflowdomain.WorkflowStatus{
				workflowdomain.OutcomeSucceeded:          workflowdomain.WorkflowSucceeded,
				workflowdomain.OutcomeFailed:             workflowdomain.WorkflowFailed,
				workflowdomain.OutcomePartiallySucceeded: workflowdomain.WorkflowPartiallySucceeded,
			}[schedule.Outcome]
			return e.finish(ctx, w, status, schedule.Reason)
		}
	})
	return result, err
}

// applyRetries applies and clears the stored retry commands. A command for a Task that is no
// longer retryable (a duplicate, or one an earlier round already applied) is dropped.
func (e *Engine) applyRetries(ctx context.Context, w *workflowdomain.Workflow) error {
	if len(w.Commands.Retries) == 0 {
		return nil
	}
	for _, command := range w.Commands.Retries {
		index := taskIndex(w, command.TaskID)
		if index < 0 || !retryable(w.Steps[index]) {
			continue
		}
		targets := []int{index}
		if job := w.Steps[index].Job; workflowdomain.JobRetry(w.Jobs, job) == workflowdomain.RetryJob {
			targets = targets[:0]
			for i := range w.Steps {
				if w.Steps[i].Job == job {
					targets = append(targets, i)
				}
			}
		}
		for _, i := range targets {
			task := &w.Steps[i]
			task.Attempt++
			task.Status = workflowdomain.TaskPending
			task.Error = nil
			task.FinishedAt = nil
			task.Progress = 0
			task.WaitingReason = ""
			task.Live = nil
			task.Resumed = false
			if err := e.transition(ctx, w, i); err != nil {
				return err
			}
		}
	}
	w.Commands.Retries = nil
	return nil
}

// resume hands back the Tasks this run must execute without beginning them again: running Tasks
// of this generation the run is not executing (a retried Advance), and running Tasks an earlier
// generation left, which are resumed or handed to the operator by their kind's ResumePolicy.
func (e *Engine) resume(ctx context.Context, w *workflowdomain.Workflow, request AdvanceRequest) ([]TaskStart, error) {
	executing := map[string]bool{}
	for _, id := range request.Running {
		executing[id] = true
	}
	var starts []TaskStart
	for index := range w.Steps {
		task := &w.Steps[index]
		if task.Status != workflowdomain.TaskRunning && task.Status != workflowdomain.TaskWaitingExternal {
			continue
		}
		if task.Generation == request.Generation {
			if !executing[task.ID] {
				starts = append(starts, TaskStart{TaskID: task.ID, Attempt: task.Attempt, Runner: task.Executor, Resumed: task.Resumed})
			}
			continue
		}
		if e.catalog.TaskKinds[task.Kind].Resume != workflowdomain.ResumeObserve {
			now := e.now()
			task.Status = workflowdomain.TaskRequiresAttention
			task.FinishedAt = &now
			task.Live = nil
			task.Error = &workflowdomain.NormalizedError{
				Code:      "executor_outcome_unknown",
				Message:   "The Task's executor stopped before recording an outcome; retry the Task to run it again.",
				Retryable: true,
			}
			if err := e.transition(ctx, w, index); err != nil {
				return nil, err
			}
			continue
		}
		task.Status = workflowdomain.TaskRunning
		task.Generation = request.Generation
		task.Resumed = true
		starts = append(starts, TaskStart{TaskID: task.ID, Attempt: task.Attempt, Runner: task.Executor, Resumed: true})
	}
	return starts, nil
}

// TaskForExecution returns what a Runner needs for ref. It fails with ErrDispatchFenced when the
// generation was superseded and ErrTaskNotRunning when the Task is not running at the attempt.
func (e *Engine) TaskForExecution(ctx context.Context, ref TaskRef) (*TaskExecution, error) {
	w, err := e.workflows.FindByID(ctx, ref.WorkflowID)
	if err != nil {
		return nil, err
	}
	index, err := runningTask(w, ref)
	if err != nil {
		return nil, err
	}
	return executionOf(w, index), nil
}

// TaskForCompensation returns what a Runner needs to compensate a Task of a canceling Workflow,
// whatever the Task's status.
func (e *Engine) TaskForCompensation(ctx context.Context, workflowID string, generation int64, taskID string) (*TaskExecution, error) {
	w, err := e.workflows.FindByID(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	if w.Dispatch.Generation != generation {
		return nil, workflowdomain.ErrDispatchFenced
	}
	index := taskIndex(w, taskID)
	if index < 0 {
		return nil, workflowdomain.ErrTaskNotFound
	}
	return executionOf(w, index), nil
}

// CheckGeneration fails with ErrDispatchFenced once generation is no longer the Workflow's, so a
// Runner can stop before a side effect.
func (e *Engine) CheckGeneration(ctx context.Context, workflowID string, generation int64) error {
	w, err := e.workflows.FindByID(ctx, workflowID)
	if err != nil {
		return err
	}
	if w.Dispatch.Generation != generation {
		return workflowdomain.ErrDispatchFenced
	}
	return nil
}

// Complete records the outcome of ref after running the Task's result completions when it
// succeeded. A failing result completion turns the outcome into a retryable completion_failed.
func (e *Engine) Complete(ctx context.Context, ref TaskRef, outcome Outcome) error {
	if outcome.Status == "" {
		outcome.Status = workflowdomain.TaskSucceeded
	}
	if outcome.Status == workflowdomain.TaskSucceeded && e.handlers.Results != nil {
		w, err := e.workflows.FindByID(ctx, ref.WorkflowID)
		if err != nil {
			return err
		}
		index, err := runningTask(w, ref)
		if err != nil {
			return err
		}
		for _, completion := range w.Steps[index].Completion {
			if e.catalog.Completions[completion.Type] != workflowdomain.CompletionResult {
				continue
			}
			handler := e.handlers.Results[completion.Type]
			if handler == nil {
				return fmt.Errorf("no handler for result completion %q", completion.Type)
			}
			if err := handler.Complete(ctx, TaskResult{Workflow: w, Task: w.Steps[index], Completion: completion, Data: outcome.Data}); err != nil {
				outcome.Status = workflowdomain.TaskFailed
				outcome.Error = &workflowdomain.NormalizedError{
					Code: "completion_failed", Message: "Recording the Task's result failed: " + err.Error(), Retryable: true,
				}
				break
			}
		}
	}
	return e.Record(ctx, ref, outcome)
}

// Record stores the outcome of one attempt. A write for a Task no longer running at that attempt
// (canceled, or already recorded) fails with ErrTaskNotRunning and changes nothing, which keeps a
// recorded outcome from being rewritten.
func (e *Engine) Record(ctx context.Context, ref TaskRef, outcome Outcome) error {
	if outcome.Status == "" {
		outcome.Status = workflowdomain.TaskSucceeded
	}
	return e.transactor.InTransaction(ctx, func(ctx context.Context) error {
		w, err := e.workflows.FindByID(ctx, ref.WorkflowID)
		if err != nil {
			return err
		}
		index, err := runningTask(w, ref)
		if err != nil {
			return err
		}
		now := e.now()
		task := &w.Steps[index]
		task.Status = outcome.Status
		task.Progress = outcome.Progress
		task.WaitingReason = outcome.WaitingReason
		task.Error = outcome.Error
		if outcome.ExternalExecution != nil {
			task.ExternalExecution = outcome.ExternalExecution
		}
		if outcome.Artifacts != nil {
			task.Artifacts = outcome.Artifacts
		}
		task.FinishedAt = &now
		task.Live = nil
		task.Resumed = false
		if err := e.transition(ctx, w, index); err != nil {
			return err
		}
		return e.workflows.Save(ctx, w)
	})
}

// UpdateLive records a running attempt's live fields; see WorkflowRepository.UpdateTaskLive.
func (e *Engine) UpdateLive(ctx context.Context, ref TaskRef, external *workflowdomain.ExternalExecutionReference, startedAt *time.Time, live *workflowdomain.TaskLive) error {
	return e.workflows.UpdateTaskLive(ctx, ref.WorkflowID, ref.Generation, ref.TaskID, ref.Attempt, external, startedAt, live)
}

// BeginCancel records the Workflow canceling and returns the compensations its Tasks declare: the
// compensation of each Task still running (interrupted by the cancel), and each Task that runs on
// cancel and has not succeeded. A terminal Workflow returns none.
func (e *Engine) BeginCancel(ctx context.Context, workflowID string, generation int64) ([]Compensation, error) {
	var compensations []Compensation
	err := e.transactor.InTransaction(ctx, func(ctx context.Context) error {
		compensations = nil
		w, err := e.workflows.FindByID(ctx, workflowID)
		if err != nil {
			return err
		}
		if w.Dispatch.Generation != generation {
			return workflowdomain.ErrDispatchFenced
		}
		if w.Status.Terminal() {
			return nil
		}
		for _, task := range w.Steps {
			if task.OnCancel == nil {
				continue
			}
			interrupted := task.Status == workflowdomain.TaskRunning || task.Status == workflowdomain.TaskWaitingExternal
			if task.OnCancel.Compensation != "" && interrupted {
				compensations = append(compensations, Compensation{
					TaskID: task.ID, Attempt: task.Attempt, Runner: task.Executor, Kind: task.OnCancel.Compensation,
				})
			}
			if task.OnCancel.Run && task.Status != workflowdomain.TaskSucceeded {
				compensations = append(compensations, Compensation{
					TaskID: task.ID, Attempt: task.Attempt, Runner: task.Executor, Run: true,
				})
			}
		}
		return e.setStatus(ctx, w, workflowdomain.WorkflowCanceling, reasonCanceling, nil)
	})
	return compensations, err
}

// FinishCanceled records the Workflow canceled: every unfinished Task (running, parked, or never
// started) becomes canceled with its projections, the Workflow becomes canceled with its cancel
// command's reason, and its resources are released — all in one transaction. Tasks that reached
// an outcome keep it.
func (e *Engine) FinishCanceled(ctx context.Context, workflowID string, generation int64) error {
	return e.transactor.InTransaction(ctx, func(ctx context.Context) error {
		w, err := e.workflows.FindByID(ctx, workflowID)
		if err != nil {
			return err
		}
		if w.Dispatch.Generation != generation {
			return workflowdomain.ErrDispatchFenced
		}
		if w.Status.Terminal() {
			return nil
		}
		now := e.now()
		for index := range w.Steps {
			task := &w.Steps[index]
			switch task.Status {
			case workflowdomain.TaskPending, workflowdomain.TaskWaitingDependency, workflowdomain.TaskWaitingExternal,
				workflowdomain.TaskRunning, workflowdomain.TaskRequiresAttention:
				task.Status = workflowdomain.TaskCanceled
				task.FinishedAt = &now
				task.Live = nil
				task.Resumed = false
				if err := e.transition(ctx, w, index); err != nil {
					return err
				}
			}
		}
		reason := workflowdomain.CanceledByOperatorReason
		if w.Commands.Cancel != nil && w.Commands.Cancel.Reason != "" {
			reason = w.Commands.Cancel.Reason
		}
		return e.finish(ctx, w, workflowdomain.WorkflowCanceled, reason)
	})
}

// RecordCompensation adds the timeline event a cancel compensation of a Task produced. Its ID is
// per Task, compensation, and attempt, so a retried activity upserts it.
func (e *Engine) RecordCompensation(ctx context.Context, workflowID, taskID, kind string, attempt int, eventType, message string) error {
	return e.workflows.AppendEvent(ctx, workflowdomain.TimelineEvent{
		ID: fmt.Sprintf("%s/step/%s/%s/%d", workflowID, taskID, kind, attempt), OperationID: workflowID, StepID: taskID,
		Type: eventType, Message: message, Details: map[string]any{"attempt": attempt}, CreatedAt: e.now(),
	})
}

// finish records a terminal status and releases the Workflow's resources in the same transaction.
func (e *Engine) finish(ctx context.Context, w *workflowdomain.Workflow, status workflowdomain.WorkflowStatus, reason string) error {
	if w.ResourcesAcquired || status == workflowdomain.WorkflowCanceled {
		if err := e.ownership.Release(ctx, w.ID); err != nil {
			return err
		}
		w.ResourcesAcquired = false
	}
	now := e.now()
	return e.setStatus(ctx, w, status, reason, &now)
}

// setStatus records the Workflow's status with its timeline event and saves the Workflow.
func (e *Engine) setStatus(ctx context.Context, w *workflowdomain.Workflow, status workflowdomain.WorkflowStatus, reason string, finishedAt *time.Time) error {
	changed := w.Status != status || w.StatusReason != reason
	w.Status, w.StatusReason = status, reason
	if finishedAt != nil {
		w.FinishedAt = finishedAt
	}
	if err := e.workflows.Save(ctx, w); err != nil {
		return err
	}
	if !changed {
		return nil
	}
	// The ID is per status, so a status the Workflow returns to (running between Tasks) keeps
	// one timeline entry, and a retried transition upserts it.
	return e.workflows.AppendEvent(ctx, workflowdomain.TimelineEvent{
		ID: w.ID + "/state/" + string(status), OperationID: w.ID, Type: "operation_status",
		Message: "Operation status changed to " + string(status) + ".",
		Details: map[string]any{"status": string(status), "reason": reason}, CreatedAt: e.now(),
	})
}

// transition records one Task transition: its timeline event, its declared projections, and the
// observers, all inside the caller's transaction.
func (e *Engine) transition(ctx context.Context, w *workflowdomain.Workflow, index int) error {
	task := w.Steps[index]
	details := map[string]any{"status": string(task.Status), "attempt": task.Attempt}
	if failure := task.Error; failure != nil {
		details["code"] = failure.Code
		details["message"] = failure.Message
		details["stage"] = failure.Stage
		details["retryable"] = failure.Retryable
	}
	if err := e.workflows.AppendEvent(ctx, workflowdomain.TimelineEvent{
		ID: fmt.Sprintf("%s/step/%s/%s/%d", w.ID, task.ID, task.Status, task.Attempt), OperationID: w.ID, StepID: task.ID,
		Type: "step_status", Message: task.Name + " changed to " + string(task.Status) + ".",
		Details: details, CreatedAt: e.now(),
	}); err != nil {
		return err
	}
	for _, completion := range task.Completion {
		if e.catalog.Completions[completion.Type] != workflowdomain.CompletionProjection {
			continue
		}
		if err := e.handlers.Projections[completion.Type].Project(ctx, TaskTransition{Workflow: w, Task: task, Completion: completion}); err != nil {
			return fmt.Errorf("project %s of task %s: %w", completion.Type, task.ID, err)
		}
	}
	for _, observer := range e.observers {
		if err := observer.ObserveStep(ctx, w.ID, task); err != nil {
			return err
		}
	}
	return nil
}

func runningTask(w *workflowdomain.Workflow, ref TaskRef) (int, error) {
	if w.Dispatch.Generation != ref.Generation {
		return -1, workflowdomain.ErrDispatchFenced
	}
	index := taskIndex(w, ref.TaskID)
	if index < 0 {
		return -1, workflowdomain.ErrTaskNotFound
	}
	task := w.Steps[index]
	if (task.Status != workflowdomain.TaskRunning && task.Status != workflowdomain.TaskWaitingExternal) || task.Attempt != ref.Attempt {
		return -1, workflowdomain.ErrTaskNotRunning
	}
	return index, nil
}

func executionOf(w *workflowdomain.Workflow, index int) *TaskExecution {
	return &TaskExecution{
		WorkflowID: w.ID, Kind: w.Kind, PlatformID: w.PlatformID, SiteID: w.SiteID,
		Generation: w.Dispatch.Generation, Task: w.Steps[index],
	}
}

func taskIndex(w *workflowdomain.Workflow, id string) int {
	for index := range w.Steps {
		if w.Steps[index].ID == id {
			return index
		}
	}
	return -1
}

func retryable(task workflowdomain.Task) bool {
	return (task.Status == workflowdomain.TaskFailed || task.Status == workflowdomain.TaskRequiresAttention) &&
		task.Error != nil && task.Error.Retryable
}

// resourceKeys returns the sorted ownership keys of a Workflow's target resources.
func resourceKeys(resources []workflowdomain.ResourceReference) []string {
	keys := make([]string, 0, len(resources))
	for _, resource := range resources {
		keys = append(keys, resource.Kind+":"+resource.ID)
	}
	sort.Strings(keys)
	return keys
}
