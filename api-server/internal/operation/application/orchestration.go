package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// WorkflowController sends control requests to the durable workflow engine. It does
// not update Mongo state directly; the workflow remains the only status writer.
type WorkflowController interface {
	Cancel(ctx context.Context, operation *operationdomain.Workflow) error
	RetryStep(ctx context.Context, operation *operationdomain.Workflow, stepID string) error
}

// CreateWorkflowInput is trusted server-side intent. Secret values are represented by
// opaque references before they enter this boundary.
type CreateWorkflowInput struct {
	ID                 string
	Kind               operationdomain.WorkflowKind
	IntentSummary      string
	IntentSnapshot     map[string]any
	Definition         string
	DefinitionVersion  int
	SiteID             string
	PlatformID         string
	TargetServerIDs    []string
	TargetResources    []operationdomain.ResourceReference
	Steps              []operationdomain.Task
	RetryOfOperationID string
	RequestedBy        string
	RequestCorrelation string
	SecretStepID       string
	SecretStepIDs      []string
	SecretValues       map[string]any
	StepSecretValues   map[string]map[string]any
}

// WorkflowItem is the public orchestration projection. Execution remains as a
// compatibility summary for clients written against Operation v2.
type WorkflowItem struct {
	ID                 string                              `json:"id"`
	SchemaVersion      int                                 `json:"schemaVersion"`
	Kind               string                              `json:"kind"`
	Intent             string                              `json:"intent"`
	IntentSnapshot     map[string]any                      `json:"intentSnapshot"`
	Definition         string                              `json:"definition"`
	DefinitionVersion  int                                 `json:"definitionVersion"`
	Status             string                              `json:"status"`
	StatusReason       *string                             `json:"statusReason"`
	StartState         string                              `json:"startState"`
	Temporal           operationdomain.TemporalReference   `json:"temporal"`
	SiteID             string                              `json:"siteId"`
	PlatformID         *string                             `json:"platformId"`
	ClusterID          *string                             `json:"clusterId"`
	TargetResources    []operationdomain.ResourceReference `json:"targetResources"`
	TargetServerIDs    []string                            `json:"targetServerIds"`
	Steps              []operationdomain.Task              `json:"steps"`
	Leases             []operationdomain.ResourceLease     `json:"leases"`
	RetryOfOperationID *string                             `json:"retryOfOperationId"`
	RequestedBy        string                              `json:"requestedBy"`
	RequestCorrelation *string                             `json:"requestCorrelation"`
	RequestedAt        string                              `json:"requestedAt"`
	StartedAt          *string                             `json:"startedAt"`
	FinishedAt         *string                             `json:"finishedAt"`
	UpdatedAt          string                              `json:"updatedAt"`
	Execution          ExecutionItem                       `json:"execution"`
}

// WorkflowService accepts durable workflows and exposes their query projection.
type WorkflowService struct {
	operations operationdomain.WorkflowRepository
	controller WorkflowController
	secrets    operationdomain.OperationSecretRepository
	leases     operationdomain.ResourceLeaseReader
}

func NewWorkflowService(operations operationdomain.WorkflowRepository, controller WorkflowController, secrets ...operationdomain.OperationSecretRepository) *WorkflowService {
	service := &WorkflowService{operations: operations, controller: controller}
	if len(secrets) > 0 {
		service.secrets = secrets[0]
	}
	return service
}

// AttachLeaseReader adds active fencing diagnostics without coupling command creation to Mongo.
func (s *WorkflowService) AttachLeaseReader(reader operationdomain.ResourceLeaseReader) {
	s.leases = reader
}

