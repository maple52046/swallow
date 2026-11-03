package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	monitoringapp "github.com/maple52046/swallow/internal/core/monitoring/application"
	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// --- health axis resolution ---

func TestHealthResolver_MapsUpMetricToHealthAxis(t *testing.T) {
	factory := newFakeMonitoringFactory()
	factory.querier.up["node-exporter"] = []monitoringdomain.Sample{
		{Labels: map[string]string{"server_id": "srv-1"}, Value: 1, Timestamp: time.Now().UTC()},
		{Labels: map[string]string{"server_id": "srv-2"}, Value: 0, Timestamp: time.Now().UTC()},
	}
	resolver := monitoringapp.NewHealthResolver(factory, everyServerInSite{siteID: testSiteID})

	health, err := resolver.ResolveHealth(context.Background(), []string{"srv-1", "srv-2", "srv-3"})
	if err != nil {
		t.Fatalf("ResolveHealth: %v", err)
	}

	if health["srv-1"] == nil || health["srv-1"].State != serverdomain.HealthUp {
		t.Errorf("srv-1 should be up, got %+v", health["srv-1"])
	}
	if health["srv-2"] == nil || health["srv-2"].State != serverdomain.HealthDown {
		t.Errorf("srv-2 should be down, got %+v", health["srv-2"])
	}
	// A server the store said nothing about is unknown. Reporting it as down would be
	// a claim nobody made.
	if _, present := health["srv-3"]; present {
		t.Error("a server with no sample must be absent from the result, not down")
	}
}

// Health is the Node Exporter's up only: a GPU exporter that answers does not make a Server up.
func TestHealthResolver_ReadsTheNodeExporterOnly(t *testing.T) {
	factory := newFakeMonitoringFactory()
	resolver := monitoringapp.NewHealthResolver(factory, everyServerInSite{siteID: testSiteID})

	if _, err := resolver.ResolveHealth(context.Background(), []string{"srv-1"}); err != nil {
		t.Fatalf("ResolveHealth: %v", err)
	}

	reads := factory.querier.upReads
	if len(reads) != 1 || reads[0].name != "node-exporter" || !slices.Equal(reads[0].servers, []string{"srv-1"}) {
		t.Errorf("up reads = %+v, want one node-exporter read for srv-1", reads)
	}
	if len(factory.querier.currentReads) != 0 {
		t.Errorf("metric reads = %+v, want none: health reads only the exporter's up", factory.querier.currentReads)
	}
}

// With no metrics store registered, health is simply unknown. It is not an error and
// must not fail the caller.
func TestHealthResolver_NoIntegrationIsNotAnError(t *testing.T) {
	factory := newFakeMonitoringFactory()
	factory.querierErr = monitoringdomain.ErrNoMetricsIntegration
	resolver := monitoringapp.NewHealthResolver(factory, everyServerInSite{siteID: testSiteID})

	health, err := resolver.ResolveHealth(context.Background(), []string{"srv-1"})
	if err != nil {
		t.Fatalf("a missing metrics integration must not be an error: %v", err)
	}
	if len(health) != 0 {
		t.Errorf("expected no health, got %v", health)
	}
}

// Each Server's health comes from its own Site's Prometheus; a Server whose Site has none, or that
// the Server projection does not know, is unknown.
func TestHealthResolver_AsksEachServersSite(t *testing.T) {
	east, west := newFakeQuerier(), newFakeQuerier()
	east.up["node-exporter"] = []monitoringdomain.Sample{{Labels: map[string]string{"server_id": "srv-e"}, Value: 1, Timestamp: time.Now().UTC()}}
	west.up["node-exporter"] = []monitoringdomain.Sample{{Labels: map[string]string{"server_id": "srv-w"}, Value: 0, Timestamp: time.Now().UTC()}}
	resolver := monitoringapp.NewHealthResolver(perSiteMonitoringFactory{"east": east, "west": west},
		fixedServerSites{"srv-e": "east", "srv-w": "west", "srv-n": "north"})

	health, err := resolver.ResolveHealth(context.Background(), []string{"srv-e", "srv-w", "srv-n", "srv-unknown"})
	if err != nil {
		t.Fatalf("ResolveHealth: %v", err)
	}
	if health["srv-e"] == nil || health["srv-e"].State != serverdomain.HealthUp ||
		health["srv-w"] == nil || health["srv-w"].State != serverdomain.HealthDown {
		t.Errorf("health = %+v, want srv-e up from east and srv-w down from west", health)
	}
	if _, ok := health["srv-n"]; ok {
		t.Error("a Server whose Site has no Prometheus must be unknown")
	}
	if len(east.upReads) != 1 || !slices.Equal(east.upReads[0].servers, []string{"srv-e"}) {
		t.Errorf("east up reads = %+v, want only srv-e", east.upReads)
	}
}

