package platformapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v3"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

// maxLogBytes bounds how much of a pod log snapshot the explorer reads. tailLines already
// caps line count at the API; this is a second guard so a single pathologically long line
// cannot stream unbounded memory into the response.
const maxLogBytes = 4 << 20 // 4 MiB

// KubernetesClient is a live read/write client against a deployed Kubernetes Platform's own
// API. It implements platformdomain.KubernetesClusterClient by speaking plain REST with the
// deployment's bearer token; it never persists and never pulls in client-go, matching the
// read-only KubernetesReader's rationale but adding the write verbs the explorer needs.
//
// It shares the reader's transport (keep-alives disabled, per-client TLS) and the shared
// error translation, so an unreachable cluster or a rejected credential surfaces as the same
// *platformdomain.ReaderError the delivery layer already maps.
type KubernetesClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

var _ platformdomain.KubernetesClusterClient = (*KubernetesClient)(nil)

// NewKubernetesClient builds an explorer client for a cluster API base URL and bearer token.
func NewKubernetesClient(rawURL, token string, timeout time.Duration, insecureSkipVerify bool) (*KubernetesClient, error) {
	baseURL, err := normalizeBaseURL(rawURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("Kubernetes API token is empty")
	}
	return &KubernetesClient{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Timeout: timeout, Transport: newReaderTransport(insecureSkipVerify)},
	}, nil
}

// do performs one authenticated request and returns the raw body. A non-2xx status or a
// transport failure is translated into a *platformdomain.ReaderError. It is the single choke
// point every verb funnels through so authentication and error mapping stay in one place.
func (c *KubernetesClient) do(ctx context.Context, method, path, contentType string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, translateError(&transportError{err: err}, "Kubernetes")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, translateError(&apiError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(raw))}, "Kubernetes")
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxLogBytes))
}

// getJSON GETs a path and decodes the JSON body.
func (c *KubernetesClient) getJSON(ctx context.Context, path string, out any) error {
	raw, err := c.do(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode kubernetes response: %w", err)
	}
	return nil
}

// exists reports whether an object path resolves, mapping 404 to a clean false so apply can
// tell "created" from "configured" without treating a normal absence as an error.
func (c *KubernetesClient) exists(ctx context.Context, path string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return false, fmt.Errorf("build kubernetes request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, translateError(&transportError{err: err}, "Kubernetes")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, translateError(&apiError{StatusCode: resp.StatusCode}, "Kubernetes")
	}
	return true, nil
}

// ---- shared JSON shapes ----

