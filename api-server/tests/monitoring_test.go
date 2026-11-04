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

	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	monitoringapp "github.com/maple52046/swallow/internal/core/monitoring/application"
	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
)

// --- health axis classification ---

// newHealthResolver composes the health classification as internal/app does: the real rule over
// the exporter discovery (with a fake inventory) and the real Node Exporter policy (with a fake
// Site automation), reading the given fake metrics store.
func newHealthResolver(factory monitoringdomain.MonitoringFactory, inventory *fakeExporterInventory, automation *fakeSiteAutomation) *monitoringapp.HealthResolver {
	return monitoringapp.NewHealthResolver(factory,
		discoveryapp.NewExporterDiscovery(newFakeServerRepo(), inventory), softwareapp.NewEnsureCoverage(automation))
}

// healthServer is a present, deployed, unlocked Server with an address at the test Site: the
// Node Exporter policy covers it whenever the Site's automation is on.
func healthServer(id string, mutate func(*serverdomain.Server)) *serverdomain.Server {
	server := &serverdomain.Server{
		ID:           id,
		Source:       serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID},
		Observed:     serverdomain.Observed{Addresses: []string{"10.0.1.10"}},
		Provisioning: &serverdomain.ProvisioningStatus{State: "deployed", PowerState: "on"},
	}
	if mutate != nil {
		mutate(server)
	}
	return server
}

// Health follows swallow's monitoring policy, in order: a Node Exporter target is up or down by its
// Site Prometheus's latest sample and unknown without one; a Server the Node Exporter policy covers
// without a target is unknown; every other Server is not_applicable, whatever the Prometheus still
// holds for it.
func TestHealthResolver_ClassifiesByTheMonitoringPolicy(t *testing.T) {
	sampledAt := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	up := []monitoringdomain.Sample{{Labels: map[string]string{"server_id": "srv-1"}, Value: 1, Timestamp: sampledAt}}
	down := []monitoringdomain.Sample{{Labels: map[string]string{"server_id": "srv-1"}, Value: 0, Timestamp: sampledAt}}

	tests := []struct {
		name       string
		mutate     func(*serverdomain.Server)
		installed  bool
		automation map[string]bool
		samples    []monitoringdomain.Sample
		querierErr error
		readErr    error
		want       string
	}{
		{name: "target that answers", installed: true, samples: up, want: serverdomain.HealthUp},
		{name: "target whose scrape failed", installed: true, samples: down, want: serverdomain.HealthDown},
		{name: "target without a sample", installed: true, want: serverdomain.HealthUnknown},
		{name: "target at a Site with no Prometheus", installed: true, samples: up,
			querierErr: monitoringdomain.ErrNoMetricsIntegration, want: serverdomain.HealthUnknown},
		{name: "target whose metrics read fails", installed: true, samples: up, readErr: errTestMetricsDown,
			want: serverdomain.HealthUnknown},
		{name: "target that is locked keeps its answer", installed: true, samples: down,
			mutate: func(s *serverdomain.Server) { s.Provisioning.Locked = true }, want: serverdomain.HealthDown},
		{name: "covered without a target", automation: map[string]bool{testSiteID: true}, want: serverdomain.HealthUnknown},
		{name: "covered and installed but without an address", installed: true, samples: up,
			automation: map[string]bool{testSiteID: true},
			mutate:     func(s *serverdomain.Server) { s.Observed.Addresses = nil }, want: serverdomain.HealthUnknown},
		{name: "locked without a target", automation: map[string]bool{testSiteID: true},
			mutate: func(s *serverdomain.Server) { s.Provisioning.Locked = true }, want: serverdomain.HealthNotApplicable},
		{name: "not deployed", automation: map[string]bool{testSiteID: true}, samples: up,
			mutate: func(s *serverdomain.Server) { s.Provisioning.State = "ready" }, want: serverdomain.HealthNotApplicable},
		{name: "never observed by its provisioner", automation: map[string]bool{testSiteID: true},
			mutate: func(s *serverdomain.Server) { s.Provisioning = nil }, want: serverdomain.HealthNotApplicable},
		{name: "absent with its last series still held", installed: true, samples: up,
			automation: map[string]bool{testSiteID: true},
			mutate:     func(s *serverdomain.Server) { s.Absent = true }, want: serverdomain.HealthNotApplicable},
		{name: "Site automation off", automation: map[string]bool{testSiteID: false}, want: serverdomain.HealthNotApplicable},
		{name: "Site without an automation configuration", want: serverdomain.HealthNotApplicable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			factory := newFakeMonitoringFactory()
			factory.querierErr = tc.querierErr
			factory.querier.err = tc.readErr
			factory.querier.up["node-exporter"] = tc.samples
			inventory := &fakeExporterInventory{}
			if tc.installed {
				inventory.installNodeExporter("srv-1")
			}
			automation := &fakeSiteAutomation{enabled: tc.automation}
			if automation.enabled == nil {
				automation.enabled = map[string]bool{}
			}

			health, err := newHealthResolver(factory, inventory, automation).
				ResolveHealth(context.Background(), []*serverdomain.Server{healthServer("srv-1", tc.mutate)})
			if err != nil {
				t.Fatalf("ResolveHealth() error = %v", err)
			}
			got, ok := health["srv-1"]
			if !ok {
				t.Fatalf("ResolveHealth() = %v, want an answer for srv-1", health)
			}
			if got.State != tc.want {
				t.Errorf("ResolveHealth() state = %q, want %q", got.State, tc.want)
			}
			sampled := tc.want == serverdomain.HealthUp || tc.want == serverdomain.HealthDown
			switch {
			case sampled && (got.ObservedAt == nil || !got.ObservedAt.Equal(sampledAt)):
				t.Errorf("ResolveHealth() observedAt = %v, want the sample's %v", got.ObservedAt, sampledAt)
			case !sampled && got.ObservedAt != nil:
				t.Errorf("ResolveHealth() observedAt = %v, want nil for %s", got.ObservedAt, tc.want)
			}
		})
	}
}