// Create persists pending intent before the starter attempts to contact Temporal.
func (s *WorkflowService) Create(ctx context.Context, input CreateWorkflowInput) (*WorkflowItem, error) {
	if !input.Kind.Valid() {
		return nil, fmt.Errorf("%w: invalid kind %q", ErrInvalidOperation, input.Kind)
	}
	if strings.TrimSpace(input.Definition) == "" || input.DefinitionVersion <= 0 {
		return nil, fmt.Errorf("%w: definition and definitionVersion are required", ErrInvalidOperation)
	}
	if strings.TrimSpace(input.SiteID) == "" {
		return nil, fmt.Errorf("%w: siteId is required", ErrInvalidOperation)
	}
	if len(input.Steps) == 0 {
		return nil, fmt.Errorf("%w: at least one Step is required", ErrInvalidOperation)
	}
	if err := validateSteps(input.Steps); err != nil {
		return nil, err
	}
	for _, serverID := range input.TargetServerIDs {
		_, total, err := s.operations.List(ctx, operationdomain.WorkflowFilter{ServerID: serverID, ActiveOnly: true, Limit: 1})
		if err != nil {
			return nil, err
		}
		if total > 0 {
			// A target already inside an unfinished Operation is a conflict, not a
			// malformed request: it is exactly operationdomain.ErrTargetsBusy, which
			// every delivery mapper renders as 409 with this message. Classifying it
			// as ErrInvalidOperation would leak it as an opaque 4xx/5xx that hides the
			// actionable "the Server is busy" reason from operators.
			return nil, fmt.Errorf("%w: Server %s already has active durable work", operationdomain.ErrTargetsBusy, serverID)
		}
	}

	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	} else if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("%w: id must be a UUID", ErrInvalidOperation)
	}
	steps := append([]operationdomain.Task(nil), input.Steps...)
	for index := range steps {
		// A recovery rerun (see Rerun) clones an Operation's Steps and carries the
		// already-succeeded (or skipped) ones so their side effects are not repeated: the
		// fresh execution treats them as satisfied dependencies and re-drives only the rest.
		// Reset every other Step to a clean first attempt. A normal launch supplies Steps with
		// no status, so both branches leave it at a pending first attempt, unchanged.
		if steps[index].Status != operationdomain.TaskSucceeded && steps[index].Status != operationdomain.TaskSkipped {
			steps[index].Status = operationdomain.TaskPending
			steps[index].Attempt = 1
		} else if steps[index].Attempt < 1 {
			steps[index].Attempt = 1
		}
		if steps[index].Artifacts == nil {
			steps[index].Artifacts = []operationdomain.ArtifactMetadata{}
		}
	}
	if len(input.SecretValues) > 0 {
		if s.secrets == nil {
			return nil, fmt.Errorf("%w: secret storage is unavailable", ErrInvalidOperation)
		}
		targetIDs := append([]string(nil), input.SecretStepIDs...)
		if len(targetIDs) == 0 {
			stepID := input.SecretStepID
			if stepID == "" {
				stepID = steps[0].ID
			}
			targetIDs = []string{stepID}
		}
		wanted := map[string]bool{}
		for _, stepID := range targetIDs {
			wanted[stepID] = true
		}
		references := map[string]string{}
		for name, value := range input.SecretValues {
			reference, err := s.secrets.Store(ctx, id, name, value)
			if err != nil {
				_ = s.secrets.DeleteForOperation(ctx, id)
				return nil, err
			}
			references[name] = reference
		}
		for index := range steps {
			if !wanted[steps[index].ID] {
				continue
			}
			delete(wanted, steps[index].ID)
			if steps[index].SecretRefs == nil {
				steps[index].SecretRefs = map[string]string{}
			}
			for name, reference := range references {
				steps[index].SecretRefs[name] = reference
			}
		}
		if len(wanted) > 0 {
			_ = s.secrets.DeleteForOperation(ctx, id)
			return nil, fmt.Errorf("%w: one or more secret Steps were not found", ErrInvalidOperation)
		}
	}
	if len(input.StepSecretValues) > 0 && s.secrets == nil {
		return nil, fmt.Errorf("%w: secret storage is unavailable", ErrInvalidOperation)
	}
	for stepID, values := range input.StepSecretValues {
		stepIndex := -1
		for index := range steps {
			if steps[index].ID == stepID {
				stepIndex = index
				break
			}
		}
		if stepIndex < 0 {
			return nil, fmt.Errorf("%w: secret Step %q was not found", ErrInvalidOperation, stepID)
		}
		if steps[stepIndex].SecretRefs == nil {
			steps[stepIndex].SecretRefs = map[string]string{}
		}
		for name, value := range values {
			reference, err := s.secrets.Store(ctx, id, stepID+"."+name, value)
			if err != nil {
				_ = s.secrets.DeleteForOperation(ctx, id)
				return nil, err
			}
			steps[stepIndex].SecretRefs[name] = reference
		}
	}
	resources := append([]operationdomain.ResourceReference(nil), input.TargetResources...)
	for _, serverID := range input.TargetServerIDs {
		resources = append(resources, operationdomain.ResourceReference{Kind: "server", ID: serverID})
	}
	resources = uniqueResources(resources)
	operation := &operationdomain.Workflow{
		ID: id, SchemaVersion: 3, Kind: input.Kind,
		Intent: cloneMap(input.IntentSnapshot), Definition: input.Definition,
		DefinitionVersion: input.DefinitionVersion,
		Status:            operationdomain.WorkflowPending,
		StartState:        "pending",
		Temporal:          operationdomain.TemporalReference{WorkflowID: "swallow-operation/" + id},
		SiteID:            input.SiteID, PlatformID: input.PlatformID,
		TargetResources: resources, TargetServerIDs: append([]string(nil), input.TargetServerIDs...),
		Steps: steps, RetryOfOperationID: input.RetryOfOperationID,
		RequestedBy: input.RequestedBy, RequestCorrelation: input.RequestCorrelation,
		RequestedAt: now, UpdatedAt: now,
	}
	operation.Intent["summary"] = strings.TrimSpace(input.IntentSummary)
	if err := s.operations.Create(ctx, operation); err != nil {
		if s.secrets != nil {
			_ = s.secrets.DeleteForOperation(ctx, id)
		}
		return nil, err
	}
	_ = s.operations.AppendEvent(ctx, operationdomain.TimelineEvent{
		ID: uuid.NewString(), OperationID: operation.ID, Type: "operation_requested",
		Message: "Operation accepted and waiting for durable workflow start.", CreatedAt: now,
	})
	item := toWorkflowItem(operation)
	return &item, nil
}

