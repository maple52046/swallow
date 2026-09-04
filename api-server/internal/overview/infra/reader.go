// Package infra adapts context-owned repositories into the operator overview read boundary.
package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

	monitoringapp "github.com/maple52046/swallow/internal/monitoring/application"
	monitoringdomain "github.com/maple52046/swallow/internal/monitoring/domain"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	overviewapp "github.com/maple52046/swallow/internal/overview/application"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// HealthResolver is the server context's best-effort, non-persisted health boundary.
type HealthResolver interface {
	ResolveHealth(ctx context.Context, serverIDs []string) (map[string]*serverdomain.HealthStatus, error)
}

// Reader projects repository-owned records into overview-owned summaries. It never writes
// or persists the derived view, and credentials are absent from every projection.
type Reader struct {
	sites          sitedomain.SiteRepository
	integrations   sitedomain.IntegrationRepository
	servers        serverdomain.ServerRepository
	health         HealthResolver
	platforms      platformdomain.PlatformRepository
	operations     operationdomain.ExecutionRepository
	orchestrations operationdomain.OrchestrationRepository
	alerts         *monitoringapp.AlertService
}

// NewReader wires the existing context repositories into the overview read boundary.
func NewReader(
	sites sitedomain.SiteRepository,
	integrations sitedomain.IntegrationRepository,
	servers serverdomain.ServerRepository,
	health HealthResolver,
	platforms platformdomain.PlatformRepository,
	operations operationdomain.ExecutionRepository,
	alerts *monitoringapp.AlertService,
	orchestrations ...operationdomain.OrchestrationRepository,
) *Reader {
	reader := &Reader{sites: sites, integrations: integrations, servers: servers, health: health, platforms: platforms, operations: operations, alerts: alerts}
	if len(orchestrations) > 0 {
		reader.orchestrations = orchestrations[0]
	}
	return reader
}

// ListSites returns identities only; Site descriptions are not part of the overview.
func (r *Reader) ListSites(ctx context.Context) ([]overviewapp.Site, error) {
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
func (r *Reader) ListIntegrations(ctx context.Context, siteID string) ([]overviewapp.Integration, error) {
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

// ListServers returns all scoped projections because counts must not inherit API pagination.
// Health is best-effort: an unavailable metrics store leaves states unknown, matching the
// Server list contract instead of failing durable inventory.
func (r *Reader) ListServers(ctx context.Context, siteID string) ([]overviewapp.Server, error) {
	result, err := r.servers.List(ctx, serverdomain.ListFilter{SiteID: siteID, IncludeAbsent: true})
	if err != nil {
		return nil, fmt.Errorf("list overview servers: %w", err)
	}
	healthByID := map[string]*serverdomain.HealthStatus{}
	if r.health != nil && len(result.Servers) > 0 {
		ids := make([]string, len(result.Servers))
		for i, server := range result.Servers {
			ids[i] = server.ID
		}
		if resolved, resolveErr := r.health.ResolveHealth(ctx, ids); resolveErr == nil {
			healthByID = resolved
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
		for _, gpu := range server.Observed.GPUs {
			item.GPUDevices += gpu.Count
		}
		if health := healthByID[server.ID]; health != nil {
			item.HealthState = string(health.State)
		}
		items[i] = item
	}
	return items, nil
}

// ListPlatforms returns reachability and the correlation gap from each platform's last sync.
func (r *Reader) ListPlatforms(ctx context.Context, siteID string) ([]overviewapp.Platform, error) {
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
func (r *Reader) ListOperations(ctx context.Context, siteID string) ([]overviewapp.Operation, error) {
	result, err := r.operations.List(ctx, operationdomain.ExecutionListFilter{SiteID: siteID})
	if err != nil {
		return nil, fmt.Errorf("list overview operations: %w", err)
	}
	items := make([]overviewapp.Operation, len(result.Operations))
	for i, operation := range result.Operations {
		items[i] = overviewapp.Operation{
			ID: operation.ID, Kind: string(operation.Kind), Intent: operation.Intent,
			SiteID: operation.SiteID, PlatformID: operation.PlatformID,
			TargetServerIDs:    append([]string(nil), operation.TargetServerIDs...),
			RetryOfOperationID: operation.RetryOfOperationID,
			RunID:              operation.Execution.RunID, Playbook: operation.Execution.Playbook,
			Status: string(operation.Execution.Status), StatusReason: operation.Execution.StatusReason,
			StartedAt: operation.Execution.StartedAt, FinishedAt: operation.Execution.FinishedAt,
			RequestedBy: operation.RequestedBy, RequestedAt: operation.RequestedAt,
			UpdatedAt: operation.UpdatedAt,
		}
	}
	if r.orchestrations != nil {
		v3, _, listErr := r.orchestrations.List(ctx, operationdomain.OrchestrationFilter{SiteID: siteID})
		if listErr != nil {
			return nil, fmt.Errorf("list overview orchestration operations: %w", listErr)
		}
		for _, operation := range v3 {
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
	}
	return items, nil
}

// ListFiringAlerts maps optional-provider failures to the overview's partial-state sentinel.
// Unexpected mapping or programming failures remain fatal.
func (r *Reader) ListFiringAlerts(ctx context.Context, siteID string) ([]overviewapp.Alert, error) {
	alerts, err := r.alerts.List(ctx, monitoringapp.ListAlertsInput{SiteID: siteID, State: "firing"})
	if err != nil {
		var queryErr *monitoringdomain.QueryError
		if errors.Is(err, monitoringdomain.ErrNoAlertSource) ||
			errors.Is(err, monitoringdomain.ErrNoMetricsIntegration) ||
			errors.As(err, &queryErr) {
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