// Identifiers are validated rather than escaped: anything outside the swallow identifier
// character set did not come from swallow and must not be interpolated into a query.
func TestHealthResolver_RejectsUnsafeServerID(t *testing.T) {
	factory := newFakeMonitoringFactory()
	resolver := monitoringapp.NewHealthResolver(factory, everyServerInSite{siteID: testSiteID})

	_, err := resolver.ResolveHealth(context.Background(), []string{`srv-1"} or up{`})
	if err == nil {
		t.Fatal("expected an unsafe identifier to be rejected")
	}
	if len(factory.querier.upReads) != 0 {
		t.Error("no query should have been issued")
	}
}

// --- alerts ---

func seedAlert(f *platformFixture, name, severity, serverID string, state monitoringdomain.AlertState) {
	f.monitoring.alerts.alerts = append(f.monitoring.alerts.alerts, &monitoringdomain.Alert{
		Fingerprint: name + "-" + serverID,
		Name:        name,
		Severity:    severity,
		State:       state,
		Summary:     name + " on " + serverID,
		Labels: map[string]string{
			"alertname": name,
			"severity":  severity,
			"server_id": serverID,
		},
		StartsAt: time.Now().UTC(),
		ServerID: serverID,
	})
}

func TestListAlerts_CorrelatesAndSortsBySeverity(t *testing.T) {
	f := setupPlatform(t)
	seedAlert(f, "GPUMemoryErrors", "warning", "srv-2", monitoringdomain.AlertStateFiring)
	seedAlert(f, "GPUTemperatureCritical", "critical", "srv-1", monitoringdomain.AlertStateFiring)

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/alerts", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	items := parseArrayBody(t, resp)
	if len(items) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(items))
	}
	if items[0]["severity"] != "critical" {
		t.Errorf("most severe first, got %v", items[0]["severity"])
	}
	if items[0]["serverId"] != "srv-1" {
		t.Errorf("expected correlation to a server, got %v", items[0]["serverId"])
	}
}

// A silenced alert stays visible as silenced. Hiding it would look like it was fixed.
func TestListAlerts_IncludesSuppressed(t *testing.T) {
	f := setupPlatform(t)
	seedAlert(f, "GPUTemperatureCritical", "critical", "srv-1", monitoringdomain.AlertStateSuppressed)

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/alerts", nil, f.adminAuth(t))
	items := parseArrayBody(t, resp)
	if len(items) != 1 {
		t.Fatalf("expected the suppressed alert to still be listed, got %d", len(items))
	}
	if items[0]["state"] != "suppressed" {
		t.Errorf("state: got %v", items[0]["state"])
	}
}