func validateSteps(steps []operationdomain.Task) error {
	ids := make(map[string]bool, len(steps))
	for _, step := range steps {
		if strings.TrimSpace(step.ID) == "" || strings.TrimSpace(step.Kind) == "" || strings.TrimSpace(step.Name) == "" {
			return fmt.Errorf("%w: each Step requires id, kind, and name", ErrInvalidOperation)
		}
		if ids[step.ID] {
			return fmt.Errorf("%w: duplicate Step id %q", ErrInvalidOperation, step.ID)
		}
		ids[step.ID] = true
	}
	for _, step := range steps {
		for _, dependency := range step.DependsOn {
			if !ids[dependency] || dependency == step.ID {
				return fmt.Errorf("%w: Step %q has invalid dependency %q", ErrInvalidOperation, step.ID, dependency)
			}
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	byID := map[string]operationdomain.Task{}
	for _, step := range steps {
		byID[step.ID] = step
	}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("%w: Step dependencies contain a cycle", ErrInvalidOperation)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range byID[id].DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for _, step := range steps {
		if err := visit(step.ID); err != nil {
			return err
		}
	}
	return nil
}

func uniqueResources(resources []operationdomain.ResourceReference) []operationdomain.ResourceReference {
	seen := map[string]bool{}
	result := make([]operationdomain.ResourceReference, 0, len(resources))
	for _, resource := range resources {
		if resource.Kind == "" || resource.ID == "" {
			continue
		}
		key := resource.Kind + ":" + resource.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, resource)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind == result[j].Kind {
			return result[i].ID < result[j].ID
		}
		return result[i].Kind < result[j].Kind
	})
	return result
}

func (s *WorkflowService) Get(ctx context.Context, id string) (*WorkflowItem, error) {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := toWorkflowItem(operation)
	if s.leases != nil {
		leases, leaseErr := s.leases.FindByOwner(ctx, operation.Temporal.WorkflowID)
		if leaseErr != nil {
			return nil, leaseErr
		}
		item.Leases = leases
	}
	return &item, nil
}

