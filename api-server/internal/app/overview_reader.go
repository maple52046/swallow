package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	monitoringapp "github.com/maple52046/swallow/internal/core/monitoring/application"
	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	overviewapp "github.com/maple52046/swallow/internal/core/overview/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// HealthResolver classifies the non-persisted health axis exactly as a Server read does
// (servers-list.md, Health), so the overview counts and the Servers it counts never disagree. It is
// the server context's port; a metrics outage leaves Servers unknown, and an error means swallow's
// own records could not be read.
type HealthResolver interface {
	ResolveHealth(ctx context.Context, servers []*serverdomain.Server) (map[string]serverdomain.HealthStatus, error)
}

// OverviewReader projects repository-owned records into overview-owned summaries. It never writes
// or persists the derived view, and credentials are absent from every projection.
type OverviewReader struct {
	sites          sitedomain.SiteRepository
	integrations   sitedomain.IntegrationRepository
	servers        serverdomain.ServerRepository
	health         HealthResolver
	platforms      platformdomain.PlatformRepository
	orchestrations workflowdomain.WorkflowRepository
	alerts         *monitoringapp.AlertService
}

// NewOverviewReader wires the existing context repositories into the overview read boundary. health
// is required: it is the same resolver the Server reads use.
func NewOverviewReader(
	sites sitedomain.SiteRepository,
	integrations sitedomain.IntegrationRepository,
	servers serverdomain.ServerRepository,
	health HealthResolver,
	platforms platformdomain.PlatformRepository,
	orchestrations workflowdomain.WorkflowRepository,
	alerts *monitoringapp.AlertService,
) *OverviewReader {
	return &OverviewReader{
		sites: sites, integrations: integrations, servers: servers, health: health,
		platforms: platforms, orchestrations: orchestrations, alerts: alerts,
	}
}

// ListSites returns identities only; Site descriptions are not part of the overview.
func (r *OverviewReader) ListSites(ctx context.Context) ([]overviewapp.Site, error) {
	sites, err := r.sites.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list overview sites: %w", err)
	}
	items := make([]overviewapp.Site, len(sites))
	for i, site := range sites {
		items[i] = overviewapp.Site{ID: site.ID}
	}
	return items, nil
}

// ListIntegrations returns sync freshness but never reads credentials.
func (r *OverviewReader) ListIntegrations(ctx context.Context, siteID string) ([]overviewapp.Integration, error) {
	integrations, err := r.integrations.List(ctx, sitedomain.IntegrationFilter{SiteID: siteID})
	if err != nil {
		return nil, fmt.Errorf("list overview integrations: %w", err)
	}
	items := make([]overviewapp.Integration, len(integrations))
	for i, integration := range integrations {
		items[i] = overviewapp.Integration{
			ID: integration.ID, SiteID: integration.SiteID, Name: integration.Name,
			Kind: string(integration.Kind), ProviderKind: integration.ProviderKind,
			Enabled: integration.Enabled, LastSucceededAt: integration.Sync.LastSucceededAt,
			LastError: integration.Sync.LastError,
		}
	}
	return items, nil
}

// ListServers returns all scoped projections, absent ones included, because counts must not
// inherit API pagination. Health is classified for the whole batch in one resolver call: an
// unavailable metrics store leaves Servers unknown, matching the Server list contract, while a
// failure to read swallow's own records fails the overview like any durable inventory failure.
func (r *OverviewReader) ListServers(ctx context.Context, siteID string) ([]overviewapp.Server, error) {
	result, err := r.servers.List(ctx, serverdomain.ListFilter{SiteID: siteID, IncludeAbsent: true})
	if err != nil {
		return nil, fmt.Errorf("list overview servers: %w", err)
	}
	var healthByID map[string]serverdomain.HealthStatus
	if len(result.Servers) > 0 {
		healthByID, err = r.health.ResolveHealth(ctx, result.Servers)
		if err != nil {
			return nil, fmt.Errorf("classify overview server health: %w", err)
		}
	}

	items := make([]overviewapp.Server, len(result.Servers))
	for i, server := range result.Servers {
		item := overviewapp.Server{Absent: server.Absent}
		if server.Provisioning != nil {
			item.ProvisioningState = server.Provisioning.State
		}
		if server.Membership != nil {
			item.PlatformID = server.Membership.PlatformID
		}
		item.GPUDevices = serverdomain.GPUCountByKind(server.Observed.GPUs, serverdomain.GPUKindCompute)
		if health, ok := healthByID[server.ID]; ok {
			item.HealthState = health.State
		}
		items[i] = item
	}
	return items, nil
}

