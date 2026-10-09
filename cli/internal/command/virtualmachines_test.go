package command

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

// TestVirtualMachineCommandsFollowTheContract checks method, path, query, and body against
// server-enrollment.md "Virtual machines (libvirt)": list reads the hypervisor's domains with an
// optional account, and enroll posts the domains by name with only the options given.
func TestVirtualMachineCommandsFollowTheContract(t *testing.T) {
	type seen struct {
		method, path, query string
		body                map[string]any
	}
	var requests []seen
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		request := seen{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&request.body)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workflowId":"wf-1","items":[]}`))
	})

	run := func(args ...string) error {
		t.Helper()
		cmd := serversVirtualMachinesCmd()
		cmd.SetArgs(args)
		cmd.SetContext(context.Background())
		return cmd.Execute()
	}
	for _, args := range [][]string{
		{"list", "hv-1", "--account", "ubuntu"},
		{"enroll", "hv-1", "lab-1", "lab,2", "--integration", "maas-a", "--boot-iso", "iso-1"},
		{"enroll", "hv-1", "lab-3", "--integration", "maas-a", "--power-off-running"},
	} {
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}

	want := []seen{
		{method: "GET", path: "/api/v1/servers/hv-1/virtual-machines", query: "account=ubuntu"},
		{method: "POST", path: "/api/v1/provisioning/virtual-machine-enrollments", body: map[string]any{
			"integrationId": "maas-a", "hypervisorServerId": "hv-1", "domains": []any{"lab-1", "lab,2"}, "bootIsoId": "iso-1",
		}},
		{method: "POST", path: "/api/v1/provisioning/virtual-machine-enrollments", body: map[string]any{
			"integrationId": "maas-a", "hypervisorServerId": "hv-1", "domains": []any{"lab-3"}, "powerOffRunning": true,
		}},
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %+v, want %d", requests, len(want))
	}
	for i := range want {
		got := requests[i]
		if got.method != want[i].method || got.path != want[i].path || got.query != want[i].query {
			t.Errorf("request %d = %+v, want %+v", i, got, want[i])
		}
		if want[i].body != nil && !reflect.DeepEqual(got.body, want[i].body) {
			t.Errorf("request %d body = %v, want %v", i, got.body, want[i].body)
		}
	}

	for _, args := range [][]string{
		{"enroll", "hv-1", "--integration", "maas-a"},
		{"enroll", "hv-1", "lab-1"},
	} {
		if err := run(args...); err == nil {
			t.Errorf("%v succeeded, want a usage error", args)
		}
	}
}
