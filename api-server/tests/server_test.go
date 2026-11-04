package tests

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// provisioning.stateSince is optional (servers-list.md): a recorded start is an RFC 3339 time,
// and an unknown one is omitted rather than rendered as year one.
func TestListServers_ProvisioningStateSince(t *testing.T) {
	f := setupPlatform(t)
	since := time.Date(2026, 10, 4, 14, 31, 5, 0, time.UTC)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Provisioning.State = "releasing"
		s.Provisioning.StateSince = since
	})
	f.seedServer("srv-2", "gpu-node-02", "10.0.1.11", nil)

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	got := map[string]any{}
	for _, raw := range parseBody(t, resp)["items"].([]any) {
		item := raw.(map[string]any)
		provisioning := serverStatusOf(item)["provisioning"].(map[string]any)
		value, present := provisioning["stateSince"]
		if !present {
			value = "<absent>"
		}
		got[item["id"].(string)] = value
	}
	if got["srv-1"] != "2026-10-04T14:31:05Z" || got["srv-2"] != "<absent>" {
		t.Errorf("stateSince by server = %v, want srv-1 2026-10-04T14:31:05Z and srv-2 absent", got)
	}
}

func TestListServers_ShapeAndAxes(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	items := parseBody(t, resp)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 server, got %d", len(items))
	}
	item := items[0].(map[string]any)

	source := serverStatusOf(item)["source"].(map[string]any)
	if source["siteId"] != testSiteID || source["providerMachineId"] != "machine-srv-1" {
		t.Errorf("unexpected source: %v", source)
	}

	provisioning, ok := serverStatusOf(item)["provisioning"].(map[string]any)
	if !ok {
		t.Fatalf("expected a provisioning axis, got %v", serverStatusOf(item)["provisioning"])
	}
	if provisioning["state"] != "deployed" {
		t.Errorf("provisioning state: got %v", provisioning["state"])
	}
	if provisioning["observedAt"] == nil {
		t.Error("every axis must carry observedAt so staleness is visible")
	}

	// Axes with no observation must be null, not an empty object or a default state.
	if _, present := serverStatusOf(item)["membership"]; !present {
		t.Error("membership must be present as a key")
	}
	if serverStatusOf(item)["membership"] != nil {
		t.Errorf("membership must be null when never observed, got %v", serverStatusOf(item)["membership"])
	}
	// Health is the exception: swallow always classifies it. Nothing monitors this Server (its
	// Site has no automation and it has no Node Exporter), so there is nothing to answer.
	health, ok := serverStatusOf(item)["health"].(map[string]any)
	if !ok {
		t.Fatalf("health must always be present, got %v", serverStatusOf(item)["health"])
	}
	if health["state"] != "not_applicable" || health["observedAt"] != nil {
		t.Errorf("health = %v, want not_applicable with a null observedAt", health)
	}
}

// Every listed Server is classified by the monitoring policy in one pass over the page: a Node
// Exporter target by its sample, a covered Server without one as unknown, and the rest as
// not_applicable.
func TestListServers_ClassifiesHealth(t *testing.T) {
	f := setupPlatform(t)
	f.automation.enabled[testSiteID] = true
	f.seedServer("srv-up", "gpu-node-01", "10.0.1.10", nil)
	f.seedServer("srv-pending", "gpu-node-02", "10.0.1.11", nil)
	f.seedServer("srv-ready", "gpu-node-03", "10.0.1.12", func(s *serverdomain.Server) { s.Provisioning.State = "ready" })
	f.exporters.installNodeExporter("srv-up")
	f.monitoring.querier.up["node-exporter"] = []monitoringdomain.Sample{
		{Labels: map[string]string{"server_id": "srv-up"}, Value: 1, Timestamp: time.Now().UTC()},
		// A ready Server's series lingers after its release; it is no longer a target.
		{Labels: map[string]string{"server_id": "srv-ready"}, Value: 0, Timestamp: time.Now().UTC()},
	}

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	got := map[string]any{}
	for _, raw := range parseBody(t, resp)["items"].([]any) {
		item := raw.(map[string]any)
		got[item["id"].(string)] = serverStatusOf(item)["health"].(map[string]any)["state"]
	}
	want := map[string]any{"srv-up": "up", "srv-pending": "unknown", "srv-ready": "not_applicable"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("health by server = %v, want %v", got, want)
	}
}

// When swallow cannot read the records the classification needs, the list fails rather than show
// a health nobody worked out.
func TestListServers_HealthRecordFailureFailsTheRead(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.exporters.err = errors.New("software assignments unreachable")

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestListServers_NullableHostnameAndAddresses(t *testing.T) {
	f := setupPlatform(t)
	// An uncommissioned machine: no hostname and no address is a normal state.
	f.seedServer("srv-new", "", "", func(s *serverdomain.Server) {
		s.Observed.FQDN = ""
		s.Provisioning.State = "new"
	})

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/", nil, f.adminAuth(t))
	item := parseBody(t, resp)["items"].([]any)[0].(map[string]any)

	if serverStatusOf(item)["hostname"] != nil {
		t.Errorf("hostname must be null when the provisioner has none, got %v", serverStatusOf(item)["hostname"])
	}
	addresses, ok := serverStatusOf(item)["addresses"].([]any)
	if !ok {
		t.Fatalf("addresses must be an array, got %v", serverStatusOf(item)["addresses"])
	}
	if len(addresses) != 0 {
		t.Errorf("expected no addresses, got %v", addresses)
	}
}

// Two sites may legitimately hold the same hostname and address. The old model made
// this unrepresentable, so it is worth asserting.
func TestListServers_DuplicateHostnameAcrossSitesIsAllowed(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-east", "gpu-node-01", "10.0.1.10", nil)
	f.seedServer("srv-west", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Source.SiteID = "site-2"
		s.Source.IntegrationID = "integration-2"
	})

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/", nil, f.adminAuth(t))
	if total := parseBody(t, resp)["total"].(float64); total != 2 {
		t.Fatalf("expected both servers, got total=%v", total)
	}
}