// List returns v3 Operations using the canonical orchestration status axis.
func (s *WorkflowService) List(ctx context.Context, input ListOperationsInput) ([]WorkflowItem, int, error) {
	items, total, err := s.operations.List(ctx, operationdomain.WorkflowFilter{
		SiteID: input.SiteID, PlatformID: input.PlatformID, ServerID: input.ServerID,
		Kind: operationdomain.WorkflowKind(input.Kind), Status: operationdomain.WorkflowStatus(input.Status),
		ActiveOnly: input.Active, Offset: input.Page.Offset(), Limit: input.Page.PageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	result := make([]WorkflowItem, len(items))
	for index, item := range items {
		result[index] = toWorkflowItem(item)
	}
	return result, total, nil
}

func (s *WorkflowService) Timeline(ctx context.Context, id string) ([]operationdomain.TimelineEvent, error) {
	if _, err := s.operations.FindByID(ctx, id); err != nil {
		return nil, err
	}
	return s.operations.Timeline(ctx, id)
}

func (s *WorkflowService) Cancel(ctx context.Context, id string) error {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if operation.Status.Terminal() {
		return fmt.Errorf("%w: a finished Operation cannot be canceled", operationdomain.ErrWorkflowControlConflict)
	}
	return s.controller.Cancel(ctx, operation)
}

func (s *WorkflowService) RetryStep(ctx context.Context, id, stepID string) error {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return err
	}
	for _, step := range operation.Steps {
		if step.ID != stepID {
			continue
		}
		if (step.Status != operationdomain.TaskFailed && step.Status != operationdomain.TaskRequiresAttention) || step.Error == nil || !step.Error.Retryable {
			return operationdomain.ErrTaskRetryUnsafe
		}
		return s.controller.RetryStep(ctx, operation, stepID)
	}
	return operationdomain.ErrTaskNotFound
}

// Rerun recovers a durable Operation that can no longer make progress by launching a fresh
// Temporal execution for the same intent, on the same Platform, without deleting it. It is the
// recovery path for an Operation whose execution was lost (a host restart or execution timeout
// leaves it non-terminal but with no live workflow) or that finished failed, partially
// succeeded, or canceled: a new attempt cannot reuse the original because the Temporal Workflow
// ID is stable and rejects a duplicate start, and a lost execution can no longer accept a Step
// retry signal.
//
// The new Operation clones the original's Steps, preserving already-succeeded and skipped ones
// so their side effects are not repeated and only the incomplete work is re-driven, and
// re-seals the original's secrets under the new Operation (via CloneForOperation) so its Steps
// resolve them. It is linked to the original through RetryOfOperationID, and because it carries
// a later requestedAt the platform lifecycle projection follows it while the original stays in
// history for diagnosis.
//
// Rerun refuses an Operation that already succeeded and one that is still actively advancing
// (pending, running, waiting, or canceling); both are ErrWorkflowControlConflict, the first
// because there is nothing to recover and the second because the caller should retry a failed
// Step or cancel first. A non-terminal but recoverable Operation (requires_attention) is driven
// terminal first — best-effort canceling any still-live parked execution and forcing the
// projection canceled — so the replacement can claim the same targets instead of being rejected
// as busy. Secret storage is required; without it Rerun returns ErrInvalidOperation.
func (s *WorkflowService) Rerun(ctx context.Context, id, requestedBy string) (*WorkflowItem, error) {
	old, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	switch {
	case old.Status == operationdomain.WorkflowSucceeded:
		return nil, fmt.Errorf("%w: a succeeded Operation has nothing to rerun", operationdomain.ErrWorkflowControlConflict)
	case !old.Status.Terminal() && old.Status != operationdomain.WorkflowRequiresAttention:
		return nil, fmt.Errorf("%w: the Operation is still running; retry a failed Step or cancel it first", operationdomain.ErrWorkflowControlConflict)
	}
	if s.secrets == nil {
		return nil, fmt.Errorf("%w: secret storage is unavailable", ErrInvalidOperation)
	}

	// Free the original's hold on its targets before creating the replacement. A
	// requires_attention Operation is non-terminal, so Create would reject the new one with
	// ErrTargetsBusy until the original finishes. Best-effort cancel stops a still-live parked
	// execution; a lost execution surfaces a control conflict we tolerate. Then force the
	// projection terminal so the targets are free for the same-request Create below.
	now := time.Now().UTC()
	if !old.Status.Terminal() {
		if cancelErr := s.controller.Cancel(ctx, old); cancelErr != nil && !errors.Is(cancelErr, operationdomain.ErrWorkflowControlConflict) {
			return nil, cancelErr
		}
		if err := s.finalizeSuperseded(ctx, old, now); err != nil {
			return nil, err
		}
	}

	newID := uuid.NewString()
	references, err := s.secrets.CloneForOperation(ctx, old.ID, newID)
	if err != nil {
		return nil, fmt.Errorf("clone operation secrets: %w", err)
	}
	item, err := s.Create(ctx, CreateWorkflowInput{
		ID: newID, Kind: old.Kind,
		IntentSummary:  rerunSummary(old),
		IntentSnapshot: cloneMap(old.Intent),
		Definition:     old.Definition, DefinitionVersion: old.DefinitionVersion,
		SiteID: old.SiteID, PlatformID: old.PlatformID,
		TargetServerIDs:    append([]string(nil), old.TargetServerIDs...),
		TargetResources:    append([]operationdomain.ResourceReference(nil), old.TargetResources...),
		Steps:              cloneStepsForRerun(old.Steps, references),
		RetryOfOperationID: old.ID,
		RequestedBy:        requestedBy,
	})
	if err != nil {
		// Roll back the just-cloned secrets so a rejected rerun (for example a target that
		// became busy again) leaves no sealed values keyed to an Operation that was never
		// created.
		_ = s.secrets.DeleteForOperation(ctx, newID)
		return nil, err
	}
	return item, nil
}

// finalizeSuperseded drives a non-terminal Operation to canceled so a rerun can immediately
// claim the same targets. It mirrors the workflow's own cancel projection: the Operation
// becomes canceled and every non-terminal Step becomes canceled, so a superseded Operation
// stops reporting active Steps. The rerun links back to it through RetryOfOperationID.
func (s *WorkflowService) finalizeSuperseded(ctx context.Context, operation *operationdomain.Workflow, at time.Time) error {
	if err := s.operations.UpdateState(ctx, operation.ID, operationdomain.WorkflowCanceled, "Superseded by a rerun.", nil, &at); err != nil {
		return err
	}
	for _, step := range operation.Steps {
		switch step.Status {
		case operationdomain.TaskPending, operationdomain.TaskWaitingDependency,
			operationdomain.TaskWaitingExternal, operationdomain.TaskRunning,
			operationdomain.TaskRequiresAttention:
			step.Status = operationdomain.TaskCanceled
			step.FinishedAt = &at
			if err := s.operations.UpdateStep(ctx, operation.ID, step); err != nil {
				return err
			}
		}
	}
	return nil
}

// cloneStepsForRerun copies an Operation's Steps for a fresh execution. Succeeded and skipped
// Steps are preserved so their side effects are not repeated (the workflow's ready/complete
// scan treats them as satisfied dependencies), and every other Step is reset to a clean pending
// attempt with its error, timing, and external-execution correlation cleared so only the
// incomplete work runs again. Secret references are remapped through references (source ->
// clone) so the new Operation resolves its own sealed copies; a reference missing from the map
// is kept as-is.
func cloneStepsForRerun(source []operationdomain.Task, references map[string]string) []operationdomain.Task {
	steps := make([]operationdomain.Task, len(source))
	for index, step := range source {
		clone := step
		clone.DependsOn = append([]string(nil), step.DependsOn...)
		clone.Targets = append([]operationdomain.ResourceReference(nil), step.Targets...)
		clone.Artifacts = append([]operationdomain.ArtifactMetadata(nil), step.Artifacts...)
		if len(step.SecretRefs) > 0 {
			remapped := make(map[string]string, len(step.SecretRefs))
			for name, reference := range step.SecretRefs {
				if mapped, ok := references[reference]; ok {
					remapped[name] = mapped
				} else {
					remapped[name] = reference
				}
			}
			clone.SecretRefs = remapped
		}
		if step.Status != operationdomain.TaskSucceeded && step.Status != operationdomain.TaskSkipped {
			clone.Status = operationdomain.TaskPending
			clone.Attempt = 0
			clone.Progress = 0
			clone.WaitingReason = ""
			clone.Error = nil
			clone.StartedAt = nil
			clone.FinishedAt = nil
			clone.ExternalExecution = nil
		}
		steps[index] = clone
	}
	return steps
}

// rerunSummary keeps the original operator-facing summary so the recovery Operation reads as
// the same intent. It falls back to a generated line only when the original summary is missing.
func rerunSummary(old *operationdomain.Workflow) string {
	if summary, ok := old.Intent["summary"].(string); ok && strings.TrimSpace(summary) != "" {
		return summary
	}
	return "Rerun of operation " + old.ID
}

func toWorkflowItem(operation *operationdomain.Workflow) WorkflowItem {
	summary, _ := operation.Intent["summary"].(string)
	playbook := ""
	runID := operation.Temporal.RunID
	if len(operation.Steps) == 1 {
		playbook = operation.Steps[0].Kind
		if operation.Steps[0].ExternalExecution != nil {
			runID = operation.Steps[0].ExternalExecution.ID
		}
	}
	return WorkflowItem{
		ID: operation.ID, SchemaVersion: 3, Kind: string(operation.Kind), Intent: summary,
		IntentSnapshot: redactSensitiveMap(cloneMap(operation.Intent)), Definition: operation.Definition,
		DefinitionVersion: operation.DefinitionVersion, Status: string(operation.Status),
		StatusReason: optionalString(operation.StatusReason), StartState: operation.StartState,
		Temporal: operation.Temporal, SiteID: operation.SiteID,
		PlatformID: optionalString(operation.PlatformID), ClusterID: optionalString(operation.PlatformID),
		TargetResources: append([]operationdomain.ResourceReference(nil), operation.TargetResources...),
		TargetServerIDs: append([]string(nil), operation.TargetServerIDs...), Steps: publicSteps(operation.Steps),
		RetryOfOperationID: optionalString(operation.RetryOfOperationID), RequestedBy: operation.RequestedBy,
		RequestCorrelation: optionalString(operation.RequestCorrelation), RequestedAt: operation.RequestedAt.UTC().Format(time.RFC3339),
		StartedAt: formatOptionalTime(operation.StartedAt), FinishedAt: formatOptionalTime(operation.FinishedAt),
		UpdatedAt: operation.UpdatedAt.UTC().Format(time.RFC3339),
		Execution: ExecutionItem{RunID: runID, Playbook: playbook, Status: string(operation.Status),
			StatusReason: optionalString(operation.StatusReason), StartedAt: formatOptionalTime(operation.StartedAt), FinishedAt: formatOptionalTime(operation.FinishedAt)},
	}
}

// publicSteps strips opaque secret references and normalizes nil collections before
// crossing the HTTP boundary. Historical schema-v3 records may contain BSON null slices,
// but the published JSON contract promises arrays so consumers can iterate safely.
func publicSteps(source []operationdomain.Task) []operationdomain.Task {
	steps := append([]operationdomain.Task(nil), source...)
	for index := range steps {
		steps[index].SecretRefs = nil
		// Step Parameters hold the frozen internal request and extraVars, which can carry
		// operator-supplied plaintext (cloud-init, credentials). They are an executor
		// input, not part of the public Step contract, so they never cross the API.
		steps[index].Parameters = nil
		steps[index].DependsOn = append([]string{}, steps[index].DependsOn...)
		steps[index].Targets = append([]operationdomain.ResourceReference{}, steps[index].Targets...)
		steps[index].Artifacts = append([]operationdomain.ArtifactMetadata{}, steps[index].Artifacts...)
	}
	return steps
}

// sensitiveKeys are dropped from operator-facing intent snapshots at any nesting depth.
// Secrets are sealed and referenced opaquely elsewhere; these keys can still hold
// operator-supplied plaintext that must never appear in an API response or (via the
// intent snapshot) in workflow history.
var sensitiveKeys = map[string]bool{
	"extravars": true, "userdata": true, "secret": true, "secrets": true,
	"password": true, "token": true, "credential": true, "credentials": true,
	"privatekey": true, "sudopassword": true,
}

// redactSensitiveMap returns a deep copy of value with sensitive keys removed at any depth.
func redactSensitiveMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		if sensitiveKeys[strings.ToLower(key)] {
			continue
		}
		result[key] = redactSensitiveValue(item)
	}
	return result
}

func redactSensitiveValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactSensitiveMap(typed)
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = redactSensitiveValue(item)
		}
		return out
	default:
		return value
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

// IsNotV3 allows delivery to fall back to the permanent v2 compatibility reader.
func IsNotV3(err error) bool { return errors.Is(err, operationdomain.ErrWorkflowNotV3) }
