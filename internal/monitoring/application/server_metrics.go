package application

import (
	"context"
	"errors"
	"fmt"

	monitoringdomain "github.com/AFDEAPAC/swallow/internal/monitoring/domain"
	"github.com/AFDEAPAC/swallow/internal/shared/wire"
)

// namedQueries is the complete set of expressions gdcm will evaluate.
//
// A fixed set rather than a PromQL passthrough: an arbitrary expression through an
// authenticated API makes gdcm responsible for query cost it cannot predict, and lets
// one client trigger a fleet-wide high-cardinality scan. Exploration belongs in Grafana,
// which gdcm deep-links to.
//
// Each expression takes the server_id label pattern via %s and must aggregate
// by (server_id) so that results can be attributed.
var namedQueries = map[string]string{
	"cpuUsagePercent": `100 - (avg by (server_id) (rate(node_cpu_seconds_total{mode="idle",server_id=~"%s"}[5m])) * 100)`,
	"memoryUsedPercent": `100 * (1 - sum by (server_id) (node_memory_MemAvailable_bytes{server_id=~"%s"}) ` +
		`/ sum by (server_id) (node_memory_MemTotal_bytes{server_id=~"%s"}))`,
	"gpuUtilizationPercent": `avg by (server_id) (DCGM_FI_DEV_GPU_UTIL{server_id=~"%s"})`,
	"gpuTemperatureCelsius": `max by (server_id) (DCGM_FI_DEV_GPU_TEMP{server_id=~"%s"})`,
	"gpuPowerWatts":         `sum by (server_id) (DCGM_FI_DEV_POWER_USAGE{server_id=~"%s"})`,
}

// MetricNames lists the queries a client may ask for.
func MetricNames() []string {
	names := make([]string, 0, len(namedQueries))
	for name := range namedQueries {
		names = append(names, name)
	}
	return names
}

// ServerMetricsItem is one server's metric values. A metric the store had no answer for
// is absent from the map rather than zero, because zero utilisation and no data are
// different facts.
type ServerMetricsItem struct {
	ServerID string             `json:"serverId"`
	Metrics  map[string]float64 `json:"metrics"`
}

type ServerMetricsService struct {
	factory monitoringdomain.MonitoringFactory
}

func NewServerMetricsService(factory monitoringdomain.MonitoringFactory) *ServerMetricsService {
	return &ServerMetricsService{factory: factory}
}

// ErrUnknownMetric means a client asked for a query gdcm does not define.
var ErrUnknownMetric = errors.New("unknown metric")

// Query evaluates the named metrics for the given servers.
func (s *ServerMetricsService) Query(
	ctx context.Context,
	serverIDs []string,
	metrics []string,
) ([]ServerMetricsItem, error) {
	if len(serverIDs) == 0 {
		return []ServerMetricsItem{}, nil
	}

	if len(metrics) == 0 {
		metrics = MetricNames()
	}
	for _, name := range metrics {
		if _, ok := namedQueries[name]; !ok {
			return nil, fmt.Errorf("%w: %q; available metrics are %v", ErrUnknownMetric, name, MetricNames())
		}
	}

	querier, err := s.factory.Querier(ctx, "")
	if err != nil {
		return nil, err
	}

	pattern, err := labelPattern(serverIDs)
	if err != nil {
		return nil, err
	}

	byServer := make(map[string]map[string]float64, len(serverIDs))
	for _, name := range metrics {
		expr := expandQuery(namedQueries[name], pattern)

		samples, err := querier.Query(ctx, expr)
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
			byServer[serverID][name] = sample.Value
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

// expandQuery substitutes the label pattern into every placeholder in an expression,
// since some queries reference the matcher more than once.
func expandQuery(template, pattern string) string {
	count := 0
	for i := 0; i+1 < len(template); i++ {
		if template[i] == '%' && template[i+1] == 's' {
			count++
		}
	}

	args := make([]any, count)
	for i := range args {
		args[i] = pattern
	}
	return fmt.Sprintf(template, args...)
}

// GrafanaLink returns a Grafana base URL for a client to deep-link into, or null when
// none is configured. Dashboards are Grafana's job; gdcm only points at it.
func (s *ServerMetricsService) GrafanaLink(ctx context.Context) *string {
	factory, ok := s.factory.(interface {
		GrafanaURL(ctx context.Context, siteID string) string
	})
	if !ok {
		return nil
	}
	return wire.String(factory.GrafanaURL(ctx, ""))
}
