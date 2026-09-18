package domain

import (
	"context"
	"errors"
)

// This file defines the live Kubernetes cluster explorer: read/write view types and the
// client port for managing a Swallow-deployed Kubernetes Platform's own resources on demand.
//
// Nothing here is persisted. Every value is read from, or written to, the deployed cluster's
// Kubernetes API at request time (ADR 001: workloads and node state are owned by Kubernetes,
// not mirrored into Swallow). The explorer is separate from membership sync: it never writes
// the Server membership axis, never touches the sync counters, and never runs on the reconcile
// interval (ADR 021 boundary, reused for Kubernetes). See
// docs/decisions/032-self-deployed-platform-management.md.

// Kubernetes Application kinds the explorer aggregates. A workload controller is one
// Application; a Pod with no controller owner is its own bare-Pod Application. See the
// Kubernetes Application glossary term.
const (
	KubernetesKindDeployment  = "Deployment"
	KubernetesKindDaemonSet   = "DaemonSet"
	KubernetesKindStatefulSet = "StatefulSet"
	KubernetesKindPod         = "Pod"
)

// systemNamespaces are the cluster-owned namespaces a client hides by default. They are the
// namespaces k0s and Kubernetes create, not operator workloads.
var systemNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
}

// IsSystemNamespace reports whether a namespace is one of the cluster-owned system namespaces
// the explorer hides unless a caller asks to include them. Deleting one is refused.
func IsSystemNamespace(name string) bool {
	return systemNamespaces[name]
}

// KubernetesClusterSummary is the small live header of a deployed Kubernetes Platform.
type KubernetesClusterSummary struct {
	// Version is the cluster's reported Kubernetes/kubelet version, empty when unreported.
	Version        string
	NodeCount      int
	ReadyNodeCount int
	NamespaceCount int
}

// KubernetesNode is one node in the explorer's node list, correlated to Server identity by the
// application layer. It carries more than the membership Member (unschedulable, kubelet
// version) because it powers cordon/uncordon, not just membership.
type KubernetesNode struct {
	Name string
	// Role is "control-plane" or "worker".
	Role  string
	Ready bool
	// Unschedulable is the cordon state: true means the scheduler places no new pods on it.
	Unschedulable  bool
	Addresses      []string
	KubeletVersion string
	// ServerID is Swallow's matched Server, filled by the application layer through identity
	// correlation; empty when no Server projection matches the node. The client never sets it.
	ServerID string
}

// KubernetesNamespace is one namespace in the explorer.
type KubernetesNamespace struct {
	Name  string
	Phase string
	// System marks a cluster-owned namespace a client hides by default.
	System bool
}

// KubernetesApplication is a live workload aggregation (see the Kubernetes Application
// glossary term). Pods is populated only by the detail read, not the list.
type KubernetesApplication struct {
	Namespace string
	Name      string
	// Kind is one of the KubernetesKind* constants.
	Kind          string
	Images        []string
	Replicas      int
	ReadyReplicas int
	// CreatedAt is the object's creation timestamp as the cluster reported it (RFC3339).
	CreatedAt string
	Pods      []KubernetesPod
}

// KubernetesPod is one pod, in a pod list or an Application's detail.
type KubernetesPod struct {
	Namespace  string
	Name       string
	Phase      string
	Ready      bool
	NodeName   string
	Restarts   int
	Containers []string
	StartedAt  string
	// OwnerKind/OwnerName reference the controlling workload for list grouping; empty for a
	// bare Pod.
	OwnerKind string
	OwnerName string
}

// KubernetesService is one Service in the explorer's read-only list.
type KubernetesService struct {
	Namespace   string
	Name        string
	Type        string
	ClusterIP   string
	Ports       []string
	ExternalIPs []string
}

// KubernetesIngress is one Ingress in the explorer's read-only list.
type KubernetesIngress struct {
	Namespace    string
	Name         string
	Hosts        []string
	IngressClass string
	Addresses    []string
}

// KubernetesConfigResource is one ConfigMap or Secret in the explorer's read-only list. Secret
// values are never included — only the data key names — so a credential surface cannot leak.
type KubernetesConfigResource struct {
	Namespace string
	Name      string
	// Type is empty for a ConfigMap and the Secret type for a Secret.
	Type      string
	Keys      []string
	DataCount int
}

