package promstack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
)

// newTestQuerier serves handler as the metrics store and returns a querier pointed at it.
func newTestQuerier(t *testing.T, handler http.HandlerFunc) *Querier {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "store-token", 5*time.Second, false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return NewQuerier(client)
}

// mustServerSet builds a checked ServerSet for a test.
func mustServerSet(t *testing.T, ids ...string) monitoringdomain.ServerSet {
	t.Helper()
	servers, err := monitoringdomain.NewServerSet(ids)
	if err != nil {
		t.Fatalf("NewServerSet(%v): %v", ids, err)
	}
	return servers
}

// namedMetric finds one of the core's named metrics, so a test uses the core's aggregate rather
// than restating it.
func namedMetric(t *testing.T, name string) monitoringdomain.Metric {
	t.Helper()
	for _, metric := range monitoringdomain.NamedMetrics() {
		if metric.Name == name {
			return metric
		}
	}
	t.Fatalf("%q is not a named metric", name)
	return monitoringdomain.Metric{}
}

// Range queries go out as a form-encoded POST, so a long server_id alternation never meets a
// URL-length limit, and carry the window in Unix seconds.
func TestQueryRange_PostsFormAndParsesMatrix(t *testing.T) {
	var gotMethod, gotQuery, gotStart, gotEnd, gotStep, gotAuth string
	querier := newTestQuerier(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAuth = r.Method, r.Header.Get("Authorization")
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotQuery, gotStart, gotEnd, gotStep = r.PostForm.Get("query"), r.PostForm.Get("start"), r.PostForm.Get("end"), r.PostForm.Get("step")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
			`{"metric":{"server_id":"srv-1"},"values":[[1760230800,"12.5"],[1760230830,"NaN"],[1760230860,"+Inf"],[1760230890,"13"]]}]}}`))
	})

	start := time.Unix(1760230800, 0)
	series, err := querier.queryRange(context.Background(), `up{server_id=~"srv-1"}`, monitoringdomain.TimeRange{
		Start: start, End: start.Add(time.Hour), Step: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("queryRange: %v", err)
	}

	if gotMethod != http.MethodPost || gotAuth != "Bearer store-token" {
		t.Errorf("request = %s with auth %q, want POST with the bearer token", gotMethod, gotAuth)
	}
	if gotQuery != `up{server_id=~"srv-1"}` || gotStart != "1760230800" || gotEnd != "1760234400" || gotStep != "30" {
		t.Errorf("form = query %q start %s end %s step %s", gotQuery, gotStart, gotEnd, gotStep)
	}
	if len(series) != 1 || series[0].ServerID() != "srv-1" {
		t.Fatalf("series = %+v, want one series for srv-1", series)
	}
	// NaN and +Inf are gaps, not values.
	points := series[0].Points
	if len(points) != 2 || points[0].Value != 12.5 || points[1].Value != 13 {
		t.Errorf("points = %+v, want the two finite values", points)
	}
}

// A 200 with status "error" is a rejected expression, not an outage.
func TestQueryRange_LogicalErrorIsRejected(t *testing.T) {
	querier := newTestQuerier(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"error","error":"parse error"}`))
	})

	_, err := querier.queryRange(context.Background(), "bad(", monitoringdomain.TimeRange{
		Start: time.Unix(0, 0), End: time.Unix(3600, 0), Step: time.Minute,
	})
	var queryErr *monitoringdomain.QueryError
	if !errors.As(err, &queryErr) || queryErr.Kind != monitoringdomain.QueryErrorRejected {
		t.Errorf("queryRange error = %v, want a rejected QueryError", err)
	}
}

// The instant read drops non-finite samples too, so a zero-sized filesystem cannot break the
// JSON response with a NaN.
func TestQuery_DropsNonFiniteSamples(t *testing.T) {
	querier := newTestQuerier(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[` +
			`{"metric":{"server_id":"srv-1"},"value":[1760230800,"NaN"]},` +
			`{"metric":{"server_id":"srv-2"},"value":[1760230800,"41.5"]}]}}`))
	})

	samples, err := querier.query(context.Background(), "x")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(samples) != 1 || samples[0].ServerID() != "srv-2" {
		t.Errorf("samples = %+v, want only the finite srv-2 sample", samples)
	}
}

