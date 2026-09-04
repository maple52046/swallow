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

// OrchestrationController sends control requests to the durable workflow engine. It does
// not update Mongo state directly; the workflow remains the only status writer.
type OrchestrationController interface {
	Cancel(ctx context.Context, operation *operationdomain.OperationV3) error
	RetryStep(ctx context.Context, operation *operationdomain.OperationV3, stepID string) error
}

// CreateOrchestrationInput is trusted server-side intent. Secret values are represented by
// opaque references before they enter this boundary.
type CreateOrchestrationInput struct {
	ID                 string
	Kind               operationdomain.OperationKind
	IntentSummary      string
	IntentSnapshot     map[string]any
	Definition         string
	DefinitionVersion  int
	SiteID             string
	PlatformID         string
	TargetServerIDs    []string
	TargetResources    []operationdomain.ResourceReference
	Steps              []operationdomain.OperationStep
	RetryOfOperationID string
	RequestedBy        string
	RequestCorrelation string
	SecretStepID       string
	SecretStepIDs      []string
	SecretValues       map[string]any
	StepSecretValues   map[string]map[string]any
}

// OperationV3Item is the public orchestration projection. Execution remains as a
// compatibility summary for clients written against Operation v2.
type OperationV3Item struct {
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
	Steps              []operationdomain.OperationStep     `json:"steps"`
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

// OrchestrationService accepts durable workflows and exposes their query projection.
type OrchestrationService struct {
	operations operationdomain.OrchestrationRepository
	controller OrchestrationController
	secrets    operationdomain.OperationSecretRepository
	leases     operationdomain.ResourceLeaseReader
}

func NewOrchestrationService(operations operationdomain.OrchestrationRepository, controller OrchestrationController, secrets ...operationdomain.OperationSecretRepository) *OrchestrationService {
	service := &OrchestrationService{operations: operations, controller: controller}
	if len(secrets) > 0 {
		service.secrets = secrets[0]
	}
	return service
}

// AttachLeaseReader adds active fencing diagnostics without coupling command creation to Mongo.
func (s *OrchestrationService) AttachLeaseReader(reader operationdomain.ResourceLeaseReader) {
	s.leases = reader
}

// Create persists pending intent before the starter attempts to contact Temporal.
func (s *OrchestrationService) Create(ctx context.Context, input CreateOrchestrationInput) (*OperationV3Item, error) {
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
		_, total, err := s.operations.List(ctx, operationdomain.OrchestrationFilter{ServerID: serverID, ActiveOnly: true, Limit: 1})
		if err != nil {
			return nil, err
		}
		if total > 0 {
			return nil, fmt.Errorf("%w: Server %s already has active durable work", ErrInvalidOperation, serverID)
		}
	}

	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	} else if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("%w: id must be a UUID", ErrInvalidOperation)
	}
	steps := append([]operationdomain.OperationStep(nil), input.Steps...)
	for index := range steps {
		steps[index].Status = operationdomain.StepPending
		steps[index].Attempt = 1
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
	operation := &operationdomain.OperationV3{
		ID: id, SchemaVersion: 3, Kind: input.Kind,
		Intent: cloneMap(input.IntentSnapshot), Definition: input.Definition,
		DefinitionVersion: input.DefinitionVersion,
		Status:            operationdomain.OrchestrationPending,
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
	item := toOperationV3Item(operation)
	return &item, nil
}

func validateSteps(steps []operationdomain.OperationStep) error {
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
	byID := map[string]operationdomain.OperationStep{}
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

func (s *OrchestrationService) Get(ctx context.Context, id string) (*OperationV3Item, error) {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := toOperationV3Item(operation)
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
func (s *OrchestrationService) List(ctx context.Context, input ListOperationsInput) ([]OperationV3Item, int, error) {
	items, total, err := s.operations.List(ctx, operationdomain.OrchestrationFilter{
		SiteID: input.SiteID, PlatformID: input.PlatformID, ServerID: input.ServerID,
		Kind: operationdomain.OperationKind(input.Kind), Status: operationdomain.OrchestrationStatus(input.Status),
		ActiveOnly: input.Active, Offset: input.Page.Offset(), Limit: input.Page.PageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	result := make([]OperationV3Item, len(items))
	for index, item := range items {
		result[index] = toOperationV3Item(item)
	}
	return result, total, nil
}

func (s *OrchestrationService) Timeline(ctx context.Context, id string) ([]operationdomain.TimelineEvent, error) {
	if _, err := s.operations.FindByID(ctx, id); err != nil {
		return nil, err
	}
	return s.operations.Timeline(ctx, id)
}

func (s *OrchestrationService) Cancel(ctx context.Context, id string) error {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if operation.Status.Terminal() {
		return fmt.Errorf("%w: a finished Operation cannot be canceled", operationdomain.ErrOperationControlConflict)
	}
	return s.controller.Cancel(ctx, operation)
}

func (s *OrchestrationService) RetryStep(ctx context.Context, id, stepID string) error {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return err
	}
	for _, step := range operation.Steps {
		if step.ID != stepID {
			continue
		}
		if (step.Status != operationdomain.StepFailed && step.Status != operationdomain.StepRequiresAttention) || step.Error == nil || !step.Error.Retryable {
			return operationdomain.ErrStepRetryUnsafe
		}
		return s.controller.RetryStep(ctx, operation, stepID)
	}
	return operationdomain.ErrStepNotFound
}

func toOperationV3Item(operation *operationdomain.OperationV3) OperationV3Item {
	summary, _ := operation.Intent["summary"].(string)
	playbook := ""
	runID := operation.Temporal.RunID
	if len(operation.Steps) == 1 {
		playbook = operation.Steps[0].Kind
		if operation.Steps[0].ExternalExecution != nil {
			runID = operation.Steps[0].ExternalExecution.ID
		}
	}
	return OperationV3Item{
		ID: operation.ID, SchemaVersion: 3, Kind: string(operation.Kind), Intent: summary,
		IntentSnapshot: cloneMap(operation.Intent), Definition: operation.Definition,
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
func publicSteps(source []operationdomain.OperationStep) []operationdomain.OperationStep {
	steps := append([]operationdomain.OperationStep(nil), source...)
	for index := range steps {
		steps[index].SecretRefs = nil
		steps[index].DependsOn = append([]string{}, steps[index].DependsOn...)
		steps[index].Targets = append([]operationdomain.ResourceReference{}, steps[index].Targets...)
		steps[index].Artifacts = append([]operationdomain.ArtifactMetadata{}, steps[index].Artifacts...)
	}
	return steps
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
func IsNotV3(err error) bool { return errors.Is(err, operationdomain.ErrOperationNotV3) }
