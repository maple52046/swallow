package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

// seedDeployedKubernetes seeds a Swallow-deployed Kubernetes platform: a platform record with a
// credential integration plus a deployed lifecycle snapshot, which is what the explorer requires.
func seedDeployedKubernetes(t *testing.T, f *platformFixture, id string) *platformdomain.Platform {
	t.Helper()
	platform := seedPlatform(t, f, id, "prod-k8s", "provisioning")
	f.platformLifecycle.snapshots[id] = platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed,
		State:  platformdomain.PlatformLifecycleActive,
	}
	return platform
}

// The explorer is available only for a Swallow-deployed Kubernetes platform. A legacy
// registered record (no deployed provenance) is refused with 409.
func TestKubernetesExplorer_RegisteredPlatformIsRefused(t *testing.T) {
	f := setupPlatform(t)
	seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning") // no deployed snapshot => registered

	resp := doRequest(t, f.app, "GET", "/api/v1/platforms/platform-1/kubernetes", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("registered platform: expected 409, got %d", resp.StatusCode)
	}
}

// A Slurm platform has no Kubernetes explorer; the surface returns 404.
func TestKubernetesExplorer_NonKubernetesIs404(t *testing.T) {
	f := setupPlatform(t)
	now := time.Now().UTC()
	if err := f.platformRepo.Create(context.Background(), &platformdomain.Platform{
		ID: "platform-slurm", SiteID: testSiteID, Name: "hpc", Type: platformdomain.PlatformTypeSlurm,
		IntegrationID: "integration-slurm", GPUStackOwner: "provisioning", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed slurm platform: %v", err)
	}
	f.platformLifecycle.snapshots["platform-slurm"] = platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleActive,
	}

	resp := doRequest(t, f.app, "GET", "/api/v1/platforms/platform-slurm/kubernetes", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("slurm platform: expected 404, got %d", resp.StatusCode)
	}
}

// The summary reads the live cluster header (version and counts) for a deployed platform.
func TestKubernetesExplorer_Summary(t *testing.T) {
	f := setupPlatform(t)
	seedDeployedKubernetes(t, f, "platform-1")
	f.kubernetesClient.client.summary = platformdomain.KubernetesClusterSummary{
		Version: "v1.30.2+k0s", NodeCount: 3, ReadyNodeCount: 3, NamespaceCount: 8,
	}

	resp := doRequest(t, f.app, "GET", "/api/v1/platforms/platform-1/kubernetes", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["version"] != "v1.30.2+k0s" || body["nodeCount"].(float64) != 3 {
		t.Errorf("unexpected summary: %v", body)
	}
}

// Node correlation resolves a cluster node to a Server projection by name.
func TestKubernetesExplorer_NodesCorrelateToServers(t *testing.T) {
	f := setupPlatform(t)
	seedDeployedKubernetes(t, f, "platform-1")
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.kubernetesClient.client.nodes = []platformdomain.KubernetesNode{
		{Name: "gpu-node-01", Role: "worker", Ready: true, Addresses: []string{"10.0.1.10"}},
	}

	resp := doRequest(t, f.app, "GET", "/api/v1/platforms/platform-1/kubernetes/nodes", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	items := parseBody(t, resp)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 node, got %d", len(items))
	}
	node := items[0].(map[string]any)
	if node["serverId"] != "srv-1" {
		t.Errorf("expected node correlated to srv-1, got %v", node["serverId"])
	}
}

// A system namespace cannot be deleted; the guard rejects it before any cluster call.
func TestKubernetesExplorer_SystemNamespaceDeleteRefused(t *testing.T) {
	f := setupPlatform(t)
	seedDeployedKubernetes(t, f, "platform-1")

	resp := doRequest(t, f.app, "DELETE", "/api/v1/platforms/platform-1/kubernetes/namespaces/kube-system", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	if len(f.kubernetesClient.client.deletedNS) != 0 {
		t.Errorf("system namespace delete must not reach the cluster")
	}
}

// Scaling requires a replicas field and drives the client with the requested count.
func TestKubernetesExplorer_ScaleApplication(t *testing.T) {
	f := setupPlatform(t)
	seedDeployedKubernetes(t, f, "platform-1")

	missing := doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/kubernetes/applications/web/Deployment/nginx/scale", map[string]any{}, f.adminAuth(t))
	if missing.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing replicas: expected 400, got %d", missing.StatusCode)
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/kubernetes/applications/web/Deployment/nginx/scale", map[string]any{"replicas": 4}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if f.kubernetesClient.client.scaledTo["Deployment/nginx"] != 4 {
		t.Errorf("expected Deployment/nginx scaled to 4, got %v", f.kubernetesClient.client.scaledTo)
	}
}

// An unsupported application kind in the path is a validation error, not a cluster call.
func TestKubernetesExplorer_UnsupportedKindRejected(t *testing.T) {
	f := setupPlatform(t)
	seedDeployedKubernetes(t, f, "platform-1")

	resp := doRequest(t, f.app, "GET", "/api/v1/platforms/platform-1/kubernetes/applications/web/ReplicaSet/x", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Apply returns per-object results from the cluster.
func TestKubernetesExplorer_Apply(t *testing.T) {
	f := setupPlatform(t)
	seedDeployedKubernetes(t, f, "platform-1")
	f.kubernetesClient.client.applyResults = []platformdomain.KubernetesApplyResult{
		{Kind: "Deployment", Namespace: "web", Name: "nginx", Action: "configured"},
	}

	empty := doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/kubernetes/apply", map[string]any{"manifest": ""}, f.adminAuth(t))
	if empty.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty manifest: expected 400, got %d", empty.StatusCode)
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/kubernetes/apply", map[string]any{"manifest": "kind: Deployment"}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	results := parseBody(t, resp)["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["action"] != "configured" {
		t.Errorf("unexpected apply results: %v", results)
	}
}

// Pod logs return the container and the log snapshot.
func TestKubernetesExplorer_PodLogs(t *testing.T) {
	f := setupPlatform(t)
	seedDeployedKubernetes(t, f, "platform-1")
	f.kubernetesClient.client.logs = "hello from nginx"

	resp := doRequest(t, f.app, "GET", "/api/v1/platforms/platform-1/kubernetes/pods/web/nginx-abc/logs?container=nginx", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["container"] != "nginx" || body["logs"] != "hello from nginx" {
		t.Errorf("unexpected logs body: %v", body)
	}
}
