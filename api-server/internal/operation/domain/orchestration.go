package domain

import (
	"context"
	"errors"
	"time"
)

// OrchestrationStatus is the operator-visible state of a durable multi-step workflow.
type OrchestrationStatus string

const (
	OrchestrationPending            OrchestrationStatus = "pending"
	OrchestrationRunning            OrchestrationStatus = "running"
	OrchestrationWaitingExternal    OrchestrationStatus = "waiting_external"
	OrchestrationWaitingDependency  OrchestrationStatus = "waiting_dependency"
	OrchestrationCanceling          OrchestrationStatus = "canceling"
	OrchestrationSucceeded          OrchestrationStatus = "succeeded"
	OrchestrationFailed             OrchestrationStatus = "failed"
	OrchestrationPartiallySucceeded OrchestrationStatus = "partially_succeeded"
	OrchestrationCanceled           OrchestrationStatus = "canceled"
	OrchestrationRequiresAttention  OrchestrationStatus = "requires_attention"
)

// Terminal reports whether an Operation can no longer advance without a new command.
func (s OrchestrationStatus) Terminal() bool {
	switch s {
	case OrchestrationSucceeded, OrchestrationFailed, OrchestrationPartiallySucceeded, OrchestrationCanceled:
		return true
	default:
		return false
	}
}

// StepStatus is the state of one public Operation Step.
type StepStatus string

const (
	StepPending           StepStatus = "pending"
	StepRunning           StepStatus = "running"
	StepWaitingExternal   StepStatus = "waiting_external"
	StepWaitingDependency StepStatus = "waiting_dependency"
	StepSucceeded         StepStatus = "succeeded"
	StepFailed            StepStatus = "failed"
	StepCanceled          StepStatus = "canceled"
	StepSkipped           StepStatus = "skipped"
	StepRequiresAttention StepStatus = "requires_attention"
)

// StepExecutor identifies the typed lifecycle adapter carrying out a Step.
type StepExecutor string

