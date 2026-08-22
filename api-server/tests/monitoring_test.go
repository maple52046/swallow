package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	monitoringapp "github.com/maple52046/swallow/internal/monitoring/application"
	monitoringdomain "github.com/maple52046/swallow/internal/monitoring/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// --- health axis resolution ---

func TestHealthResolver_MapsUpMetricToHealthAxis(t *testing.T) {
	factory := newFakeMonitoringFactory()
	factory.querier.samples["up{"] = []monitoringdomain.Sample{
		{Labels: map[string]string{"server_id": "srv-1"}, Value: 1, Timestamp: time.Now().UTC()},
		{Labels: map[string]string{"server_id": "srv-2"}, Value: 0, Timestamp: time.Now().UTC()},
	}
	resolver := monitoringapp.NewHealthResolver(factory)

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

// A server has several scrape jobs, so being reachable on any of them means up.
func TestHealthResolver_AggregatesAcrossScrapeJobs(t *testing.T) {
	factory := newFakeMonitoringFactory()
	resolver := monitoringapp.NewHealthResolver(factory)

	if _, err := resolver.ResolveHealth(context.Background(), []string{"srv-1"}); err != nil {
		t.Fatalf("ResolveHealth: %v", err)
	}

	if len(factory.querier.queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(factory.querier.queries))
	}
	expr := factory.querier.queries[0]
	if !strings.Contains(expr, "max by (server_id)") {
		t.Errorf("expected aggregation over jobs, got %q", expr)
	}
	if !strings.Contains(expr, `server_id=~"srv-1"`) {
		t.Errorf("expected the server_id matcher, got %q", expr)
	}
}

// With no metrics store registered, health is simply unknown. It is not an error and
// must not fail the caller.
func TestHealthResolver_NoIntegrationIsNotAnError(t *testing.T) {
	factory := newFakeMonitoringFactory()
	factory.querierErr = monitoringdomain.ErrNoMetricsIntegration
	resolver := monitoringapp.NewHealthResolver(factory)

	health, err := resolver.ResolveHealth(context.Background(), []string{"srv-1"})
	if err != nil {
		t.Fatalf("a missing metrics integration must not be an error: %v", err)
	}
	if len(health) != 0 {
		t.Errorf("expected no health, got %v", health)
	}
}

// Identifiers are validated rather than escaped: anything outside the swallow identifier
// character set did not come from swallow and must not be interpolated into a query.
func TestHealthResolver_RejectsUnsafeServerID(t *testing.T) {
	factory := newFakeMonitoringFactory()
	resolver := monitoringapp.NewHealthResolver(factory)

	_, err := resolver.ResolveHealth(context.Background(), []string{`srv-1"} or up{`})
	if err == nil {
		t.Fatal("expected an unsafe identifier to be rejected")
	}
	if len(factory.querier.queries) != 0 {
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

func TestAcknowledgeAlert_RequiresMatchers(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/monitoring/alerts/abc/acknowledge",
		map[string]any{"duration": "2h"}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
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
	f.monitoring.querier.samples["DCGM_FI_DEV_GPU_UTIL"] = []monitoringdomain.Sample{
		{Labels: map[string]string{"server_id": "srv-1"}, Value: 87.5, Timestamp: time.Now().UTC()},
	}
	f.monitoring.grafanaURL = "https://grafana.example.com"

	resp := doRequest(t, f.app, "GET",
		"/api/v1/monitoring/metrics?serverIds=srv-1&metrics=gpuUtilizationPercent", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := parseBody(t, resp)
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	metrics := items[0].(map[string]any)["metrics"].(map[string]any)
	if metrics["gpuUtilizationPercent"].(float64) != 87.5 {
		t.Errorf("value: got %v", metrics["gpuUtilizationPercent"])
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
		"/api/v1/monitoring/metrics?serverIds=srv-1&metrics=gpuUtilizationPercent", nil, f.adminAuth(t))
	items := parseBody(t, resp)["items"].([]any)
	metrics := items[0].(map[string]any)["metrics"].(map[string]any)
	if len(metrics) != 0 {
		t.Fatalf("expected no metric values, got %v", metrics)
	}
}

// swallow exposes a fixed query set rather than a PromQL passthrough, so an unknown metric
// is a validation error that names what is available.
func TestServerMetrics_RejectsUnknownMetric(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET",
		"/api/v1/monitoring/metrics?serverIds=srv-1&metrics=rm-rf-slash", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	message, _ := parseBody(t, resp)["error"].(map[string]any)["message"].(string)
	if !strings.Contains(message, "gpuUtilizationPercent") {
		t.Errorf("the error should list the available metrics, got %q", message)
	}
}

func TestServerMetrics_RequiresServerIDs(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/metrics", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMetricNames_IsDiscoverable(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/monitoring/metrics/names", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body := rawBody(t, resp); !strings.Contains(body, "gpuTemperatureCelsius") {
		t.Fatalf("expected the query set to be listed, got %s", body)
	}
}