func TestListAlerts_FiltersByServerAndSeverity(t *testing.T) {
	f := setupPlatform(t)
	seedAlert(f, "A", "critical", "srv-1", monitoringdomain.AlertStateFiring)
	seedAlert(f, "B", "warning", "srv-2", monitoringdomain.AlertStateFiring)
	auth := f.adminAuth(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/alerts?serverId=srv-2", nil, auth)
	if items := parseArrayBody(t, resp); len(items) != 1 || items[0]["serverId"] != "srv-2" {
		t.Errorf("serverId filter: got %v", items)
	}

	resp = doRequest(t, f.app, "GET", "/api/v1/monitoring/alerts?severity=critical", nil, auth)
	if items := parseArrayBody(t, resp); len(items) != 1 || items[0]["severity"] != "critical" {
		t.Errorf("severity filter: got %v", items)
	}
}

// Acknowledging is creating a silence in Alertmanager, so that swallow and the alerting
// pipeline cannot disagree about whether something was dealt with.
func TestAcknowledgeAlert_CreatesSilence(t *testing.T) {
	f := setupPlatform(t)
	seedAlert(f, "GPUTemperatureCritical", "critical", "srv-1", monitoringdomain.AlertStateFiring)

	resp := doRequest(t, f.app, "POST",
		"/api/v1/monitoring/alerts/GPUTemperatureCritical-srv-1/acknowledge",
		map[string]any{
			"matchers": map[string]string{"alertname": "GPUTemperatureCritical", "server_id": "srv-1"},
			"duration": "2h",
			"comment":  "replacing the fan",
		}, f.adminAuth(t))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if parseBody(t, resp)["silenceId"] != "silence-1" {
		t.Errorf("expected the silence id, got %v", parseBody(t, resp)["silenceId"])
	}

	if len(f.monitoring.alerts.silences) != 1 {
		t.Fatalf("expected 1 silence, got %d", len(f.monitoring.alerts.silences))
	}
	silence := f.monitoring.alerts.silences[0]
	if silence.Duration != 2*time.Hour {
		t.Errorf("duration: got %v", silence.Duration)
	}
	if silence.CreatedBy != "admin" {
		t.Errorf("createdBy should come from the token, got %q", silence.CreatedBy)
	}
	if silence.Matchers["server_id"] != "srv-1" {
		t.Errorf("matchers: got %v", silence.Matchers)
	}
}

// The path fingerprint names the alert being acknowledged. An unknown one is 404; without
// matchers the silence matches the alert's full label set; a matcher the alert does not carry is
// 400, because the silence would not cover the alert.
func TestAcknowledgeAlert_ResolvesTheFingerprint(t *testing.T) {
	f := setupPlatform(t)
	seedAlert(f, "GPUTemperatureCritical", "critical", "srv-1", monitoringdomain.AlertStateFiring)
	auth := f.adminAuth(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/monitoring/alerts/abc/acknowledge",
		map[string]any{"duration": "2h"}, auth)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown fingerprint: expected 404, got %d", resp.StatusCode)
	}
	if len(f.monitoring.alerts.silences) != 0 {
		t.Fatal("an unknown fingerprint created a silence")
	}

	resp = doRequest(t, f.app, "POST", "/api/v1/monitoring/alerts/GPUTemperatureCritical-srv-1/acknowledge",
		map[string]any{"matchers": map[string]string{"server_id": "srv-2"}}, auth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a matcher the alert lacks: expected 400, got %d", resp.StatusCode)
	}

	resp = doRequest(t, f.app, "POST", "/api/v1/monitoring/alerts/GPUTemperatureCritical-srv-1/acknowledge",
		map[string]any{}, auth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("no matchers: expected 200, got %d", resp.StatusCode)
	}
	if len(f.monitoring.alerts.silences) != 1 {
		t.Fatalf("expected 1 silence, got %d", len(f.monitoring.alerts.silences))
	}
	matchers := f.monitoring.alerts.silences[0].Matchers
	if matchers["alertname"] != "GPUTemperatureCritical" || matchers["server_id"] != "srv-1" || matchers["severity"] != "critical" {
		t.Errorf("matchers = %v, want the alert's full label set", matchers)
	}
}

func TestAlerts_NoAlertSourceConfigured(t *testing.T) {
	f := setupPlatform(t)
	f.monitoring.alertsErr = monitoringdomain.ErrNoAlertSource

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/alerts", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "provider_unavailable" {
		t.Fatalf("expected provider_unavailable, got %s", code)
	}
}

// --- server metrics ---

