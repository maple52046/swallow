package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/shared/apicontract"
)

// explorerCall is one cluster call and the credential of the client that made it.
type explorerCall struct {
	credential string
	method     string
}

// recordingCluster is a cluster client that records each call under the credential it was built
// from, and answers with the smallest response the contract accepts.
type recordingCluster struct {
	credential string
	calls      *[]explorerCall
}

func (r recordingCluster) record(method string) {
	*r.calls = append(*r.calls, explorerCall{credential: r.credential, method: method})
}

func (r recordingCluster) Summary(context.Context) (*platformdomain.KubernetesClusterSummary, error) {
	r.record("Summary")
	return &platformdomain.KubernetesClusterSummary{Version: "v1.30.2+k0s"}, nil
}

func (r recordingCluster) ListNodes(context.Context) ([]platformdomain.KubernetesNode, error) {
	r.record("ListNodes")
	return []platformdomain.KubernetesNode{{Name: "node-1", Role: "worker"}}, nil
}

func (r recordingCluster) SetNodeSchedulable(_ context.Context, name string, _ bool) (*platformdomain.KubernetesNode, error) {
	r.record("SetNodeSchedulable")
	return &platformdomain.KubernetesNode{Name: name, Role: "worker"}, nil
}

func (r recordingCluster) ListNamespaces(context.Context) ([]platformdomain.KubernetesNamespace, error) {
	r.record("ListNamespaces")
	return nil, nil
}

func (r recordingCluster) CreateNamespace(_ context.Context, name string) (*platformdomain.KubernetesNamespace, error) {
	r.record("CreateNamespace")
	return &platformdomain.KubernetesNamespace{Name: name, Phase: "Active"}, nil
}

func (r recordingCluster) DeleteNamespace(context.Context, string) error {
	r.record("DeleteNamespace")
	return nil
}

func (r recordingCluster) ListApplications(context.Context, string, bool) ([]platformdomain.KubernetesApplication, error) {
	r.record("ListApplications")
	return nil, nil
}

func (r recordingCluster) GetApplication(_ context.Context, namespace, kind, name string) (*platformdomain.KubernetesApplication, error) {
	r.record("GetApplication")
	return &platformdomain.KubernetesApplication{Namespace: namespace, Name: name, Kind: kind}, nil
}

func (r recordingCluster) DeleteApplication(context.Context, string, string, string) error {
	r.record("DeleteApplication")
	return nil
}

func (r recordingCluster) ScaleApplication(_ context.Context, namespace, kind, name string, replicas int) (*platformdomain.KubernetesApplication, error) {
	r.record("ScaleApplication")
	return &platformdomain.KubernetesApplication{Namespace: namespace, Name: name, Kind: kind, Replicas: replicas}, nil
}

func (r recordingCluster) RestartApplication(context.Context, string, string, string) error {
	r.record("RestartApplication")
	return nil
}

func (r recordingCluster) ListPods(context.Context, string, bool) ([]platformdomain.KubernetesPod, error) {
	r.record("ListPods")
	return nil, nil
}

func (r recordingCluster) PodLogs(_ context.Context, _, _, _ string, _ int) (string, string, error) {
	r.record("PodLogs")
	return "nginx", "ready\n", nil
}

func (r recordingCluster) DeletePod(context.Context, string, string) error {
	r.record("DeletePod")
	return nil
}

func (r recordingCluster) ListServices(context.Context, string, bool) ([]platformdomain.KubernetesService, error) {
	r.record("ListServices")
	return nil, nil
}

func (r recordingCluster) ListIngresses(context.Context, string, bool) ([]platformdomain.KubernetesIngress, error) {
	r.record("ListIngresses")
	return nil, nil
}

func (r recordingCluster) ListConfigMaps(context.Context, string, bool) ([]platformdomain.KubernetesConfigResource, error) {
	r.record("ListConfigMaps")
	return nil, nil
}

func (r recordingCluster) ListSecrets(context.Context, string, bool) ([]platformdomain.KubernetesConfigResource, error) {
	r.record("ListSecrets")
	return nil, nil
}

