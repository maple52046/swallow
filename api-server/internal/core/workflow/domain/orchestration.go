package domain

import (
	"context"
	"errors"
	"time"
)

// WorkflowStatus is the operator-visible state of a durable multi-step workflow.
type WorkflowStatus string

const (
	WorkflowPending            WorkflowStatus = "pending"
	WorkflowRunning            WorkflowStatus = "running"
	WorkflowWaitingExternal    WorkflowStatus = "waiting_external"
	WorkflowWaitingDependency  WorkflowStatus = "waiting_dependency"
	WorkflowCanceling          WorkflowStatus = "canceling"
	WorkflowSucceeded          WorkflowStatus = "succeeded"
	WorkflowFailed             WorkflowStatus = "failed"
	WorkflowPartiallySucceeded WorkflowStatus = "partially_succeeded"
	WorkflowCanceled           WorkflowStatus = "canceled"
	WorkflowRequiresAttention  WorkflowStatus = "requires_attention"
)

// Terminal reports whether an Operation can no longer advance without a new command.
func (s WorkflowStatus) Terminal() bool {
	switch s {
	case WorkflowSucceeded, WorkflowFailed, WorkflowPartiallySucceeded, WorkflowCanceled:
		return true
	default:
		return false
	}
}

// TaskStatus is the state of one public Operation Step.
type TaskStatus string

const (
	TaskPending           TaskStatus = "pending"
	TaskRunning           TaskStatus = "running"
	TaskWaitingExternal   TaskStatus = "waiting_external"
	TaskWaitingDependency TaskStatus = "waiting_dependency"
	TaskSucceeded         TaskStatus = "succeeded"
	TaskFailed            TaskStatus = "failed"
	TaskCanceled          TaskStatus = "canceled"
	TaskSkipped           TaskStatus = "skipped"
	TaskRequiresAttention TaskStatus = "requires_attention"
)

// RunnerKind identifies the typed lifecycle adapter carrying out a Step.
type RunnerKind string

const (
	RunnerKindInternal    RunnerKind = "internal"
	RunnerKindAnsible     RunnerKind = "ansible"
	RunnerKindProvisioner RunnerKind = "maas"
)

// ResourceReference identifies a target without embedding another context's model.
type ResourceReference struct {
	Kind string `json:"kind" bson:"kind"`
	ID   string `json:"id" bson:"id"`
}

// NormalizedError is the stable diagnostic shown to operators. Raw provider details may
// be attached separately and are never used to infer workflow policy.
type NormalizedError struct {
	Code      string `json:"code" bson:"code"`
	Message   string `json:"message" bson:"message"`
	Retryable bool   `json:"retryable" bson:"retryable"`
	Stage     string `json:"stage,omitempty" bson:"stage,omitempty"`
}

// ExternalExecutionReference correlates a Step to the provider or executor lifecycle.
type ExternalExecutionReference struct {
	Provider   string `json:"provider" bson:"provider"`
	ID         string `json:"id" bson:"id"`
	Generation int    `json:"generation" bson:"generation"`
}

// ArtifactMetadata describes retained output without putting its bytes in Mongo or
// Temporal history.
type ArtifactMetadata struct {
	ID          string    `json:"id" bson:"id"`
	Name        string    `json:"name" bson:"name"`
	MediaType   string    `json:"mediaType" bson:"mediaType"`
	SizeBytes   int64     `json:"sizeBytes" bson:"sizeBytes"`
	RelativeKey string    `json:"-" bson:"relativeKey"`
	CreatedAt   time.Time `json:"createdAt" bson:"createdAt"`
}

// TaskLive is the live, coarse progress of an in-flight Task, projected from its Runner while it
// runs (currently only the ansible Runner populates it). It carries no task output and is advisory:
// the authoritative outcome is Status/Error. It is cleared implicitly when the Task reaches a
// terminal state and the workflow writes the final projection.
type TaskLive struct {
	CurrentPlay string    `json:"currentPlay,omitempty" bson:"currentPlay,omitempty"`
	CurrentTask string    `json:"currentTask,omitempty" bson:"currentTask,omitempty"`
	Total       int       `json:"total" bson:"total"`
	OK          int       `json:"ok" bson:"ok"`
	Changed     int       `json:"changed" bson:"changed"`
	Failed      int       `json:"failed" bson:"failed"`
	Unreachable int       `json:"unreachable" bson:"unreachable"`
	Skipped     int       `json:"skipped" bson:"skipped"`
	UpdatedAt   time.Time `json:"updatedAt" bson:"updatedAt"`
}