type objectMetaJSON struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	CreationTimestamp string            `json:"creationTimestamp"`
	Labels            map[string]string `json:"labels"`
	OwnerReferences   []struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"ownerReferences"`
}

// ---- Summary ----

type versionJSON struct {
	GitVersion string `json:"gitVersion"`
}

// Summary reads the cluster version and node/namespace counts in three light reads.
func (c *KubernetesClient) Summary(ctx context.Context) (*platformdomain.KubernetesClusterSummary, error) {
	var version versionJSON
	if err := c.getJSON(ctx, "/version", &version); err != nil {
		return nil, err
	}
	nodes, err := c.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	namespaces, err := c.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	ready := 0
	for _, node := range nodes {
		if node.Ready {
			ready++
		}
	}
	return &platformdomain.KubernetesClusterSummary{
		Version:        version.GitVersion,
		NodeCount:      len(nodes),
		ReadyNodeCount: ready,
		NamespaceCount: len(namespaces),
	}, nil
}

// ---- Nodes ----

type nodeItemJSON struct {
	Metadata objectMetaJSON `json:"metadata"`
	Spec     struct {
		Unschedulable bool `json:"unschedulable"`
	} `json:"spec"`
	Status struct {
		Addresses []struct {
			Type    string `json:"type"`
			Address string `json:"address"`
		} `json:"addresses"`
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
		NodeInfo struct {
			KubeletVersion string `json:"kubeletVersion"`
		} `json:"nodeInfo"`
	} `json:"status"`
}

func (item nodeItemJSON) toNode() platformdomain.KubernetesNode {
	node := platformdomain.KubernetesNode{
		Name:           item.Metadata.Name,
		Role:           "worker",
		Unschedulable:  item.Spec.Unschedulable,
		KubeletVersion: item.Status.NodeInfo.KubeletVersion,
	}
	if _, ok := item.Metadata.Labels[labelControlPlane]; ok {
		node.Role = "control-plane"
	} else if _, ok := item.Metadata.Labels[labelMaster]; ok {
		node.Role = "control-plane"
	}
	for _, condition := range item.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == "True" {
			node.Ready = true
		}
	}
	for _, address := range item.Status.Addresses {
		if address.Type == "InternalIP" && address.Address != "" {
			node.Addresses = append(node.Addresses, address.Address)
		}
	}
	return node
}

// ListNodes lists cluster nodes. ServerID correlation is added by the application layer.
func (c *KubernetesClient) ListNodes(ctx context.Context) ([]platformdomain.KubernetesNode, error) {
	var out struct {
		Items []nodeItemJSON `json:"items"`
	}
	if err := c.getJSON(ctx, "/api/v1/nodes", &out); err != nil {
		return nil, err
	}
	nodes := make([]platformdomain.KubernetesNode, 0, len(out.Items))
	for _, item := range out.Items {
		nodes = append(nodes, item.toNode())
	}
	return nodes, nil
}

// SetNodeSchedulable cordons or uncordons a node with a merge patch on spec.unschedulable.
func (c *KubernetesClient) SetNodeSchedulable(ctx context.Context, name string, schedulable bool) (*platformdomain.KubernetesNode, error) {
	patch := fmt.Sprintf(`{"spec":{"unschedulable":%t}}`, !schedulable)
	raw, err := c.do(ctx, http.MethodPatch, "/api/v1/nodes/"+url.PathEscape(name), "application/merge-patch+json", []byte(patch))
	if err != nil {
		return nil, err
	}
	var item nodeItemJSON
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("decode kubernetes node: %w", err)
	}
	node := item.toNode()
	return &node, nil
}

// ---- Namespaces ----

type namespaceItemJSON struct {
	Metadata objectMetaJSON `json:"metadata"`
	Status   struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

func (item namespaceItemJSON) toNamespace() platformdomain.KubernetesNamespace {
	return platformdomain.KubernetesNamespace{
		Name:   item.Metadata.Name,
		Phase:  item.Status.Phase,
		System: platformdomain.IsSystemNamespace(item.Metadata.Name),
	}
}

func (c *KubernetesClient) ListNamespaces(ctx context.Context) ([]platformdomain.KubernetesNamespace, error) {
	var out struct {
		Items []namespaceItemJSON `json:"items"`
	}
	if err := c.getJSON(ctx, "/api/v1/namespaces", &out); err != nil {
		return nil, err
	}
	namespaces := make([]platformdomain.KubernetesNamespace, 0, len(out.Items))
	for _, item := range out.Items {
		namespaces = append(namespaces, item.toNamespace())
	}
	return namespaces, nil
}

func (c *KubernetesClient) CreateNamespace(ctx context.Context, name string) (*platformdomain.KubernetesNamespace, error) {
	body := fmt.Sprintf(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":%q}}`, name)
	raw, err := c.do(ctx, http.MethodPost, "/api/v1/namespaces", "application/json", []byte(body))
	if err != nil {
		return nil, err
	}
	var item namespaceItemJSON
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("decode kubernetes namespace: %w", err)
	}
	ns := item.toNamespace()
	return &ns, nil
}

func (c *KubernetesClient) DeleteNamespace(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodDelete, "/api/v1/namespaces/"+url.PathEscape(name), "", nil)
	return err
}

// ---- Applications (workload aggregation) ----