// Every named metric the core defines has an expression here, and its scope aggregate has an
// operator, so no name the API lists fails when it is read.
func TestMetricExpressions_CoverEveryNamedMetric(t *testing.T) {
	servers := mustServerSet(t, "srv-1")
	for _, metric := range monitoringdomain.NamedMetrics() {
		for _, combined := range []bool{false, true} {
			expr, err := metricExpression(metric, servers, combined)
			if err != nil {
				t.Errorf("metricExpression(%q, combined=%t) error = %v", metric.Name, combined, err)
				continue
			}
			if strings.Contains(expr, "%!") || !strings.Contains(expr, `server_id=~"srv-1"`) {
				t.Errorf("metricExpression(%q, combined=%t) = %q, want every matcher filled with the server set", metric.Name, combined, expr)
			}
		}
	}
	if len(metricExpressions) != len(monitoringdomain.NamedMetrics()) {
		t.Errorf("metricExpressions has %d entries, want one per named metric (%d)", len(metricExpressions), len(monitoringdomain.NamedMetrics()))
	}
}

// The expressions are the Node Exporter queries swallow has always evaluated: CPU busy, memory and
// root-filesystem fill, and network throughput without loopback and virtual devices, so traffic
// bridged into a container is not counted twice. A scope wraps them in the metric's aggregate.
func TestMetricExpressions_ReadTheNodeExporterSeries(t *testing.T) {
	const devices = `device!~"lo|veth.*|docker.*|br-.*|virbr.*|cali.*|flannel.*|cni.*|vxlan.*|tun.*|tap.*|kube-.*"`
	tests := []struct {
		metric       string
		wantServer   string
		wantCombined string
	}{
		{
			metric:       monitoringdomain.MetricCPUUsagePercent,
			wantServer:   `100 - (avg by (server_id) (rate(node_cpu_seconds_total{mode="idle",server_id=~"srv-1|srv-2"}[5m])) * 100)`,
			wantCombined: `avg (100 - (avg by (server_id) (rate(node_cpu_seconds_total{mode="idle",server_id=~"srv-1|srv-2"}[5m])) * 100))`,
		},
		{
			metric: monitoringdomain.MetricMemoryUsedPercent,
			wantServer: `100 * (1 - sum by (server_id) (node_memory_MemAvailable_bytes{server_id=~"srv-1|srv-2"}) ` +
				`/ sum by (server_id) (node_memory_MemTotal_bytes{server_id=~"srv-1|srv-2"}))`,
			wantCombined: `avg (100 * (1 - sum by (server_id) (node_memory_MemAvailable_bytes{server_id=~"srv-1|srv-2"}) ` +
				`/ sum by (server_id) (node_memory_MemTotal_bytes{server_id=~"srv-1|srv-2"})))`,
		},
		{
			metric: monitoringdomain.MetricDiskUsedPercent,
			wantServer: `100 * (1 - sum by (server_id) (node_filesystem_avail_bytes{mountpoint="/",server_id=~"srv-1|srv-2"}) ` +
				`/ sum by (server_id) (node_filesystem_size_bytes{mountpoint="/",server_id=~"srv-1|srv-2"}))`,
			wantCombined: `avg (100 * (1 - sum by (server_id) (node_filesystem_avail_bytes{mountpoint="/",server_id=~"srv-1|srv-2"}) ` +
				`/ sum by (server_id) (node_filesystem_size_bytes{mountpoint="/",server_id=~"srv-1|srv-2"})))`,
		},
		{
			metric:       monitoringdomain.MetricNetworkReceiveBytesPerSecond,
			wantServer:   `sum by (server_id) (rate(node_network_receive_bytes_total{` + devices + `,server_id=~"srv-1|srv-2"}[5m]))`,
			wantCombined: `sum (sum by (server_id) (rate(node_network_receive_bytes_total{` + devices + `,server_id=~"srv-1|srv-2"}[5m])))`,
		},
		{
			metric:       monitoringdomain.MetricNetworkTransmitBytesPerSecond,
			wantServer:   `sum by (server_id) (rate(node_network_transmit_bytes_total{` + devices + `,server_id=~"srv-1|srv-2"}[5m]))`,
			wantCombined: `sum (sum by (server_id) (rate(node_network_transmit_bytes_total{` + devices + `,server_id=~"srv-1|srv-2"}[5m])))`,
		},
	}
	servers := mustServerSet(t, "srv-1", "srv-2")
	for _, tc := range tests {
		t.Run(tc.metric, func(t *testing.T) {
			metric := namedMetric(t, tc.metric)
			if got, err := metricExpression(metric, servers, false); err != nil || got != tc.wantServer {
				t.Errorf("metricExpression(%q, per Server) = %q, %v\nwant %q", tc.metric, got, err, tc.wantServer)
			}
			if got, err := metricExpression(metric, servers, true); err != nil || got != tc.wantCombined {
				t.Errorf("metricExpression(%q, combined) = %q, %v\nwant %q", tc.metric, got, err, tc.wantCombined)
			}
		})
	}
}