func TestServerMetrics_ReturnsNamedQueries(t *testing.T) {
	f := setupPlatform(t)
	f.monitoring.querier.samples["cpuUsagePercent"] = []monitoringdomain.Sample{
		{Labels: map[string]string{"server_id": "srv-1"}, Value: 87.5, Timestamp: time.Now().UTC()},
	}
	f.monitoring.grafanaURL = "https://grafana.example.com"

	resp := doRequest(t, f.app, "GET",
		"/api/v1/monitoring/metrics?serverIds=srv-1&metrics=cpuUsagePercent", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := parseBody(t, resp)
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	metrics := items[0].(map[string]any)["metrics"].(map[string]any)
	if metrics["cpuUsagePercent"].(float64) != 87.5 {
		t.Errorf("value: got %v", metrics["cpuUsagePercent"])
	}
	// Exploration belongs in Grafana, so the API points at it rather than growing
	// dashboard features.
	if body["grafana"] != "https://grafana.example.com" {
		t.Errorf("expected a Grafana deep link, got %v", body["grafana"])
	}
}

// A metric the store had no answer for is absent, not zero: no data and zero
// utilisation are different facts.
func TestServerMetrics_MissingDataIsAbsentNotZero(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET",
		"/api/v1/monitoring/metrics?serverIds=srv-1&metrics=cpuUsagePercent", nil, f.adminAuth(t))
	items := parseBody(t, resp)["items"].([]any)
	metrics := items[0].(map[string]any)["metrics"].(map[string]any)
	if len(metrics) != 0 {
		t.Fatalf("expected no metric values, got %v", metrics)
	}
}

// swallow exposes a fixed query set rather than a PromQL passthrough, so an unknown metric
// is a validation error that names what is available. The GPU metrics are no longer named
// metrics: the API offers basic monitoring only.
func TestServerMetrics_RejectsUnknownMetric(t *testing.T) {
	f := setupPlatform(t)

	for _, metric := range []string{"rm-rf-slash", "gpuUtilizationPercent", "gpuMemoryUsedPercent", "gpuTemperatureCelsius", "gpuPowerWatts"} {
		resp := doRequest(t, f.app, "GET",
			"/api/v1/monitoring/metrics?serverIds=srv-1&metrics="+metric, nil, f.adminAuth(t))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("metrics=%s: expected 400, got %d", metric, resp.StatusCode)
		}
		message, _ := parseBody(t, resp)["error"].(map[string]any)["message"].(string)
		if !strings.Contains(message, "networkTransmitBytesPerSecond") {
			t.Errorf("metrics=%s: the error should list the available metrics, got %q", metric, message)
		}
	}
	if len(f.monitoring.querier.currentReads) != 0 {
		t.Errorf("a refused request must not read the store, got %+v", f.monitoring.querier.currentReads)
	}
}