// workloadItemJSON is the subset of a Deployment/DaemonSet/StatefulSet the explorer reads.
type workloadItemJSON struct {
	Metadata objectMetaJSON `json:"metadata"`
	Spec     struct {
		Replicas *int `json:"replicas"`
		Selector struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
		Template struct {
			Spec struct {
				Containers []struct {
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		Replicas               int `json:"replicas"`
		ReadyReplicas          int `json:"readyReplicas"`
		DesiredNumberScheduled int `json:"desiredNumberScheduled"`
		NumberReady            int `json:"numberReady"`
	} `json:"status"`
}

func (w workloadItemJSON) toApplication(kind string) platformdomain.KubernetesApplication {
	images := make([]string, 0, len(w.Spec.Template.Spec.Containers))
	for _, container := range w.Spec.Template.Spec.Containers {
		if container.Image != "" {
			images = append(images, container.Image)
		}
	}
	var replicas, ready int
	if kind == platformdomain.KubernetesKindDaemonSet {
		replicas = w.Status.DesiredNumberScheduled
		ready = w.Status.NumberReady
	} else {
		replicas = 1
		if w.Spec.Replicas != nil {
			replicas = *w.Spec.Replicas
		}
		ready = w.Status.ReadyReplicas
	}
	return platformdomain.KubernetesApplication{
		Namespace:     w.Metadata.Namespace,
		Name:          w.Metadata.Name,
		Kind:          kind,
		Images:        images,
		Replicas:      replicas,
		ReadyReplicas: ready,
		CreatedAt:     w.Metadata.CreationTimestamp,
	}
}

// workloadKindPlural maps an Application kind to its apps/v1 resource plural.
var workloadKindPlural = map[string]string{
	platformdomain.KubernetesKindDeployment:  "deployments",
	platformdomain.KubernetesKindDaemonSet:   "daemonsets",
	platformdomain.KubernetesKindStatefulSet: "statefulsets",
}

// workloadPath builds the apps/v1 collection or object path for a workload kind.
func workloadPath(kind, namespace, name string) (string, bool) {
	plural, ok := workloadKindPlural[kind]
	if !ok {
		return "", false
	}
	path := "/apis/apps/v1"
	if namespace != "" {
		path += "/namespaces/" + url.PathEscape(namespace)
	}
	path += "/" + plural
	if name != "" {
		path += "/" + url.PathEscape(name)
	}
	return path, true
}

func (c *KubernetesClient) listWorkloads(ctx context.Context, kind, namespace string) ([]platformdomain.KubernetesApplication, error) {
	path, _ := workloadPath(kind, namespace, "")
	var out struct {
		Items []workloadItemJSON `json:"items"`
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	apps := make([]platformdomain.KubernetesApplication, 0, len(out.Items))
	for _, item := range out.Items {
		apps = append(apps, item.toApplication(kind))
	}
	return apps, nil
}

// ListApplications aggregates workloads and owner-less pods (see the Kubernetes Application
// glossary term). System namespaces are excluded from the all-namespaces case unless asked.
func (c *KubernetesClient) ListApplications(ctx context.Context, namespace string, includeSystem bool) ([]platformdomain.KubernetesApplication, error) {
	apps := make([]platformdomain.KubernetesApplication, 0)
	for _, kind := range []string{
		platformdomain.KubernetesKindDeployment,
		platformdomain.KubernetesKindDaemonSet,
		platformdomain.KubernetesKindStatefulSet,
	} {
		workloads, err := c.listWorkloads(ctx, kind, namespace)
		if err != nil {
			return nil, err
		}
		apps = append(apps, workloads...)
	}

	// Bare pods (no controller) are their own Applications; controller-owned pods belong to
	// the workloads above and are not repeated here.
	pods, err := c.ListPods(ctx, namespace, includeSystem)
	if err != nil {
		return nil, err
	}
	for _, pod := range pods {
		if pod.OwnerKind != "" {
			continue
		}
		ready := 0
		if pod.Ready {
			ready = 1
		}
		apps = append(apps, platformdomain.KubernetesApplication{
			Namespace: pod.Namespace, Name: pod.Name, Kind: platformdomain.KubernetesKindPod,
			// The pod list carries container names, not images, to stay light; a bare-pod
			// Application shows those names so the row is not blank.
			Replicas: 1, ReadyReplicas: ready, Images: pod.Containers,
		})
	}

	filtered := apps[:0]
	for _, app := range apps {
		if namespace == "" && !includeSystem && platformdomain.IsSystemNamespace(app.Namespace) {
			continue
		}
		filtered = append(filtered, app)
	}
	sortApplications(filtered)
	return filtered, nil
}

func sortApplications(apps []platformdomain.KubernetesApplication) {
	sort.Slice(apps, func(i, j int) bool {
		if apps[i].Namespace != apps[j].Namespace {
			return apps[i].Namespace < apps[j].Namespace
		}
		if apps[i].Kind != apps[j].Kind {
			return apps[i].Kind < apps[j].Kind
		}
		return apps[i].Name < apps[j].Name
	})
}

// GetApplication reads one workload and the pods it owns (matched by its selector labels), or
// a single bare pod when kind is Pod.
func (c *KubernetesClient) GetApplication(ctx context.Context, namespace, kind, name string) (*platformdomain.KubernetesApplication, error) {
	if kind == platformdomain.KubernetesKindPod {
		pod, err := c.getPod(ctx, namespace, name)
		if err != nil {
			return nil, err
		}
		ready := 0
		if pod.Ready {
			ready = 1
		}
		return &platformdomain.KubernetesApplication{
			Namespace: namespace, Name: name, Kind: platformdomain.KubernetesKindPod,
			Replicas: 1, ReadyReplicas: ready, Images: pod.Containers,
			Pods: []platformdomain.KubernetesPod{*pod},
		}, nil
	}

	path, ok := workloadPath(kind, namespace, name)
	if !ok {
		return nil, fmt.Errorf("%w: unsupported application kind %q", platformdomain.ErrInvalidKubernetesRequest, kind)
	}
	var item workloadItemJSON
	if err := c.getJSON(ctx, path, &item); err != nil {
		return nil, err
	}
	app := item.toApplication(kind)

	pods, err := c.listPodsBySelector(ctx, namespace, item.Spec.Selector.MatchLabels)
	if err != nil {
		return nil, err
	}
	app.Pods = pods
	return &app, nil
}

// DeleteApplication deletes the workload object (or a bare pod).
func (c *KubernetesClient) DeleteApplication(ctx context.Context, namespace, kind, name string) error {
	if kind == platformdomain.KubernetesKindPod {
		return c.DeletePod(ctx, namespace, name)
	}
	path, ok := workloadPath(kind, namespace, name)
	if !ok {
		return fmt.Errorf("%w: unsupported application kind %q", platformdomain.ErrInvalidKubernetesRequest, kind)
	}
	_, err := c.do(ctx, http.MethodDelete, path, "", nil)
	return err
}

// ScaleApplication sets replicas via a merge patch; only Deployment and StatefulSet are
// scalable (a DaemonSet has no replica count, a bare Pod is a single object).
func (c *KubernetesClient) ScaleApplication(ctx context.Context, namespace, kind, name string, replicas int) (*platformdomain.KubernetesApplication, error) {
	if kind != platformdomain.KubernetesKindDeployment && kind != platformdomain.KubernetesKindStatefulSet {
		return nil, fmt.Errorf("%w: only Deployment and StatefulSet can be scaled", platformdomain.ErrInvalidKubernetesRequest)
	}
	if replicas < 0 {
		return nil, fmt.Errorf("%w: replicas must be zero or greater", platformdomain.ErrInvalidKubernetesRequest)
	}
	path, _ := workloadPath(kind, namespace, name)
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	raw, err := c.do(ctx, http.MethodPatch, path, "application/merge-patch+json", []byte(patch))
	if err != nil {
		return nil, err
	}
	var item workloadItemJSON
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("decode kubernetes workload: %w", err)
	}
	app := item.toApplication(kind)
	return &app, nil
}

// RestartApplication triggers a rolling restart by stamping the pod template with a fresh
// annotation, the same mechanism kubectl rollout restart uses.
func (c *KubernetesClient) RestartApplication(ctx context.Context, namespace, kind, name string) error {
	if _, ok := workloadKindPlural[kind]; !ok {
		return fmt.Errorf("%w: only Deployment, DaemonSet, and StatefulSet can be restarted", platformdomain.ErrInvalidKubernetesRequest)
	}
	path, _ := workloadPath(kind, namespace, name)
	stamp := time.Now().UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"swallow.maple52046.io/restartedAt":%q}}}}}`, stamp)
	_, err := c.do(ctx, http.MethodPatch, path, "application/merge-patch+json", []byte(patch))
	return err
}

// ---- Pods ----

type podItemJSON struct {
	Metadata objectMetaJSON `json:"metadata"`
	Spec     struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name string `json:"name"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		StartTime         string `json:"startTime"`
		ContainerStatuses []struct {
			Ready        bool `json:"ready"`
			RestartCount int  `json:"restartCount"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

func (item podItemJSON) toPod() platformdomain.KubernetesPod {
	pod := platformdomain.KubernetesPod{
		Namespace: item.Metadata.Namespace,
		Name:      item.Metadata.Name,
		Phase:     item.Status.Phase,
		NodeName:  item.Spec.NodeName,
		StartedAt: item.Status.StartTime,
	}
	for _, container := range item.Spec.Containers {
		pod.Containers = append(pod.Containers, container.Name)
	}
	// Ready means every container is ready; an empty status list is not ready yet.
	pod.Ready = len(item.Status.ContainerStatuses) > 0
	for _, status := range item.Status.ContainerStatuses {
		pod.Restarts += status.RestartCount
		if !status.Ready {
			pod.Ready = false
		}
	}
	for _, owner := range item.Metadata.OwnerReferences {
		// A pod is owned by a ReplicaSet (for a Deployment), a DaemonSet, a StatefulSet, or a
		// Job. Report the immediate controller; the aggregation treats any owner as "not bare".
		pod.OwnerKind = owner.Kind
		pod.OwnerName = owner.Name
		break
	}
	return pod
}

func podsPath(namespace string) string {
	if namespace == "" {
		return "/api/v1/pods"
	}
	return "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods"
}

func (c *KubernetesClient) ListPods(ctx context.Context, namespace string, includeSystem bool) ([]platformdomain.KubernetesPod, error) {
	var out struct {
		Items []podItemJSON `json:"items"`
	}
	if err := c.getJSON(ctx, podsPath(namespace), &out); err != nil {
		return nil, err
	}
	pods := make([]platformdomain.KubernetesPod, 0, len(out.Items))
	for _, item := range out.Items {
		if namespace == "" && !includeSystem && platformdomain.IsSystemNamespace(item.Metadata.Namespace) {
			continue
		}
		pods = append(pods, item.toPod())
	}
	return pods, nil
}

func (c *KubernetesClient) listPodsBySelector(ctx context.Context, namespace string, matchLabels map[string]string) ([]platformdomain.KubernetesPod, error) {
	path := podsPath(namespace)
	if len(matchLabels) > 0 {
		selectors := make([]string, 0, len(matchLabels))
		for key, value := range matchLabels {
			selectors = append(selectors, key+"="+value)
		}
		sort.Strings(selectors)
		path += "?labelSelector=" + url.QueryEscape(strings.Join(selectors, ","))
	}
	var out struct {
		Items []podItemJSON `json:"items"`
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	pods := make([]platformdomain.KubernetesPod, 0, len(out.Items))
	for _, item := range out.Items {
		pods = append(pods, item.toPod())
	}
	return pods, nil
}

func (c *KubernetesClient) getPod(ctx context.Context, namespace, name string) (*platformdomain.KubernetesPod, error) {
	var item podItemJSON
	if err := c.getJSON(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods/"+url.PathEscape(name), &item); err != nil {
		return nil, err
	}
	pod := item.toPod()
	return &pod, nil
}

// PodLogs returns a bounded log snapshot for one container. When container is empty the API
// returns the pod's default (first) container's logs.
func (c *KubernetesClient) PodLogs(ctx context.Context, namespace, name, container string, tailLines int) (string, string, error) {
	query := url.Values{}
	if tailLines > 0 {
		query.Set("tailLines", fmt.Sprintf("%d", tailLines))
	}
	if container != "" {
		query.Set("container", container)
	}
	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(name) + "/log"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	raw, err := c.do(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return "", "", err
	}
	return container, string(raw), nil
}

func (c *KubernetesClient) DeletePod(ctx context.Context, namespace, name string) error {
	_, err := c.do(ctx, http.MethodDelete, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods/"+url.PathEscape(name), "", nil)
	return err
}

// ---- Subsidiary resources ----

func collectionPath(apiPath, plural, namespace string) string {
	if namespace == "" {
		return apiPath + "/" + plural
	}
	return apiPath + "/namespaces/" + url.PathEscape(namespace) + "/" + plural
}

func (c *KubernetesClient) ListServices(ctx context.Context, namespace string, includeSystem bool) ([]platformdomain.KubernetesService, error) {
	var out struct {
		Items []struct {
			Metadata objectMetaJSON `json:"metadata"`
			Spec     struct {
				Type        string   `json:"type"`
				ClusterIP   string   `json:"clusterIP"`
				ExternalIPs []string `json:"externalIPs"`
				Ports       []struct {
					Port     int    `json:"port"`
					Protocol string `json:"protocol"`
				} `json:"ports"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := c.getJSON(ctx, collectionPath("/api/v1", "services", namespace), &out); err != nil {
		return nil, err
	}
	services := make([]platformdomain.KubernetesService, 0, len(out.Items))
	for _, item := range out.Items {
		if skipSystem(namespace, includeSystem, item.Metadata.Namespace) {
			continue
		}
		ports := make([]string, 0, len(item.Spec.Ports))
		for _, port := range item.Spec.Ports {
			protocol := port.Protocol
			if protocol == "" {
				protocol = "TCP"
			}
			ports = append(ports, fmt.Sprintf("%d/%s", port.Port, protocol))
		}
		services = append(services, platformdomain.KubernetesService{
			Namespace: item.Metadata.Namespace, Name: item.Metadata.Name,
			Type: item.Spec.Type, ClusterIP: item.Spec.ClusterIP,
			Ports: ports, ExternalIPs: item.Spec.ExternalIPs,
		})
	}
	return services, nil
}

