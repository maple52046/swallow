package delivery

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// This file holds the Kubernetes cluster explorer HTTP handlers: the live read/write surface
// for a Swallow-deployed Kubernetes Platform, contracted in
// docs/development/api-contracts/api-server/platforms-kubernetes.md. Each handler resolves the
// explorer client through the use case (which enforces eligibility) and maps the cluster's
// live response into the contract's wire shapes. Nothing here is persisted.

const (
	defaultPodLogTailLines = 200
	maxPodLogTailLines     = 2000
)

// explorerClient resolves the eligible cluster client for the path's platform, mapping every
// eligibility failure through the shared error responder. Returns nil when it already wrote an
// error response, so callers return immediately on a nil client.
func (h *PlatformHandler) explorerClient(c *fiber.Ctx) platformdomain.KubernetesClusterClient {
	if h.kubernetes == nil {
		_ = apierror.Respond(c, apierror.New(apierror.CodeNotFound, "The Kubernetes cluster explorer is unavailable."))
		return nil
	}
	client, err := h.kubernetes.Client(c.Context(), c.Params("id"))
	if err != nil {
		_ = respondError(c, err)
		return nil
	}
	return client
}

// includeSystem reads the includeSystem query flag; system namespaces are hidden by default.
func includeSystem(c *fiber.Ctx) bool {
	return c.Query("includeSystem") == "true"
}

// ---- response DTOs (wire shapes per the contract) ----

type kubernetesSummaryResponse struct {
	Version        string `json:"version"`
	NodeCount      int    `json:"nodeCount"`
	ReadyNodeCount int    `json:"readyNodeCount"`
	NamespaceCount int    `json:"namespaceCount"`
}

type kubernetesNodeResponse struct {
	Name           string   `json:"name"`
	Role           string   `json:"role"`
	Ready          bool     `json:"ready"`
	Unschedulable  bool     `json:"unschedulable"`
	ServerID       *string  `json:"serverId"`
	Addresses      []string `json:"addresses"`
	KubeletVersion string   `json:"kubeletVersion"`
}

func nodeResponse(node platformdomain.KubernetesNode) kubernetesNodeResponse {
	var serverID *string
	if node.ServerID != "" {
		id := node.ServerID
		serverID = &id
	}
	addresses := node.Addresses
	if addresses == nil {
		addresses = []string{}
	}
	return kubernetesNodeResponse{
		Name: node.Name, Role: node.Role, Ready: node.Ready, Unschedulable: node.Unschedulable,
		ServerID: serverID, Addresses: addresses, KubeletVersion: node.KubeletVersion,
	}
}

type kubernetesNamespaceResponse struct {
	Name   string `json:"name"`
	Phase  string `json:"phase"`
	System bool   `json:"system"`
}

func namespaceResponse(namespace platformdomain.KubernetesNamespace) kubernetesNamespaceResponse {
	return kubernetesNamespaceResponse{Name: namespace.Name, Phase: namespace.Phase, System: namespace.System}
}

type kubernetesPodResponse struct {
	Namespace  string   `json:"namespace"`
	Name       string   `json:"name"`
	Phase      string   `json:"phase"`
	Ready      bool     `json:"ready"`
	NodeName   string   `json:"nodeName"`
	Restarts   int      `json:"restarts"`
	Containers []string `json:"containers"`
	StartedAt  string   `json:"startedAt"`
}

func podResponse(pod platformdomain.KubernetesPod) kubernetesPodResponse {
	containers := pod.Containers
	if containers == nil {
		containers = []string{}
	}
	return kubernetesPodResponse{
		Namespace: pod.Namespace, Name: pod.Name, Phase: pod.Phase, Ready: pod.Ready,
		NodeName: pod.NodeName, Restarts: pod.Restarts, Containers: containers, StartedAt: pod.StartedAt,
	}
}

type kubernetesApplicationResponse struct {
	Namespace     string                  `json:"namespace"`
	Name          string                  `json:"name"`
	Kind          string                  `json:"kind"`
	Images        []string                `json:"images"`
	Replicas      int                     `json:"replicas"`
	ReadyReplicas int                     `json:"readyReplicas"`
	CreatedAt     string                  `json:"createdAt"`
	Pods          []kubernetesPodResponse `json:"pods,omitempty"`
}

