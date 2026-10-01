package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/pagination"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
	"github.com/maple52046/swallow/internal/shared/wire"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// ExecutionService owns the public v2 operation model.
type ExecutionService struct {
	operations     operationdomain.ExecutionRepository
	servers        serverdomain.ServerRepository
	configurations operationdomain.AutomationConfigurationRepository
	catalog        operationdomain.PlaybookCatalog
	runner         operationdomain.Runner
	// events is the durable stream of per-task Ansible events. EventsForRun reads it so a
	// durable Step's task list is available live during a run, independent of the artifact
	// filesystem. Nil falls back to the runner's on-disk events (post-hoc only).
	events     operationdomain.AnsibleEventRepository
	protection serverdomain.MutationGuard
	policy     PolicyChecker
	durable    *WorkflowService
}

// NewExecutionService constructs the embedded-execution use case.
func NewExecutionService(
	operations operationdomain.ExecutionRepository,
	servers serverdomain.ServerRepository,
	configurations operationdomain.AutomationConfigurationRepository,
	catalog operationdomain.PlaybookCatalog,
	runner operationdomain.Runner,
	events operationdomain.AnsibleEventRepository,
	policy PolicyChecker,
	protection ...serverdomain.MutationGuard,
) *ExecutionService {
	service := &ExecutionService{
		operations: operations, servers: servers, configurations: configurations,
		catalog: catalog, runner: runner, events: events, policy: policy,
	}
	if len(protection) > 0 {
		service.protection = protection[0]
	}
	return service
}

// AttachWorkflow moves newly accepted automation to Operation v3 while the
// compatibility dispatcher drains persisted v2 work.
func (s *ExecutionService) AttachWorkflow(durable *WorkflowService) { s.durable = durable }

// ExecutionOperationItem is the public operation representation.
type ExecutionOperationItem struct {
	SchemaVersion      int                    `json:"schemaVersion"`
	Steps              []operationdomain.Task `json:"steps"`
	ID                 string                 `json:"id"`
	Kind               string                 `json:"kind"`
	Intent             string                 `json:"intent"`
	SiteID             string                 `json:"siteId"`
	PlatformID         *string                `json:"platformId"`
	ClusterID          *string                `json:"clusterId"`
	TargetServerIDs    []string               `json:"targetServerIds"`
	RetryOfOperationID *string                `json:"retryOfOperationId"`
	Execution          ExecutionItem          `json:"execution"`
	RequestedBy        string                 `json:"requestedBy"`
	RequestedAt        string                 `json:"requestedAt"`
	UpdatedAt          string                 `json:"updatedAt"`
}

// ExecutionItem contains no controller-specific fields.
type ExecutionItem struct {
	RunID        string  `json:"runId"`
	Playbook     string  `json:"playbook"`
	Status       string  `json:"status"`
	StatusReason *string `json:"statusReason"`
	StartedAt    *string `json:"startedAt"`
	FinishedAt   *string `json:"finishedAt"`
}

// CreateExecutionInput is accepted by POST /operations.
type CreateExecutionInput struct {
	Kind            string
	Intent          string
	TargetServerIDs []string
	PlatformID      string
	PlaybookName    string
	ExtraVars       map[string]any
	// TrustedVars are extra vars the caller is a trusted server-side use case, so they
	// bypass the swallow_ prefix filter that operator-supplied ExtraVars are subject to.
	// A platform deployment uses this to assign node roles the client cannot forge.
	TrustedVars map[string]any
	// SecretVars are sealed at rest and materialised only for the run, for values such as
	// a VRRP password that must not persist in plain text.
	SecretVars         map[string]any
	RetryOfOperationID string
	RequestedBy        string
	RequestCorrelation string
}