// Task is one durable, observable phase of an Operation.
type Task struct {
	ID   string `json:"id" bson:"id"`
	Kind string `json:"kind" bson:"kind"`
	Name string `json:"name" bson:"name"`
	// Job groups Tasks into a reusable, convergent unit (ADR 017). When set, the
	// Workflow runs each Job as a Temporal child workflow; empty means the Task runs in
	// the flat, single-workflow path. Cross-Job dependencies order the child workflows.
	Job       string     `json:"job,omitempty" bson:"job,omitempty"`
	Executor  RunnerKind `json:"executor" bson:"runner"`
	DependsOn []string   `json:"dependsOn" bson:"dependsOn"`
	// ContinueOn lists the dependency outcomes that still let this Task run. Empty is the
	// default and preserves the original behaviour: the Task runs only when every dependency is
	// satisfied (succeeded, or skipped as already-satisfied) and is otherwise skipped. A Task
	// widens this — typically a compensation/cleanup Task that must run even when the work it
	// follows was skipped because an earlier Task failed — by listing the extra permitted
	// outcomes (for example "skipped"). A retryable dependency failure is never treated as
	// settled, so a dependent still waits through operator retries rather than skipping past a
	// failure that may yet succeed.
	ContinueOn        []TaskStatus                `json:"continueOn,omitempty" bson:"continueOn,omitempty"`
	Targets           []ResourceReference         `json:"targets" bson:"targets"`
	Parameters        map[string]any              `json:"parameters,omitempty" bson:"parameters,omitempty"`
	SecretRefs        map[string]string           `json:"secretRefs,omitempty" bson:"secretRefs,omitempty"`
	Status            TaskStatus                  `json:"status" bson:"status"`
	Attempt           int                         `json:"attempt" bson:"attempt"`
	Progress          int                         `json:"progress" bson:"progress"`
	WaitingReason     string                      `json:"waitingReason,omitempty" bson:"waitingReason,omitempty"`
	Error             *NormalizedError            `json:"error" bson:"error,omitempty"`
	ExternalExecution *ExternalExecutionReference `json:"externalExecution" bson:"externalExecution,omitempty"`
	// Live is the in-flight progress of the Task, set by its Runner while it runs and cleared by
	// the final projection. Nil for a Task that has not started or has no live-progress Runner.
	Live       *TaskLive          `json:"live,omitempty" bson:"live,omitempty"`
	Artifacts  []ArtifactMetadata `json:"artifacts" bson:"artifacts"`
	StartedAt  *time.Time         `json:"startedAt" bson:"startedAt,omitempty"`
	FinishedAt *time.Time         `json:"finishedAt" bson:"finishedAt,omitempty"`
	// Completion and OnCancel are the Task's declared behaviors (plan.go); they are part of the
	// accepted plan, never of the public projection.
	Completion []Completion    `json:"-" bson:"completion,omitempty"`
	OnCancel   *CancelBehavior `json:"-" bson:"onCancel,omitempty"`
	// Generation is the dispatch generation that began the running attempt; a running Task of an
	// older generation is resumed by the next dispatch (ResumePolicy).
	Generation int64 `json:"-" bson:"generation,omitempty"`
	// Resumed marks a running attempt a later dispatch continued, so its Runner observes the
	// external work it already started instead of starting it again.
	Resumed bool `json:"-" bson:"resumed,omitempty"`
}

// TemporalReference correlates the query projection with its durable workflow.
type TemporalReference struct {
	WorkflowID string `json:"workflowId" bson:"workflowId"`
	RunID      string `json:"runId,omitempty" bson:"runId,omitempty"`
}

// Workflow stores durable intent and its query projection. Secret values are never
// part of this aggregate; intent may carry opaque secret references only.
type Workflow struct {
	ID                string              `json:"id" bson:"_id"`
	SchemaVersion     int                 `json:"schemaVersion" bson:"schemaVersion"`
	Kind              WorkflowKind        `json:"kind" bson:"kind"`
	Intent            map[string]any      `json:"intent" bson:"intent"`
	Definition        string              `json:"definition" bson:"definition"`
	DefinitionVersion int                 `json:"definitionVersion" bson:"definitionVersion"`
	Status            WorkflowStatus      `json:"status" bson:"status"`
	StatusReason      string              `json:"statusReason,omitempty" bson:"statusReason,omitempty"`
	StartState        string              `json:"startState" bson:"startState"`
	Temporal          TemporalReference   `json:"temporal" bson:"temporal"`
	SiteID            string              `json:"siteId" bson:"siteId"`
	PlatformID        string              `json:"platformId,omitempty" bson:"platformId,omitempty"`
	TargetResources   []ResourceReference `json:"targetResources" bson:"targetResources"`
	TargetServerIDs   []string            `json:"targetServerIds" bson:"targetServerIds"`
	Steps             []Task              `json:"steps" bson:"tasks"`
	// Jobs declares every named Job of Steps with its retry granularity (plan.go).
	Jobs     []JobDeclaration `json:"-" bson:"jobs,omitempty"`
	Dispatch Dispatch         `json:"-" bson:"dispatch"`
	Commands Commands         `json:"-" bson:"commands"`
	// ResourcesAcquired is true from the transition that took the Workflow's resources until the
	// terminal transition that released them.
	ResourcesAcquired  bool       `json:"-" bson:"resourcesAcquired,omitempty"`
	RetryOfOperationID string     `json:"retryOfOperationId,omitempty" bson:"retryOfOperationId,omitempty"`
	RequestedBy        string     `json:"requestedBy" bson:"requestedBy"`
	RequestCorrelation string     `json:"requestCorrelation,omitempty" bson:"requestCorrelation,omitempty"`
	RequestedAt        time.Time  `json:"requestedAt" bson:"requestedAt"`
	StartedAt          *time.Time `json:"startedAt" bson:"startedAt,omitempty"`
	FinishedAt         *time.Time `json:"finishedAt" bson:"finishedAt,omitempty"`
	UpdatedAt          time.Time  `json:"updatedAt" bson:"updatedAt"`
}