func applicationResponse(app platformdomain.KubernetesApplication, withPods bool) kubernetesApplicationResponse {
	images := app.Images
	if images == nil {
		images = []string{}
	}
	response := kubernetesApplicationResponse{
		Namespace: app.Namespace, Name: app.Name, Kind: app.Kind, Images: images,
		Replicas: app.Replicas, ReadyReplicas: app.ReadyReplicas, CreatedAt: app.CreatedAt,
	}
	if withPods {
		pods := make([]kubernetesPodResponse, 0, len(app.Pods))
		for _, pod := range app.Pods {
			pods = append(pods, podResponse(pod))
		}
		response.Pods = pods
	}
	return response
}

// ---- Summary ----

// GetKubernetesCluster returns the live cluster summary for a deployed Kubernetes Platform.
func (h *PlatformHandler) GetKubernetesCluster(c *fiber.Ctx) error {
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	summary, err := client.Summary(c.Context())
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(kubernetesSummaryResponse{
		Version: summary.Version, NodeCount: summary.NodeCount,
		ReadyNodeCount: summary.ReadyNodeCount, NamespaceCount: summary.NamespaceCount,
	})
}

// ---- Nodes ----

// ListKubernetesNodes lists nodes with Server correlation via the use case.
func (h *PlatformHandler) ListKubernetesNodes(c *fiber.Ctx) error {
	if h.kubernetes == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "The Kubernetes cluster explorer is unavailable."))
	}
	nodes, err := h.kubernetes.Nodes(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	items := make([]kubernetesNodeResponse, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, nodeResponse(node))
	}
	return c.JSON(fiber.Map{"items": items})
}

// CordonKubernetesNode marks a node unschedulable.
func (h *PlatformHandler) CordonKubernetesNode(c *fiber.Ctx) error {
	return h.setNodeSchedulable(c, false)
}

// UncordonKubernetesNode clears a node's unschedulable flag.
func (h *PlatformHandler) UncordonKubernetesNode(c *fiber.Ctx) error {
	return h.setNodeSchedulable(c, true)
}

func (h *PlatformHandler) setNodeSchedulable(c *fiber.Ctx, schedulable bool) error {
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	node, err := client.SetNodeSchedulable(c.Context(), c.Params("node"), schedulable)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(nodeResponse(*node))
}

// ---- Namespaces ----

// ListKubernetesNamespaces returns the cluster's namespaces; system namespaces are flagged so a
// client can hide them by default.
func (h *PlatformHandler) ListKubernetesNamespaces(c *fiber.Ctx) error {
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	namespaces, err := client.ListNamespaces(c.Context())
	if err != nil {
		return respondError(c, err)
	}
	items := make([]kubernetesNamespaceResponse, 0, len(namespaces))
	for _, namespace := range namespaces {
		items = append(items, namespaceResponse(namespace))
	}
	return c.JSON(fiber.Map{"items": items})
}

type createNamespaceRequest struct {
	Name string `json:"name"`
}

// CreateKubernetesNamespace creates a namespace from a required name and returns it (201).
func (h *PlatformHandler) CreateKubernetesNamespace(c *fiber.Ctx) error {
	var req createNamespaceRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if strings.TrimSpace(req.Name) == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "name is required."))
	}
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	namespace, err := client.CreateNamespace(c.Context(), strings.TrimSpace(req.Name))
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(namespaceResponse(*namespace))
}

// DeleteKubernetesNamespace deletes a namespace. A system namespace (kube-system, kube-public,
// kube-node-lease) is refused before any cluster call, so the guard cannot be bypassed.
func (h *PlatformHandler) DeleteKubernetesNamespace(c *fiber.Ctx) error {
	name := c.Params("namespace")
	if platformdomain.IsSystemNamespace(name) {
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "A system namespace cannot be deleted."))
	}
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	if err := client.DeleteNamespace(c.Context(), name); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

// ---- Applications ----

// applicationKind validates the {kind} path segment against the supported Application kinds.
func applicationKind(c *fiber.Ctx) (string, bool) {
	switch c.Params("kind") {
	case platformdomain.KubernetesKindDeployment:
		return platformdomain.KubernetesKindDeployment, true
	case platformdomain.KubernetesKindDaemonSet:
		return platformdomain.KubernetesKindDaemonSet, true
	case platformdomain.KubernetesKindStatefulSet:
		return platformdomain.KubernetesKindStatefulSet, true
	case platformdomain.KubernetesKindPod:
		return platformdomain.KubernetesKindPod, true
	default:
		return "", false
	}
}

