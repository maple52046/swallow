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
	policy         PolicyChecker
}

// NewExecutionService constructs the embedded-execution use case.
func NewExecutionService(
	operations operationdomain.ExecutionRepository,
	servers serverdomain.ServerRepository,
	configurations operationdomain.AutomationConfigurationRepository,
	catalog operationdomain.PlaybookCatalog,
	runner operationdomain.Runner,
	policy PolicyChecker,
) *ExecutionService {
	return &ExecutionService{
		operations: operations, servers: servers, configurations: configurations,
		catalog: catalog, runner: runner, policy: policy,
	}
}

// ExecutionOperationItem is the public operation representation.
type ExecutionOperationItem struct {
	ID                 string        `json:"id"`
	Kind               string        `json:"kind"`
	Intent             string        `json:"intent"`
	SiteID             string        `json:"siteId"`
	ClusterID          *string       `json:"clusterId"`
	TargetServerIDs    []string      `json:"targetServerIds"`
	RetryOfOperationID *string       `json:"retryOfOperationId"`
	Execution          ExecutionItem `json:"execution"`
	RequestedBy        string        `json:"requestedBy"`
	RequestedAt        string        `json:"requestedAt"`
	UpdatedAt          string        `json:"updatedAt"`
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
	ClusterID       string
	PlaybookName    string
	ExtraVars       map[string]any
	// TrustedVars are extra vars the caller is a trusted server-side use case, so they
	// bypass the swallow_ prefix filter that operator-supplied ExtraVars are subject to.
	// A cluster deployment uses this to assign node roles the client cannot forge.
	TrustedVars map[string]any
	// SecretVars are sealed at rest and materialised only for the run, for values such as
	// a VRRP password that must not persist in plain text.
	SecretVars         map[string]any
	RetryOfOperationID string
	RequestedBy        string
}

// Create validates intent and persists pending state before dispatch.
func (s *ExecutionService) Create(ctx context.Context, input CreateExecutionInput) (*ExecutionOperationItem, error) {
	kind := operationdomain.OperationKind(strings.TrimSpace(input.Kind))
	if !kind.Valid() {
		return nil, fmt.Errorf("%w: kind must be one of %v", ErrInvalidOperation, operationdomain.ValidOperationKinds)
	}
	if len(input.TargetServerIDs) == 0 {
		return nil, fmt.Errorf("%w: targetServerIds is required", ErrInvalidOperation)
	}
	targets, siteID, err := s.resolveTargets(ctx, kind, input.TargetServerIDs)
	if err != nil {
		return nil, err
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
		if err := s.policy.CheckOperation(ctx, kind, input.TargetServerIDs); err != nil {
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
	if kind == operationdomain.OperationKindCustom {
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
		SiteID: siteID, ClusterID: input.ClusterID,
		TargetServerIDs:    append([]string(nil), input.TargetServerIDs...),
		SecretVars:         input.SecretVars,
		RetryOfOperationID: input.RetryOfOperationID,
		RequestedBy:        input.RequestedBy, RequestedAt: now, UpdatedAt: now,
	}
	operation.Execution = operationdomain.Execution{
		RunID: uuid.NewString(), Playbook: playbook, Status: operationdomain.StatusPending,
	}
	operation.ExtraVars = buildExecutionExtraVars(operation, targets, input.ExtraVars, input.TrustedVars)
	if err := s.operations.Create(ctx, operation); err != nil {
		return nil, err
	}
	item := toExecutionOperationItem(operation)
	return &item, nil
}

func (s *ExecutionService) resolveTargets(ctx context.Context, kind operationdomain.OperationKind, ids []string) ([]*serverdomain.Server, string, error) {
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
	"swallow_cluster_id":       true,
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
	if operation.ClusterID != "" {
		vars["swallow_cluster_id"] = operation.ClusterID
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
		SiteID: input.SiteID, ClusterID: input.ClusterID, ServerID: input.ServerID,
		Kind: operationdomain.OperationKind(input.Kind), Status: operationdomain.Status(input.Status),
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
// the kind, targets, cluster, playbook, and both the carried extra vars and sealed secret
// vars, so a cluster deployment retry keeps the same VRRP password and role assignment
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
		ClusterID:          original.ClusterID,
		PlaybookName:       original.Execution.Playbook,
		TrustedVars:        carried,
		SecretVars:         secretVars,
		RetryOfOperationID: original.ID,
		RequestedBy:        requestedBy,
	})
}

// OperationEventsItem is the task-level progress of a run.
type OperationEventsItem struct {
	RunID        string          `json:"runId"`
	Status       string          `json:"status"`
	OKCount      int             `json:"okCount"`
	ChangedCount int             `json:"changedCount"`
	FailedCount  int             `json:"failedCount"`
	Events       []TaskEventItem `json:"events"`
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
		case "failed", "unreachable":
			item.FailedCount++
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

func toExecutionOperationItem(operation *operationdomain.ExecutionOperation) ExecutionOperationItem {
	return ExecutionOperationItem{
		ID: operation.ID, Kind: string(operation.Kind), Intent: operation.Intent,
		SiteID: operation.SiteID, ClusterID: wire.String(operation.ClusterID),
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
}

// NewAutomationConfigurationService constructs site automation use cases.
func NewAutomationConfigurationService(configurations operationdomain.AutomationConfigurationRepository, sites sitedomain.SiteRepository, catalog operationdomain.PlaybookCatalog) *AutomationConfigurationService {
	return &AutomationConfigurationService{configurations: configurations, sites: sites, catalog: catalog}
}

// Get returns settings and credential presence only.
func (s *AutomationConfigurationService) Get(ctx context.Context, siteID string) (*operationdomain.AutomationConfiguration, error) {
	return s.configurations.FindBySiteID(ctx, siteID)
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
	if configuration.Enabled && (configuration.SSHUser == "" || configuration.KnownHosts == "") {
		return nil, fmt.Errorf("%w: enabled automation requires sshUser and knownHosts", ErrInvalidOperation)
	}
	for kind, playbook := range configuration.PlaybookMappings {
		if !kind.Valid() || kind == operationdomain.OperationKindCustom {
			return nil, fmt.Errorf("%w: invalid mapped operation kind %q", ErrInvalidOperation, kind)
		}
		if _, err := s.catalog.Resolve(playbook); err != nil {
			return nil, err
		}
	}
	if err := s.configurations.Upsert(ctx, configuration); err != nil {
		return nil, err
	}
	return s.configurations.FindBySiteID(ctx, configuration.SiteID)
}

// ReplaceCredential validates and writes encrypted secrets.
func (s *AutomationConfigurationService) ReplaceCredential(ctx context.Context, siteID string, credential operationdomain.AutomationCredential) error {
	if strings.TrimSpace(credential.SSHPrivateKey) == "" {
		return fmt.Errorf("%w: sshPrivateKey is required", ErrInvalidOperation)
	}
	return s.configurations.ReplaceCredential(ctx, siteID, credential)
}
