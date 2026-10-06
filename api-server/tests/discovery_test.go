package tests

import (
	"net/http"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

func discoveryAuth() map[string]string {
	return map[string]string{"Authorization": "Bearer " + testMachineToken}
}

// The labels are attached by the same component that owns server identity, which is
// what makes the metrics join key impossible to drift.
func TestPrometheusTargets_CarryTheLabelContract(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Membership = &serverdomain.MembershipStatus{
			PlatformID: "platform-1", NodeName: "gpu-node-01", Role: "worker",
			ObservedAt: time.Now().UTC(),
		}
	})

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus", nil, discoveryAuth())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	entries := parseArrayBody(t, resp)
	if len(entries) != 1 {
		t.Fatalf("expected 1 target, got %d", len(entries))
	}

	targets := entries[0]["targets"].([]any)
	if len(targets) != 1 || targets[0] != "10.0.1.10:9100" {
		t.Errorf("expected the default node_exporter port, got %v", targets)
	}

	labels := entries[0]["labels"].(map[string]any)
	if labels["server_id"] != "srv-1" {
		t.Errorf("server_id: got %v", labels["server_id"])
	}
	if labels["site"] != testSiteID {
		t.Errorf("site: got %v", labels["site"])
	}
	if labels["platform_id"] != "platform-1" {
		t.Errorf("platform_id: got %v", labels["platform_id"])
	}
	if labels["cluster"] != "platform-1" {
		t.Errorf("legacy cluster: got %v", labels["cluster"])
	}
}

// One endpoint serves several scrape jobs by being asked for a different port.
func TestPrometheusTargets_HonoursPortParameter(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus?port=9400", nil, discoveryAuth())
	targets := parseArrayBody(t, resp)[0]["targets"].([]any)
	if targets[0] != "10.0.1.10:9400" {
		t.Fatalf("expected the dcgm-exporter port, got %v", targets)
	}
}

// The RDC exporter job asks for AMD GPU servers only by passing tag=amd-gpu, so the
// endpoint must return just the tagged servers on the requested port.
func TestPrometheusTargets_FiltersByTag(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-amd", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Observed.Tags = []string{"amd-gpu"}
	})
	f.seedServer("srv-cpu", "cpu-node-01", "10.0.1.20", nil)

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus?port=5000&tag=amd-gpu", nil, discoveryAuth())
	entries := parseArrayBody(t, resp)
	if len(entries) != 1 {
		t.Fatalf("expected only the amd-gpu server, got %d", len(entries))
	}
	if entries[0]["labels"].(map[string]any)["server_id"] != "srv-amd" {
		t.Errorf("wrong server included: %v", entries[0])
	}
	if targets := entries[0]["targets"].([]any); targets[0] != "10.0.1.10:5000" {
		t.Errorf("expected the rdc-exporter port, got %v", targets)
	}
}

// Lock prevents mutation, not observation; existing exporters remain discoverable.
func TestPrometheusTargets_IncludesLockedServers(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-open", "node-01", "10.0.1.10", nil)
	f.seedServer("srv-locked", "node-02", "10.0.1.11", func(s *serverdomain.Server) {
		s.Provisioning.Locked = true
	})

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus", nil, discoveryAuth())
	entries := parseArrayBody(t, resp)
	if len(entries) != 2 {
		t.Fatalf("expected locked and unlocked servers, got %d", len(entries))
	}
	ids := map[any]bool{}
	for _, entry := range entries {
		ids[entry["labels"].(map[string]any)["server_id"]] = true
	}
	if !ids["srv-open"] || !ids["srv-locked"] {
		t.Errorf("both Servers must remain discoverable, got %v", ids)
	}
}