// Health is the Node Exporter's up only, read for the targets only: a GPU exporter that answers
// does not make a Server up, and a Server that is not a target is never asked about.
func TestHealthResolver_ReadsTheNodeExporterOfTargetsOnly(t *testing.T) {
	factory := newFakeMonitoringFactory()
	inventory := &fakeExporterInventory{installed: []discoveryapp.InstalledExporter{
		{ServerID: "srv-2", Name: "device-metrics-exporter", Port: 5001},
	}}
	inventory.installNodeExporter("srv-1")
	resolver := newHealthResolver(factory, inventory, &fakeSiteAutomation{enabled: map[string]bool{}})

	health, err := resolver.ResolveHealth(context.Background(), []*serverdomain.Server{healthServer("srv-1", nil), healthServer("srv-2", nil)})
	if err != nil {
		t.Fatalf("ResolveHealth() error = %v", err)
	}
	reads := factory.querier.upReads
	if len(reads) != 1 || reads[0].name != "node-exporter" || !slices.Equal(reads[0].servers, []string{"srv-1"}) {
		t.Errorf("up reads = %+v, want one node-exporter read for srv-1", reads)
	}
	if len(factory.querier.currentReads) != 0 {
		t.Errorf("metric reads = %+v, want none: health reads only the exporter's up", factory.querier.currentReads)
	}
	if health["srv-2"].State != serverdomain.HealthNotApplicable {
		t.Errorf("srv-2 health = %+v, want not_applicable: only the Node Exporter makes a Server monitored", health["srv-2"])
	}
}