func TestServerMetrics_RequiresServerIDs(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/metrics", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Disk and network are host-layer metrics like CPU and memory, attributed by server_id; every
// requested metric is one read of the Server's Site Prometheus. (Their expressions are the
// metrics adapter's and are tested there.)
func TestServerMetrics_DiskAndNetworkNamedQueries(t *testing.T) {
	f := setupPlatform(t)
	f.monitoring.querier.samples["diskUsedPercent"] = []monitoringdomain.Sample{
		{Labels: map[string]string{"server_id": "srv-1"}, Value: 41.5, Timestamp: time.Now().UTC()},
	}
	f.monitoring.querier.samples["networkReceiveBytesPerSecond"] = []monitoringdomain.Sample{
		{Labels: map[string]string{"server_id": "srv-1"}, Value: 1250000, Timestamp: time.Now().UTC()},
	}

	resp := doRequest(t, f.app, "GET",
		"/api/v1/monitoring/metrics?serverIds=srv-1&metrics=diskUsedPercent,networkReceiveBytesPerSecond", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	metrics := parseBody(t, resp)["items"].([]any)[0].(map[string]any)["metrics"].(map[string]any)
	if metrics["diskUsedPercent"] != 41.5 || metrics["networkReceiveBytesPerSecond"] != 1250000.0 {
		t.Errorf("metrics = %v, want disk 41.5 and receive 1250000", metrics)
	}
	var read []string
	for _, item := range f.monitoring.querier.currentReads {
		read = append(read, item.name)
	}
	if !slices.Equal(read, []string{"diskUsedPercent", "networkReceiveBytesPerSecond"}) {
		t.Errorf("reads = %v, want disk then network receive", read)
	}
}

// --- usage history (range) ---

func rangePoints(values ...float64) []monitoringdomain.Point {
	points := make([]monitoringdomain.Point, len(values))
	for i, value := range values {
		points[i] = monitoringdomain.Point{Timestamp: testMetricsNow.Add(time.Duration(i-len(values)) * time.Minute), Value: value}
	}
	return points
}

// A Pool is charted as one series per metric: its current members are resolved to server ids
// and the named expression is wrapped in the metric's scope aggregate.
func TestMetricRange_AggregatesPoolScope(t *testing.T) {
	f := setupPlatform(t)
	f.metricScopes.members["pool/pool-research"] = []string{"srv-1", "srv-2"}
	f.monitoring.querier.series["cpuUsagePercent"] = []monitoringdomain.Series{
		{Labels: map[string]string{}, Points: rangePoints(12.5, 14)},
	}

	resp := doRequest(t, f.app, "GET",
		"/api/v1/monitoring/metrics/range?poolId=pool-research&metrics=cpuUsagePercent&window=6h", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	body := parseBody(t, resp)

	scope := body["scope"].(map[string]any)
	if scope["kind"] != "pool" || scope["id"] != "pool-research" || scope["serverCount"] != 2.0 {
		t.Errorf("scope = %v, want pool pool-research with 2 servers", scope)
	}
	if body["mode"] != "aggregate" || body["stepSeconds"] != 120.0 {
		t.Errorf("mode/step = %v/%v, want aggregate/120", body["mode"], body["stepSeconds"])
	}
	if body["start"] != "2026-10-11T19:00:00Z" || body["end"] != "2026-10-12T01:00:00Z" {
		t.Errorf("range = %v..%v, want the 6h window ending at the request time", body["start"], body["end"])
	}

	series := body["series"].([]any)
	if len(series) != 1 {
		t.Fatalf("expected one aggregate series, got %v", series)
	}
	first := series[0].(map[string]any)
	if first["metric"] != "cpuUsagePercent" || first["serverId"] != nil || len(first["points"].([]any)) != 2 {
		t.Errorf("series = %v, want cpuUsagePercent for the whole scope with 2 points", first)
	}

	read := f.monitoring.querier.historyReads[0]
	if read.aggregate != monitoringdomain.ScopeAverage || !slices.Equal(read.servers, []string{"srv-1", "srv-2"}) {
		t.Errorf("history read = %+v, want an average over the pool members", read)
	}
	if got := read.window.Step; got != 2*time.Minute {
		t.Errorf("step = %v, want 2m for a 6h window", got)
	}
}

// Utilisation and fill levels average across a scope; throughput adds up.
func TestMetricRange_UsesEachMetricsScopeAggregate(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET",
		"/api/v1/monitoring/metrics/range?serverIds=srv-1,srv-2&metrics=networkReceiveBytesPerSecond,cpuUsagePercent,networkTransmitBytesPerSecond,diskUsedPercent",
		nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}

	want := []monitoringdomain.ScopeAggregate{
		monitoringdomain.ScopeSum, monitoringdomain.ScopeAverage, monitoringdomain.ScopeSum, monitoringdomain.ScopeAverage,
	}
	reads := f.monitoring.querier.historyReads
	if len(reads) != len(want) {
		t.Fatalf("expected %d history reads, got %+v", len(want), reads)
	}
	for i, aggregate := range want {
		if reads[i].aggregate != aggregate {
			t.Errorf("read %d (%s) aggregate = %q, want %q", i, reads[i].name, reads[i].aggregate, aggregate)
		}
	}
	// A request without a window charts the last hour.
	if body := parseBody(t, resp); body["stepSeconds"] != 30.0 {
		t.Errorf("stepSeconds = %v, want 30 for the default 1h window", body["stepSeconds"])
	}
}

// perServer keeps each Server's own series, in scope order, and drops what cannot be attributed
// or has no points.
func TestMetricRange_PerServerOrdersByScope(t *testing.T) {
	f := setupPlatform(t)
	f.monitoring.querier.series["memoryUsedPercent"] = []monitoringdomain.Series{
		{Labels: map[string]string{"server_id": "srv-1"}, Points: rangePoints(40)},
		{Labels: map[string]string{"server_id": "srv-2"}, Points: rangePoints(55, 56)},
		{Labels: map[string]string{"server_id": "srv-3"}, Points: nil},
		{Labels: map[string]string{}, Points: rangePoints(1)},
	}

	resp := doRequest(t, f.app, "GET",
		"/api/v1/monitoring/metrics/range?serverIds=srv-2,srv-1,srv-3&metrics=memoryUsedPercent&mode=perServer",
		nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	series := parseBody(t, resp)["series"].([]any)
	var got []any
	for _, item := range series {
		got = append(got, item.(map[string]any)["serverId"])
	}
	if len(got) != 2 || got[0] != "srv-2" || got[1] != "srv-1" {
		t.Errorf("series servers = %v, want [srv-2 srv-1]", got)
	}
	if read := f.monitoring.querier.historyReads[0]; read.aggregate != "" {
		t.Errorf("perServer must not aggregate, got %+v", read)
	}
}

// No data is no series — never a flat zero line — and the list is an array, not null.
func TestMetricRange_MissingDataHasNoSeries(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET",
		"/api/v1/monitoring/metrics/range?serverIds=srv-1&metrics=diskUsedPercent", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	series, ok := parseBody(t, resp)["series"].([]any)
	if !ok || len(series) != 0 {
		t.Errorf("series = %v, want an empty array", series)
	}
}

// An empty grouping is a fact about the grouping: it is answered without a metrics backend.
func TestMetricRange_EmptyScopeSkipsBackend(t *testing.T) {
	f := setupPlatform(t)
	f.metricScopes.members["zone/zone-empty"] = []string{}
	f.monitoring.querierErr = monitoringdomain.ErrNoMetricsIntegration

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/metrics/range?zoneId=zone-empty", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	if scope := parseBody(t, resp)["scope"].(map[string]any); scope["serverCount"] != 0.0 {
		t.Errorf("serverCount = %v, want 0", scope["serverCount"])
	}
	if len(f.monitoring.querier.historyReads) != 0 {
		t.Error("an empty scope must not query the store")
	}
}

// An explicit range derives its step from the span, like a named window.
func TestMetricRange_ExplicitRangeDerivesStep(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET",
		"/api/v1/monitoring/metrics/range?serverIds=srv-1&metrics=cpuUsagePercent&start=2026-10-10T00:00:00Z&end=2026-10-11T00:00:00Z",
		nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	body := parseBody(t, resp)
	if body["stepSeconds"] != 480.0 || body["start"] != "2026-10-10T00:00:00Z" {
		t.Errorf("step/start = %v/%v, want 480 and the requested start", body["stepSeconds"], body["start"])
	}
}

func TestMetricRange_RejectsInvalidRequests(t *testing.T) {
	f := setupPlatform(t)
	large := make([]string, 51)
	for i := range large {
		large[i] = "srv-" + strings.Repeat("a", i+1)
	}
	huge := make([]string, 1001)
	for i := range huge {
		huge[i] = "srv-h"
	}
	f.metricScopes.members["pool/pool-large"] = large
	f.metricScopes.members["platform/platform-huge"] = huge

	tests := []struct {
		name  string
		query string
	}{
		{"no scope", "metrics=cpuUsagePercent"},
		{"two scopes", "serverIds=srv-1&poolId=pool-large"},
		{"unknown window", "serverIds=srv-1&window=2h"},
		{"window with start and end", "serverIds=srv-1&window=1h&start=2026-10-10T00:00:00Z&end=2026-10-10T01:00:00Z"},
		{"start without end", "serverIds=srv-1&start=2026-10-10T00:00:00Z"},
		{"unparsable start", "serverIds=srv-1&start=yesterday&end=2026-10-10T01:00:00Z"},
		{"start after end", "serverIds=srv-1&start=2026-10-10T02:00:00Z&end=2026-10-10T01:00:00Z"},
		{"span over 7 days", "serverIds=srv-1&start=2026-10-01T00:00:00Z&end=2026-10-10T00:00:00Z"},
		{"span under 5 minutes", "serverIds=srv-1&start=2026-10-10T00:00:00Z&end=2026-10-10T00:01:00Z"},
		{"unknown mode", "serverIds=srv-1&mode=stacked"},
		{"unknown metric", "serverIds=srv-1&metrics=loadAverage"},
		{"retired GPU metric", "serverIds=srv-1&metrics=gpuTemperatureCelsius"},
		{"perServer over 50 servers", "poolId=pool-large&mode=perServer"},
		{"scope over 1000 servers", "platformId=platform-huge"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/metrics/range?"+tc.query, nil, f.adminAuth(t))
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("GET range?%s = %d, want 400", tc.query, resp.StatusCode)
			}
		})
	}
	if len(f.monitoring.querier.historyReads) != 0 {
		t.Errorf("a refused request must not query the store, got %+v", f.monitoring.querier.historyReads)
	}
}

