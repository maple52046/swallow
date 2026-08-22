package tests

import (
	"net/http"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

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

	source := item["source"].(map[string]any)
	if source["siteId"] != testSiteID || source["providerMachineId"] != "machine-srv-1" {
		t.Errorf("unexpected source: %v", source)
	}

	provisioning, ok := item["provisioning"].(map[string]any)
	if !ok {
		t.Fatalf("expected a provisioning axis, got %v", item["provisioning"])
	}
	if provisioning["state"] != "deployed" {
		t.Errorf("provisioning state: got %v", provisioning["state"])
	}
	if provisioning["observedAt"] == nil {
		t.Error("every axis must carry observedAt so staleness is visible")
	}

	// Axes with no observation must be null, not an empty object or a default state.
	if _, present := item["membership"]; !present {
		t.Error("membership must be present as a key")
	}
	if item["membership"] != nil {
		t.Errorf("membership must be null when never observed, got %v", item["membership"])
	}
	if item["health"] != nil {
		t.Errorf("health must be null with no metrics integration, got %v", item["health"])
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

	if item["hostname"] != nil {
		t.Errorf("hostname must be null when the provisioner has none, got %v", item["hostname"])
	}
	addresses, ok := item["addresses"].([]any)
	if !ok {
		t.Fatalf("addresses must be an array, got %v", item["addresses"])
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
	f.seedServer("srv-3", "storage-01", "10.0.1.12", nil)
	auth := f.adminAuth(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/?provisioningState=ready", nil, auth)
	if total := parseBody(t, resp)["total"].(float64); total != 1 {
		t.Errorf("provisioningState filter: got total=%v", total)
	}

	resp = doRequest(t, f.app, "GET", "/api/v1/servers/?keyword=GPU-NODE", nil, auth)
	if total := parseBody(t, resp)["total"].(float64); total != 2 {
		t.Errorf("keyword filter should be case-insensitive: got total=%v", total)
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
	f.health.health["srv-1"] = &serverdomain.HealthStatus{
		State: serverdomain.HealthUp, ObservedAt: time.Now().UTC(),
	}

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	health, ok := parseBody(t, resp)["health"].(map[string]any)
	if !ok {
		t.Fatalf("expected a health axis, got %v", parseBody(t, resp)["health"])
	}
	if health["state"] != "up" {
		t.Errorf("health state: got %v", health["state"])
	}
}

// An unreachable metrics store must leave health unknown, not make the request fail and
// not make the server look down.
func TestGetServer_MetricsFailureLeavesHealthNull(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.health.err = errTestMetricsDown

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a metrics outage must not fail the request, got %d", resp.StatusCode)
	}
	if parseBody(t, resp)["health"] != nil {
		t.Error("health must be null rather than reporting a state nobody observed")
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

// Servers are produced by reconciliation, so there is no creation endpoint. Asserting
// it stays absent keeps a second, weaker creation path from reappearing.
func TestServers_HaveNoCreateOrDeleteEndpoint(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	auth := f.adminAuth(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/", map[string]string{
		"hostname": "manual-node", "ip": "10.0.9.9",
	}, auth)
	if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected no create route, got %d", resp.StatusCode)
	}

	resp = doRequest(t, f.app, "DELETE", "/api/v1/servers/srv-1", nil, auth)
	if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected no delete route, got %d", resp.StatusCode)
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
