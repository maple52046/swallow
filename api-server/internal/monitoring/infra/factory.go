package infra

import (
	"context"
	"fmt"
	"sync"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/monitoring/domain"
	"github.com/maple52046/swallow/internal/monitoring/infra/promstack"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

const defaultTimeout = 30 * time.Second

// Setting keys an operator may set on a metrics integration.
const (
	SettingTimeout            = "timeout"
	SettingInsecureSkipVerify = "insecureSkipVerify"
	// SettingAlertmanagerURL points at Alertmanager, which is a separate address
	// from the query API. Kept as a setting on the metrics integration rather than a
	// separate integration kind, because they are one monitoring stack operationally.
	SettingAlertmanagerURL = "alertmanagerUrl"
	// SettingGrafanaURL is used to build deep links, so that exploration stays in
	// Grafana instead of being reimplemented.
	SettingGrafanaURL = "grafanaUrl"
)

// MonitoringFactory builds monitoring clients from registered integrations.
type MonitoringFactory struct {
	integrations sitedomain.IntegrationRepository

	mu       sync.Mutex
	queriers map[string]cachedQuerier
	alerts   map[string]cachedAlertSource
}

type cachedQuerier struct {
	querier   monitoringdomain.MetricsQuerier
	updatedAt time.Time
}

type cachedAlertSource struct {
	source    monitoringdomain.AlertSource
	updatedAt time.Time
}

func NewMonitoringFactory(integrations sitedomain.IntegrationRepository) *MonitoringFactory {
	return &MonitoringFactory{
		integrations: integrations,
		queriers:     make(map[string]cachedQuerier),
		alerts:       make(map[string]cachedAlertSource),
	}
}

func (f *MonitoringFactory) Querier(ctx context.Context, siteID string) (monitoringdomain.MetricsQuerier, error) {
	integration, err := f.metricsIntegration(ctx, siteID)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	if hit, ok := f.queriers[integration.ID]; ok && hit.updatedAt.Equal(integration.UpdatedAt) {
		f.mu.Unlock()
		return hit.querier, nil
	}
	f.mu.Unlock()

	client, err := f.clientFor(ctx, integration, integration.Endpoint)
	if err != nil {
		return nil, err
	}
	querier := promstack.NewQuerier(client)

	f.mu.Lock()
	f.queriers[integration.ID] = cachedQuerier{querier: querier, updatedAt: integration.UpdatedAt}
	f.mu.Unlock()

	return querier, nil
}

func (f *MonitoringFactory) Alerts(ctx context.Context, siteID string) (monitoringdomain.AlertSource, error) {
	integration, err := f.metricsIntegration(ctx, siteID)
	if err != nil {
		return nil, err
	}

	alertmanagerURL := integration.Setting(SettingAlertmanagerURL, "")
	if alertmanagerURL == "" {
		return nil, monitoringdomain.ErrNoAlertSource
	}

	f.mu.Lock()
	if hit, ok := f.alerts[integration.ID]; ok && hit.updatedAt.Equal(integration.UpdatedAt) {
		f.mu.Unlock()
		return hit.source, nil
	}
	f.mu.Unlock()

	client, err := f.clientFor(ctx, integration, alertmanagerURL)
	if err != nil {
		return nil, err
	}
	source := promstack.NewAlertSource(client)

	f.mu.Lock()
	f.alerts[integration.ID] = cachedAlertSource{source: source, updatedAt: integration.UpdatedAt}
	f.mu.Unlock()

	return source, nil
}

// GrafanaURL returns the configured Grafana base address, or "" when none is set.
func (f *MonitoringFactory) GrafanaURL(ctx context.Context, siteID string) string {
	integration, err := f.metricsIntegration(ctx, siteID)
	if err != nil {
		return ""
	}
	return integration.Setting(SettingGrafanaURL, "")
}

// metricsIntegration finds the metrics integration to use.
//
// An empty siteID matches any, which is correct while an installation has one central
// store: every site's Prometheus remote-writes into it, so any site's registration
// points at the same place.
func (f *MonitoringFactory) metricsIntegration(ctx context.Context, siteID string) (*sitedomain.Integration, error) {
	integrations, err := f.integrations.List(ctx, sitedomain.IntegrationFilter{
		SiteID:      siteID,
		Kind:        sitedomain.IntegrationKindMetrics,
		EnabledOnly: true,
	})
	if err != nil {
		return nil, err
	}
	if len(integrations) == 0 {
		return nil, monitoringdomain.ErrNoMetricsIntegration
	}
	return integrations[0], nil
}

func (f *MonitoringFactory) clientFor(
	ctx context.Context,
	integration *sitedomain.Integration,
	endpoint string,
) (*promstack.Client, error) {
	if integration.ProviderKind != sitedomain.ProviderKindPrometheus {
		return nil, fmt.Errorf("unsupported metrics backend kind %q", integration.ProviderKind)
	}

	// A central store often sits behind no authentication at all, so a missing
	// credential is not an error here.
	token := ""
	if stored, err := f.integrations.Credential(ctx, integration.ID); err == nil {
		token = stored
	}

	timeout := defaultTimeout
	if raw := integration.Setting(SettingTimeout, ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}

	return promstack.NewClient(
		endpoint,
		token,
		timeout,
		integration.SettingBool(SettingInsecureSkipVerify),
	)
}