func (r recordingCluster) ListPersistentVolumeClaims(context.Context, string, bool) ([]platformdomain.KubernetesPersistentVolumeClaim, error) {
	r.record("ListPersistentVolumeClaims")
	return nil, nil
}

func (r recordingCluster) Apply(context.Context, string, bool) ([]platformdomain.KubernetesApplyResult, error) {
	r.record("Apply")
	return []platformdomain.KubernetesApplyResult{{Kind: "ConfigMap", Namespace: "web", Name: "settings", Action: "created"}}, nil
}

// credentialClusters builds a recording client per credential: For the read one, ForOperator the
// operator one.
type credentialClusters struct{ calls *[]explorerCall }

func (f credentialClusters) For(context.Context, *platformdomain.Platform) (platformdomain.KubernetesClusterClient, error) {
	return recordingCluster{credential: "read", calls: f.calls}, nil
}

func (f credentialClusters) ForOperator(context.Context, *platformdomain.Platform) (platformdomain.KubernetesClusterClient, error) {
	return recordingCluster{credential: "operator", calls: f.calls}, nil
}

// deployedPlatforms knows one swallow-deployed Kubernetes Platform, "k8s".
type deployedPlatforms struct {
	platformdomain.PlatformRepository
}

func (deployedPlatforms) FindByID(_ context.Context, id string) (*platformdomain.Platform, error) {
	if id != "k8s" {
		return nil, platformdomain.ErrPlatformNotFound
	}
	return &platformdomain.Platform{ID: id, Type: platformdomain.PlatformTypeKubernetes, IntegrationID: "i1"}, nil
}

type deployedLifecycle struct{}

func (deployedLifecycle) Read(_ context.Context, ids []string) (map[string]platformdomain.LifecycleSnapshot, error) {
	snapshots := make(map[string]platformdomain.LifecycleSnapshot, len(ids))
	for _, id := range ids {
		snapshots[id] = platformdomain.LifecycleSnapshot{Origin: platformdomain.PlatformOriginDeployed}
	}
	return snapshots, nil
}

type emptyServers struct{ serverdomain.ServerRepository }

func (emptyServers) List(context.Context, serverdomain.ListFilter) (serverdomain.ListResult, error) {
	return serverdomain.ListResult{}, nil
}

