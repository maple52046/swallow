package platformapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A reader is created per read and never cached, so its transport must close connections
// after each response. Keeping them alive leaked connections into slurmrestd's small
// concurrent-connection cap (~124) until it refused new ones and membership sync stalled with
// "Could not reach the Slurm API"; this guards that regression.
func TestReaderTransportDisablesKeepAlives(t *testing.T) {
	secure := newReaderTransport(false)
	if !secure.DisableKeepAlives {
		t.Error("reader transport must disable keep-alives so connections do not accumulate on slurmrestd")
	}
	if secure.TLSClientConfig != nil {
		t.Error("a verifying reader must not set a TLS client config")
	}
	insecure := newReaderTransport(true)
	if !insecure.DisableKeepAlives {
		t.Error("insecure reader transport must also disable keep-alives")
	}
	if insecure.TLSClientConfig == nil || !insecure.TLSClientConfig.InsecureSkipVerify {
		t.Error("insecure reader must skip TLS verification")
	}
}

// The bodies mirror slurmrestd responses on a live cluster: /ping lists controllers in
// SlurmctldHost order with primary/backup and UP/DOWN, /partitions carries the configured node
// set, and /nodes reports per-node scheduler state plus cpu/memory/gres.
const slurmNodesBody = `{
  "nodes": [
    {"name":"compute-1","address":"192.168.100.4","state":["IDLE"],"partitions":["main"],"cpus":8,"real_memory":16000,"gres":"gpu:8"},
    {"name":"compute-2","address":"192.168.100.5","state":["ALLOCATED"],"partitions":["main"],"cpus":8,"real_memory":16000,"gres":""}
  ]
}`

const slurmPingBody = `{
  "pings": [
    {"hostname":"control-1","pinged":"UP","mode":"primary"},
    {"hostname":"control-2","pinged":"DOWN","mode":"backup"}
  ]
}`

const slurmPartitionsBody = `{
  "partitions": [
    {"name":"main","nodes":{"configured":"compute[1-2]","total":2},"partition":{"state":["UP"]}}
  ]
}`

func newSlurmTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// slurmrestd authenticates with its own header, not Authorization; assert the reader
		// sends it so a regression that drops the token surfaces here.
		if r.Header.Get("X-SLURM-USER-TOKEN") == "" {
			http.Error(w, "missing slurm token", http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/nodes"):
			_, _ = w.Write([]byte(slurmNodesBody))
		case strings.HasSuffix(r.URL.Path, "/ping"):
			_, _ = w.Write([]byte(slurmPingBody))
		case strings.HasSuffix(r.URL.Path, "/partitions"):
			_, _ = w.Write([]byte(slurmPartitionsBody))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
}

func TestSlurmGetClusterState(t *testing.T) {
	server := newSlurmTestServer(t)
	defer server.Close()

	reader, err := NewSlurmReader(server.URL, "token", "", time.Second, false)
	if err != nil {
		t.Fatalf("NewSlurmReader: %v", err)
	}

	state, err := reader.GetClusterState(context.Background())
	if err != nil {
		t.Fatalf("GetClusterState: %v", err)
	}

	if len(state.Controllers) != 2 {
		t.Fatalf("controllers = %d, want 2: %+v", len(state.Controllers), state.Controllers)
	}
	if !state.Controllers[0].Primary || state.Controllers[0].Status != "up" {
		t.Errorf("controller[0] = %+v, want primary up", state.Controllers[0])
	}
	if state.Controllers[1].Primary || state.Controllers[1].Status != "down" {
		t.Errorf("controller[1] = %+v, want backup down", state.Controllers[1])
	}

	if len(state.Partitions) != 1 {
		t.Fatalf("partitions = %d, want 1: %+v", len(state.Partitions), state.Partitions)
	}
	if p := state.Partitions[0]; p.Name != "main" || p.State != "up" || p.NodeSpec != "compute[1-2]" || p.TotalNodes != 2 {
		t.Errorf("partition = %+v, want main/up/compute[1-2]/2", p)
	}

	if len(state.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2: %+v", len(state.Nodes), state.Nodes)
	}
	if n := state.Nodes[0]; n.State != "idle" || n.CPUs != 8 || n.RealMemoryMiB != 16000 || n.Gres != "gpu:8" {
		t.Errorf("node[0] = %+v, want idle/8/16000/gpu:8", n)
	}
	if state.Nodes[1].State != "allocated" {
		t.Errorf("node[1].State = %q, want allocated", state.Nodes[1].State)
	}
}

// TestSlurmListMembersUnchanged guards that adding the cluster read did not alter the
// membership-sync path: ListMembers still reports each node with its partitions as Role and its
// collapsed Slurm state.
func TestSlurmListMembersUnchanged(t *testing.T) {
	server := newSlurmTestServer(t)
	defer server.Close()

	reader, err := NewSlurmReader(server.URL, "token", "", time.Second, false)
	if err != nil {
		t.Fatalf("NewSlurmReader: %v", err)
	}
	members, err := reader.ListMembers(context.Background())
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("members = %d, want 2", len(members))
	}
	if members[0].Role != "main" || members[0].State != "idle" {
		t.Errorf("member[0] = %+v, want role main state idle", members[0])
	}
}
