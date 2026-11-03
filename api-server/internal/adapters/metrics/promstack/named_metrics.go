package promstack

import (
	"fmt"
	"strings"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
)

// networkDeviceExclusion matches the loopback and the virtual devices container runtimes and
// hypervisors create. Counting a veth or a bridge as well as the physical NIC it forwards to
// would double the reported traffic of a host running containers or VMs.
const networkDeviceExclusion = `lo|veth.*|docker.*|br-.*|virbr.*|cali.*|flannel.*|cni.*|vxlan.*|tun.*|tap.*|kube-.*`

// metricExpressions computes each of the core's named metrics from Node Exporter series. Every
// `%[1]s` takes the server_id regex alternation, and every expression aggregates by (server_id)
// so one result is attributed to exactly one Server.
//
// The current-value and history reads evaluate these same expressions, so a tile and a chart for
// one metric cannot disagree about what they measure. A metric in monitoringdomain.NamedMetrics
// without an entry here cannot be read; the package tests keep the two in step.
var metricExpressions = map[string]string{
	monitoringdomain.MetricCPUUsagePercent: `100 - (avg by (server_id) (rate(node_cpu_seconds_total{mode="idle",server_id=~"%[1]s"}[5m])) * 100)`,
	monitoringdomain.MetricMemoryUsedPercent: `100 * (1 - sum by (server_id) (node_memory_MemAvailable_bytes{server_id=~"%[1]s"}) ` +
		`/ sum by (server_id) (node_memory_MemTotal_bytes{server_id=~"%[1]s"}))`,
	// The root filesystem only: per-mount series would need a mount label the caller cannot
	// choose, and "/" is the filesystem an OS deployment fills.
	monitoringdomain.MetricDiskUsedPercent: `100 * (1 - sum by (server_id) (node_filesystem_avail_bytes{mountpoint="/",server_id=~"%[1]s"}) ` +
		`/ sum by (server_id) (node_filesystem_size_bytes{mountpoint="/",server_id=~"%[1]s"}))`,
	monitoringdomain.MetricNetworkReceiveBytesPerSecond: `sum by (server_id) (rate(node_network_receive_bytes_total{device!~"` +
		networkDeviceExclusion + `",server_id=~"%[1]s"}[5m]))`,
	monitoringdomain.MetricNetworkTransmitBytesPerSecond: `sum by (server_id) (rate(node_network_transmit_bytes_total{device!~"` +
		networkDeviceExclusion + `",server_id=~"%[1]s"}[5m]))`,
}

// aggregationOperators are the PromQL operators of the core's scope aggregates. PromQL aggregation
// skips a Server that has no value at a timestamp, which is the contract's rule that a Server
// without data is not counted as zero.
var aggregationOperators = map[monitoringdomain.ScopeAggregate]string{
	monitoringdomain.ScopeAverage: "avg",
	monitoringdomain.ScopeSum:     "sum",
	monitoringdomain.ScopeMaximum: "max",
}

// serverMatcher is the regex alternation of a ServerSet's ids. The ids are already restricted to
// letters, digits, '-', and '_' by monitoringdomain.NewServerSet, so none needs escaping.
func serverMatcher(servers monitoringdomain.ServerSet) string {
	return strings.Join(servers.IDs(), "|")
}

// metricExpression is metric's per-Server expression over servers, or, when combined is true, the
// same expression wrapped in the metric's scope aggregate so the store answers one series without
// a server_id. A metric or aggregate this adapter does not know is an error rather than a guess.
func metricExpression(metric monitoringdomain.Metric, servers monitoringdomain.ServerSet, combined bool) (string, error) {
	template, ok := metricExpressions[metric.Name]
	if !ok {
		return "", fmt.Errorf("prometheus adapter has no expression for metric %q", metric.Name)
	}
	expr := fmt.Sprintf(template, serverMatcher(servers))
	if !combined {
		return expr, nil
	}
	operator, ok := aggregationOperators[metric.Aggregate]
	if !ok {
		return "", fmt.Errorf("prometheus adapter has no operator for scope aggregate %q of metric %q", metric.Aggregate, metric.Name)
	}
	return operator + " (" + expr + ")", nil
}

// exporterUpExpression is the per-Server scrape result of one exporter: Prometheus's `up` of the
// exporter's targets, which the http_sd target list labels with server_id and exporter.
func exporterUpExpression(exporter string, servers monitoringdomain.ServerSet) string {
	return fmt.Sprintf(`max by (%s) (up{%s=%q, %s=~"%s"})`,
		monitoringdomain.LabelServerID, monitoringdomain.LabelExporter, exporter,
		monitoringdomain.LabelServerID, serverMatcher(servers))
}

// exporterUpWithinExpression is the best scrape result of one exporter per Server over the window:
// 1 when any scrape in it succeeded, 0 when every one failed. The window is whole seconds, rounded
// down, which is how Prometheus range selectors are written.
func exporterUpWithinExpression(exporter string, servers monitoringdomain.ServerSet, window time.Duration) string {
	return fmt.Sprintf(`max by (%s) (max_over_time(up{%s=%q, %s=~"%s"}[%ds]))`,
		monitoringdomain.LabelServerID, monitoringdomain.LabelExporter, exporter,
		monitoringdomain.LabelServerID, serverMatcher(servers), int64(window/time.Second))
}
