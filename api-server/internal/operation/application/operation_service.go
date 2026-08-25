// Package application coordinates operations: swallow's intent, executed by AWX.
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
	"github.com/maple52046/swallow/internal/operation/infra/awx"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/pagination"
	"github.com/maple52046/swallow/internal/shared/wire"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// templateSettingPrefix is how an operator maps an operation kind to a job template
// name on the automation integration, e.g. "template.install-gpu-driver".
//
// The mapping lives on the integration because which templates exist is a property of
// the installation, not of swallow.
const templateSettingPrefix = "template."

// PolicyChecker rejects operations that contradict cluster policy.
//
// An interface because the policy lives in the cluster context: an operation must not
// need to know what a GPU operator is to refuse installing drivers behind one.
type PolicyChecker interface {
	CheckOperation(ctx context.Context, kind operationdomain.OperationKind, serverIDs []string) error
}

type OperationService struct {
	operations   operationdomain.OperationRepository
	servers      serverdomain.ServerRepository
	integrations sitedomain.IntegrationRepository
	controllers  operationdomain.ControllerFactory
	policy       PolicyChecker
}

func NewOperationService(
	operations operationdomain.OperationRepository,
	servers serverdomain.ServerRepository,
	integrations sitedomain.IntegrationRepository,
	controllers operationdomain.ControllerFactory,
	policy PolicyChecker,
) *OperationService {
	return &OperationService{
		operations:   operations,
		servers:      servers,
		integrations: integrations,
		controllers:  controllers,
		policy:       policy,
	}
}