// ListKubernetesApplications returns the aggregated workloads in an optional namespace scope.
func (h *PlatformHandler) ListKubernetesApplications(c *fiber.Ctx) error {
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	apps, err := client.ListApplications(c.Context(), c.Query("namespace"), includeSystem(c))
	if err != nil {
		return respondError(c, err)
	}
	items := make([]kubernetesApplicationResponse, 0, len(apps))
	for _, app := range apps {
		items = append(items, applicationResponse(app, false))
	}
	return c.JSON(fiber.Map{"items": items})
}

// GetKubernetesApplication returns one Application with the pods it owns; the kind path segment
// must be a supported Application kind.
func (h *PlatformHandler) GetKubernetesApplication(c *fiber.Ctx) error {
	kind, ok := applicationKind(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Unsupported application kind."))
	}
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	app, err := client.GetApplication(c.Context(), c.Params("namespace"), kind, c.Params("name"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(applicationResponse(*app, true))
}

// DeleteKubernetesApplication deletes the workload object (or a bare pod) behind an Application.
func (h *PlatformHandler) DeleteKubernetesApplication(c *fiber.Ctx) error {
	kind, ok := applicationKind(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Unsupported application kind."))
	}
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	if err := client.DeleteApplication(c.Context(), c.Params("namespace"), kind, c.Params("name")); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

type scaleApplicationRequest struct {
	Replicas *int `json:"replicas"`
}

// ScaleKubernetesApplication sets an Application's replica count. replicas is required; only
// Deployment and StatefulSet are scalable (the client rejects other kinds).
func (h *PlatformHandler) ScaleKubernetesApplication(c *fiber.Ctx) error {
	kind, ok := applicationKind(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Unsupported application kind."))
	}
	var req scaleApplicationRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.Replicas == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "replicas is required."))
	}
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	app, err := client.ScaleApplication(c.Context(), c.Params("namespace"), kind, c.Params("name"), *req.Replicas)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(applicationResponse(*app, false))
}

// RestartKubernetesApplication triggers a rolling restart of a Deployment, DaemonSet, or
// StatefulSet; a bare Pod is rejected (delete it instead).
func (h *PlatformHandler) RestartKubernetesApplication(c *fiber.Ctx) error {
	kind, ok := applicationKind(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Unsupported application kind."))
	}
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	if err := client.RestartApplication(c.Context(), c.Params("namespace"), kind, c.Params("name")); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

// ---- Pods ----

// ListKubernetesPods returns pods in an optional namespace scope.
func (h *PlatformHandler) ListKubernetesPods(c *fiber.Ctx) error {
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	pods, err := client.ListPods(c.Context(), c.Query("namespace"), includeSystem(c))
	if err != nil {
		return respondError(c, err)
	}
	items := make([]kubernetesPodResponse, 0, len(pods))
	for _, pod := range pods {
		items = append(items, podResponse(pod))
	}
	return c.JSON(fiber.Map{"items": items})
}

// GetKubernetesPodLogs returns a bounded log snapshot (not a stream). tailLines defaults to 200
// and is capped at 2000; an invalid value is a validation error.
func (h *PlatformHandler) GetKubernetesPodLogs(c *fiber.Ctx) error {
	tailLines := defaultPodLogTailLines
	if raw := c.Query("tailLines"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "tailLines must be a positive integer."))
		}
		tailLines = parsed
	}
	if tailLines > maxPodLogTailLines {
		tailLines = maxPodLogTailLines
	}
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	container, logs, err := client.PodLogs(c.Context(), c.Params("namespace"), c.Params("name"), c.Query("container"), tailLines)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"container": container, "logs": logs})
}

