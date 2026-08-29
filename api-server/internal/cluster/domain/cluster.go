// Package domain defines Cluster: a Kubernetes or Slurm cluster swallow knows about.
//
// swallow owns the cluster's registration and its policy. It does not own the cluster's
// membership — that is read from the cluster's own API, and when the two disagree the
// cluster is right. See docs/decisions/002-server-identity.md.
package domain

import (
	"context"
	"errors"
	"time"
)

// ClusterType is the kind of cluster.
type ClusterType string

const (
	ClusterTypeKubernetes ClusterType = "kubernetes"
	ClusterTypeSlurm      ClusterType = "slurm"
)

var ValidClusterTypes = []ClusterType{ClusterTypeKubernetes, ClusterTypeSlurm}

func (t ClusterType) Valid() bool {
	for _, valid := range ValidClusterTypes {
		if t == valid {
			return true
		}
	}
	return false
}

// GPUStackOwner decides which subsystem installs GPU drivers and the DCGM exporter.
//
// Both provisioning and an in-cluster GPU operator want to own them, and they cannot
// coexist on one host. Each cluster declares which one wins, once and explicitly, and
// swallow refuses operations that contradict it.
//
// See docs/decisions/003-metrics-label-contract.md.
type GPUStackOwner string

const (
	// GPUStackOwnerProvisioning means an embedded Ansible playbook installs the driver after OS
	// deployment and the DCGM exporter runs on the host. One driver version per site
	// under change control, and it works for servers not in any cluster, at the cost
	// of reprovisioning to change a driver.
	GPUStackOwnerProvisioning GPUStackOwner = "provisioning"
	// GPUStackOwnerGPUOperator means the cluster's GPU operator manages the driver
	// and DCGM. Per-cluster driver versions and in-cluster upgrades, at the cost of
	// only working for cluster members.
	GPUStackOwnerGPUOperator GPUStackOwner = "gpu-operator"
)

var ValidGPUStackOwners = []GPUStackOwner{GPUStackOwnerProvisioning, GPUStackOwnerGPUOperator}

func (o GPUStackOwner) Valid() bool {
	for _, valid := range ValidGPUStackOwners {
		if o == valid {
			return true
		}
	}
	return false
}

// ExporterOwner decides which subsystem installs the Prometheus exporters on this
// cluster's member hosts. It generalises GPUStackOwner from GPU drivers to all host
// exporters, so exactly one subsystem installs an exporter on a host and two never
// contend for the fixed ports (9100 node, 5000 RDC).
//
// See docs/decisions/003-metrics-label-contract.md (2026-08-27 amendment).
type ExporterOwner string

const (
	// ExporterOwnerAnsible means swallow installs the exporters as host containers via
	// embedded Ansible. This is the default, and the effective owner for any host that
	// is in no cluster.
	ExporterOwnerAnsible ExporterOwner = "ansible"
	// ExporterOwnerK8s means a Kubernetes DaemonSet installs the exporters on the node.
	// The DaemonSet uses hostNetwork on the same fixed ports, so swallow's scrape and
	// join are unchanged.
	ExporterOwnerK8s ExporterOwner = "k8s"
	// ExporterOwnerUnmanaged is not a cluster policy value. It is the resolved effective
	// owner of a host swallow must not touch: a locked machine, or one where the
	// operator installed exporters by hand. It is deliberately excluded from
	// ValidExporterOwners so it can never be set as a cluster policy.
	ExporterOwnerUnmanaged ExporterOwner = "unmanaged"
)

// ValidExporterOwners lists the values a cluster policy may take. ExporterOwnerUnmanaged
// is intentionally absent: it is only ever a resolved per-host effective value.
var ValidExporterOwners = []ExporterOwner{ExporterOwnerAnsible, ExporterOwnerK8s}

func (o ExporterOwner) Valid() bool {
	for _, valid := range ValidExporterOwners {
		if o == valid {
			return true
		}
	}
	return false
}

