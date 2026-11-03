package application

import (
	"context"
	"errors"
	"fmt"
	"slices"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// MetricNames lists the metrics a client may ask for, in user scan order.
func MetricNames() []string {
	metrics := monitoringdomain.NamedMetrics()
	names := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		names = append(names, metric.Name)
	}
	return names
}

// resolveMetrics validates requested metric names, defaulting to every name. An unknown name is
// ErrUnknownMetric with the available names, so a typo is answered with what would have worked.
func resolveMetrics(requested []string) ([]monitoringdomain.Metric, error) {
	metrics := monitoringdomain.NamedMetrics()
	if len(requested) == 0 {
		return metrics, nil
	}
	resolved := make([]monitoringdomain.Metric, 0, len(requested))
	for _, name := range requested {
		at := slices.IndexFunc(metrics, func(metric monitoringdomain.Metric) bool { return metric.Name == name })
		if at < 0 {
			return nil, fmt.Errorf("%w: %q; available metrics are %v", ErrUnknownMetric, name, MetricNames())
		}
		resolved = append(resolved, metrics[at])
	}
	return resolved, nil
}

// ServerMetricsItem is one server's metric values. A metric the store had no answer for
// is absent from the map rather than zero, because zero utilisation and no data are
// different facts.
type ServerMetricsItem struct {
	ServerID string             `json:"serverId"`
	Metrics  map[string]float64 `json:"metrics"`
}

// ServerMetricsService answers current-value reads of the named metrics for explicit Servers.
//
// It reads the store through MonitoringFactory and stores nothing. A missing metrics
// Integration surfaces as monitoringdomain.ErrNoMetricsIntegration so the delivery layer can
// answer provider_unavailable; an unknown metric name is ErrUnknownMetric.
type ServerMetricsService struct {
	factory monitoringdomain.MonitoringFactory
	sites   monitoringdomain.ServerSiteResolver
}

// NewServerMetricsService builds the current-value read; each Server is read from its Site's
// Prometheus.
func NewServerMetricsService(factory monitoringdomain.MonitoringFactory, sites monitoringdomain.ServerSiteResolver) *ServerMetricsService {
	return &ServerMetricsService{factory: factory, sites: sites}
}

// ErrUnknownMetric means a client asked for a query swallow does not define.
var ErrUnknownMetric = errors.New("unknown metric")

// Query evaluates the named metrics for the given servers, asking each Server's Site Prometheus
// about its own Servers only. Every id is validated before any store is asked, so one unsafe id
// fails the request with monitoringdomain.ErrInvalidServerID instead of a partial answer.
func (s *ServerMetricsService) Query(
	ctx context.Context,
	serverIDs []string,
	metrics []string,
) ([]ServerMetricsItem, error) {
	if len(serverIDs) == 0 {
		return []ServerMetricsItem{}, nil
	}

	resolved, err := resolveMetrics(metrics)
	if err != nil {
		return nil, err
	}

	if _, err := monitoringdomain.NewServerSet(serverIDs); err != nil {
		return nil, err
	}
	groups, err := groupBySite(ctx, s.sites, serverIDs)
	if err != nil {
		return nil, err
	}

	byServer := make(map[string]map[string]float64, len(serverIDs))
	if len(groups) > 0 {
		sites, err := siteQueriers(ctx, s.factory, groups)
		if err != nil {
			return nil, err
		}
		for _, site := range sites {
			servers, err := monitoringdomain.NewServerSet(site.serverIDs)
			if err != nil {
				return nil, err
			}
			for _, metric := range resolved {
				samples, err := site.querier.Current(ctx, metric, servers)
				if err != nil {
					return nil, err
				}
				for _, sample := range samples {
					serverID := sample.ServerID()
					if serverID == "" {
						continue
					}
					if byServer[serverID] == nil {
						byServer[serverID] = map[string]float64{}
					}
					byServer[serverID][metric.Name] = sample.Value
				}
			}
		}
	}

	items := make([]ServerMetricsItem, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		values := byServer[serverID]
		if values == nil {
			values = map[string]float64{}
		}
		items = append(items, ServerMetricsItem{ServerID: serverID, Metrics: values})
	}
	return items, nil
}

// GrafanaLink returns the optional Grafana base URL for advanced exploration, or null when
// none is configured. swallow's own charts never depend on it; it is the escape hatch for
// questions the named queries cannot answer.
func (s *ServerMetricsService) GrafanaLink(ctx context.Context) *string {
	return grafanaLink(ctx, s.factory)
}

// grafanaLink reads the optional Grafana URL from a factory that knows one. The capability is
// optional on MonitoringFactory because only the production factory reads Integration
// settings; any other factory simply has no link.
func grafanaLink(ctx context.Context, factory monitoringdomain.MonitoringFactory) *string {
	linker, ok := factory.(interface {
		GrafanaURL(ctx context.Context, siteID string) string
	})
	if !ok {
		return nil
	}
	return wire.String(linker.GrafanaURL(ctx, ""))
}