// Each scope aggregate maps to its PromQL operator; maximum has no named metric yet but must not
// fail once one uses it.
func TestMetricExpression_MapsEveryScopeAggregate(t *testing.T) {
	servers := mustServerSet(t, "srv-1")
	for aggregate, want := range map[monitoringdomain.ScopeAggregate]string{
		monitoringdomain.ScopeAverage: "avg (",
		monitoringdomain.ScopeSum:     "sum (",
		monitoringdomain.ScopeMaximum: "max (",
	} {
		metric := monitoringdomain.Metric{Name: monitoringdomain.MetricCPUUsagePercent, Aggregate: aggregate}
		if got, err := metricExpression(metric, servers, true); err != nil || !strings.HasPrefix(got, want) {
			t.Errorf("metricExpression(aggregate %q) = %q, %v, want prefix %q", aggregate, got, err, want)
		}
	}
}

// A metric or aggregate the adapter does not know is an error, never a guessed expression.
func TestMetricExpression_UnknownIsAnError(t *testing.T) {
	servers := mustServerSet(t, "srv-1")
	if _, err := metricExpression(monitoringdomain.Metric{Name: "gpuUtilizationPercent", Aggregate: monitoringdomain.ScopeAverage}, servers, false); err == nil {
		t.Error("metricExpression(unknown metric) error = nil, want an error")
	}
	if _, err := metricExpression(monitoringdomain.Metric{Name: monitoringdomain.MetricCPUUsagePercent, Aggregate: "median"}, servers, true); err == nil {
		t.Error("metricExpression(unknown aggregate) error = nil, want an error")
	}
}

// Health reads the named exporter's up per Server and nothing else.
func TestExporterUpExpression(t *testing.T) {
	got := exporterUpExpression(monitoringdomain.HealthExporter, mustServerSet(t, "srv-1", "srv-2"))
	want := `max by (server_id) (up{exporter="node-exporter", server_id=~"srv-1|srv-2"})`
	if got != want {
		t.Errorf("exporterUpExpression = %q, want %q", got, want)
	}
}

// The structured reads send the named expressions: instant reads as GET, history as a POST with the
// combined expression in aggregate mode.
func TestQuerier_SendsTheNamedExpressions(t *testing.T) {
	var got []string
	querier := newTestQuerier(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		got = append(got, r.Method+" "+r.URL.Path+" "+r.Form.Get("query"))
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	})
	ctx := context.Background()
	servers := mustServerSet(t, "srv-1")
	cpu := namedMetric(t, monitoringdomain.MetricCPUUsagePercent)

	if _, err := querier.Current(ctx, cpu, servers); err != nil {
		t.Fatalf("Current: %v", err)
	}
	window := monitoringdomain.TimeRange{Start: time.Unix(0, 0), End: time.Unix(3600, 0), Step: 30 * time.Second}
	if _, err := querier.History(ctx, cpu, servers, window, true); err != nil {
		t.Fatalf("History: %v", err)
	}
	if _, err := querier.ExporterUp(ctx, monitoringdomain.HealthExporter, servers); err != nil {
		t.Fatalf("ExporterUp: %v", err)
	}

	perServer, _ := metricExpression(cpu, servers, false)
	combined, _ := metricExpression(cpu, servers, true)
	want := []string{
		"GET /api/v1/query " + perServer,
		"POST /api/v1/query_range " + combined,
		"GET /api/v1/query " + exporterUpExpression(monitoringdomain.HealthExporter, servers),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// An empty ServerSet matches no Server, so it is answered without a store request; an empty
// matcher would otherwise select series that carry no server_id at all.
func TestQuerier_EmptyServerSetAsksNothing(t *testing.T) {
	requests := 0
	querier := newTestQuerier(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	})
	ctx := context.Background()
	cpu := namedMetric(t, monitoringdomain.MetricCPUUsagePercent)
	var empty monitoringdomain.ServerSet

	if samples, err := querier.Current(ctx, cpu, empty); err != nil || samples != nil {
		t.Errorf("Current(empty) = %v, %v, want no samples", samples, err)
	}
	if series, err := querier.History(ctx, cpu, empty, monitoringdomain.TimeRange{}, true); err != nil || series != nil {
		t.Errorf("History(empty) = %v, %v, want no series", series, err)
	}
	if samples, err := querier.ExporterUp(ctx, monitoringdomain.HealthExporter, empty); err != nil || samples != nil {
		t.Errorf("ExporterUp(empty) = %v, %v, want no samples", samples, err)
	}
	if requests != 0 {
		t.Errorf("store requests = %d, want 0", requests)
	}
}
