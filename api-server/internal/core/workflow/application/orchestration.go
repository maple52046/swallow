package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/maple52046/swallow/internal/core/store"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// WorkflowController sends control requests to the durable workflow engine. It does
// not update Mongo state directly; the workflow remains the only status writer.
type WorkflowController interface {
	Cancel(ctx context.Context, operation *workflowdomain.Workflow) error
	RetryStep(ctx context.Context, operation *workflowdomain.Workflow, stepID string) error
}

// CreateWorkflowInput is trusted server-side intent. Secret values are represented by
// opaque references before they enter this boundary.
type CreateWorkflowInput struct {
	ID                string
	Kind              workflowdomain.WorkflowKind
	IntentSummary     string
	IntentSnapshot    map[string]any
	Definition        string
	DefinitionVersion int
	SiteID            string
	PlatformID        string
	TargetServerIDs   []string
	TargetResources   []workflowdomain.ResourceReference
	Steps             []workflowdomain.Task
	// Jobs declares every Job the Steps name, with its retry granularity (decision 072).
	Jobs               []workflowdomain.JobDeclaration
	RetryOfOperationID string
	RequestedBy        string
	RequestCorrelation string
	SecretStepID       string
	SecretStepIDs      []string
	SecretValues       map[string]any
	StepSecretValues   map[string]map[string]any
	// SupersededOperationIDs names unfinished Operations on the targets whose cancellation the
	// caller has just requested, so the target-busy check ignores them (decision 061). The new
	// Workflow still cannot act before they end: it waits for their resource leases.
	SupersededOperationIDs []string
}

// WorkflowItem is the public orchestration projection. Execution remains as a
// compatibility summary for clients written against Operation v2.
type WorkflowItem struct {
	ID                 string                             `json:"id"`
	SchemaVersion      int                                `json:"schemaVersion"`
	Kind               string                             `json:"kind"`
	Intent             string                             `json:"intent"`
	IntentSnapshot     map[string]any                     `json:"intentSnapshot"`
	Definition         string                             `json:"definition"`
	DefinitionVersion  int                                `json:"definitionVersion"`
	Status             string                             `json:"status"`
	StatusReason       *string                            `json:"statusReason"`
	StartState         string                             `json:"startState"`
	Temporal           workflowdomain.TemporalReference   `json:"temporal"`
	SiteID             string                             `json:"siteId"`
	PlatformID         *string                            `json:"platformId"`
	ClusterID          *string                            `json:"clusterId"`
	TargetResources    []workflowdomain.ResourceReference `json:"targetResources"`
	TargetServerIDs    []string                           `json:"targetServerIds"`
	Steps              []workflowdomain.Task              `json:"steps"`
	Leases             []WorkflowLeaseItem                `json:"leases"`
	RetryOfOperationID *string                            `json:"retryOfOperationId"`
	RequestedBy        string                             `json:"requestedBy"`
	RequestCorrelation *string                            `json:"requestCorrelation"`
	RequestedAt        string                             `json:"requestedAt"`
	StartedAt          *string                            `json:"startedAt"`
	FinishedAt         *string                            `json:"finishedAt"`
	UpdatedAt          string                             `json:"updatedAt"`
	Execution          ExecutionItem                      `json:"execution"`
}