// DeleteKubernetesPod deletes a pod; a controller recreates it.
func (h *PlatformHandler) DeleteKubernetesPod(c *fiber.Ctx) error {
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	if err := client.DeletePod(c.Context(), c.Params("namespace"), c.Params("name")); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

// ---- Subsidiary resources ----

// ListKubernetesServices returns the read-only Service list in an optional namespace scope.
func (h *PlatformHandler) ListKubernetesServices(c *fiber.Ctx) error {
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	services, err := client.ListServices(c.Context(), c.Query("namespace"), includeSystem(c))
	if err != nil {
		return respondError(c, err)
	}
	items := make([]fiber.Map, 0, len(services))
	for _, service := range services {
		items = append(items, fiber.Map{
			"namespace": service.Namespace, "name": service.Name, "type": service.Type,
			"clusterIP": service.ClusterIP, "ports": nonNilStrings(service.Ports),
			"externalIPs": nonNilStrings(service.ExternalIPs),
		})
	}
	return c.JSON(fiber.Map{"items": items})
}

// ListKubernetesIngresses returns the read-only Ingress list in an optional namespace scope.
func (h *PlatformHandler) ListKubernetesIngresses(c *fiber.Ctx) error {
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	ingresses, err := client.ListIngresses(c.Context(), c.Query("namespace"), includeSystem(c))
	if err != nil {
		return respondError(c, err)
	}
	items := make([]fiber.Map, 0, len(ingresses))
	for _, ingress := range ingresses {
		items = append(items, fiber.Map{
			"namespace": ingress.Namespace, "name": ingress.Name,
			"hosts": nonNilStrings(ingress.Hosts), "ingressClass": ingress.IngressClass,
			"addresses": nonNilStrings(ingress.Addresses),
		})
	}
	return c.JSON(fiber.Map{"items": items})
}

// ListKubernetesConfigMaps returns the read-only ConfigMap list (data key names only).
func (h *PlatformHandler) ListKubernetesConfigMaps(c *fiber.Ctx) error {
	h.listConfigResources(c, func(client platformdomain.KubernetesClusterClient, namespace string, system bool) ([]platformdomain.KubernetesConfigResource, error) {
		return client.ListConfigMaps(c.Context(), namespace, system)
	})
	return nil
}

// ListKubernetesSecrets returns the read-only Secret list. Only data key names are returned;
// Secret values are never exposed by this surface.
func (h *PlatformHandler) ListKubernetesSecrets(c *fiber.Ctx) error {
	h.listConfigResources(c, func(client platformdomain.KubernetesClusterClient, namespace string, system bool) ([]platformdomain.KubernetesConfigResource, error) {
		return client.ListSecrets(c.Context(), namespace, system)
	})
	return nil
}

// listConfigResources shares the ConfigMap and Secret list shape. Secret values are never
// included by the client, only key names, so this response cannot leak a secret.
func (h *PlatformHandler) listConfigResources(
	c *fiber.Ctx,
	read func(client platformdomain.KubernetesClusterClient, namespace string, system bool) ([]platformdomain.KubernetesConfigResource, error),
) {
	client := h.explorerClient(c)
	if client == nil {
		return
	}
	resources, err := read(client, c.Query("namespace"), includeSystem(c))
	if err != nil {
		_ = respondError(c, err)
		return
	}
	items := make([]fiber.Map, 0, len(resources))
	for _, resource := range resources {
		items = append(items, fiber.Map{
			"namespace": resource.Namespace, "name": resource.Name, "type": resource.Type,
			"keys": nonNilStrings(resource.Keys), "dataCount": resource.DataCount,
		})
	}
	_ = c.JSON(fiber.Map{"items": items})
}

// ListKubernetesPersistentVolumeClaims returns the read-only PVC list in an optional namespace scope.
func (h *PlatformHandler) ListKubernetesPersistentVolumeClaims(c *fiber.Ctx) error {
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	claims, err := client.ListPersistentVolumeClaims(c.Context(), c.Query("namespace"), includeSystem(c))
	if err != nil {
		return respondError(c, err)
	}
	items := make([]fiber.Map, 0, len(claims))
	for _, claim := range claims {
		items = append(items, fiber.Map{
			"namespace": claim.Namespace, "name": claim.Name, "phase": claim.Phase,
			"capacity": claim.Capacity, "storageClass": claim.StorageClass,
			"accessModes": nonNilStrings(claim.AccessModes),
		})
	}
	return c.JSON(fiber.Map{"items": items})
}

// ---- Apply ----

type applyManifestRequest struct {
	Manifest string `json:"manifest"`
	DryRun   bool   `json:"dryRun"`
}

// ApplyKubernetesManifest server-side applies a YAML manifest (optionally dry-run) and returns
// the per-object results.
func (h *PlatformHandler) ApplyKubernetesManifest(c *fiber.Ctx) error {
	var req applyManifestRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if strings.TrimSpace(req.Manifest) == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "manifest is required."))
	}
	client := h.explorerClient(c)
	if client == nil {
		return nil
	}
	results, err := client.Apply(c.Context(), req.Manifest, req.DryRun)
	if err != nil {
		return respondError(c, err)
	}
	items := make([]fiber.Map, 0, len(results))
	for _, result := range results {
		items = append(items, fiber.Map{
			"kind": result.Kind, "namespace": result.Namespace,
			"name": result.Name, "action": result.Action,
		})
	}
	return c.JSON(fiber.Map{"results": items})
}

// nonNilStrings returns an empty slice instead of nil so a JSON array field is never null.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