func TestListServers_ExcludesAbsentUnlessAsked(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-present", "gpu-node-01", "10.0.1.10", nil)
	f.seedServer("srv-gone", "gpu-node-02", "10.0.1.11", func(s *serverdomain.Server) {
		s.Absent = true
	})

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/", nil, f.adminAuth(t))
	if total := parseBody(t, resp)["total"].(float64); total != 1 {
		t.Fatalf("absent servers must be excluded by default, got total=%v", total)
	}

	resp = doRequest(t, f.app, "GET", "/api/v1/servers/?includeAbsent=true", nil, f.adminAuth(t))
	if total := parseBody(t, resp)["total"].(float64); total != 2 {
		t.Fatalf("absent servers must remain findable, got total=%v", total)
	}
}

func TestListServers_FiltersAndPagination(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.seedServer("srv-2", "gpu-node-02", "10.0.1.11", func(s *serverdomain.Server) {
		s.Provisioning.State = "ready"
	})
	f.seedServer("srv-3", "storage-01", "10.0.1.12", func(s *serverdomain.Server) {
		s.Hardware.SerialNumber = "SN-STORAGE-01"
	})
	auth := f.adminAuth(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/?provisioningState=ready", nil, auth)
	if total := parseBody(t, resp)["total"].(float64); total != 1 {
		t.Errorf("provisioningState filter: got total=%v", total)
	}

	resp = doRequest(t, f.app, "GET", "/api/v1/servers/?keyword=GPU-NODE", nil, auth)
	if total := parseBody(t, resp)["total"].(float64); total != 2 {
		t.Errorf("keyword filter should be case-insensitive: got total=%v", total)
	}

	// A serial number matches, and a regex metacharacter is an ordinary character: no query
	// value is refused.
	resp = doRequest(t, f.app, "GET", "/api/v1/servers/?keyword=sn-storage", nil, auth)
	if total := parseBody(t, resp)["total"].(float64); total != 1 {
		t.Errorf("keyword on the serial number: got total=%v", total)
	}
	resp = doRequest(t, f.app, "GET", "/api/v1/servers/?keyword=%28", nil, auth)
	if resp.StatusCode != 200 {
		t.Errorf("keyword=( status = %d, want 200", resp.StatusCode)
	}

	resp = doRequest(t, f.app, "GET", "/api/v1/servers/?page=2&pageSize=2", nil, auth)
	body := parseBody(t, resp)
	if body["total"].(float64) != 3 {
		t.Errorf("total must count all matches, got %v", body["total"])
	}
	if items := body["items"].([]any); len(items) != 1 {
		t.Errorf("expected 1 item on the last page, got %d", len(items))
	}
}

func TestGetServer_ResolvesHealthAxis(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.exporters.installNodeExporter("srv-1")
	sampledAt := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	f.monitoring.querier.up["node-exporter"] = []monitoringdomain.Sample{
		{Labels: map[string]string{"server_id": "srv-1"}, Value: 1, Timestamp: sampledAt},
	}

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	health, ok := serverStatusOf(parseBody(t, resp))["health"].(map[string]any)
	if !ok {
		t.Fatalf("expected a health axis, got %v", serverStatusOf(parseBody(t, resp))["health"])
	}
	if health["state"] != "up" || health["observedAt"] != "2026-11-04T08:00:00Z" {
		t.Errorf("health = %v, want up observed at the sample time", health)
	}
}

// An unreachable metrics store must leave a target's health unknown, not make the request fail and
// not make the server look down.
func TestGetServer_MetricsFailureLeavesHealthUnknown(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.exporters.installNodeExporter("srv-1")
	f.monitoring.querier.err = errTestMetricsDown

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a metrics outage must not fail the request, got %d", resp.StatusCode)
	}
	health, _ := serverStatusOf(parseBody(t, resp))["health"].(map[string]any)
	if health["state"] != "unknown" || health["observedAt"] != nil {
		t.Errorf("health = %v, want unknown with a null observedAt rather than a state nobody observed", health)
	}
}

func TestGetServer_NotFound(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/nope", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "not_found" {
		t.Fatalf("expected not_found, got %s", code)
	}
}

// Servers are produced by reconciliation, so there is no creation endpoint. Explicit
// deletion is covered separately because it must remove the provider Machine first.
func TestServers_HaveNoCreateEndpoint(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	auth := f.adminAuth(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/", map[string]string{
		"hostname": "manual-node", "ip": "10.0.9.9",
	}, auth)
	if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected no create route, got %d", resp.StatusCode)
	}
}

func TestServerRoutes_NonAdminForbidden(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/", nil, map[string]string{
		"Authorization": "Bearer " + userToken(t, f.jwtSvc),
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestServerRoutes_NoToken(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