// Each target's health comes from its own Site's Prometheus, in one read per Site; a target whose
// Site has none is unknown. The coverage policy reads each Site's automation once.
func TestHealthResolver_AsksEachServersSite(t *testing.T) {
	east, west := newFakeQuerier(), newFakeQuerier()
	east.up["node-exporter"] = []monitoringdomain.Sample{{Labels: map[string]string{"server_id": "srv-e"}, Value: 1, Timestamp: time.Now().UTC()}}
	west.up["node-exporter"] = []monitoringdomain.Sample{{Labels: map[string]string{"server_id": "srv-w"}, Value: 0, Timestamp: time.Now().UTC()}}
	inventory := &fakeExporterInventory{}
	inventory.installNodeExporter("srv-e", "srv-w", "srv-n")
	automation := &fakeSiteAutomation{enabled: map[string]bool{"east": true}}
	resolver := newHealthResolver(perSiteMonitoringFactory{"east": east, "west": west}, inventory, automation)
	inSite := func(siteID string) func(*serverdomain.Server) {
		return func(s *serverdomain.Server) { s.Source.SiteID = siteID }
	}
	servers := []*serverdomain.Server{
		healthServer("srv-e", inSite("east")), healthServer("srv-w", inSite("west")),
		healthServer("srv-n", inSite("north")), healthServer("srv-c1", inSite("east")), healthServer("srv-c2", inSite("east")),
	}

	health, err := resolver.ResolveHealth(context.Background(), servers)
	if err != nil {
		t.Fatalf("ResolveHealth() error = %v", err)
	}
	want := map[string]string{
		"srv-e": serverdomain.HealthUp, "srv-w": serverdomain.HealthDown, "srv-n": serverdomain.HealthUnknown,
		"srv-c1": serverdomain.HealthUnknown, "srv-c2": serverdomain.HealthUnknown,
	}
	for id, state := range want {
		if health[id].State != state {
			t.Errorf("health[%s] = %+v, want %s", id, health[id], state)
		}
	}
	if len(east.upReads) != 1 || !slices.Equal(east.upReads[0].servers, []string{"srv-e"}) {
		t.Errorf("east up reads = %+v, want only srv-e", east.upReads)
	}
	if !slices.Equal(automation.reads, []string{"east"}) {
		t.Errorf("automation reads = %v, want one read of east for its two covered Servers", automation.reads)
	}
}

// Identifiers are validated rather than escaped: anything outside the swallow identifier
// character set did not come from swallow and must not be interpolated into a query. Its Site is
// not read, so the target is unknown.
func TestHealthResolver_RejectsUnsafeServerID(t *testing.T) {
	factory := newFakeMonitoringFactory()
	unsafe := `srv-1"} or up{`
	inventory := &fakeExporterInventory{}
	inventory.installNodeExporter(unsafe)
	resolver := newHealthResolver(factory, inventory, &fakeSiteAutomation{enabled: map[string]bool{}})

	health, err := resolver.ResolveHealth(context.Background(), []*serverdomain.Server{healthServer(unsafe, nil)})
	if err != nil {
		t.Fatalf("ResolveHealth() error = %v, want the unsafe target left unknown", err)
	}
	if health[unsafe].State != serverdomain.HealthUnknown {
		t.Errorf("health = %+v, want unknown", health[unsafe])
	}
	if len(factory.querier.upReads) != 0 {
		t.Error("no query should have been issued")
	}
}

// The target list and the coverage policy read swallow's own records; when either read fails there
// is no classification, and the error says so instead of a guessed state.
func TestHealthResolver_RecordReadFailureIsAnError(t *testing.T) {
	errStore := errors.New("store unreachable")
	tests := []struct {
		name       string
		inventory  *fakeExporterInventory
		automation *fakeSiteAutomation
	}{
		{"exporter inventory", &fakeExporterInventory{err: errStore}, &fakeSiteAutomation{enabled: map[string]bool{}}},
		{"site automation", &fakeExporterInventory{}, &fakeSiteAutomation{err: errStore}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver := newHealthResolver(newFakeMonitoringFactory(), tc.inventory, tc.automation)
			if _, err := resolver.ResolveHealth(context.Background(), []*serverdomain.Server{healthServer("srv-1", nil)}); !errors.Is(err, errStore) {
				t.Errorf("ResolveHealth() error = %v, want %v", err, errStore)
			}
		})
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
