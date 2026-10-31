package application

import (
	"context"
	"errors"
	"fmt"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// namedQuery is one metric swallow will evaluate: its per-server PromQL expression and how
// several servers combine into one series for a grouping (the contract's "scope aggregate").
type namedQuery struct {
	name string
	// expr takes the server_id label pattern via every %s and must aggregate by (server_id),
	// so a result can be attributed to exactly one server.
	expr string
	// aggregate is the PromQL aggregation operator applied over the per-server vector when a
	// scope is charted as one series: avg for utilisation and fill levels, sum for throughput
	// and power, max for temperature. Servers without a value at a timestamp do not take part,
	// which is what PromQL aggregation does over a vector.
	aggregate string
}

// networkDeviceExclusion matches the loopback and the virtual devices container runtimes and
// hypervisors create. Counting a veth or a bridge as well as the physical NIC it forwards to
// would double the reported traffic of a host running containers or VMs.
const networkDeviceExclusion = `lo|veth.*|docker.*|br-.*|virbr.*|cali.*|flannel.*|cni.*|vxlan.*|tun.*|tap.*|kube-.*`

// namedQueries is the complete, ordered set of expressions swallow will evaluate. The order is
// the operator scan order and is what MetricNames reports.
//
// A fixed set rather than a PromQL passthrough: an arbitrary expression through an
// authenticated API makes swallow responsible for query cost it cannot predict, and lets
// one client trigger a fleet-wide high-cardinality scan. Exploration beyond these belongs in
// the optional Grafana the metrics Integration may link to.
//
// The current-value and range endpoints evaluate these same expressions, so a tile and a chart
// for one metric cannot disagree about what they measure.
//
// The GPU expressions are vendor-neutral: each evaluates the NVIDIA DCGM series or the
// AMD RDC series with `or`, so one metric name serves both stacks and the caller never
// has to know which exporter a server runs. Only the first non-empty vector per server
// contributes, because a host runs one GPU exporter, not both. Units are aligned across
// vendors by the RDC exporter's metric catalog (Celsius, Watts), so no PromQL rescaling
// is needed here.
var namedQueries = []namedQuery{
	{
		name:      "cpuUsagePercent",
		expr:      `100 - (avg by (server_id) (rate(node_cpu_seconds_total{mode="idle",server_id=~"%s"}[5m])) * 100)`,
		aggregate: "avg",
	},
	{
		name: "memoryUsedPercent",
		expr: `100 * (1 - sum by (server_id) (node_memory_MemAvailable_bytes{server_id=~"%s"}) ` +
			`/ sum by (server_id) (node_memory_MemTotal_bytes{server_id=~"%s"}))`,
		aggregate: "avg",
	},
	{
		// The root filesystem only: per-mount series would need a mount label the caller cannot
		// choose, and "/" is the filesystem an OS deployment fills.
		name: "diskUsedPercent",
		expr: `100 * (1 - sum by (server_id) (node_filesystem_avail_bytes{mountpoint="/",server_id=~"%s"}) ` +
			`/ sum by (server_id) (node_filesystem_size_bytes{mountpoint="/",server_id=~"%s"}))`,
		aggregate: "avg",
	},
	{
		name: "networkReceiveBytesPerSecond",
		expr: `sum by (server_id) (rate(node_network_receive_bytes_total{device!~"` + networkDeviceExclusion +
			`",server_id=~"%s"}[5m]))`,
		aggregate: "sum",
	},
	{
		name: "networkTransmitBytesPerSecond",
		expr: `sum by (server_id) (rate(node_network_transmit_bytes_total{device!~"` + networkDeviceExclusion +
			`",server_id=~"%s"}[5m]))`,
		aggregate: "sum",
	},
	{
		name: "gpuUtilizationPercent",
		expr: `avg by (server_id) (DCGM_FI_DEV_GPU_UTIL{server_id=~"%s"}) ` +
			`or avg by (server_id) (gpu_util{server_id=~"%s"})`,
		aggregate: "avg",
	},
	{
		name: "gpuTemperatureCelsius",
		expr: `max by (server_id) (DCGM_FI_DEV_GPU_TEMP{server_id=~"%s"}) ` +
			`or max by (server_id) (gpu_temp{server_id=~"%s"})`,
		aggregate: "max",
	},
	{
		name: "gpuPowerWatts",
		expr: `sum by (server_id) (DCGM_FI_DEV_POWER_USAGE{server_id=~"%s"}) ` +
			`or sum by (server_id) (power_usage{server_id=~"%s"})`,
		aggregate: "sum",
	},
	{
		name: "gpuMemoryUsedPercent",
		expr: `100 * sum by (server_id) (DCGM_FI_DEV_FB_USED{server_id=~"%s"}) ` +
			`/ (sum by (server_id) (DCGM_FI_DEV_FB_USED{server_id=~"%s"}) + sum by (server_id) (DCGM_FI_DEV_FB_FREE{server_id=~"%s"})) ` +
			`or 100 * sum by (server_id) (gpu_memory_usage{server_id=~"%s"}) / sum by (server_id) (gpu_memory_total{server_id=~"%s"})`,
		aggregate: "avg",
	},
}

// MetricNames lists the queries a client may ask for, in operator scan order.
func MetricNames() []string {
	names := make([]string, 0, len(namedQueries))
	for _, query := range namedQueries {
		names = append(names, query.name)
	}
	return names
}

// lookupQuery finds a named query, reporting false for a name swallow does not define.
func lookupQuery(name string) (namedQuery, bool) {
	for _, query := range namedQueries {
		if query.name == name {
			return query, true
		}
	}
	return namedQuery{}, false
}

// resolveMetrics validates requested metric names, defaulting to every name. An unknown name is
// ErrUnknownMetric with the available names, so a typo is answered with what would have worked.
func resolveMetrics(requested []string) ([]namedQuery, error) {
	if len(requested) == 0 {
		return append([]namedQuery(nil), namedQueries...), nil
	}
	queries := make([]namedQuery, 0, len(requested))
	for _, name := range requested {
		query, ok := lookupQuery(name)
		if !ok {
			return nil, fmt.Errorf("%w: %q; available metrics are %v", ErrUnknownMetric, name, MetricNames())
		}
		queries = append(queries, query)
	}
	return queries, nil
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
}

// NewServerMetricsService builds the current-value read over the given factory.
func NewServerMetricsService(factory monitoringdomain.MonitoringFactory) *ServerMetricsService {
	return &ServerMetricsService{factory: factory}
}

// ErrUnknownMetric means a client asked for a query swallow does not define.
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

	queries, err := resolveMetrics(metrics)
	if err != nil {
		return nil, err
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
	for _, query := range queries {
		expr := expandQuery(query.expr, pattern)

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
			byServer[serverID][query.name] = sample.Value
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