// WorkflowLeaseItem is one resource a Workflow owns, as the API reports it: the owner is the
// Workflow id and the fencing token its dispatch generation (decision 072).
type WorkflowLeaseItem struct {
	ResourceKey  string    `json:"resourceKey"`
	Owner        string    `json:"owner"`
	FencingToken int64     `json:"fencingToken"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// WorkflowService accepts durable workflows and exposes their query projection.
//
// Accepting a Workflow writes its sealed secrets, the Workflow record, and its first timeline
// event in one store transaction, so a failure leaves none of them behind (decision 069). A caller
// that persists its own records with the Workflow (a Platform deployment, a Software
// Assignment) calls Create inside its own transaction, which Create joins.
type WorkflowService struct {
	transactor    store.Transactor
	operations    workflowdomain.WorkflowRepository
	catalog       workflowdomain.PlanCatalog
	controller    WorkflowController
	secrets       workflowdomain.OperationSecretRepository
	leases        workflowdomain.ResourceLeaseReader
	ownership     workflowdomain.OwnershipRepository
	leaseReleaser workflowdomain.ResourceLeaseReleaser
}

// NewWorkflowService builds the service. transactor is required: every multi-document write it
// makes runs through it. catalog validates every accepted plan (decision 072); a zero catalog
// accepts any plan and exists only for tests. secrets is optional for compositions that never
// create a Workflow with secret values or rerun one.
func NewWorkflowService(transactor store.Transactor, operations workflowdomain.WorkflowRepository, catalog workflowdomain.PlanCatalog, secrets ...workflowdomain.OperationSecretRepository) *WorkflowService {
	service := &WorkflowService{transactor: transactor, operations: operations, catalog: catalog}
	if len(secrets) > 0 {
		service.secrets = secrets[0]
	}
	return service
}

// AttachLegacyController lets Cancel, RetryStep, and Rerun control Workflows that run on the
// driver before decision 072; Workflows on the current driver are controlled through stored
// commands and need no controller.
func (s *WorkflowService) AttachLegacyController(controller WorkflowController) {
	s.controller = controller
}

// AttachLeaseReader adds active fencing diagnostics without coupling command creation to Mongo.
func (s *WorkflowService) AttachLeaseReader(reader workflowdomain.ResourceLeaseReader) {
	s.leases = reader
}

// AttachOwnershipReader lets Get report the resources a Workflow on the current driver owns.
func (s *WorkflowService) AttachOwnershipReader(ownership workflowdomain.OwnershipRepository) {
	s.ownership = ownership
}

// AttachLeaseReleaser lets Cancel and Rerun free the leases of a Workflow whose execution is
// gone. Without it the projection is still closed, but the leases stay until they expire.
func (s *WorkflowService) AttachLeaseReleaser(releaser workflowdomain.ResourceLeaseReleaser) {
	s.leaseReleaser = releaser
}

// Create persists pending intent before the starter attempts to contact Temporal. The target-busy
// check, the sealed secrets, the Workflow record, and its operation_requested event share one
// transaction: either the Workflow is accepted with everything it needs, or nothing is written.
func (s *WorkflowService) Create(ctx context.Context, input CreateWorkflowInput) (*WorkflowItem, error) {
	var item *WorkflowItem
	err := s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		created, err := s.create(ctx, input)
		item = created
		return err
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

// create is Create's unit of work; it must run inside a transaction.
func (s *WorkflowService) create(ctx context.Context, input CreateWorkflowInput) (*WorkflowItem, error) {
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
	if s.catalog.TaskKinds != nil {
		if err := s.catalog.Validate(input.Steps, input.Jobs); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidOperation, err)
		}
	}
	superseded := make(map[string]bool, len(input.SupersededOperationIDs))
	for _, operationID := range input.SupersededOperationIDs {
		superseded[operationID] = true
	}
	for _, serverID := range input.TargetServerIDs {
		limit := 1
		if len(superseded) > 0 {
			limit = 100
		}
		active, total, err := s.operations.List(ctx, workflowdomain.WorkflowFilter{ServerID: serverID, ActiveOnly: true, Limit: limit})
		if err != nil {
			return nil, err
		}
		blocking := total
		var holder *workflowdomain.Workflow
		for _, operation := range active {
			if superseded[operation.ID] {
				blocking--
			} else if holder == nil {
				holder = operation
			}
		}
		if blocking > 0 {
			// A target already inside an unfinished Operation is a conflict, not a
			// malformed request: it is exactly workflowdomain.ErrTargetsBusy, which
			// every delivery mapper renders as 409 with this message. Classifying it
			// as ErrInvalidOperation would leak it as an opaque 4xx/5xx that hides the
			// actionable "the Server is busy" reason from operators, and naming the
			// holder tells them which Workflow to finish or cancel.
			if holder == nil {
				return nil, fmt.Errorf("%w: Server %s already has active durable work", workflowdomain.ErrTargetsBusy, serverID)
			}
			return nil, fmt.Errorf("%w: Server %s already has active durable work: Workflow %s (%q, %s) still holds it; finish or cancel it first",
				workflowdomain.ErrTargetsBusy, serverID, holder.ID, rerunSummary(holder), holder.Status)
		}
	}

	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	} else if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("%w: id must be a UUID", ErrInvalidOperation)
	}
	steps := append([]workflowdomain.Task(nil), input.Steps...)
	for index := range steps {
		// A recovery rerun (see Rerun) clones an Operation's Steps: it carries the succeeded (or
		// skipped) Steps that must not repeat, which the fresh execution treats as satisfied
		// dependencies, and resets the rest to pending. A convergent Step it re-runs keeps its
		// next attempt number, so its executor observes before acting the way an explicit retry
		// does; every other pending Step starts at attempt 1. A normal launch supplies Steps with
		// no status and attempt 0 or 1, so it gets a pending first attempt, unchanged.
		if steps[index].Status != workflowdomain.TaskSucceeded && steps[index].Status != workflowdomain.TaskSkipped {
			steps[index].Status = workflowdomain.TaskPending
		}
		if steps[index].Attempt < 1 {
			steps[index].Attempt = 1
		}
		if steps[index].Artifacts == nil {
			steps[index].Artifacts = []workflowdomain.ArtifactMetadata{}
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
				return nil, err
			}
			steps[stepIndex].SecretRefs[name] = reference
		}
	}
	resources := append([]workflowdomain.ResourceReference(nil), input.TargetResources...)
	for _, serverID := range input.TargetServerIDs {
		resources = append(resources, workflowdomain.ResourceReference{Kind: "server", ID: serverID})
	}
	resources = uniqueResources(resources)
	operation := &workflowdomain.Workflow{
		ID: id, SchemaVersion: 3, Kind: input.Kind,
		Intent: cloneMap(input.IntentSnapshot), Definition: input.Definition,
		DefinitionVersion: input.DefinitionVersion,
		Status:            workflowdomain.WorkflowPending,
		StartState:        "pending",
		Temporal:          workflowdomain.TemporalReference{WorkflowID: "swallow-operation/" + id},
		SiteID:            input.SiteID, PlatformID: input.PlatformID,
		TargetResources: resources, TargetServerIDs: append([]string(nil), input.TargetServerIDs...),
		Steps: steps, Jobs: append([]workflowdomain.JobDeclaration(nil), input.Jobs...),
		Dispatch:           workflowdomain.Dispatch{Driver: workflowdomain.DriverVersion},
		RetryOfOperationID: input.RetryOfOperationID,
		RequestedBy:        input.RequestedBy, RequestCorrelation: input.RequestCorrelation,
		RequestedAt: now, UpdatedAt: now,
	}
	operation.Intent["summary"] = strings.TrimSpace(input.IntentSummary)
	if err := s.operations.Create(ctx, operation); err != nil {
		return nil, err
	}
	if err := s.operations.AppendEvent(ctx, workflowdomain.TimelineEvent{
		ID: uuid.NewString(), OperationID: operation.ID, Type: "operation_requested",
		Message: "Operation accepted and waiting for durable workflow start.", CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("record workflow %s requested: %w", operation.ID, err)
	}
	item := toWorkflowItem(operation)
	return &item, nil
}

func validateSteps(steps []workflowdomain.Task) error {
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
	byID := map[string]workflowdomain.Task{}
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

func uniqueResources(resources []workflowdomain.ResourceReference) []workflowdomain.ResourceReference {
	seen := map[string]bool{}
	result := make([]workflowdomain.ResourceReference, 0, len(resources))
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
	if s.ownership != nil && operation.Dispatch.Driver == workflowdomain.DriverVersion {
		owned, err := s.ownership.FindByOwner(ctx, operation.ID)
		if err != nil {
			return nil, err
		}
		item.Leases = make([]WorkflowLeaseItem, len(owned))
		for index, resource := range owned {
			item.Leases[index] = WorkflowLeaseItem{
				ResourceKey: resource.ResourceKey, Owner: operation.ID,
				FencingToken: operation.Dispatch.Generation, UpdatedAt: resource.AcquiredAt,
			}
		}
		return &item, nil
	}
	if s.leases != nil {
		leases, leaseErr := s.leases.FindByOwner(ctx, operation.Temporal.WorkflowID)
		if leaseErr != nil {
			return nil, leaseErr
		}
		item.Leases = make([]WorkflowLeaseItem, len(leases))
		for index, lease := range leases {
			item.Leases[index] = WorkflowLeaseItem{
				ResourceKey: lease.ResourceKey, Owner: operation.ID,
				FencingToken: lease.FencingToken, UpdatedAt: lease.UpdatedAt,
			}
		}
	}
	return &item, nil
}

// List returns v3 Operations using the canonical orchestration status axis.
func (s *WorkflowService) List(ctx context.Context, input ListOperationsInput) ([]WorkflowItem, int, error) {
	items, total, err := s.operations.List(ctx, workflowdomain.WorkflowFilter{
		SiteID: input.SiteID, PlatformID: input.PlatformID, ServerID: input.ServerID,
		Kind: workflowdomain.WorkflowKind(input.Kind), Status: workflowdomain.WorkflowStatus(input.Status),
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

func (s *WorkflowService) Timeline(ctx context.Context, id string) ([]workflowdomain.TimelineEvent, error) {
	if _, err := s.operations.FindByID(ctx, id); err != nil {
		return nil, err
	}
	return s.operations.Timeline(ctx, id)
}

// Cancel asks the Workflow's execution to cancel; the workflow then records canceling and
// canceled itself. A finished Workflow is a control conflict.
//
// When the execution is already gone (ErrWorkflowExecutionGone) no workflow code will ever record
// the outcome, so Cancel records it here: in one transaction, the leases the dead execution still
// holds are released and the projection is closed as canceled with
// CanceledWithoutExecutionReason. A failure therefore leaves the Workflow cancelable again, never
// canceled with its Servers still leased. Without this a Workflow whose execution Temporal had
// terminated stayed non-terminal forever and blocked every new Workflow on its Servers.
func (s *WorkflowService) Cancel(ctx context.Context, id string) error {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if operation.Status.Terminal() {
		return fmt.Errorf("%w: a finished Operation cannot be canceled", workflowdomain.ErrWorkflowControlConflict)
	}
	if operation.Dispatch.Driver == workflowdomain.DriverVersion {
		return s.operations.RequestCancel(ctx, id, workflowdomain.CancelCommand{
			Reason: workflowdomain.CanceledByOperatorReason, RequestedAt: time.Now().UTC(),
		})
	}
	err = s.controller.Cancel(ctx, operation)
	if !errors.Is(err, workflowdomain.ErrWorkflowExecutionGone) {
		return err
	}
	return s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if err := ReleaseExecutionLeases(ctx, s.leaseReleaser, operation); err != nil {
			return err
		}
		return FinalizeCanceledProjection(ctx, s.operations, operation, CanceledWithoutExecutionReason, time.Now().UTC())
	})
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
		if (step.Status != workflowdomain.TaskFailed && step.Status != workflowdomain.TaskRequiresAttention) || step.Error == nil || !step.Error.Retryable {
			return workflowdomain.ErrTaskRetryUnsafe
		}
		if operation.Dispatch.Driver == workflowdomain.DriverVersion {
			return s.operations.RequestRetry(ctx, id, workflowdomain.RetryCommand{TaskID: stepID, RequestedAt: time.Now().UTC()})
		}
		return s.controller.RetryStep(ctx, operation, stepID)
	}
	return workflowdomain.ErrTaskNotFound
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
//
// The cancel request to Temporal happens before, and outside, the transaction. Everything that
// follows — releasing a lost execution's leases, closing the original's projection, cloning its
// secrets, and accepting the replacement — is one transaction, so a rejected rerun (for example a
// target that became busy again) leaves the original exactly as it was and no orphaned secrets.
func (s *WorkflowService) Rerun(ctx context.Context, id, requestedBy string) (*WorkflowItem, error) {
	old, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	switch {
	case old.Status == workflowdomain.WorkflowSucceeded:
		return nil, fmt.Errorf("%w: a succeeded Operation has nothing to rerun", workflowdomain.ErrWorkflowControlConflict)
	case !old.Status.Terminal() && old.Status != workflowdomain.WorkflowRequiresAttention:
		return nil, fmt.Errorf("%w: the Operation is still running; retry a failed Step or cancel it first", workflowdomain.ErrWorkflowControlConflict)
	}
	if s.secrets == nil {
		return nil, fmt.Errorf("%w: secret storage is unavailable", ErrInvalidOperation)
	}

	// Free the original's hold on its targets before creating the replacement. A
	// requires_attention Operation is non-terminal, so Create would reject the new one with
	// ErrTargetsBusy until the original finishes. Best-effort cancel stops a still-live parked
	// execution; a lost execution surfaces a control conflict we tolerate. Then force the
	// projection terminal so the targets are free for the same-request Create below. Leases are
	// released here only for a lost execution: a live one still needs them for its own cancel
	// path (for example a verification's compensation) and releases them as it exits.
	now := time.Now().UTC()
	if old.Dispatch.Driver == workflowdomain.DriverVersion {
		return s.rerunSuperseding(ctx, old, requestedBy, now)
	}
	executionGone := false
	if !old.Status.Terminal() {
		cancelErr := s.controller.Cancel(ctx, old)
		if cancelErr != nil && !errors.Is(cancelErr, workflowdomain.ErrWorkflowControlConflict) {
			return nil, cancelErr
		}
		executionGone = errors.Is(cancelErr, workflowdomain.ErrWorkflowExecutionGone)
	}

	var item *WorkflowItem
	err = s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if !old.Status.Terminal() {
			if executionGone {
				if err := ReleaseExecutionLeases(ctx, s.leaseReleaser, old); err != nil {
					return err
				}
			}
			if err := FinalizeCanceledProjection(ctx, s.operations, old, "Superseded by a rerun.", now); err != nil {
				return err
			}
		}
		newID := uuid.NewString()
		references, err := s.secrets.CloneForOperation(ctx, old.ID, newID)
		if err != nil {
			return fmt.Errorf("clone operation secrets: %w", err)
		}
		item, err = s.create(ctx, CreateWorkflowInput{
			ID: newID, Kind: old.Kind,
			IntentSummary:  rerunSummary(old),
			IntentSnapshot: cloneMap(old.Intent),
			Definition:     old.Definition, DefinitionVersion: old.DefinitionVersion,
			SiteID: old.SiteID, PlatformID: old.PlatformID,
			TargetServerIDs:    append([]string(nil), old.TargetServerIDs...),
			TargetResources:    append([]workflowdomain.ResourceReference(nil), old.TargetResources...),
			Steps:              s.cloneStepsForRerun(old.Steps, references),
			Jobs:               rerunJobs(old),
			RetryOfOperationID: old.ID,
			RequestedBy:        requestedBy,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

// rerunSuperseding reruns a Workflow on the current driver: in one transaction a parked original
// gets a cancel command ("Superseded by a rerun."), its secrets are cloned, and the new Workflow is
// accepted past the busy check. The original runs its own cancel path — compensations included —
// and the new Workflow waits for its resources until that path released them.
func (s *WorkflowService) rerunSuperseding(ctx context.Context, old *workflowdomain.Workflow, requestedBy string, now time.Time) (*WorkflowItem, error) {
	var item *WorkflowItem
	err := s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		var superseded []string
		if !old.Status.Terminal() {
			if err := s.operations.RequestCancel(ctx, old.ID, workflowdomain.CancelCommand{Reason: "Superseded by a rerun.", RequestedAt: now}); err != nil {
				return err
			}
			superseded = []string{old.ID}
		}
		newID := uuid.NewString()
		references, err := s.secrets.CloneForOperation(ctx, old.ID, newID)
		if err != nil {
			return fmt.Errorf("clone operation secrets: %w", err)
		}
		item, err = s.create(ctx, CreateWorkflowInput{
			ID: newID, Kind: old.Kind,
			IntentSummary:  rerunSummary(old),
			IntentSnapshot: cloneMap(old.Intent),
			Definition:     old.Definition, DefinitionVersion: old.DefinitionVersion,
			SiteID: old.SiteID, PlatformID: old.PlatformID,
			TargetServerIDs:        append([]string(nil), old.TargetServerIDs...),
			TargetResources:        append([]workflowdomain.ResourceReference(nil), old.TargetResources...),
			Steps:                  s.cloneStepsForRerun(old.Steps, references),
			Jobs:                   rerunJobs(old),
			RetryOfOperationID:     old.ID,
			RequestedBy:            requestedBy,
			SupersededOperationIDs: superseded,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

// cloneStepsForRerun copies an Operation's Steps for a fresh execution that converges the
// intent to its desired state (decision 060), by each Task kind's registered RerunPolicy. A
// converging Task runs again whatever its earlier outcome, at the attempt after its last one, so
// its Runner first observes the resource and acts only where it has not converged. A
// RerunConvergeOnLiveHosts Task that already succeeded is kept once a Task whose kind releases
// hosts succeeded, because a released host no longer runs the OS it needs. Any other Task keeps
// the earlier rule: a succeeded or skipped one is preserved so its side effects are not repeated
// (the run treats it as a satisfied dependency), and the rest restart at a clean first attempt. A
// re-run Task has its error, timing, and external-execution correlation cleared. Secret
// references are remapped through references (source -> clone); a reference missing from the map
// is kept as-is. With a zero catalog every Task keeps the earlier rule.
func (s *WorkflowService) cloneStepsForRerun(source []workflowdomain.Task, references map[string]string) []workflowdomain.Task {
	hostsReleased := false
	for _, step := range source {
		if s.catalog.TaskKinds[step.Kind].ReleasesHosts && step.Status == workflowdomain.TaskSucceeded {
			hostsReleased = true
			break
		}
	}
	steps := make([]workflowdomain.Task, len(source))
	for index, step := range source {
		clone := step
		clone.DependsOn = append([]string(nil), step.DependsOn...)
		clone.Targets = append([]workflowdomain.ResourceReference(nil), step.Targets...)
		clone.Artifacts = append([]workflowdomain.ArtifactMetadata(nil), step.Artifacts...)
		clone.Generation = 0
		clone.Resumed = false
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
		converges := false
		switch s.catalog.TaskKinds[step.Kind].Rerun {
		case workflowdomain.RerunConverge:
			converges = true
		case workflowdomain.RerunConvergeOnLiveHosts:
			converges = !hostsReleased || step.Status != workflowdomain.TaskSucceeded
		}
		if converges || (step.Status != workflowdomain.TaskSucceeded && step.Status != workflowdomain.TaskSkipped) {
			clone.Status = workflowdomain.TaskPending
			clone.Attempt = 0
			if converges {
				clone.Attempt = max(step.Attempt, 1) + 1
			}
			clone.Progress = 0
			clone.WaitingReason = ""
			clone.Error = nil
			clone.StartedAt = nil
			clone.FinishedAt = nil
			clone.ExternalExecution = nil
			clone.Live = nil
		}
		steps[index] = clone
	}
	return steps
}

// rerunJobs returns the Job declarations of a rerun: the original's, or for a Workflow accepted
// before Jobs were declared, each Job it names retrying as a whole, which is how that driver ran
// Jobs.
func rerunJobs(old *workflowdomain.Workflow) []workflowdomain.JobDeclaration {
	if len(old.Jobs) > 0 {
		return old.Jobs
	}
	var jobs []workflowdomain.JobDeclaration
	seen := map[string]bool{}
	for _, task := range old.Steps {
		if task.Job != "" && !seen[task.Job] {
			seen[task.Job] = true
			jobs = append(jobs, workflowdomain.JobDeclaration{Name: task.Job, Retry: workflowdomain.RetryJob})
		}
	}
	return jobs
}

// rerunSummary keeps the original operator-facing summary so the recovery Operation reads as
// the same intent. It falls back to a generated line only when the original summary is missing.
func rerunSummary(old *workflowdomain.Workflow) string {
	if summary, ok := old.Intent["summary"].(string); ok && strings.TrimSpace(summary) != "" {
		return summary
	}
	return "Rerun of operation " + old.ID
}

func toWorkflowItem(operation *workflowdomain.Workflow) WorkflowItem {
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
		TargetResources: append([]workflowdomain.ResourceReference(nil), operation.TargetResources...),
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
func publicSteps(source []workflowdomain.Task) []workflowdomain.Task {
	steps := append([]workflowdomain.Task(nil), source...)
	for index := range steps {
		steps[index].SecretRefs = nil
		// Step Parameters hold the frozen internal request and extraVars, which can carry
		// operator-supplied plaintext (cloud-init, credentials). They are an executor
		// input, not part of the public Step contract, so they never cross the API.
		steps[index].Parameters = nil
		steps[index].DependsOn = append([]string{}, steps[index].DependsOn...)
		steps[index].Targets = append([]workflowdomain.ResourceReference{}, steps[index].Targets...)
		steps[index].Artifacts = append([]workflowdomain.ArtifactMetadata{}, steps[index].Artifacts...)
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
func IsNotV3(err error) bool { return errors.Is(err, workflowdomain.ErrWorkflowNotV3) }
