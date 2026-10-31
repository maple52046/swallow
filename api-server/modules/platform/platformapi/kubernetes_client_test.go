package platformapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// newTestKubernetesClient builds a client pointed at a test server with a trivial token.
func newTestKubernetesClient(t *testing.T, url string) *KubernetesClient {
	t.Helper()
	client, err := NewKubernetesClient(url, "test-token", 5*time.Second, false)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

// TestListApplicationsAggregatesWorkloadsAndBarePods verifies the Application aggregation: each
// workload controller is one Application, a Pod owned by a controller is folded into it (not
// repeated), and an owner-less Pod becomes its own bare-Pod Application. This is the core
// Portainer-style logic borrowed for the explorer.
func TestListApplicationsAggregatesWorkloadsAndBarePods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/apps/v1/deployments":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"nginx","namespace":"web"},"spec":{"replicas":2,"template":{"spec":{"containers":[{"image":"nginx:1.27"}]}}},"status":{"readyReplicas":2}}]}`))
		case "/apis/apps/v1/daemonsets":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/apis/apps/v1/statefulsets":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/api/v1/pods":
			// One pod owned by a ReplicaSet (belongs to the Deployment, not a bare pod) and one
			// owner-less pod (a bare-Pod Application).
			_, _ = w.Write([]byte(`{"items":[
				{"metadata":{"name":"nginx-abc","namespace":"web","ownerReferences":[{"kind":"ReplicaSet","name":"nginx-6d8"}]},"spec":{"containers":[{"name":"nginx"}]},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}},
				{"metadata":{"name":"debug","namespace":"web"},"spec":{"containers":[{"name":"debug"}]},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestKubernetesClient(t, server.URL)
	apps, err := client.ListApplications(context.Background(), "", false)
	if err != nil {
		t.Fatalf("list applications: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 applications (one Deployment, one bare Pod), got %d: %+v", len(apps), apps)
	}

	var deployment, barePod *platformdomain.KubernetesApplication
	for i := range apps {
		switch apps[i].Kind {
		case platformdomain.KubernetesKindDeployment:
			deployment = &apps[i]
		case platformdomain.KubernetesKindPod:
			barePod = &apps[i]
		}
	}
	if deployment == nil {
		t.Fatal("expected a Deployment application")
	}
	if deployment.Replicas != 2 || deployment.ReadyReplicas != 2 {
		t.Errorf("deployment replicas = %d/%d, want 2/2", deployment.ReadyReplicas, deployment.Replicas)
	}
	if barePod == nil || barePod.Name != "debug" {
		t.Errorf("expected an owner-less pod 'debug' as a bare-Pod application, got %+v", barePod)
	}
}

// TestListNodesReportsAMDGPUResources verifies the explorer's GPU read: allocatable amd.com/*
// resources come from the node status, DRA devices are counted per node from gpu.amd.com
// ResourceSlices, and a cluster without the DRA API reports the count as unknown, not zero.
func TestListNodesReportsAMDGPUResources(t *testing.T) {
	serve := func(dra bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/nodes":
				_, _ = w.Write([]byte(`{"items":[
					{"metadata":{"name":"gpu-1"},"status":{"allocatable":{"cpu":"64","amd.com/gpu":"8"}}},
					{"metadata":{"name":"cpu-1"},"status":{"allocatable":{"cpu":"16"}}}
				]}`))
			case "/apis/resource.k8s.io/v1/resourceslices":
				if !dra {
					http.NotFound(w, r)
					return
				}
				if r.URL.Query().Get("fieldSelector") != "spec.driver=gpu.amd.com" {
					t.Errorf("resourceslices fieldSelector = %q, want spec.driver=gpu.amd.com", r.URL.Query().Get("fieldSelector"))
				}
				_, _ = w.Write([]byte(`{"items":[
					{"spec":{"driver":"gpu.amd.com","nodeName":"gpu-1","devices":[{"name":"gpu-0"},{"name":"gpu-1"}]}},
					{"spec":{"driver":"gpu.amd.com","nodeName":"gpu-1","devices":[{"name":"gpu-2"}]}}
				]}`))
			default:
				http.NotFound(w, r)
			}
		}))
	}

	server := serve(true)
	defer server.Close()
	nodes, err := newTestKubernetesClient(t, server.URL).ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if got := nodes[0].GPUAllocatable["amd.com/gpu"]; got != 8 || len(nodes[0].GPUAllocatable) != 1 {
		t.Errorf("gpu-1 allocatable = %v, want only amd.com/gpu=8", nodes[0].GPUAllocatable)
	}
	if nodes[0].GPUDRADevices == nil || *nodes[0].GPUDRADevices != 3 {
		t.Errorf("gpu-1 DRA devices = %v, want 3 across its slices", nodes[0].GPUDRADevices)
	}
	if nodes[1].GPUDRADevices == nil || *nodes[1].GPUDRADevices != 0 || len(nodes[1].GPUAllocatable) != 0 {
		t.Errorf("cpu-1 GPU = %v / %v, want no allocatable and 0 DRA devices", nodes[1].GPUAllocatable, nodes[1].GPUDRADevices)
	}

	legacy := serve(false)
	defer legacy.Close()
	nodes, err = newTestKubernetesClient(t, legacy.URL).ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes without DRA: %v", err)
	}
	if nodes[0].GPUDRADevices != nil {
		t.Errorf("DRA devices without resource.k8s.io/v1 = %v, want unknown", *nodes[0].GPUDRADevices)
	}
}

// TestScaleApplicationRejectsUnscalableKinds verifies scaling is refused for a DaemonSet and a
// bare Pod before any request is sent.
func TestScaleApplicationRejectsUnscalableKinds(t *testing.T) {
	client := newTestKubernetesClient(t, "http://127.0.0.1:1")
	for _, kind := range []string{platformdomain.KubernetesKindDaemonSet, platformdomain.KubernetesKindPod} {
		if _, err := client.ScaleApplication(context.Background(), "web", kind, "x", 3); err == nil {
			t.Errorf("kind %q: expected a validation error", kind)
		}
	}
}

// TestApplyRejectsUnsupportedKind verifies a manifest with an unknown kind is refused as a
// validation error rather than dispatched to the cluster.
func TestApplyRejectsUnsupportedKind(t *testing.T) {
	client := newTestKubernetesClient(t, "http://127.0.0.1:1")
	_, err := client.Apply(context.Background(), "apiVersion: v1\nkind: Frobnicator\nmetadata:\n  name: x\n", true)
	if err == nil {
		t.Fatal("expected an error for an unsupported kind")
	}
}
