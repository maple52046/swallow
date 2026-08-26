package clusterapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// nodeListBody and leaseListBody mirror the shapes observed on a live k0s HA cluster:
// dedicated controllers do not appear as nodes, and are visible only as k0s-ctrl-* leases
// whose name suffix is the controller's hostname.
const nodeListBody = `{
  "items": [
    {"metadata": {"name": "lab-compute-1", "labels": {}},
     "status": {"conditions": [{"type": "Ready", "status": "True"}],
                "addresses": [{"type": "InternalIP", "address": "192.168.100.4"}]}},
    {"metadata": {"name": "lab-compute-2", "labels": {}},
     "status": {"conditions": [{"type": "Ready", "status": "False"}],
                "addresses": [{"type": "InternalIP", "address": "192.168.100.7"}]}}
  ]
}`

func leaseListBody(renewTime string) string {
	return `{
  "items": [
    {"metadata": {"name": "k0s-ctrl-lab-control-1"},
     "spec": {"renewTime": "` + renewTime + `", "leaseDurationSeconds": 60}},
    {"metadata": {"name": "k0s-endpoint-reconciler"},
     "spec": {"renewTime": "` + renewTime + `", "leaseDurationSeconds": 60}},
    {"metadata": {"name": "lab-compute-1"},
     "spec": {"renewTime": "` + renewTime + `", "leaseDurationSeconds": 60}}
  ]
}`
}

func newTestServer(t *testing.T, leaseBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/nodes":
			_, _ = w.Write([]byte(nodeListBody))
		case "/apis/coordination.k8s.io/v1/namespaces/kube-node-lease/leases":
			_, _ = w.Write([]byte(leaseBody))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
}

func TestListMembersNodesOnly(t *testing.T) {
	server := newTestServer(t, leaseListBody("2000-01-01T00:00:00Z"))
	defer server.Close()

	reader, err := NewKubernetesReader(server.URL, "token", time.Second, false, false)
	if err != nil {
		t.Fatalf("NewKubernetesReader: %v", err)
	}

	members, err := reader.ListMembers(context.Background())
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members without lease discovery, got %d: %+v", len(members), members)
	}
	for _, member := range members {
		if member.Role != "worker" {
			t.Errorf("expected worker, got %q for %s", member.Role, member.Name)
		}
	}
}

func TestListMembersWithControllerLeases(t *testing.T) {
	fresh := time.Now().UTC().Format(time.RFC3339Nano)
	server := newTestServer(t, leaseListBody(fresh))
	defer server.Close()

	reader, err := NewKubernetesReader(server.URL, "token", time.Second, false, true)
	if err != nil {
		t.Fatalf("NewKubernetesReader: %v", err)
	}

	members, err := reader.ListMembers(context.Background())
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}

	byName := map[string]struct {
		role  string
		state string
	}{}
	for _, member := range members {
		byName[member.Name] = struct {
			role  string
			state string
		}{member.Role, member.State}
	}

	// The controller lease becomes a control-plane member named by its hostname suffix.
	controller, ok := byName["lab-control-1"]
	if !ok {
		t.Fatalf("expected lab-control-1 from controller lease, got %+v", byName)
	}
	if controller.role != "control-plane" {
		t.Errorf("expected control-plane role, got %q", controller.role)
	}
	if controller.state != "ready" {
		t.Errorf("expected ready from a fresh lease, got %q", controller.state)
	}

	// Non-controller leases (endpoint-reconciler) are ignored, and a lease that
	// duplicates a node name does not add a second member.
	if _, ok := byName["k0s-endpoint-reconciler"]; ok {
		t.Error("endpoint-reconciler lease should be ignored")
	}
	if len(members) != 3 {
		t.Fatalf("expected 2 nodes + 1 controller, got %d: %+v", len(members), members)
	}
	if byName["lab-compute-1"].role != "worker" {
		t.Errorf("node lab-compute-1 should stay worker, got %q", byName["lab-compute-1"].role)
	}
}

func TestLeaseStateStaleIsNotReady(t *testing.T) {
	now := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)
	dur := 60
	cases := []struct {
		name      string
		renewTime string
		want      string
	}{
		{"fresh", now.Add(-5 * time.Second).Format(time.RFC3339Nano), "ready"},
		{"withinWindow", now.Add(-170 * time.Second).Format(time.RFC3339Nano), "ready"},
		{"stale", now.Add(-10 * time.Minute).Format(time.RFC3339Nano), "notready"},
		{"missing", "", "notready"},
		{"unparseable", "not-a-time", "notready"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := leaseState(tc.renewTime, &dur, now); got != tc.want {
				t.Errorf("leaseState(%q) = %q, want %q", tc.renewTime, got, tc.want)
			}
		})
	}
}