// Cluster is a registered cluster.
type Cluster struct {
	ID     string
	SiteID string
	Name   string
	Type   ClusterType
	// IntegrationID is the cluster-kind integration swallow reads live state through.
	// Empty when the cluster is registered but not yet reachable, which is the normal
	// state between deciding to build one and having built it.
	IntegrationID string
	GPUStackOwner GPUStackOwner
	// ExporterOwner decides who installs this cluster's exporters. Defaults to
	// ExporterOwnerAnsible; only ExporterOwnerK8s is honoured as an alternative.
	ExporterOwner ExporterOwner

	// OwnedIntegrationID is set only for a credential integration created by Swallow.
	// It is private persistence metadata and is never exposed by the cluster API.
	OwnedIntegrationID string
	Sync               SyncState
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// SyncState records the freshness of the membership read, so that a stale view is
// visible as stale.
type SyncState struct {
	LastStartedAt   *time.Time
	LastSucceededAt *time.Time
	LastError       string
	// MemberCount is how many members the last successful read saw.
	MemberCount int
	// MatchedCount is how many of them swallow could match to a server. A gap between
	// the two means the cluster contains machines swallow does not manage, which is
	// worth seeing rather than silently ignoring.
	MatchedCount int
}

// Member is a node as the cluster's own API reports it.
type Member struct {
	// Name is the cluster's name for it, which is the join key for in-cluster metrics.
	Name string
	// Role is "control-plane", "worker", or a Slurm partition role.
	Role string
	// State is the cluster's own readiness word, normalized to lower case.
	State string
	// Addresses help match the member to a server when the name does not.
	Addresses []string
}

// ClusterReader reads live state from a cluster's own API.
//
// Read-only on purpose: swallow does not create, drain, or modify anything through this
// port. Changing a cluster is an operation executed by the embedded Ansible runner.
type ClusterReader interface {
	// ListMembers returns the cluster's current nodes.
	ListMembers(ctx context.Context) ([]Member, error)
}

// ReaderFactory resolves a cluster into a reader for its API.
type ReaderFactory interface {
	For(ctx context.Context, cluster *Cluster) (ClusterReader, error)
}

var (
	ErrClusterNotFound  = errors.New("cluster not found")
	ErrClusterNameTaken = errors.New("a cluster with this name already exists at this site")
	// ErrNoClusterIntegration means the cluster has no integration to read through,
	// so its membership cannot be refreshed.
	ErrNoClusterIntegration = errors.New("cluster has no integration configured")
	// ErrUnsupportedClusterType means no reader is implemented for the cluster's type.
	ErrUnsupportedClusterType = errors.New("unsupported cluster type")
)

// ReaderErrorKind classifies a cluster API failure.
type ReaderErrorKind string

const (
	ReaderErrorUnavailable ReaderErrorKind = "unavailable"
	ReaderErrorAuth        ReaderErrorKind = "auth"
	ReaderErrorRejected    ReaderErrorKind = "rejected"
)

// ReaderError is a failure reported by, or while reaching, a cluster API.
type ReaderError struct {
	Kind   ReaderErrorKind
	Detail string
	Err    error
}

func (e *ReaderError) Error() string {
	if e.Detail != "" {
		return string(e.Kind) + ": " + e.Detail
	}
	return string(e.Kind)
}

func (e *ReaderError) Unwrap() error { return e.Err }

// ClusterRepository persists cluster registrations.
type ClusterRepository interface {
	Create(ctx context.Context, cluster *Cluster) error
	FindByID(ctx context.Context, id string) (*Cluster, error)
	List(ctx context.Context, siteID string) ([]*Cluster, error)
	Update(ctx context.Context, cluster *Cluster) error
	// UpdateSyncState writes only while the Cluster still references integrationID,
	// preventing an in-flight membership read from restoring state after uninstall.
	UpdateSyncState(ctx context.Context, id, integrationID string, state SyncState) error
	Delete(ctx context.Context, id string) error
}