const (
	StepExecutorInternal StepExecutor = "internal"
	StepExecutorAnsible  StepExecutor = "ansible"
	StepExecutorMAAS     StepExecutor = "maas"
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

// OperationStep is one durable, observable phase of an Operation.
type OperationStep struct {
	ID                string                      `json:"id" bson:"id"`
	Kind              string                      `json:"kind" bson:"kind"`
	Name              string                      `json:"name" bson:"name"`
	Executor          StepExecutor                `json:"executor" bson:"executor"`
	DependsOn         []string                    `json:"dependsOn" bson:"dependsOn"`
	Targets           []ResourceReference         `json:"targets" bson:"targets"`
	Parameters        map[string]any              `json:"parameters,omitempty" bson:"parameters,omitempty"`
	SecretRefs        map[string]string           `json:"secretRefs,omitempty" bson:"secretRefs,omitempty"`
	Status            StepStatus                  `json:"status" bson:"status"`
	Attempt           int                         `json:"attempt" bson:"attempt"`
	Progress          int                         `json:"progress" bson:"progress"`
	WaitingReason     string                      `json:"waitingReason,omitempty" bson:"waitingReason,omitempty"`
	Error             *NormalizedError            `json:"error" bson:"error,omitempty"`
	ExternalExecution *ExternalExecutionReference `json:"externalExecution" bson:"externalExecution,omitempty"`
	Artifacts         []ArtifactMetadata          `json:"artifacts" bson:"artifacts"`
	StartedAt         *time.Time                  `json:"startedAt" bson:"startedAt,omitempty"`
	FinishedAt        *time.Time                  `json:"finishedAt" bson:"finishedAt,omitempty"`
}

// TemporalReference correlates the query projection with its durable workflow.
type TemporalReference struct {
	WorkflowID string `json:"workflowId" bson:"workflowId"`
	RunID      string `json:"runId,omitempty" bson:"runId,omitempty"`
}

// OperationV3 stores durable intent and its query projection. Secret values are never
// part of this aggregate; intent may carry opaque secret references only.
type OperationV3 struct {
	ID                 string              `json:"id" bson:"_id"`
	SchemaVersion      int                 `json:"schemaVersion" bson:"schemaVersion"`
	Kind               OperationKind       `json:"kind" bson:"kind"`
	Intent             map[string]any      `json:"intent" bson:"intent"`
	Definition         string              `json:"definition" bson:"definition"`
	DefinitionVersion  int                 `json:"definitionVersion" bson:"definitionVersion"`
	Status             OrchestrationStatus `json:"status" bson:"status"`
	StatusReason       string              `json:"statusReason,omitempty" bson:"statusReason,omitempty"`
	StartState         string              `json:"startState" bson:"startState"`
	Temporal           TemporalReference   `json:"temporal" bson:"temporal"`
	SiteID             string              `json:"siteId" bson:"siteId"`
	PlatformID         string              `json:"platformId,omitempty" bson:"platformId,omitempty"`
	TargetResources    []ResourceReference `json:"targetResources" bson:"targetResources"`
	TargetServerIDs    []string            `json:"targetServerIds" bson:"targetServerIds"`
	Steps              []OperationStep     `json:"steps" bson:"steps"`
	RetryOfOperationID string              `json:"retryOfOperationId,omitempty" bson:"retryOfOperationId,omitempty"`
	RequestedBy        string              `json:"requestedBy" bson:"requestedBy"`
	RequestCorrelation string              `json:"requestCorrelation,omitempty" bson:"requestCorrelation,omitempty"`
	RequestedAt        time.Time           `json:"requestedAt" bson:"requestedAt"`
	StartedAt          *time.Time          `json:"startedAt" bson:"startedAt,omitempty"`
	FinishedAt         *time.Time          `json:"finishedAt" bson:"finishedAt,omitempty"`
	UpdatedAt          time.Time           `json:"updatedAt" bson:"updatedAt"`
}

// TimelineEvent is an immutable normalized Operation event.
type TimelineEvent struct {
	ID          string         `json:"id" bson:"id"`
	OperationID string         `json:"operationId" bson:"operationId"`
	StepID      string         `json:"stepId,omitempty" bson:"stepId,omitempty"`
	Type        string         `json:"type" bson:"type"`
	Message     string         `json:"message" bson:"message"`
	Details     map[string]any `json:"details,omitempty" bson:"details,omitempty"`
	CreatedAt   time.Time      `json:"createdAt" bson:"createdAt"`
}

var (
	ErrStepNotFound             = errors.New("operation step not found")
	ErrStepRetryUnsafe          = errors.New("operation step cannot be retried safely")
	ErrOperationNotV3           = errors.New("operation is not an orchestration operation")
	ErrOperationControlConflict = errors.New("operation cannot accept this control in its current state")
	ErrLeaseConflict            = errors.New("one or more resources are already leased")
	ErrLeaseFenced              = errors.New("resource lease fencing token is no longer current")
)

// OrchestrationFilter narrows v3 Operation listings.
type OrchestrationFilter struct {
	SiteID, PlatformID, ServerID string
	PlatformIDs                  []string
	Kind                         OperationKind
	Status                       OrchestrationStatus
	ActiveOnly                   bool
	Offset, Limit                int
}

// OrchestrationRepository stores v3 intent and its rebuildable query projection.
type OrchestrationRepository interface {
	Create(ctx context.Context, operation *OperationV3) error
	FindByID(ctx context.Context, id string) (*OperationV3, error)
	List(ctx context.Context, filter OrchestrationFilter) ([]*OperationV3, int, error)
	ListPendingStart(ctx context.Context, limit int) ([]*OperationV3, error)
	MarkWorkflowStarted(ctx context.Context, id, runID string) error
	UpdateState(ctx context.Context, id string, status OrchestrationStatus, reason string, startedAt, finishedAt *time.Time) error
	UpdateStep(ctx context.Context, operationID string, step OperationStep) error
	AppendEvent(ctx context.Context, event TimelineEvent) error
	Timeline(ctx context.Context, operationID string) ([]TimelineEvent, error)
}

// ResourceLease serializes all mutations for a Server or Platform.
type ResourceLease struct {
	ResourceKey  string    `json:"resourceKey" bson:"_id"`
	Owner        string    `json:"owner" bson:"owner"`
	FencingToken int64     `json:"fencingToken" bson:"fencingToken"`
	ExpiresAt    time.Time `json:"expiresAt" bson:"expiresAt"`
	UpdatedAt    time.Time `json:"updatedAt" bson:"updatedAt"`
}

// ResourceLeaseRepository owns ordered multi-resource acquisition and fencing.
type ResourceLeaseRepository interface {
	Acquire(ctx context.Context, resourceKeys []string, owner string, expiresAt time.Time) ([]ResourceLease, error)
	Renew(ctx context.Context, leases []ResourceLease, expiresAt time.Time) error
	Validate(ctx context.Context, lease ResourceLease) error
	Release(ctx context.Context, leases []ResourceLease) error
}

// ResourceLeaseReader exposes only active leases for operator diagnostics.
type ResourceLeaseReader interface {
	FindByOwner(ctx context.Context, owner string) ([]ResourceLease, error)
}

// OperationSecretRepository stores encrypted values outside Temporal history. Returned
// references are opaque and safe to put in a workflow input.
type OperationSecretRepository interface {
	Store(ctx context.Context, operationID, name string, value any) (string, error)
	Resolve(ctx context.Context, reference string) (any, error)
	DeleteForOperation(ctx context.Context, operationID string) error
}
