package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// ExecutionService accepts Ansible automation (a single-Task Workflow, or an Ansible Task a larger
// Workflow composes) and reads the retained output and task events of Ansible Tasks.
type ExecutionService struct {
	servers        serverdomain.ServerRepository
	configurations workflowdomain.AutomationConfigurationRepository
	catalog        workflowdomain.PlaybookCatalog
	kinds          workflowdomain.WorkflowKinds
	runner         workflowdomain.Runner
	// events is the durable stream of per-task Ansible events. EventsForRun reads it so a
	// durable Step's task list is available live during a run, independent of the artifact
	// filesystem. Nil falls back to the runner's on-disk events (post-hoc only).
	events     workflowdomain.AnsibleEventRepository
	protection serverdomain.MutationGuard
	policy     PolicyChecker
	durable    *WorkflowService
}

// NewExecutionService constructs the Ansible automation use case. kinds are the Workflow kinds the
// build accepts, the same set the WorkflowService's plan catalog carries.
func NewExecutionService(
	servers serverdomain.ServerRepository,
	configurations workflowdomain.AutomationConfigurationRepository,
	catalog workflowdomain.PlaybookCatalog,
	kinds workflowdomain.WorkflowKinds,
	runner workflowdomain.Runner,
	events workflowdomain.AnsibleEventRepository,
	policy PolicyChecker,
	protection ...serverdomain.MutationGuard,
) *ExecutionService {
	service := &ExecutionService{
		servers: servers, configurations: configurations,
		catalog: catalog, kinds: kinds, runner: runner, events: events, policy: policy,
	}
	if len(protection) > 0 {
		service.protection = protection[0]
	}
	return service
}

// AttachWorkflow gives Create the Workflow service it accepts automation into. Without it Create
// refuses every request.
func (s *ExecutionService) AttachWorkflow(durable *WorkflowService) { s.durable = durable }

// ExecutionItem is the `execution` summary every Workflow response still carries (WorkflowItem).
// It contains no controller-specific fields.
type ExecutionItem struct {
	RunID        string  `json:"runId"`
	Playbook     string  `json:"playbook"`
	Status       string  `json:"status"`
	StatusReason *string `json:"statusReason"`
	StartedAt    *string `json:"startedAt"`
	FinishedAt   *string `json:"finishedAt"`
}

// CreateExecutionInput is accepted by POST /workflows.
type CreateExecutionInput struct {
	Kind            string
	Intent          string
	TargetServerIDs []string
	PlatformID      string
	PlaybookName    string
	ExtraVars       map[string]any
	// TrustedVars are extra vars the caller is a trusted server-side use case, so they
	// bypass the swallow_ prefix filter that user-supplied ExtraVars are subject to.
	// A platform deployment uses this to assign node roles the client cannot forge.
	TrustedVars map[string]any
	// SecretVars are sealed at rest and materialised only for the run, for values such as
	// a VRRP password that must not persist in plain text.
	SecretVars         map[string]any
	RetryOfOperationID string
	RequestedBy        string
	RequestCorrelation string
}

