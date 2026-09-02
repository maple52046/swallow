package tests

import (
	"net/http"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// seedActionableServer wires a server whose provider machine exists, so a capability
// action has something to drive.
func seedActionableServer(t *testing.T, f *platformFixture) string {
	t.Helper()
	server := f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine(server.Source.ProviderMachineID, "gpu-node-01"))
	return server.ID
}

func TestServerActions_DriveTheProvisioner(t *testing.T) {
	cases := []struct {
		path   string
		wantOp string
	}{
		{"power-on", "power_on machine-srv-1"},
		{"power-off", "power_off machine-srv-1"},
		{"commission", "commission machine-srv-1"},
		{"test", "test machine-srv-1"},
		{"abort", "abort machine-srv-1"},
		{"override-failed-testing", "override_failed_testing machine-srv-1"},
		{"lock", "lock machine-srv-1"},
		{"unlock", "unlock machine-srv-1"},
		{"mark-broken", "mark_broken machine-srv-1"},
		{"mark-fixed", "mark_fixed machine-srv-1"},
		{"rescue-mode", "rescue_mode machine-srv-1"},
		{"exit-rescue-mode", "exit_rescue_mode machine-srv-1"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			f := setupPlatform(t)
			seedActionableServer(t, f)

			resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/"+tc.path, nil, f.adminAuth(t))
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("expected 202, got %d", resp.StatusCode)
			}
			last := f.provider.actions[len(f.provider.actions)-1]
			if last != tc.wantOp {
				t.Errorf("provider action: got %q, want %q", last, tc.wantOp)
			}
		})
	}
}

func TestServerActions_QueryPowerState(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	machine := testMachine("machine-srv-1", "gpu-node-01")
	machine.PowerState = provisioningdomain.PowerStateOn
	f.provider.withMachine(machine)

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/srv-1/power-state", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["powerState"] != "on" {
		t.Errorf("powerState: got %v, want on", body["powerState"])
	}
}

// The whole point of the capability model is that an action a provisioner cannot do is
// refused, not silently dropped. A provider that implements only the base interface must
// produce a 400, and must never be asked to do the thing.
func TestServerActions_RefusedWhenProvisionerLacksCapability(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.factory.providers[testIntegrationID] = &minimalProvider{
		machines: map[string]*provisioningdomain.Machine{
			"machine-srv-1": testMachine("machine-srv-1", "gpu-node-01"),
		},
	}

	for _, path := range []string{"power-on", "commission", "lock"} {
		resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/"+path, nil, f.adminAuth(t))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", path, resp.StatusCode)
		}
	}
}

func TestServerActions_UnknownServerIs404(t *testing.T) {
	f := setupPlatform(t)
	resp := doRequest(t, f.app, "POST", "/api/v1/servers/nope/power-on", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestProvisionerDetail_ReturnsCapabilitiesAndSections(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))
	f.provider.detail = &provisioningdomain.MachineDetail{
		Sections: []provisioningdomain.DetailSection{
			{Title: "System", Fields: []provisioningdomain.DetailField{{Label: "Vendor", Value: "Dell Inc."}}},
		},
		Tables: []provisioningdomain.DetailTable{
			{Title: "Storage", Columns: []string{"Name"}, Rows: [][]string{{"nvme0n1"}}},
		},
	}

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/srv-1/provisioner-detail", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)

	caps, ok := body["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("expected capabilities object, got %v", body["capabilities"])
	}
	if caps["power"] != true || caps["machineDetail"] != true || caps["machineRemoval"] != true {
		t.Errorf("capabilities should reflect the provider, got %v", caps)
	}
	sections, ok := body["sections"].([]any)
	if !ok || len(sections) != 1 {
		t.Fatalf("expected one section, got %v", body["sections"])
	}
}

// Capabilities describe the adapter, so they must answer even for a provisioner that
// offers no live detail: a client still needs to know which actions to show.
func TestProvisionerDetail_CapabilitiesWithoutDetailSupport(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.factory.providers[testIntegrationID] = &minimalProvider{
		machines: map[string]*provisioningdomain.Machine{
			"machine-srv-1": testMachine("machine-srv-1", "gpu-node-01"),
		},
	}

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/srv-1/provisioner-detail", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	caps := body["capabilities"].(map[string]any)
	if caps["machineDetail"] != false || caps["power"] != false || caps["machineRemoval"] != false || caps["releaseOptions"] != false {
		t.Errorf("a base-only provisioner must advertise no optional capabilities, got %v", caps)
	}
	if sections, ok := body["sections"].([]any); !ok || len(sections) != 0 {
		t.Errorf("expected no sections without detail support, got %v", body["sections"])
	}
}

func TestProviderEvents_ReturnsMachineHistory(t *testing.T) {
	f := setupPlatform(t)
	seedActionableServer(t, f)
	f.provider.events = []provisioningdomain.MachineEvent{{
		ID:          "4812",
		Level:       "audit",
		Type:        "Request from user",
		Description: "Started releasing machine.",
		Actor:       "admin",
		OccurredAt:  "2026-09-02T01:02:03.000000Z",
	}}

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/srv-1/events?limit=17", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["supported"] != true {
		t.Fatalf("supported: got %v, want true", body["supported"])
	}
	events, ok := body["events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("events: got %v, want one item", body["events"])
	}
	event := events[0].(map[string]any)
	if event["id"] != "4812" || event["message"] != "Started releasing machine." {
		t.Errorf("event: got %v", event)
	}
	if f.provider.eventMachineID != "machine-srv-1" || f.provider.eventLimit != 17 {
		t.Errorf("provider query: machine=%q limit=%d", f.provider.eventMachineID, f.provider.eventLimit)
	}
}

func TestProviderEvents_ReportsUnsupportedProvider(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.factory.providers[testIntegrationID] = &minimalProvider{
		machines: map[string]*provisioningdomain.Machine{
			"machine-srv-1": testMachine("machine-srv-1", "gpu-node-01"),
		},
	}

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/srv-1/events", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["supported"] != false {
		t.Errorf("supported: got %v, want false", body["supported"])
	}
	if events, ok := body["events"].([]any); !ok || len(events) != 0 {
		t.Errorf("events: got %v, want empty array", body["events"])
	}
}

func TestProviderEvents_ValidatesLimitAndServer(t *testing.T) {
	f := setupPlatform(t)
	for _, path := range []string{
		"/api/v1/servers/srv-1/events?limit=0",
		"/api/v1/servers/srv-1/events?limit=101",
		"/api/v1/servers/srv-1/events?limit=wat",
	} {
		resp := doRequest(t, f.app, "GET", path, nil, f.adminAuth(t))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", path, resp.StatusCode)
		}
	}

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/missing/events", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing Server: got %d, want 404", resp.StatusCode)
	}
}