// TimelineEvent is an immutable normalized Operation event.
type TimelineEvent struct {
	ID          string         `json:"id" bson:"id"`
	OperationID string         `json:"operationId" bson:"workflowId"`
	StepID      string         `json:"stepId,omitempty" bson:"stepId,omitempty"`
	Type        string         `json:"type" bson:"type"`
	Message     string         `json:"message" bson:"message"`
	Details     map[string]any `json:"details,omitempty" bson:"details,omitempty"`
	CreatedAt   time.Time      `json:"createdAt" bson:"createdAt"`
}

var (
	ErrTaskNotFound            = errors.New("operation step not found")
	ErrTaskRetryUnsafe         = errors.New("operation step cannot be retried safely")
	ErrWorkflowNotV3           = errors.New("operation is not an orchestration operation")
	ErrWorkflowControlConflict = errors.New("operation cannot accept this control in its current state")
	// ErrWorkflowExecutionGone reports that a Workflow has no running execution, so a signal or
	// cancel cannot reach it; the dispatcher then dispatches the Workflow again (decision 072).
	ErrWorkflowExecutionGone = errors.New("the workflow execution is no longer running")
)

// WorkflowFilter narrows v3 Operation listings.
type WorkflowFilter struct {
	SiteID, PlatformID, ServerID string
	PlatformIDs                  []string
	Kind                         WorkflowKind
	Status                       WorkflowStatus
	ActiveOnly                   bool
	Offset, Limit                int
}

// WorkflowRepository stores v3 intent and its rebuildable query projection.
type WorkflowRepository interface {
	Create(ctx context.Context, operation *Workflow) error
	FindByID(ctx context.Context, id string) (*Workflow, error)
	List(ctx context.Context, filter WorkflowFilter) ([]*Workflow, int, error)
	AppendEvent(ctx context.Context, event TimelineEvent) error
	Timeline(ctx context.Context, operationID string) ([]TimelineEvent, error)

	// Save replaces the stored Workflow with workflow, stamping UpdatedAt. Only the core Workflow
	// service calls it, inside the transaction of one transition, which a concurrent write to the
	// same Workflow makes retry with fresh state.
	Save(ctx context.Context, workflow *Workflow) error
	// UpdateTaskLive sets the live fields of a running Task without touching the rest: ref (the
	// external execution reference), startedAt, and live are each applied when non-nil. It returns
	// ErrDispatchFenced when generation is no longer the Workflow's, and ErrTaskNotRunning when
	// the Task is not running at attempt.
	UpdateTaskLive(ctx context.Context, id string, generation int64, taskID string, attempt int,
		ref *ExternalExecutionReference, startedAt *time.Time, live *TaskLive) error
	// RequestCancel stores a cancel command and advances the command sequence; a Workflow that
	// already has one keeps the first. It returns ErrWorkflowControlConflict for a terminal
	// Workflow.
	RequestCancel(ctx context.Context, id string, command CancelCommand) error
	// RequestRetry stores a retry command and advances the command sequence. It returns
	// ErrWorkflowControlConflict for a terminal Workflow.
	RequestRetry(ctx context.Context, id string, command RetryCommand) error
	// MarkCommandsDelivered records that the commands up to sequence reached a run.
	MarkCommandsDelivered(ctx context.Context, id string, sequence int64) error
	// RecordRun stores the executor run of a dispatch; a generation that is no longer the
	// Workflow's changes nothing.
	RecordRun(ctx context.Context, id string, generation int64, runID string) error
	// ListUnfinishedIDs returns the ids of every Workflow that is not terminal.
	ListUnfinishedIDs(ctx context.Context) ([]string, error)
}

// OperationSecretRepository stores encrypted values outside Temporal history. Returned
// references are opaque and safe to put in a workflow input.
type OperationSecretRepository interface {
	Store(ctx context.Context, operationID, name string, value any) (string, error)
	Resolve(ctx context.Context, reference string) (any, error)
	// CloneForOperation copies every sealed secret owned by sourceOperationID to a new set
	// owned by targetOperationID and returns a mapping from each source reference to its
	// clone. It exists so a recovery rerun (a new Operation cloning a failed one) can carry
	// the original's secrets without decrypting them in the application layer or coupling the
	// two Operations' lifetimes: the clones are independent documents keyed to the new
	// Operation, so deleting the original never invalidates the rerun. An empty source yields
	// an empty map and no error.
	CloneForOperation(ctx context.Context, sourceOperationID, targetOperationID string) (map[string]string, error)
	DeleteForOperation(ctx context.Context, operationID string) error
}