// Every explorer route declares the credential it needs: the reads use the read credential, and
// the routes that change the cluster (a dry-run apply included) and the Secret list, which the
// read credential is denied, use the operator credential (platforms-kubernetes.md, Cluster
// Credentials). Each request makes exactly the one cluster call its route maps to.
func TestKubernetesExplorerRoutesUseTheCredentialTheirAccessNeeds(t *testing.T) {
	var calls []explorerCall
	explorer := application.NewKubernetesExplorerUseCase(deployedPlatforms{}, deployedLifecycle{}, credentialClusters{calls: &calls}, emptyServers{})
	handler := NewPlatformHandler(nil, nil, nil, explorer, nil)

	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	routes := app.Group("/api/v1/platforms")
	routes.Get("/:id/kubernetes", handler.GetKubernetesCluster)
	routes.Get("/:id/kubernetes/nodes", handler.ListKubernetesNodes)
	routes.Post("/:id/kubernetes/nodes/:node/cordon", handler.CordonKubernetesNode)
	routes.Post("/:id/kubernetes/nodes/:node/uncordon", handler.UncordonKubernetesNode)
	routes.Get("/:id/kubernetes/namespaces", handler.ListKubernetesNamespaces)
	routes.Post("/:id/kubernetes/namespaces", handler.CreateKubernetesNamespace)
	routes.Delete("/:id/kubernetes/namespaces/:namespace", handler.DeleteKubernetesNamespace)
	routes.Get("/:id/kubernetes/applications", handler.ListKubernetesApplications)
	routes.Get("/:id/kubernetes/applications/:namespace/:kind/:name", handler.GetKubernetesApplication)
	routes.Delete("/:id/kubernetes/applications/:namespace/:kind/:name", handler.DeleteKubernetesApplication)
	routes.Post("/:id/kubernetes/applications/:namespace/:kind/:name/scale", handler.ScaleKubernetesApplication)
	routes.Post("/:id/kubernetes/applications/:namespace/:kind/:name/restart", handler.RestartKubernetesApplication)
	routes.Get("/:id/kubernetes/pods", handler.ListKubernetesPods)
	routes.Get("/:id/kubernetes/pods/:namespace/:name/logs", handler.GetKubernetesPodLogs)
	routes.Delete("/:id/kubernetes/pods/:namespace/:name", handler.DeleteKubernetesPod)
	routes.Get("/:id/kubernetes/services", handler.ListKubernetesServices)
	routes.Get("/:id/kubernetes/ingresses", handler.ListKubernetesIngresses)
	routes.Get("/:id/kubernetes/configmaps", handler.ListKubernetesConfigMaps)
	routes.Get("/:id/kubernetes/secrets", handler.ListKubernetesSecrets)
	routes.Get("/:id/kubernetes/persistentvolumeclaims", handler.ListKubernetesPersistentVolumeClaims)
	routes.Post("/:id/kubernetes/apply", handler.ApplyKubernetesManifest)

	const base = "/api/v1/platforms/k8s/kubernetes"
	cases := []struct {
		method, path, body string
		wantCall           explorerCall
	}{
		{http.MethodGet, base, "", explorerCall{"read", "Summary"}},
		{http.MethodGet, base + "/nodes", "", explorerCall{"read", "ListNodes"}},
		{http.MethodPost, base + "/nodes/node-1/cordon", "", explorerCall{"operator", "SetNodeSchedulable"}},
		{http.MethodPost, base + "/nodes/node-1/uncordon", "", explorerCall{"operator", "SetNodeSchedulable"}},
		{http.MethodGet, base + "/namespaces", "", explorerCall{"read", "ListNamespaces"}},
		{http.MethodPost, base + "/namespaces", `{"name":"web"}`, explorerCall{"operator", "CreateNamespace"}},
		{http.MethodDelete, base + "/namespaces/web", "", explorerCall{"operator", "DeleteNamespace"}},
		{http.MethodGet, base + "/applications", "", explorerCall{"read", "ListApplications"}},
		{http.MethodGet, base + "/applications/web/Deployment/nginx", "", explorerCall{"read", "GetApplication"}},
		{http.MethodDelete, base + "/applications/web/Deployment/nginx", "", explorerCall{"operator", "DeleteApplication"}},
		{http.MethodPost, base + "/applications/web/Deployment/nginx/scale", `{"replicas":2}`, explorerCall{"operator", "ScaleApplication"}},
		{http.MethodPost, base + "/applications/web/Deployment/nginx/restart", "", explorerCall{"operator", "RestartApplication"}},
		{http.MethodGet, base + "/pods", "", explorerCall{"read", "ListPods"}},
		{http.MethodGet, base + "/pods/web/nginx-1/logs", "", explorerCall{"read", "PodLogs"}},
		{http.MethodDelete, base + "/pods/web/nginx-1", "", explorerCall{"operator", "DeletePod"}},
		{http.MethodGet, base + "/services", "", explorerCall{"read", "ListServices"}},
		{http.MethodGet, base + "/ingresses", "", explorerCall{"read", "ListIngresses"}},
		{http.MethodGet, base + "/configmaps", "", explorerCall{"read", "ListConfigMaps"}},
		{http.MethodGet, base + "/secrets", "", explorerCall{"operator", "ListSecrets"}},
		{http.MethodGet, base + "/persistentvolumeclaims", "", explorerCall{"read", "ListPersistentVolumeClaims"}},
		{http.MethodPost, base + "/apply", `{"manifest":"kind: ConfigMap","dryRun":true}`, explorerCall{"operator", "Apply"}},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+strings.TrimPrefix(tc.path, base), func(t *testing.T) {
			calls = nil
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
				t.Fatalf("status = %d, want success", resp.StatusCode)
			}
			if len(calls) != 1 || calls[0] != tc.wantCall {
				t.Errorf("cluster calls = %+v, want only %+v", calls, tc.wantCall)
			}
		})
	}
}