type OperationItem struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Intent          string   `json:"intent"`
	SiteID          string   `json:"siteId"`
	ClusterID       *string  `json:"clusterId"`
	TargetServerIDs []string `json:"targetServerIds"`

	Automation AutomationItem `json:"automation"`

	RequestedBy string `json:"requestedBy"`
	RequestedAt string `json:"requestedAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// AutomationItem is a mirror of the controller's job, with the time it was observed.
// swallow never infers that a job finished because time passed.
type AutomationItem struct {
	IntegrationID string  `json:"integrationId"`
	JobTemplateID string  `json:"jobTemplateId"`
	JobName       string  `json:"jobName"`
	JobID         *string `json:"jobId"`
	Status        string  `json:"status"`
	Terminal      bool    `json:"terminal"`
	StartedAt     *string `json:"startedAt"`
	FinishedAt    *string `json:"finishedAt"`
	ObservedAt    string  `json:"observedAt"`
}

type CreateOperationInput struct {
	Kind            string
	Intent          string
	TargetServerIDs []string
	ClusterID       string
	// JobTemplateName overrides the kind-to-template mapping. Required for the
	// custom kind, which by definition has no mapping.
	JobTemplateName string
	ExtraVars       map[string]any
	RequestedBy     string
}

// ErrInvalidOperation covers validation failures the delivery layer turns into a 400.
var ErrInvalidOperation = errors.New("invalid operation")

// Create validates, launches, and records an operation.
//
// If AWX cannot be reached, nothing is created: a half-state that has to be reconciled
// later is worse than a failed request the operator can retry.
func (s *OperationService) Create(ctx context.Context, input CreateOperationInput) (*OperationItem, error) {
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

	if err := s.ensureTargetsFree(ctx, input.TargetServerIDs); err != nil {
		return nil, err
	}

	if s.policy != nil {
		if err := s.policy.CheckOperation(ctx, kind, input.TargetServerIDs); err != nil {
			return nil, err
		}
	}

	integration, err := s.automationFor(ctx, siteID)
	if err != nil {
		return nil, err
	}

	controller, err := s.controllers.For(ctx, integration.ID)
	if err != nil {
		return nil, err
	}

	templateName := input.JobTemplateName
	if templateName == "" {
		templateName = integration.Setting(templateSettingPrefix+string(kind), string(kind))
	}
	if kind == operationdomain.OperationKindCustom && input.JobTemplateName == "" {
		return nil, fmt.Errorf("%w: jobTemplateName is required for the custom kind", ErrInvalidOperation)
	}

	// Resolved before anything is stored, so a missing template is reported now rather
	// than by a job that never starts.
	templateID, err := controller.FindJobTemplate(ctx, templateName)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	operation := &operationdomain.Operation{
		ID:              uuid.NewString(),
		Kind:            kind,
		Intent:          strings.TrimSpace(input.Intent),
		SiteID:          siteID,
		ClusterID:       input.ClusterID,
		TargetServerIDs: input.TargetServerIDs,
		Automation: operationdomain.AutomationRef{
			IntegrationID: integration.ID,
			JobTemplateID: templateID,
			JobName:       templateName,
			Status:        operationdomain.StatusPending,
			ObservedAt:    now,
		},
		RequestedBy: input.RequestedBy,
		RequestedAt: now,
		UpdatedAt:   now,
	}

	jobID, state, err := controller.Launch(ctx, operationdomain.LaunchRequest{
		JobTemplateID: templateID,
		Limit:         input.TargetServerIDs,
		ExtraVars:     buildExtraVars(operation, targets, input.ExtraVars),
	})
	if err != nil {
		return nil, err
	}

	operation.Automation.JobID = jobID
	operation.Automation.Status = state.Status
	operation.Automation.StartedAt = state.StartedAt
	operation.Automation.FinishedAt = state.FinishedAt
	operation.Automation.ObservedAt = time.Now().UTC()

	if err := s.operations.Create(ctx, operation); err != nil {
		return nil, err
	}

	item := toOperationItem(operation)
	return &item, nil
}

// resolveTargets checks every target exists and is in the state this kind needs, and
// returns the site they belong to.
//
// Targets must share a site: an operation runs through one automation controller, and
// controllers are registered per site.
func (s *OperationService) resolveTargets(
	ctx context.Context,
	kind operationdomain.OperationKind,
	serverIDs []string,
) ([]*serverdomain.Server, string, error) {
	requiredState := kind.RequiredProvisioningState()

	var targets []*serverdomain.Server
	siteID := ""

	for _, id := range serverIDs {
		server, err := s.servers.FindByID(ctx, id)
		if err != nil {
			return nil, "", err
		}

		if siteID == "" {
			siteID = server.Source.SiteID
		} else if server.Source.SiteID != siteID {
			return nil, "", fmt.Errorf(
				"%w: targets span more than one site, but an operation runs through one site's automation controller",
				ErrInvalidOperation)
		}

		if requiredState != "" {
			if server.Provisioning == nil || server.Provisioning.State != requiredState {
				actual := "unknown"
				if server.Provisioning != nil {
					actual = server.Provisioning.State
				}
				return nil, "", fmt.Errorf("%w: %s is %q, but %s requires %q",
					operationdomain.ErrTargetStateInvalid,
					server.DisplayName(), actual, kind, requiredState)
			}
		}

		targets = append(targets, server)
	}

	return targets, siteID, nil
}

// ensureTargetsFree refuses overlapping work. The controller will not stop it, because
// it sees two unrelated jobs.
func (s *OperationService) ensureTargetsFree(ctx context.Context, serverIDs []string) error {
	active, err := s.operations.FindActiveByServerIDs(ctx, serverIDs)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		return nil
	}

	ids := make([]string, 0, len(active))
	for _, operation := range active {
		ids = append(ids, operation.ID)
	}
	sort.Strings(ids)

	return fmt.Errorf("%w: still running in operation(s) %s",
		operationdomain.ErrTargetsBusy, strings.Join(ids, ", "))
}

func (s *OperationService) automationFor(ctx context.Context, siteID string) (*sitedomain.Integration, error) {
	integrations, err := s.integrations.List(ctx, sitedomain.IntegrationFilter{
		SiteID:      siteID,
		Kind:        sitedomain.IntegrationKindAutomation,
		EnabledOnly: true,
	})
	if err != nil {
		return nil, err
	}
	if len(integrations) == 0 {
		return nil, operationdomain.ErrNoAutomationIntegration
	}
	// One controller per site is the assumption; taking the first is deterministic
	// enough while that holds, and a second one would be a modelling question rather
	// than a selection problem.
	return integrations[0], nil
}

// buildExtraVars passes swallow's identifiers to the playbook so that it can report back
// in terms swallow understands, and so that a playbook never has to guess which operation
// it belongs to.
func buildExtraVars(
	operation *operationdomain.Operation,
	targets []*serverdomain.Server,
	operatorVars map[string]any,
) map[string]any {
	vars := map[string]any{
		"swallow_operation_id":   operation.ID,
		"swallow_operation_kind": string(operation.Kind),
		"swallow_site_id":        operation.SiteID,
		"swallow_server_ids":     operation.TargetServerIDs,
	}
	if operation.ClusterID != "" {
		vars["swallow_cluster_id"] = operation.ClusterID
	}
	if len(targets) > 0 {
		hostnames := make([]string, 0, len(targets))
		for _, target := range targets {
			hostnames = append(hostnames, target.DisplayName())
		}
		vars["swallow_server_hostnames"] = hostnames
	}

	// Operator-supplied vars last so that a playbook parameter can be overridden,
	// but never the swallow identifiers a playbook reports against.
	for key, value := range operatorVars {
		if strings.HasPrefix(key, "swallow_") {
			continue
		}
		vars[key] = value
	}
	return vars
}

type ListOperationsInput struct {
	SiteID    string
	ClusterID string
	ServerID  string
	Kind      string
	Status    string
	Active    bool
	Page      pagination.Page
}

func (s *OperationService) List(ctx context.Context, input ListOperationsInput) (pagination.Result[OperationItem], error) {
	var empty pagination.Result[OperationItem]

	result, err := s.operations.List(ctx, operationdomain.ListFilter{
		SiteID:     input.SiteID,
		ClusterID:  input.ClusterID,
		ServerID:   input.ServerID,
		Kind:       operationdomain.OperationKind(input.Kind),
		Status:     operationdomain.Status(input.Status),
		ActiveOnly: input.Active,
		Offset:     input.Page.Offset(),
		Limit:      input.Page.PageSize,
	})
	if err != nil {
		return empty, err
	}

	items := make([]OperationItem, 0, len(result.Operations))
	for _, operation := range result.Operations {
		items = append(items, toOperationItem(operation))
	}
	return pagination.NewResult(items, result.Total, input.Page), nil
}

func (s *OperationService) Get(ctx context.Context, id string) (*OperationItem, error) {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := toOperationItem(operation)
	return &item, nil
}

// Logs proxies the controller's output. swallow never stores logs: the controller already
// retains them, a copy could not be complete for a running job, and swallow's storage
// should not grow with playbook verbosity.
func (s *OperationService) Logs(ctx context.Context, id string) (string, error) {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	if operation.Automation.JobID == "" {
		return "", operationdomain.ErrNotLaunched
	}

	controller, err := s.controllers.For(ctx, operation.Automation.IntegrationID)
	if err != nil {
		return "", err
	}
	return controller.JobLogs(ctx, operation.Automation.JobID)
}

// Refresh re-reads one operation's job state from the controller.
func (s *OperationService) Refresh(ctx context.Context, id string) (*OperationItem, error) {
	operation, err := s.operations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.refresh(ctx, operation); err != nil {
		return nil, err
	}
	item := toOperationItem(operation)
	return &item, nil
}

// RefreshByJob refreshes the operation mirroring a controller job.
//
// Used by the controller's webhook, which is treated as a hint to go and look rather
// than as a source of truth: the state swallow records always comes from a read it made
// itself.
func (s *OperationService) RefreshByJob(ctx context.Context, integrationID, jobID string) error {
	operation, err := s.operations.FindByJobID(ctx, integrationID, jobID)
	if err != nil {
		return err
	}
	return s.refresh(ctx, operation)
}

// RefreshActive brings every unfinished operation up to date. This is the poller's
// work: webhooks make status feel live, polling makes it correct.
func (s *OperationService) RefreshActive(ctx context.Context) (refreshed int, err error) {
	result, err := s.operations.List(ctx, operationdomain.ListFilter{ActiveOnly: true})
	if err != nil {
		return 0, err
	}

	for _, operation := range result.Operations {
		if err := s.refresh(ctx, operation); err != nil {
			// One controller being unreachable must not stop the others.
			continue
		}
		refreshed++
	}
	return refreshed, nil
}

func (s *OperationService) refresh(ctx context.Context, operation *operationdomain.Operation) error {
	if operation.Automation.JobID == "" {
		return nil
	}

	controller, err := s.controllers.For(ctx, operation.Automation.IntegrationID)
	if err != nil {
		return err
	}

	state, err := controller.JobState(ctx, operation.Automation.JobID)
	if errors.Is(err, awx.ErrJobNotFound) {
		// The controller no longer has the job. Absence is not an outcome, so this
		// becomes indeterminate rather than failed.
		operation.Automation.Status = operationdomain.StatusIndeterminate
		operation.Automation.ObservedAt = time.Now().UTC()
		return s.operations.UpdateAutomation(ctx, operation.ID, operation.Automation)
	}
	if err != nil {
		// Leave the last known status in place with its old ObservedAt, so a reader
		// can see the mirror is stale rather than being told something untrue.
		return err
	}

	operation.Automation.Status = state.Status
	operation.Automation.StartedAt = state.StartedAt
	operation.Automation.FinishedAt = state.FinishedAt
	operation.Automation.ObservedAt = time.Now().UTC()

	return s.operations.UpdateAutomation(ctx, operation.ID, operation.Automation)
}

func toOperationItem(operation *operationdomain.Operation) OperationItem {
	return OperationItem{
		ID:              operation.ID,
		Kind:            string(operation.Kind),
		Intent:          operation.Intent,
		SiteID:          operation.SiteID,
		ClusterID:       wire.String(operation.ClusterID),
		TargetServerIDs: wire.Strings(operation.TargetServerIDs),
		Automation: AutomationItem{
			IntegrationID: operation.Automation.IntegrationID,
			JobTemplateID: operation.Automation.JobTemplateID,
			JobName:       operation.Automation.JobName,
			JobID:         wire.String(operation.Automation.JobID),
			Status:        string(operation.Automation.Status),
			Terminal:      operation.Automation.Status.Terminal(),
			StartedAt:     optionalTime(operation.Automation.StartedAt),
			FinishedAt:    optionalTime(operation.Automation.FinishedAt),
			ObservedAt:    wire.Time(operation.Automation.ObservedAt),
		},
		RequestedBy: operation.RequestedBy,
		RequestedAt: wire.Time(operation.RequestedAt),
		UpdatedAt:   wire.Time(operation.UpdatedAt),
	}
}

func optionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return wire.TimePtr(*t)
}