// ListPlatforms returns reachability and the correlation gap from each platform's last sync.
func (r *OverviewReader) ListPlatforms(ctx context.Context, siteID string) ([]overviewapp.Platform, error) {
	platforms, err := r.platforms.List(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("list overview platforms: %w", err)
	}
	items := make([]overviewapp.Platform, len(platforms))
	for i, platform := range platforms {
		items[i] = overviewapp.Platform{
			IntegrationID: platform.IntegrationID,
			MemberCount:   platform.Sync.MemberCount,
			MatchedCount:  platform.Sync.MatchedCount,
		}
	}
	return items, nil
}

// ListOperations returns public fields only; sealed extra vars and credentials never cross
// this adapter.
func (r *OverviewReader) ListOperations(ctx context.Context, siteID string) ([]overviewapp.Operation, error) {
	workflows, _, err := r.orchestrations.List(ctx, workflowdomain.WorkflowFilter{SiteID: siteID})
	if err != nil {
		return nil, fmt.Errorf("list overview operations: %w", err)
	}
	items := make([]overviewapp.Operation, 0, len(workflows))
	for _, operation := range workflows {
		summary, _ := operation.Intent["summary"].(string)
		playbook, runID := "", operation.Temporal.RunID
		if len(operation.Steps) == 1 {
			playbook = operation.Steps[0].Kind
			if operation.Steps[0].ExternalExecution != nil {
				runID = operation.Steps[0].ExternalExecution.ID
			}
		}
		items = append(items, overviewapp.Operation{ID: operation.ID, Kind: string(operation.Kind), Intent: summary, SiteID: operation.SiteID, PlatformID: operation.PlatformID,
			TargetServerIDs: append([]string(nil), operation.TargetServerIDs...), RetryOfOperationID: operation.RetryOfOperationID, RunID: runID, Playbook: playbook,
			Status: string(operation.Status), StatusReason: operation.StatusReason, StartedAt: operation.StartedAt, FinishedAt: operation.FinishedAt,
			RequestedBy: operation.RequestedBy, RequestedAt: operation.RequestedAt, UpdatedAt: operation.UpdatedAt})
	}
	return items, nil
}

// ListFiringAlerts maps the monitoring context's outcomes onto the overview's two partial
// states: no metrics Integration or no Alertmanager address is "not configured" (Alertmanager
// is optional), and a store failure on a configured Alertmanager is "unavailable". Unexpected
// mapping or programming failures remain fatal.
func (r *OverviewReader) ListFiringAlerts(ctx context.Context, siteID string) ([]overviewapp.Alert, error) {
	alerts, err := r.alerts.List(ctx, monitoringapp.ListAlertsInput{SiteID: siteID, State: "firing"})
	if err != nil {
		var queryErr *monitoringdomain.QueryError
		switch {
		case errors.Is(err, monitoringdomain.ErrNoAlertSource),
			errors.Is(err, monitoringdomain.ErrNoMetricsIntegration):
			return nil, fmt.Errorf("%w: %v", overviewapp.ErrMonitoringNotConfigured, err)
		case errors.As(err, &queryErr):
			return nil, fmt.Errorf("%w: %v", overviewapp.ErrMonitoringUnavailable, err)
		}
		return nil, fmt.Errorf("list overview alerts: %w", err)
	}
	items := make([]overviewapp.Alert, len(alerts))
	for i, alert := range alerts {
		items[i] = overviewapp.Alert{
			Fingerprint: alert.Fingerprint, Name: alert.Name, Severity: alert.Severity,
			State: alert.State, Summary: alert.Summary, Description: alert.Description,
			Labels: alert.Labels, StartsAt: parseTime(alert.StartsAt),
			ServerID: stringValue(alert.ServerID), SiteID: stringValue(alert.SiteID),
			PlatformID: stringValue(alert.PlatformID),
		}
	}
	return items, nil
}

func parseTime(value *string) *time.Time {
	if value == nil {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil
	}
	return &parsed
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