// A grouping id that does not exist is a 404, not an empty chart.
func TestMetricRange_UnknownGroupingIsNotFound(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/metrics/range?zoneId=zone-missing", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "not_found" {
		t.Errorf("code = %s, want not_found", code)
	}
}

// Without a metrics Integration the charts cannot be drawn; the API says so in a way a client
// can turn into "register a metrics backend" rather than "the servers are down".
func TestMetricRange_NoIntegrationIsProviderUnavailable(t *testing.T) {
	f := setupPlatform(t)
	f.monitoring.querierErr = monitoringdomain.ErrNoMetricsIntegration

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/metrics/range?serverIds=srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "provider_unavailable" {
		t.Errorf("code = %s, want provider_unavailable", code)
	}
}

func TestMetricNames_IsDiscoverable(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/metrics/names", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var names []string
	if err := json.Unmarshal([]byte(rawBody(t, resp)), &names); err != nil {
		t.Fatalf("decode names: %v", err)
	}
	want := []string{"cpuUsagePercent", "memoryUsedPercent", "diskUsedPercent", "networkReceiveBytesPerSecond", "networkTransmitBytesPerSecond"}
	if !slices.Equal(names, want) {
		t.Errorf("names = %v, want the five basic-monitoring metrics %v", names, want)
	}
}