// KubernetesPersistentVolumeClaim is one PVC in the explorer's read-only list.
type KubernetesPersistentVolumeClaim struct {
	Namespace    string
	Name         string
	Phase        string
	Capacity     string
	StorageClass string
	AccessModes  []string
}

// KubernetesApplyResult is one object's outcome from a manifest apply.
type KubernetesApplyResult struct {
	Kind      string
	Namespace string
	Name      string
	// Action is "created", "configured", "unchanged", or "validated" (dry run).
	Action string
}

// KubernetesClusterClient is a live read/write client against a deployed Kubernetes Platform's
// own API.
//
// It is read/write on purpose, unlike PlatformReader: the explorer manages in-cluster
// resources. Every method calls the cluster API at request time and persists nothing in
// Swallow. Implementations translate transport and API failures into a *ReaderError so the
// delivery layer maps them to a stable status.
type KubernetesClusterClient interface {
	Summary(ctx context.Context) (*KubernetesClusterSummary, error)

	ListNodes(ctx context.Context) ([]KubernetesNode, error)
	// SetNodeSchedulable cordons (schedulable=false) or uncordons (true) a node and returns the
	// updated node.
	SetNodeSchedulable(ctx context.Context, name string, schedulable bool) (*KubernetesNode, error)

	ListNamespaces(ctx context.Context) ([]KubernetesNamespace, error)
	CreateNamespace(ctx context.Context, name string) (*KubernetesNamespace, error)
	DeleteNamespace(ctx context.Context, name string) error

	// ListApplications aggregates workloads in namespace, or across all namespaces when
	// namespace is empty. includeSystem includes system namespaces in the all-namespaces case.
	ListApplications(ctx context.Context, namespace string, includeSystem bool) ([]KubernetesApplication, error)
	GetApplication(ctx context.Context, namespace, kind, name string) (*KubernetesApplication, error)
	DeleteApplication(ctx context.Context, namespace, kind, name string) error
	// ScaleApplication sets replicas; valid only for Deployment and StatefulSet.
	ScaleApplication(ctx context.Context, namespace, kind, name string, replicas int) (*KubernetesApplication, error)
	// RestartApplication triggers a rolling restart; valid for Deployment, DaemonSet, StatefulSet.
	RestartApplication(ctx context.Context, namespace, kind, name string) error

	ListPods(ctx context.Context, namespace string, includeSystem bool) ([]KubernetesPod, error)
	// PodLogs returns a bounded snapshot (not a stream) of one container's logs.
	PodLogs(ctx context.Context, namespace, name, container string, tailLines int) (container_ string, logs string, err error)
	DeletePod(ctx context.Context, namespace, name string) error

	ListServices(ctx context.Context, namespace string, includeSystem bool) ([]KubernetesService, error)
	ListIngresses(ctx context.Context, namespace string, includeSystem bool) ([]KubernetesIngress, error)
	ListConfigMaps(ctx context.Context, namespace string, includeSystem bool) ([]KubernetesConfigResource, error)
	ListSecrets(ctx context.Context, namespace string, includeSystem bool) ([]KubernetesConfigResource, error)
	ListPersistentVolumeClaims(ctx context.Context, namespace string, includeSystem bool) ([]KubernetesPersistentVolumeClaim, error)

	// Apply server-side applies a YAML manifest (one or more documents). dryRun validates
	// without persisting.
	Apply(ctx context.Context, manifest string, dryRun bool) ([]KubernetesApplyResult, error)
}

// KubernetesClientFactory resolves a deployed Kubernetes Platform into a cluster client using
// its Swallow-owned credential Integration.
type KubernetesClientFactory interface {
	For(ctx context.Context, platform *Platform) (KubernetesClusterClient, error)
}

var (
	// ErrPlatformNotKubernetes means the explorer was asked for a non-Kubernetes Platform.
	ErrPlatformNotKubernetes = errors.New("platform is not a Kubernetes platform")
	// ErrClusterExplorerUnavailable means the Platform is not eligible for the explorer: it is a
	// legacy registered record, or a deployed Platform whose credential is not recorded yet.
	ErrClusterExplorerUnavailable = errors.New("cluster explorer is unavailable for this platform")
	// ErrInvalidKubernetesRequest is a client-side validation failure (bad kind, replicas, name,
	// or manifest) the delivery layer maps to 400.
	ErrInvalidKubernetesRequest = errors.New("invalid kubernetes request")
)