// Create validates intent and persists pending state before dispatch.
func (s *ExecutionService) Create(ctx context.Context, input CreateExecutionInput) (*ExecutionOperationItem, error) {
	kind := operationdomain.WorkflowKind(strings.TrimSpace(input.Kind))
	if !kind.Valid() {
		return nil, fmt.Errorf("%w: kind must be one of %v", ErrInvalidOperation, operationdomain.ValidWorkflowKinds)
	}
	if len(input.TargetServerIDs) == 0 {
		return nil, fmt.Errorf("%w: targetServerIds is required", ErrInvalidOperation)
	}
	targets, siteID, err := s.resolveTargets(ctx, kind, input.TargetServerIDs)
	if err != nil {
		return nil, err
	}
	if s.protection != nil {
		if err := s.protection.RequireUnlocked(ctx, input.TargetServerIDs); err != nil {
			return nil, err
		}
	}
	active, err := s.operations.FindActiveByServerIDs(ctx, input.TargetServerIDs)
	if err != nil {
		return nil, err
	}
	if len(active) > 0 {
		ids := make([]string, len(active))
		for i := range active {
			ids[i] = active[i].ID
		}
		sort.Strings(ids)
		return nil, fmt.Errorf("%w: still running in operation(s) %s",
			operationdomain.ErrTargetsBusy, strings.Join(ids, ", "))
	}
	if s.policy != nil {
		if err := s.policy.CheckOperation(ctx, kind, input.PlatformID, input.TargetServerIDs); err != nil {
			return nil, err
		}
	}
	configuration, err := s.configurations.FindBySiteID(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if !configuration.Enabled {
		return nil, operationdomain.ErrAutomationDisabled
	}
	if !configuration.HasCredential {
		return nil, operationdomain.ErrAutomationCredentialMissing
	}

	playbook := strings.TrimSpace(input.PlaybookName)
	if kind == operationdomain.WorkflowKindCustom {
		if playbook == "" {
			return nil, fmt.Errorf("%w: playbookName is required for the custom kind", ErrInvalidOperation)
		}
	} else if playbook == "" {
		playbook = configuration.PlaybookMappings[kind]
	}
	if playbook == "" {
		return nil, fmt.Errorf("%w: no playbook mapping for %s", ErrInvalidOperation, kind)
	}
	if _, err := s.catalog.Resolve(playbook); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	operation := &operationdomain.ExecutionOperation{
		ID: uuid.NewString(), Kind: kind, Intent: strings.TrimSpace(input.Intent),
		SiteID: siteID, PlatformID: input.PlatformID,
		TargetServerIDs:    append([]string(nil), input.TargetServerIDs...),
		SecretVars:         input.SecretVars,
		RetryOfOperationID: input.RetryOfOperationID,
		RequestedBy:        input.RequestedBy, RequestedAt: now, UpdatedAt: now,
	}
	operation.Execution = operationdomain.Execution{
		RunID: uuid.NewString(), Playbook: playbook, Status: operationdomain.StatusPending,
	}
	operation.ExtraVars = buildExecutionExtraVars(operation, targets, input.ExtraVars, input.TrustedVars)
	// Temporal orchestration is the sole execution engine (ADR 016/017). Every accepted
	// custom Workflow is created as a single-Task durable v3 Workflow; the v2 embedded
	// dispatcher write path was removed.
	if s.durable == nil {
		return nil, fmt.Errorf("%w: durable Workflow orchestration is unavailable", ErrInvalidOperation)
	}
	stepTargets := make([]operationdomain.ResourceReference, 0, len(operation.TargetServerIDs))
	for _, serverID := range operation.TargetServerIDs {
		stepTargets = append(stepTargets, operationdomain.ResourceReference{Kind: "server", ID: serverID})
	}
	targetResources := append([]operationdomain.ResourceReference(nil), stepTargets...)
	if operation.PlatformID != "" {
		targetResources = append(targetResources, operationdomain.ResourceReference{Kind: "platform", ID: operation.PlatformID})
	}
	created, err := s.durable.Create(ctx, CreateWorkflowInput{
		Kind: operation.Kind, IntentSummary: operation.Intent, Definition: "ansible-operation", DefinitionVersion: 1,
		SiteID: operation.SiteID, PlatformID: operation.PlatformID, TargetServerIDs: operation.TargetServerIDs,
		TargetResources: targetResources, RequestedBy: operation.RequestedBy,
		RequestCorrelation: input.RequestCorrelation, RetryOfOperationID: operation.RetryOfOperationID,
		IntentSnapshot: map[string]any{"playbook": operation.Execution.Playbook, "extraVars": operation.ExtraVars},
		Steps: []operationdomain.Task{{ID: "ansible", Kind: "ansible-playbook", Name: "Run " + operation.Execution.Playbook,
			Executor: operationdomain.RunnerKindAnsible, Targets: stepTargets, Parameters: map[string]any{"playbook": operation.Execution.Playbook, "extraVars": operation.ExtraVars}}},
		SecretStepID: "ansible", SecretValues: operation.SecretVars,
	})
	if err != nil {
		return nil, err
	}
	return legacyCompatibleV3(created), nil
}

func (s *ExecutionService) resolveTargets(ctx context.Context, kind operationdomain.WorkflowKind, ids []string) ([]*serverdomain.Server, string, error) {
	required := kind.RequiredProvisioningState()
	targets := make([]*serverdomain.Server, 0, len(ids))
	siteID := ""
	for _, id := range ids {
		server, err := s.servers.FindByID(ctx, id)
		if err != nil {
			return nil, "", err
		}
		if siteID == "" {
			siteID = server.Source.SiteID
		}
		if server.Source.SiteID != siteID {
			return nil, "", fmt.Errorf("%w: targets must belong to one site", ErrInvalidOperation)
		}
		if required != "" && (server.Provisioning == nil || server.Provisioning.State != required) {
			return nil, "", fmt.Errorf("%w: %s requires %q",
				operationdomain.ErrTargetStateInvalid, server.DisplayName(), required)
		}
		// Every Operation mutates its target host, regardless of which playbook carries it.
		if s.protection == nil && kind.RefusedWhenLocked() && server.Provisioning != nil && server.Provisioning.Locked {
			return nil, "", fmt.Errorf("%w: Server %q is locked. Unlock it before starting this Operation.",
				operationdomain.ErrTargetLocked, server.DisplayName())
		}
		targets = append(targets, server)
	}
	return targets, siteID, nil
}

// standardExtraVarKeys are the swallow_-prefixed vars Create regenerates for every
// operation. They are listed so a retry can strip them from a carried-over var set and
// have them rebuilt for the new operation, while keeping trusted swallow_-prefixed vars
// such as a deployment's role assignment.
var standardExtraVarKeys = map[string]bool{
	"swallow_operation_id":     true,
	"swallow_operation_kind":   true,
	"swallow_site_id":          true,
	"swallow_server_ids":       true,
	"swallow_platform_id":      true,
	"swallow_server_hostnames": true,
}

// buildExecutionExtraVars assembles a run's extra vars from three sources: the standard
// swallow_ identifiers, operator-supplied vars (which may not use the swallow_ prefix, so
// a client cannot forge a trusted variable), and trusted server-side vars (which may, and
// are used for things like node-role assignment the client must not control).
func buildExecutionExtraVars(operation *operationdomain.ExecutionOperation, targets []*serverdomain.Server, operator, trusted map[string]any) map[string]any {
	vars := map[string]any{
		"swallow_operation_id":   operation.ID,
		"swallow_operation_kind": string(operation.Kind),
		"swallow_site_id":        operation.SiteID,
		"swallow_server_ids":     operation.TargetServerIDs,
	}
	if operation.PlatformID != "" {
		vars["swallow_platform_id"] = operation.PlatformID
	}
	hostnames := make([]string, 0, len(targets))
	for _, target := range targets {
		hostnames = append(hostnames, target.DisplayName())
	}
	vars["swallow_server_hostnames"] = hostnames
	for key, value := range operator {
		if !strings.HasPrefix(key, "swallow_") {
			vars[key] = value
		}
	}
	// Trusted vars are applied last and unfiltered; the standard identifiers above still
	// win, so a caller cannot override the operation's own identity.
	for key, value := range trusted {
		if standardExtraVarKeys[key] {
			continue
		}
		vars[key] = value
	}
	return vars
}

// List returns a page of operations.
func (s *ExecutionService) List(ctx context.Context, input ListOperationsInput) (pagination.Result[ExecutionOperationItem], error) {
	var empty pagination.Result[ExecutionOperationItem]
	result, err := s.operations.List(ctx, operationdomain.ExecutionListFilter{
		SiteID: input.SiteID, PlatformID: input.PlatformID, ServerID: input.ServerID,
		Kind: operationdomain.WorkflowKind(input.Kind), Status: operationdomain.Status(input.Status),
		ActiveOnly: input.Active, Offset: input.Page.Offset(), Limit: input.Page.PageSize,
	})
	if err != nil {
		return empty, err
	}
	items := make([]ExecutionOperationItem, len(result.Operations))
	for i := range result.Operations {
		items[i] = toExecutionOperationItem(result.Operations[i])
	}
	return pagination.NewResult(items, result.Total, input.Page), nil
}

// Get reads an operation.
func (s *ExecutionService) Get(ctx context.Context, id string) (*ExecutionOperationItem, error) {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := toExecutionOperationItem(operation)
	return &item, nil
}

// Logs reads retained local artifacts.
func (s *ExecutionService) Logs(ctx context.Context, id string) (string, error) {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	return s.runner.Logs(ctx, operation.Execution.RunID)
}

// Retry creates a new operation repeating a finished one, linked back to it.
//
// The original is left untouched, so its logs and events remain. The new operation copies
// the kind, targets, platform, playbook, and both the carried extra vars and sealed secret
// vars, so a platform deployment retry keeps the same VRRP password and role assignment
// that the already-installed nodes were built with.
func (s *ExecutionService) Retry(ctx context.Context, id, requestedBy string) (*ExecutionOperationItem, error) {
	original, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !original.Execution.Status.Terminal() {
		return nil, fmt.Errorf("%w: only a finished operation can be retried", ErrInvalidOperation)
	}

	secretVars, err := s.operations.SecretVars(ctx, id)
	if err != nil {
		return nil, err
	}

	// Carry over every var except the standard identifiers, which Create regenerates for
	// the new operation. Passed as trusted so a trusted swallow_-prefixed var survives.
	carried := make(map[string]any, len(original.ExtraVars))
	for key, value := range original.ExtraVars {
		if standardExtraVarKeys[key] {
			continue
		}
		carried[key] = value
	}

	return s.Create(ctx, CreateExecutionInput{
		Kind:               string(original.Kind),
		Intent:             original.Intent,
		TargetServerIDs:    original.TargetServerIDs,
		PlatformID:         original.PlatformID,
		PlaybookName:       original.Execution.Playbook,
		TrustedVars:        carried,
		SecretVars:         secretVars,
		RetryOfOperationID: original.ID,
		RequestedBy:        requestedBy,
	})
}

// OperationEventsItem is the task-level progress of a run.
type OperationEventsItem struct {
	RunID            string          `json:"runId"`
	Status           string          `json:"status"`
	OKCount          int             `json:"okCount"`
	ChangedCount     int             `json:"changedCount"`
	FailedCount      int             `json:"failedCount"`
	UnreachableCount int             `json:"unreachableCount"`
	SkippedCount     int             `json:"skippedCount"`
	Events           []TaskEventItem `json:"events"`
}

// TaskEventItem is one task result on one host.
type TaskEventItem struct {
	Play      string  `json:"play"`
	Task      string  `json:"task"`
	Host      string  `json:"host"`
	Status    string  `json:"status"`
	Changed   bool    `json:"changed"`
	StartedAt *string `json:"startedAt"`
	EndedAt   *string `json:"endedAt"`
}

// Events returns a run's task-level progress, derived from the runner's retained events.
func (s *ExecutionService) Events(ctx context.Context, id string) (*OperationEventsItem, error) {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	events, err := s.runner.Events(ctx, operation.Execution.RunID)
	if err != nil {
		return nil, err
	}

	item := &OperationEventsItem{
		RunID:  operation.Execution.RunID,
		Status: string(operation.Execution.Status),
		Events: make([]TaskEventItem, 0, len(events)),
	}
	for _, event := range events {
		switch event.Status {
		case "ok":
			item.OKCount++
		case "failed":
			item.FailedCount++
		case "unreachable":
			item.UnreachableCount++
		case "skipped":
			item.SkippedCount++
		}
		if event.Changed {
			item.ChangedCount++
		}
		item.Events = append(item.Events, TaskEventItem{
			Play: event.Play, Task: event.Task, Host: event.Host,
			Status: event.Status, Changed: event.Changed,
			StartedAt: optionalTime(event.StartedAt), EndedAt: optionalTime(event.EndedAt),
		})
	}
	return item, nil
}

func legacyCompatibleV3(operation *WorkflowItem) *ExecutionOperationItem {
	return &ExecutionOperationItem{
		SchemaVersion: operation.SchemaVersion, Steps: operation.Steps,
		ID: operation.ID, Kind: operation.Kind, Intent: operation.Intent, SiteID: operation.SiteID,
		PlatformID: operation.PlatformID, ClusterID: operation.ClusterID, TargetServerIDs: operation.TargetServerIDs,
		RetryOfOperationID: operation.RetryOfOperationID, RequestedBy: operation.RequestedBy,
		RequestedAt: operation.RequestedAt, UpdatedAt: operation.UpdatedAt, Execution: operation.Execution,
	}
}

func toExecutionOperationItem(operation *operationdomain.ExecutionOperation) ExecutionOperationItem {
	return ExecutionOperationItem{
		SchemaVersion: 2, Steps: []operationdomain.Task{legacySyntheticStep(operation)},
		ID: operation.ID, Kind: string(operation.Kind), Intent: operation.Intent,
		SiteID: operation.SiteID, PlatformID: wire.String(operation.PlatformID),
		ClusterID:          wire.String(operation.PlatformID),
		TargetServerIDs:    wire.Strings(operation.TargetServerIDs),
		RetryOfOperationID: wire.String(operation.RetryOfOperationID),
		Execution: ExecutionItem{
			RunID: operation.Execution.RunID, Playbook: operation.Execution.Playbook,
			Status: string(operation.Execution.Status), StatusReason: wire.String(operation.Execution.StatusReason),
			StartedAt:  optionalTime(operation.Execution.StartedAt),
			FinishedAt: optionalTime(operation.Execution.FinishedAt),
		},
		RequestedBy: operation.RequestedBy, RequestedAt: wire.Time(operation.RequestedAt),
		UpdatedAt: wire.Time(operation.UpdatedAt),
	}
}

// AutomationConfigurationService owns site-local runner settings.
type AutomationConfigurationService struct {
	configurations operationdomain.AutomationConfigurationRepository
	sites          sitedomain.SiteRepository
	catalog        operationdomain.PlaybookCatalog
	deploymentKeys DeploymentKeySource
}

// NewAutomationConfigurationService constructs site automation use cases.
func NewAutomationConfigurationService(configurations operationdomain.AutomationConfigurationRepository, sites sitedomain.SiteRepository, catalog operationdomain.PlaybookCatalog) *AutomationConfigurationService {
	return &AutomationConfigurationService{configurations: configurations, sites: sites, catalog: catalog}
}

// AttachDeploymentKeys lets reads report CredentialSource deploymentKey for a Site without a
// private-key override. Without it such a Site reports none.
func (s *AutomationConfigurationService) AttachDeploymentKeys(keys DeploymentKeySource) {
	s.deploymentKeys = keys
}

// Get returns settings, credential presence, and the effective credential source; never secrets.
func (s *AutomationConfigurationService) Get(ctx context.Context, siteID string) (*operationdomain.AutomationConfiguration, error) {
	configuration, err := s.configurations.FindBySiteID(ctx, siteID)
	if err != nil {
		return nil, err
	}
	return s.withCredentialSource(ctx, configuration)
}

// withCredentialSource derives the Site's effective credential source with the same precedence
// EffectiveAutomationConfigurations applies at run time: a site private key overrides, else the
// Deployment Key.
func (s *AutomationConfigurationService) withCredentialSource(ctx context.Context, configuration *operationdomain.AutomationConfiguration) (*operationdomain.AutomationConfiguration, error) {
	configuration.CredentialSource = operationdomain.CredentialSourceNone
	switch {
	case configuration.HasPrivateKeyOverride:
		configuration.CredentialSource = operationdomain.CredentialSourceSite
	case s.deploymentKeys != nil:
		exists, err := s.deploymentKeys.HasDeploymentKey(ctx)
		if err != nil {
			return nil, err
		}
		if exists {
			configuration.CredentialSource = operationdomain.CredentialSourceDeploymentKey
		}
	}
	return configuration, nil
}

// Put validates and replaces non-secret settings.
func (s *AutomationConfigurationService) Put(ctx context.Context, configuration *operationdomain.AutomationConfiguration) (*operationdomain.AutomationConfiguration, error) {
	if _, err := s.sites.FindByID(ctx, configuration.SiteID); err != nil {
		return nil, err
	}
	configuration.SSHUser = strings.TrimSpace(configuration.SSHUser)
	configuration.KnownHosts = strings.TrimSpace(configuration.KnownHosts)
	if configuration.SSHPort == 0 {
		configuration.SSHPort = 22
	}
	if configuration.SSHPort < 1 || configuration.SSHPort > 65535 {
		return nil, fmt.Errorf("%w: sshPort must be between 1 and 65535", ErrInvalidOperation)
	}
	// sshUser is only a fallback login user since decision 039 (the OS Image default user comes
	// first), so it is optional; known-hosts remains required because host-key verification
	// cannot be disabled.
	if configuration.Enabled && configuration.KnownHosts == "" {
		return nil, fmt.Errorf("%w: enabled automation requires knownHosts", ErrInvalidOperation)
	}
	for kind, playbook := range configuration.PlaybookMappings {
		if !kind.Valid() || kind == operationdomain.WorkflowKindCustom {
			return nil, fmt.Errorf("%w: invalid mapped operation kind %q", ErrInvalidOperation, kind)
		}
		if _, err := s.catalog.Resolve(playbook); err != nil {
			return nil, err
		}
	}
	if err := s.configurations.Upsert(ctx, configuration); err != nil {
		return nil, err
	}
	return s.Get(ctx, configuration.SiteID)
}

// ReplaceCredential validates and writes encrypted secrets, replacing the whole site credential.
// An empty private key clears the site override so the Site uses the Deployment Key; a non-empty
// one must parse as an unencrypted private key, because automation uses it unattended and a key
// that cannot be parsed would only fail later, deep inside a Workflow.
func (s *AutomationConfigurationService) ReplaceCredential(ctx context.Context, siteID string, credential operationdomain.AutomationCredential) error {
	if strings.TrimSpace(credential.SSHPrivateKey) != "" {
		credential.SSHPrivateKey = strings.TrimSpace(credential.SSHPrivateKey) + "\n"
		if _, err := sshprobe.ParseSigner(credential.SSHPrivateKey); err != nil {
			return fmt.Errorf("%w: sshPrivateKey is not a usable unencrypted private key", ErrInvalidOperation)
		}
	} else {
		credential.SSHPrivateKey = ""
	}
	return s.configurations.ReplaceCredential(ctx, siteID, credential)
}

// LogsForRun reads retained output for a durable Ansible Step by its external run ID.
func (s *ExecutionService) LogsForRun(ctx context.Context, runID string) (string, error) {
	return s.runner.Logs(ctx, runID)
}

// StderrForRun returns the error-only report (failed and unreachable tasks) for a durable
// Ansible Step by its external run ID, powering the Step's Stderr view. It is empty when the
// run recorded no failure.
func (s *ExecutionService) StderrForRun(ctx context.Context, runID string) (string, error) {
	return s.runner.Stderr(ctx, runID)
}

// EventsForRun projects a durable Step's Ansible task events for the API.
//
// It reads the durable event stream when one is configured, so the task list is available live
// while the run is still in progress; without it, it falls back to the runner's on-disk events,
// which are only complete after the run ends. Counts are derived from the events so the summary and
// the list can never disagree.
func (s *ExecutionService) EventsForRun(ctx context.Context, runID, status string) (*OperationEventsItem, error) {
	var events []operationdomain.TaskEvent
	var err error
	if s.events != nil {
		events, err = s.events.ListEvents(ctx, runID)
	} else {
		events, err = s.runner.Events(ctx, runID)
	}
	if err != nil {
		return nil, err
	}
	item := &OperationEventsItem{RunID: runID, Status: status, Events: make([]TaskEventItem, 0, len(events))}
	for _, event := range events {
		switch event.Status {
		case "ok":
			item.OKCount++
		case "failed":
			item.FailedCount++
		case "unreachable":
			item.UnreachableCount++
		case "skipped":
			item.SkippedCount++
		}
		if event.Changed {
			item.ChangedCount++
		}
		item.Events = append(item.Events, TaskEventItem{
			Play: event.Play, Task: event.Task, Host: event.Host,
			Status: event.Status, Changed: event.Changed,
			StartedAt: optionalTime(event.StartedAt), EndedAt: optionalTime(event.EndedAt),
		})
	}
	return item, nil
}

// PreparedAnsibleStep is validated automation intent ready to join a larger Operation.
type PreparedAnsibleStep struct {
	SiteID    string
	Playbook  string
	ExtraVars map[string]any
	Targets   []operationdomain.ResourceReference
}

// PrepareAnsibleStep applies the same catalog, credential, policy, and lock checks as
// Create while letting a preceding provision-os Step satisfy the final deployed state.
func (s *ExecutionService) PrepareAnsibleStep(ctx context.Context, input CreateExecutionInput, operationID string, provisionFirst bool) (*PreparedAnsibleStep, error) {
	kind := operationdomain.WorkflowKind(strings.TrimSpace(input.Kind))
	if !kind.Valid() || len(input.TargetServerIDs) == 0 {
		return nil, fmt.Errorf("%w: valid kind and targetServerIds are required", ErrInvalidOperation)
	}
	targets := make([]*serverdomain.Server, 0, len(input.TargetServerIDs))
	siteID := ""
	for _, id := range input.TargetServerIDs {
		server, err := s.servers.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if siteID == "" {
			siteID = server.Source.SiteID
		}
		if server.Source.SiteID != siteID {
			return nil, fmt.Errorf("%w: targets must belong to one site", ErrInvalidOperation)
		}
		if !provisionFirst {
			required := kind.RequiredProvisioningState()
			if required != "" && (server.Provisioning == nil || server.Provisioning.State != required) {
				return nil, fmt.Errorf("%w: %s requires %q", operationdomain.ErrTargetStateInvalid, server.DisplayName(), required)
			}
		}
		targets = append(targets, server)
	}
	if s.protection != nil {
		if err := s.protection.RequireUnlocked(ctx, input.TargetServerIDs); err != nil {
			return nil, err
		}
	}
	if s.policy != nil {
		if err := s.policy.CheckOperation(ctx, kind, input.PlatformID, input.TargetServerIDs); err != nil {
			return nil, err
		}
	}
	configuration, err := s.configurations.FindBySiteID(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if !configuration.Enabled {
		return nil, operationdomain.ErrAutomationDisabled
	}
	if !configuration.HasCredential {
		return nil, operationdomain.ErrAutomationCredentialMissing
	}
	playbook := strings.TrimSpace(input.PlaybookName)
	if playbook == "" {
		playbook = configuration.PlaybookMappings[kind]
	}
	if playbook == "" {
		return nil, fmt.Errorf("%w: no playbook mapping for %s", ErrInvalidOperation, kind)
	}
	if _, err := s.catalog.Resolve(playbook); err != nil {
		return nil, err
	}
	operation := &operationdomain.ExecutionOperation{
		ID: operationID, Kind: kind, SiteID: siteID, PlatformID: input.PlatformID,
		TargetServerIDs: append([]string(nil), input.TargetServerIDs...),
	}
	stepTargets := make([]operationdomain.ResourceReference, 0, len(targets))
	for _, target := range targets {
		stepTargets = append(stepTargets, operationdomain.ResourceReference{Kind: "server", ID: target.ID})
	}
	return &PreparedAnsibleStep{
		SiteID: siteID, Playbook: playbook,
		ExtraVars: buildExecutionExtraVars(operation, targets, input.ExtraVars, input.TrustedVars),
		Targets:   stepTargets,
	}, nil
}

func legacySyntheticStep(operation *operationdomain.ExecutionOperation) operationdomain.Task {
	status := operationdomain.TaskStatus(operation.Execution.Status)
	var normalized *operationdomain.NormalizedError
	if operation.Execution.Status == operationdomain.StatusIndeterminate {
		status = operationdomain.TaskRequiresAttention
		normalized = &operationdomain.NormalizedError{
			Code: "legacy_outcome_indeterminate", Message: operation.Execution.StatusReason,
			Retryable: false,
		}
	} else if operation.Execution.Status == operationdomain.StatusFailed {
		normalized = &operationdomain.NormalizedError{
			Code: "legacy_ansible_failed", Message: operation.Execution.StatusReason,
			Retryable: false,
		}
	}
	targets := make([]operationdomain.ResourceReference, len(operation.TargetServerIDs))
	for index, serverID := range operation.TargetServerIDs {
		targets[index] = operationdomain.ResourceReference{Kind: "server", ID: serverID}
	}
	return operationdomain.Task{
		ID: "ansible", Kind: "ansible-playbook", Name: "Run " + operation.Execution.Playbook,
		Executor: operationdomain.RunnerKindAnsible, Targets: targets, Status: status,
		Attempt: 1, Error: normalized, StartedAt: operation.Execution.StartedAt,
		FinishedAt: operation.Execution.FinishedAt, Artifacts: []operationdomain.ArtifactMetadata{},
	}
}