func TestPrometheusTargets_RejectsInvalidPort(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus?port=nope", nil, discoveryAuth())
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// A machine with no address cannot be scraped. Emitting it would create a permanently
// failing series attributed to that server.
func TestPrometheusTargets_SkipsServersWithoutAddress(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-addressed", "gpu-node-01", "10.0.1.10", nil)
	f.seedServer("srv-bare", "gpu-node-02", "", nil)

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus", nil, discoveryAuth())
	entries := parseArrayBody(t, resp)
	if len(entries) != 1 {
		t.Fatalf("expected only the addressed server, got %d", len(entries))
	}
	if entries[0]["labels"].(map[string]any)["server_id"] != "srv-addressed" {
		t.Errorf("wrong server included: %v", entries[0])
	}
}

// Only machines actually running an OS are scrape targets: a "ready" machine is powered
// off by definition.
func TestPrometheusTargets_DefaultsToDeployedOnly(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-deployed", "gpu-node-01", "10.0.1.10", nil)
	f.seedServer("srv-ready", "gpu-node-02", "10.0.1.11", func(s *serverdomain.Server) {
		s.Provisioning.State = "ready"
	})

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus", nil, discoveryAuth())
	if entries := parseArrayBody(t, resp); len(entries) != 1 {
		t.Fatalf("expected only the deployed server, got %d", len(entries))
	}

	resp = doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus?provisioningState=all", nil, discoveryAuth())
	if entries := parseArrayBody(t, resp); len(entries) != 2 {
		t.Fatalf("expected both servers when asked for all, got %d", len(entries))
	}
}

// Hosts are keyed by server ID with ansible_host carrying the address, so a playbook
// refers to swallow's stable identifier rather than an IP that changes on reinstall.
func TestAnsibleInventory_KeysHostsByServerID(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Observed.GPUs = []serverdomain.GPU{
			{Vendor: "NVIDIA", Model: "A100", Count: 8, Kind: serverdomain.GPUKindCompute},
			{Vendor: "ASPEED", Model: "Graphics Family", Count: 1, Kind: serverdomain.GPUKindDisplay},
		}
		s.Observed.Tags = []string{"amd-gpu"}
		s.Membership = &serverdomain.MembershipStatus{
			PlatformID: "platform-1", NodeName: "gpu-node-01", Role: "worker",
			ObservedAt: time.Now().UTC(),
		}
	})

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/ansible", nil, discoveryAuth())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	inventory := parseBody(t, resp)

	meta := inventory["_meta"].(map[string]any)
	hostvars := meta["hostvars"].(map[string]any)
	vars, ok := hostvars["srv-1"].(map[string]any)
	if !ok {
		t.Fatalf("expected the host to be keyed by server id, got %v", hostvars)
	}
	if vars["ansible_host"] != "10.0.1.10" {
		t.Errorf("ansible_host: got %v", vars["ansible_host"])
	}
	if vars["server_id"] != "srv-1" {
		t.Errorf("server_id must be a host var so a playbook can report back in swallow ids: %v", vars["server_id"])
	}
	if vars["platform_role"] != "worker" {
		t.Errorf("platform_role: got %v", vars["platform_role"])
	}

	// Groups automation needs to branch on. tag_amd_gpu is what the install-exporters
	// playbook uses to run the RDC exporter play on AMD GPU servers only.
	for _, group := range []string{"site_site_1", "provisioning_deployed", "platform_platform_1", "role_worker", "gpu_nvidia", "tag_amd_gpu"} {
		entry, ok := inventory[group].(map[string]any)
		if !ok {
			t.Errorf("expected group %q, got groups %v", group, groupNames(inventory))
			continue
		}
		hosts := entry["hosts"].([]any)
		if len(hosts) != 1 || hosts[0] != "srv-1" {
			t.Errorf("group %q membership: %v", group, hosts)
		}
	}
	if _, exists := inventory["gpu_aspeed"]; exists {
		t.Error("display GPU vendor must not create an accelerator inventory group")
	}

	all := inventory["all"].(map[string]any)
	if children, ok := all["children"].([]any); !ok || len(children) == 0 {
		t.Error("all must list the groups as children")
	}
}

// default_user carries the effective Server Default User (decision 045) — the value set on the
// Server over the image's — while image_default_user keeps the image's own value, and neither is
// emitted when unknown so the runner falls back to probing.
func TestAnsibleInventory_EmitsEffectiveDefaultUser(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-set", "tainan-ci", "10.0.1.11", func(s *serverdomain.Server) {
		s.DefaultUser = "amd"
		s.Provisioning.DeployedImageDefaultUser = "ubuntu"
	})
	f.seedServer("srv-image", "lab-compute-2", "10.0.1.12", func(s *serverdomain.Server) {
		s.Provisioning.DeployedImageDefaultUser = "ubuntu"
	})
	f.seedServer("srv-unknown", "lab-custom", "10.0.1.13", nil)

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/ansible", nil, discoveryAuth())
	hostvars := parseBody(t, resp)["_meta"].(map[string]any)["hostvars"].(map[string]any)
	tests := []struct {
		id, wantDefault, wantImage string
	}{
		{"srv-set", "amd", "ubuntu"},
		{"srv-image", "ubuntu", "ubuntu"},
		{"srv-unknown", "", ""},
	}
	for _, tc := range tests {
		vars := hostvars[tc.id].(map[string]any)
		gotDefault, _ := vars["default_user"].(string)
		gotImage, _ := vars["image_default_user"].(string)
		if gotDefault != tc.wantDefault || gotImage != tc.wantImage {
			t.Errorf("%s default_user=%q image_default_user=%q, want %q and %q", tc.id, gotDefault, gotImage, tc.wantDefault, tc.wantImage)
		}
	}
}

// An inventory with no hosts is a normal early state, and "children" must still be
// present as an array so that parsers do not treat the document as malformed.
func TestAnsibleInventory_EmptyInventoryIsStillWellFormed(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/ansible", nil, discoveryAuth())
	inventory := parseBody(t, resp)

	all, ok := inventory["all"].(map[string]any)
	if !ok {
		t.Fatalf("expected an all group, got %v", inventory["all"])
	}
	children, ok := all["children"].([]any)
	if !ok {
		t.Fatalf("children must be an array even when empty, got %v", all["children"])
	}
	if len(children) != 0 {
		t.Errorf("expected no children, got %v", children)
	}
}

func TestAnsibleInventory_SkipsServersWithoutAddress(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-bare", "gpu-node-02", "", nil)

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/ansible", nil, discoveryAuth())
	hostvars := parseBody(t, resp)["_meta"].(map[string]any)["hostvars"].(map[string]any)
	if len(hostvars) != 0 {
		t.Fatalf("an unreachable host would only produce a task failure: %v", hostvars)
	}
}

func TestDiscovery_RejectsBadToken(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus", nil, map[string]string{
		"Authorization": "Bearer wrong-token",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestDiscovery_RequiresAuth(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// An admin JWT also works, so an operator can inspect the endpoints without
// provisioning a discovery credential first.
func TestDiscovery_AcceptsAdminJWT(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/discovery/prometheus", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// The discovery token is scoped to discovery: it is not a second way into the API.
func TestDiscoveryToken_DoesNotWorkElsewhere(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/", nil, discoveryAuth())
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func groupNames(inventory map[string]any) []string {
	var names []string
	for name := range inventory {
		if name != "_meta" && name != "all" {
			names = append(names, name)
		}
	}
	return names
}