func (c *KubernetesClient) ListIngresses(ctx context.Context, namespace string, includeSystem bool) ([]platformdomain.KubernetesIngress, error) {
	var out struct {
		Items []struct {
			Metadata objectMetaJSON `json:"metadata"`
			Spec     struct {
				IngressClassName string `json:"ingressClassName"`
				Rules            []struct {
					Host string `json:"host"`
				} `json:"rules"`
			} `json:"spec"`
			Status struct {
				LoadBalancer struct {
					Ingress []struct {
						IP       string `json:"ip"`
						Hostname string `json:"hostname"`
					} `json:"ingress"`
				} `json:"loadBalancer"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := c.getJSON(ctx, collectionPath("/apis/networking.k8s.io/v1", "ingresses", namespace), &out); err != nil {
		return nil, err
	}
	ingresses := make([]platformdomain.KubernetesIngress, 0, len(out.Items))
	for _, item := range out.Items {
		if skipSystem(namespace, includeSystem, item.Metadata.Namespace) {
			continue
		}
		hosts := make([]string, 0, len(item.Spec.Rules))
		for _, rule := range item.Spec.Rules {
			if rule.Host != "" {
				hosts = append(hosts, rule.Host)
			}
		}
		addresses := make([]string, 0)
		for _, lb := range item.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				addresses = append(addresses, lb.IP)
			} else if lb.Hostname != "" {
				addresses = append(addresses, lb.Hostname)
			}
		}
		ingresses = append(ingresses, platformdomain.KubernetesIngress{
			Namespace: item.Metadata.Namespace, Name: item.Metadata.Name,
			Hosts: hosts, IngressClass: item.Spec.IngressClassName, Addresses: addresses,
		})
	}
	return ingresses, nil
}

// configResourceItemJSON reads a ConfigMap or Secret. Only the data key names are read, never
// the values: a Secret's values must not leave the cluster through this surface.
type configResourceItemJSON struct {
	Metadata objectMetaJSON    `json:"metadata"`
	Type     string            `json:"type"`
	Data     map[string]any    `json:"data"`
	String   map[string]string `json:"stringData"`
}

func (item configResourceItemJSON) toConfigResource() platformdomain.KubernetesConfigResource {
	keys := make([]string, 0, len(item.Data)+len(item.String))
	for key := range item.Data {
		keys = append(keys, key)
	}
	for key := range item.String {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return platformdomain.KubernetesConfigResource{
		Namespace: item.Metadata.Namespace, Name: item.Metadata.Name,
		Type: item.Type, Keys: keys, DataCount: len(keys),
	}
}

func (c *KubernetesClient) ListConfigMaps(ctx context.Context, namespace string, includeSystem bool) ([]platformdomain.KubernetesConfigResource, error) {
	return c.listConfigResources(ctx, "configmaps", namespace, includeSystem)
}

func (c *KubernetesClient) ListSecrets(ctx context.Context, namespace string, includeSystem bool) ([]platformdomain.KubernetesConfigResource, error) {
	return c.listConfigResources(ctx, "secrets", namespace, includeSystem)
}

func (c *KubernetesClient) listConfigResources(ctx context.Context, plural, namespace string, includeSystem bool) ([]platformdomain.KubernetesConfigResource, error) {
	var out struct {
		Items []configResourceItemJSON `json:"items"`
	}
	if err := c.getJSON(ctx, collectionPath("/api/v1", plural, namespace), &out); err != nil {
		return nil, err
	}
	resources := make([]platformdomain.KubernetesConfigResource, 0, len(out.Items))
	for _, item := range out.Items {
		if skipSystem(namespace, includeSystem, item.Metadata.Namespace) {
			continue
		}
		resources = append(resources, item.toConfigResource())
	}
	return resources, nil
}

func (c *KubernetesClient) ListPersistentVolumeClaims(ctx context.Context, namespace string, includeSystem bool) ([]platformdomain.KubernetesPersistentVolumeClaim, error) {
	var out struct {
		Items []struct {
			Metadata objectMetaJSON `json:"metadata"`
			Spec     struct {
				StorageClassName *string  `json:"storageClassName"`
				AccessModes      []string `json:"accessModes"`
			} `json:"spec"`
			Status struct {
				Phase    string            `json:"phase"`
				Capacity map[string]string `json:"capacity"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := c.getJSON(ctx, collectionPath("/api/v1", "persistentvolumeclaims", namespace), &out); err != nil {
		return nil, err
	}
	claims := make([]platformdomain.KubernetesPersistentVolumeClaim, 0, len(out.Items))
	for _, item := range out.Items {
		if skipSystem(namespace, includeSystem, item.Metadata.Namespace) {
			continue
		}
		storageClass := ""
		if item.Spec.StorageClassName != nil {
			storageClass = *item.Spec.StorageClassName
		}
		claims = append(claims, platformdomain.KubernetesPersistentVolumeClaim{
			Namespace: item.Metadata.Namespace, Name: item.Metadata.Name,
			Phase: item.Status.Phase, Capacity: item.Status.Capacity["storage"],
			StorageClass: storageClass, AccessModes: item.Spec.AccessModes,
		})
	}
	return claims, nil
}

// skipSystem reports whether a cluster-wide list item in a system namespace should be dropped.
// It only applies to the all-namespaces case; an explicit namespace is always shown.
func skipSystem(requestedNamespace string, includeSystem bool, itemNamespace string) bool {
	return requestedNamespace == "" && !includeSystem && platformdomain.IsSystemNamespace(itemNamespace)
}

// ---- Apply ----

// applyKindMeta maps a kind to its resource plural and namespacing for building the apply
// path. The apiVersion comes from the manifest itself, so only the kind-specific facts are
// stored here; unsupported kinds are rejected rather than guessed.
type applyKindMeta struct {
	plural     string
	namespaced bool
}

var applyKinds = map[string]applyKindMeta{
	"Deployment":              {"deployments", true},
	"DaemonSet":               {"daemonsets", true},
	"StatefulSet":             {"statefulsets", true},
	"ReplicaSet":              {"replicasets", true},
	"Pod":                     {"pods", true},
	"Service":                 {"services", true},
	"ConfigMap":               {"configmaps", true},
	"Secret":                  {"secrets", true},
	"PersistentVolumeClaim":   {"persistentvolumeclaims", true},
	"ServiceAccount":          {"serviceaccounts", true},
	"Namespace":               {"namespaces", false},
	"Ingress":                 {"ingresses", true},
	"IngressClass":            {"ingressclasses", false},
	"Job":                     {"jobs", true},
	"CronJob":                 {"cronjobs", true},
	"Role":                    {"roles", true},
	"RoleBinding":             {"rolebindings", true},
	"ClusterRole":             {"clusterroles", false},
	"ClusterRoleBinding":      {"clusterrolebindings", false},
	"HorizontalPodAutoscaler": {"horizontalpodautoscalers", true},
	"PersistentVolume":        {"persistentvolumes", false},
	"StorageClass":            {"storageclasses", false},
}

// Apply server-side applies each YAML document in a manifest. dryRun validates without
// persisting. Objects are applied in document order; the first rejected document aborts and
// nothing after it is applied.
func (c *KubernetesClient) Apply(ctx context.Context, manifest string, dryRun bool) ([]platformdomain.KubernetesApplyResult, error) {
	decoder := yaml.NewDecoder(strings.NewReader(manifest))
	results := make([]platformdomain.KubernetesApplyResult, 0)
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: manifest is not valid YAML: %v", platformdomain.ErrInvalidKubernetesRequest, err)
		}
		if len(doc) == 0 {
			continue
		}
		result, err := c.applyDocument(ctx, doc, dryRun)
		if err != nil {
			return nil, err
		}
		results = append(results, *result)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("%w: manifest contained no objects", platformdomain.ErrInvalidKubernetesRequest)
	}
	return results, nil
}

func (c *KubernetesClient) applyDocument(ctx context.Context, doc map[string]any, dryRun bool) (*platformdomain.KubernetesApplyResult, error) {
	apiVersion, _ := doc["apiVersion"].(string)
	kind, _ := doc["kind"].(string)
	meta, _ := doc["metadata"].(map[string]any)
	name, _ := meta["name"].(string)
	namespace, _ := meta["namespace"].(string)
	if apiVersion == "" || kind == "" || name == "" {
		return nil, fmt.Errorf("%w: each manifest document needs apiVersion, kind, and metadata.name", platformdomain.ErrInvalidKubernetesRequest)
	}
	info, ok := applyKinds[kind]
	if !ok {
		return nil, fmt.Errorf("%w: unsupported kind %q", platformdomain.ErrInvalidKubernetesRequest, kind)
	}

	apiPath := "/apis/" + apiVersion
	if !strings.Contains(apiVersion, "/") {
		apiPath = "/api/" + apiVersion
	}
	var path string
	if info.namespaced {
		ns := namespace
		if ns == "" {
			ns = "default"
		}
		path = fmt.Sprintf("%s/namespaces/%s/%s/%s", apiPath, url.PathEscape(ns), info.plural, url.PathEscape(name))
	} else {
		path = fmt.Sprintf("%s/%s/%s", apiPath, info.plural, url.PathEscape(name))
	}

	action := "configured"
	if dryRun {
		action = "validated"
	} else {
		present, err := c.exists(ctx, path)
		if err != nil {
			return nil, err
		}
		if !present {
			action = "created"
		}
	}

	query := "?fieldManager=swallow&force=true"
	if dryRun {
		query += "&dryRun=All"
	}
	body, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encoding manifest failed: %v", platformdomain.ErrInvalidKubernetesRequest, err)
	}
	if _, err := c.do(ctx, http.MethodPatch, path+query, "application/apply-patch+yaml", body); err != nil {
		return nil, err
	}
	return &platformdomain.KubernetesApplyResult{Kind: kind, Namespace: namespace, Name: name, Action: action}, nil
}