// A siteId that names no Site is 404, not "no metrics backend" (503).
func TestAlerts_UnknownSiteIsNotFound(t *testing.T) {
	f := setupPlatform(t)

	for _, request := range []struct{ method, path string }{
		{"GET", "/api/v1/monitoring/alerts?siteId=no-such-site"},
		{"POST", "/api/v1/monitoring/alerts/abc/acknowledge?siteId=no-such-site"},
	} {
		resp := doRequest(t, f.app, request.method, request.path, map[string]any{"matchers": map[string]string{"alertname": "X"}}, f.adminAuth(t))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s: expected 404, got %d", request.method, request.path, resp.StatusCode)
		}
	}
}

// A range read sends each Server to its own Site's Prometheus: perServer merges their series in
// the caller's order, and an aggregate over several Sites is refused rather than averaged across
// stores.
func TestMetricRange_ReadsEachServersSite(t *testing.T) {
	east, west := newFakeQuerier(), newFakeQuerier()
	point := []monitoringdomain.Point{{Timestamp: time.Now().UTC(), Value: 1}}
	east.series["cpuUsagePercent"] = []monitoringdomain.Series{{Labels: map[string]string{"server_id": "srv-e"}, Points: point}}
	west.series["cpuUsagePercent"] = []monitoringdomain.Series{{Labels: map[string]string{"server_id": "srv-w"}, Points: point}}
	service := monitoringapp.NewMetricRangeService(perSiteMonitoringFactory{"east": east, "west": west},
		&fakeScopeResolver{members: map[string][]string{}}, fixedServerSites{"srv-e": "east", "srv-w": "west"})
	input := monitoringapp.MetricRangeInput{
		ScopeKind: monitoringdomain.ScopeServers, ServerIDs: []string{"srv-w", "srv-e"},
		Metrics: []string{"cpuUsagePercent"}, Mode: "perServer",
	}

	result, err := service.Query(context.Background(), input)
	if err != nil {
		t.Fatalf("perServer across Sites: %v", err)
	}
	if len(result.Series) != 2 || result.Series[0].ServerID == nil || *result.Series[0].ServerID != "srv-w" ||
		result.Series[1].ServerID == nil || *result.Series[1].ServerID != "srv-e" {
		t.Errorf("series = %+v, want srv-w then srv-e", result.Series)
	}
	if len(east.historyReads) != 1 || !slices.Equal(east.historyReads[0].servers, []string{"srv-e"}) {
		t.Errorf("east history reads = %+v, want only srv-e", east.historyReads)
	}

	input.Mode = ""
	if _, err := service.Query(context.Background(), input); !errors.Is(err, monitoringapp.ErrInvalidMetricRange) {
		t.Errorf("aggregate across Sites: error = %v, want ErrInvalidMetricRange", err)
	}
}