// Create validates the automation request and accepts it as a single-Task Workflow, which the
// dispatcher starts after the commit. It returns the accepted Workflow.
func (s *ExecutionService) Create(ctx context.Context, input CreateExecutionInput) (*WorkflowItem, error) {
	kind := workflowdomain.WorkflowKind(strings.TrimSpace(input.Kind))
	if !s.kinds.Valid(kind) {
		return nil, fmt.Errorf("%w: kind must be one of %v", ErrInvalidOperation, s.kinds.List())
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
	if s.policy != nil {
		if err := s.policy.CheckOperation(ctx, kind, input.PlatformID, input.TargetServerIDs); err != nil {
			return nil, err
		}
	}
	// s.configurations is the EffectiveAutomationConfigurations wrapper, which derives the
	// credential source CheckRunnable gates on.
	configuration, err := s.configurations.FindBySiteID(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if err := configuration.CheckRunnable(); err != nil {
		return nil, err
	}

	playbook := strings.TrimSpace(input.PlaybookName)
	if kind == workflowdomain.WorkflowKindCustom {
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
	operation := &workflowdomain.ExecutionOperation{
		ID: uuid.NewString(), Kind: kind, Intent: strings.TrimSpace(input.Intent),
		SiteID: siteID, PlatformID: input.PlatformID,
		TargetServerIDs:    append([]string(nil), input.TargetServerIDs...),
		SecretVars:         input.SecretVars,
		RetryOfOperationID: input.RetryOfOperationID,
		RequestedBy:        input.RequestedBy, RequestedAt: now, UpdatedAt: now,
	}
	operation.Execution = workflowdomain.Execution{
		RunID: uuid.NewString(), Playbook: playbook, Status: workflowdomain.StatusPending,
	}
	operation.ExtraVars = buildExecutionExtraVars(operation, targets, input.ExtraVars, input.TrustedVars)
	if s.durable == nil {
		return nil, fmt.Errorf("%w: durable Workflow orchestration is unavailable", ErrInvalidOperation)
	}
	stepTargets := make([]workflowdomain.ResourceReference, 0, len(operation.TargetServerIDs))
	for _, serverID := range operation.TargetServerIDs {
		stepTargets = append(stepTargets, workflowdomain.ResourceReference{Kind: "server", ID: serverID})
	}
	targetResources := append([]workflowdomain.ResourceReference(nil), stepTargets...)
	if operation.PlatformID != "" {
		targetResources = append(targetResources, workflowdomain.ResourceReference{Kind: "platform", ID: operation.PlatformID})
	}
	created, err := s.durable.Create(ctx, CreateWorkflowInput{
		Kind: operation.Kind, IntentSummary: operation.Intent, Definition: "ansible-operation", DefinitionVersion: 1,
		SiteID: operation.SiteID, PlatformID: operation.PlatformID, TargetServerIDs: operation.TargetServerIDs,
		TargetResources: targetResources, RequestedBy: operation.RequestedBy,
		RequestCorrelation: input.RequestCorrelation, RetryOfOperationID: operation.RetryOfOperationID,
		IntentSnapshot: map[string]any{"playbook": operation.Execution.Playbook, "extraVars": operation.ExtraVars},
		Steps: []workflowdomain.Task{{ID: "ansible", Kind: "ansible-playbook", Name: "Run " + operation.Execution.Playbook,
			Executor: workflowdomain.RunnerKindHostAutomation, Targets: stepTargets, Parameters: map[string]any{"playbook": operation.Execution.Playbook, "extraVars": operation.ExtraVars}}},
		SecretStepID: "ansible", SecretValues: operation.SecretVars,
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *ExecutionService) resolveTargets(ctx context.Context, kind workflowdomain.WorkflowKind, ids []string) ([]*serverdomain.Server, string, error) {
	required := s.kinds.RequiredProvisioningState(kind)
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
			return nil, "", fmt.Errorf("%w: %w", ErrInvalidOperation, ErrTargetsSpanSites)
		}
		if required != "" && (server.Provisioning == nil || server.Provisioning.State != required) {
			return nil, "", fmt.Errorf("%w: %s requires %q",
				workflowdomain.ErrTargetStateInvalid, server.DisplayName(), required)
		}
		// Every Operation mutates its target host, regardless of which playbook carries it.
		if s.protection == nil && s.kinds.RefusedWhenLocked(kind) && server.Provisioning != nil && server.Provisioning.Locked {
			return nil, "", fmt.Errorf("%w: Server %q is locked. Unlock it before starting this Operation.",
				workflowdomain.ErrTargetLocked, server.DisplayName())
		}
		targets = append(targets, server)
	}
	return targets, siteID, nil
}

// standardExtraVarKeys are the swallow_-prefixed vars buildExecutionExtraVars generates for every
// operation. A trusted var of the same name is dropped, so no caller overrides the operation's
// own identity, while other trusted swallow_-prefixed vars (a deployment's role assignment) pass.
var standardExtraVarKeys = map[string]bool{
	"swallow_operation_id":     true,
	"swallow_operation_kind":   true,
	"swallow_site_id":          true,
	"swallow_server_ids":       true,
	"swallow_platform_id":      true,
	"swallow_server_hostnames": true,
}

// buildExecutionExtraVars assembles a run's extra vars from three sources: the standard
// swallow_ identifiers, user-supplied vars (which may not use the swallow_ prefix, so
// a client cannot forge a trusted variable), and trusted server-side vars (which may, and
// are used for things like node-role assignment the client must not control).
func buildExecutionExtraVars(operation *workflowdomain.ExecutionOperation, targets []*serverdomain.Server, operator, trusted map[string]any) map[string]any {
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

// AutomationConfigurationService owns site-local runner settings.
type AutomationConfigurationService struct {
	configurations workflowdomain.AutomationConfigurationRepository
	sites          sitedomain.SiteRepository
	catalog        workflowdomain.PlaybookCatalog
	kinds          workflowdomain.WorkflowKinds
	deploymentKeys DeploymentKeySource
}

// NewAutomationConfigurationService constructs site automation use cases. kinds are the Workflow
// kinds a playbook mapping may name.
func NewAutomationConfigurationService(
	configurations workflowdomain.AutomationConfigurationRepository,
	sites sitedomain.SiteRepository,
	catalog workflowdomain.PlaybookCatalog,
	kinds workflowdomain.WorkflowKinds,
) *AutomationConfigurationService {
	return &AutomationConfigurationService{configurations: configurations, sites: sites, catalog: catalog, kinds: kinds}
}

// AttachDeploymentKeys lets reads report CredentialSource deploymentKey once the Deployment Key
// exists. Without it every Site reports none.
func (s *AutomationConfigurationService) AttachDeploymentKeys(keys DeploymentKeySource) {
	s.deploymentKeys = keys
}

// Get returns settings, become-password presence, and the credential source; never secrets.
// The source uses the same derivation as EffectiveAutomationConfigurations, so a Site reported
// as deploymentKey is one automation can run.
func (s *AutomationConfigurationService) Get(ctx context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error) {
	configuration, err := s.configurations.FindBySiteID(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if configuration.CredentialSource, err = credentialSource(ctx, s.deploymentKeys); err != nil {
		return nil, err
	}
	return configuration, nil
}

// Put validates and replaces non-secret settings.
func (s *AutomationConfigurationService) Put(ctx context.Context, configuration *workflowdomain.AutomationConfiguration) (*workflowdomain.AutomationConfiguration, error) {
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
		if !s.kinds.Valid(kind) || kind == workflowdomain.WorkflowKindCustom {
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

// ReplaceCredential seals becomePassword as the Site's whole credential, replacing what it
// stored before; an empty password clears it. It never accepts SSH key material: the Deployment
// Key is the only automation key (decision 039), and delivery refuses a request that carries
// one. An unconfigured Site is ErrAutomationConfigNotFound.
func (s *AutomationConfigurationService) ReplaceCredential(ctx context.Context, siteID, becomePassword string) error {
	return s.configurations.ReplaceCredential(ctx, siteID, workflowdomain.AutomationCredential{BecomePassword: becomePassword})
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
	var events []workflowdomain.TaskEvent
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
	Targets   []workflowdomain.ResourceReference
}

// PrepareAnsibleStep applies the same catalog, credential, policy, and lock checks as
// Create while letting a preceding provision-os Step satisfy the final deployed state.
func (s *ExecutionService) PrepareAnsibleStep(ctx context.Context, input CreateExecutionInput, operationID string, provisionFirst bool) (*PreparedAnsibleStep, error) {
	kind := workflowdomain.WorkflowKind(strings.TrimSpace(input.Kind))
	if !s.kinds.Valid(kind) || len(input.TargetServerIDs) == 0 {
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
			return nil, fmt.Errorf("%w: %w", ErrInvalidOperation, ErrTargetsSpanSites)
		}
		if !provisionFirst {
			required := s.kinds.RequiredProvisioningState(kind)
			if required != "" && (server.Provisioning == nil || server.Provisioning.State != required) {
				return nil, fmt.Errorf("%w: %s requires %q", workflowdomain.ErrTargetStateInvalid, server.DisplayName(), required)
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
	if err := configuration.CheckRunnable(); err != nil {
		return nil, err
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
	operation := &workflowdomain.ExecutionOperation{
		ID: operationID, Kind: kind, SiteID: siteID, PlatformID: input.PlatformID,
		TargetServerIDs: append([]string(nil), input.TargetServerIDs...),
	}
	stepTargets := make([]workflowdomain.ResourceReference, 0, len(targets))
	for _, target := range targets {
		stepTargets = append(stepTargets, workflowdomain.ResourceReference{Kind: "server", ID: target.ID})
	}
	return &PreparedAnsibleStep{
		SiteID: siteID, Playbook: playbook,
		ExtraVars: buildExecutionExtraVars(operation, targets, input.ExtraVars, input.TrustedVars),
		Targets:   stepTargets,
	}, nil
}
