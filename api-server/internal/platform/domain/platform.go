// Package domain defines Platform: a Kubernetes or Slurm platform swallow knows about.
//
// swallow owns the platform's registration and its policy. It does not own the platform's
// membership — that is read from the platform's own API, and when the two disagree the
// platform is right. See docs/decisions/002-server-identity.md.
package domain

import (
	"context"
	"errors"
	"time"
)

// PlatformType is the kind of platform.
type PlatformType string

const (
	PlatformTypeKubernetes PlatformType = "kubernetes"
	PlatformTypeSlurm      PlatformType = "slurm"
)

var ValidPlatformTypes = []PlatformType{PlatformTypeKubernetes, PlatformTypeSlurm}

func (t PlatformType) Valid() bool {
	for _, valid := range ValidPlatformTypes {
		if t == valid {
			return true
		}
	}
	return false
}

// GPUStackOwner decides which subsystem installs GPU drivers and the DCGM exporter.
//
// Both provisioning and an in-platform GPU operator want to own them, and they cannot
// coexist on one host. Each platform declares which one wins, once and explicitly, and
// swallow refuses operations that contradict it.
//
// See docs/decisions/003-metrics-label-contract.md.
type GPUStackOwner string

const (
	// GPUStackOwnerProvisioning means an embedded Ansible playbook installs the driver after OS
	// deployment and the DCGM exporter runs on the host. One driver version per site
	// under change control, and it works for servers not in any platform, at the cost
	// of reprovisioning to change a driver.
	GPUStackOwnerProvisioning GPUStackOwner = "provisioning"
	// GPUStackOwnerGPUOperator means the platform's GPU operator manages the driver
	// and DCGM. Per-platform driver versions and in-platform upgrades, at the cost of
	// only working for platform members.
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
// platform's member hosts. It generalises GPUStackOwner from GPU drivers to all host
// exporters, so exactly one subsystem installs an exporter on a host and two never
// contend for the fixed ports (9100 node, 5000 RDC).
//
// See docs/decisions/003-metrics-label-contract.md (2026-08-27 amendment).
type ExporterOwner string

const (
	// ExporterOwnerAnsible means swallow installs the exporters as host containers via
	// embedded Ansible. This is the default, and the effective owner for any host that
	// is in no platform.
	ExporterOwnerAnsible ExporterOwner = "ansible"
	// ExporterOwnerK8s means a Kubernetes DaemonSet installs the exporters on the node.
	// The DaemonSet uses hostNetwork on the same fixed ports, so swallow's scrape and
	// join are unchanged.
	ExporterOwnerK8s ExporterOwner = "k8s"
	// ExporterOwnerUnmanaged is not a platform policy value. It is the resolved effective
	// owner of a host swallow must not touch: a locked machine, or one where the
	// operator installed exporters by hand. It is deliberately excluded from
	// ValidExporterOwners so it can never be set as a platform policy.
	ExporterOwnerUnmanaged ExporterOwner = "unmanaged"
)

// ValidExporterOwners lists the values a platform policy may take. ExporterOwnerUnmanaged
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

// Platform is a registered platform.
type Platform struct {
	ID     string
	SiteID string
	Name   string
	Type   PlatformType
	// IntegrationID is the platform-kind integration swallow reads live state through.
	// Empty when the platform is registered but not yet reachable, which is the normal
	// state between deciding to build one and having built it.
	IntegrationID string
	GPUStackOwner GPUStackOwner
	// ExporterOwner decides who installs this platform's exporters. Defaults to
	// ExporterOwnerAnsible; only ExporterOwnerK8s is honoured as an alternative.
	ExporterOwner ExporterOwner

	// OwnedIntegrationID is set only for a credential integration created by Swallow.
	// It is private persistence metadata and is never exposed by the platform API.
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
	// the two means the platform contains machines swallow does not manage, which is
	// worth seeing rather than silently ignoring.
	MatchedCount int
}

// Member is a node as the platform's own API reports it.
type Member struct {
	// Name is the platform's name for it, which is the join key for in-platform metrics.
	Name string
	// Role is "control-plane", "worker", or a Slurm partition role.
	Role string
	// State is the platform's own readiness word, normalized to lower case.
	State string
	// Addresses help match the member to a server when the name does not.
	Addresses []string
}

// PlatformReader reads live state from a platform's own API.
//
// Read-only on purpose: swallow does not create, drain, or modify anything through this
// port. Changing a platform is an operation executed by the embedded Ansible runner.
type PlatformReader interface {
	// ListMembers returns the platform's current nodes.
	ListMembers(ctx context.Context) ([]Member, error)
}

// ReaderFactory resolves a platform into a reader for its API.
type ReaderFactory interface {
	For(ctx context.Context, platform *Platform) (PlatformReader, error)
}

// SlurmClusterState is a Slurm platform's live cluster state, read on demand from its API.
//
// It is deliberately Slurm-shaped and separate from both the generic membership axis
// (which carries only compute/slurmd scheduler nodes) and the deployment-intent projection
// (what was requested). It reports the controller daemons (which are not scheduler nodes and
// so never appear as members), the scheduling partitions, and each compute node's live Slurm
// state, none of which the generic Member view can represent. This axis is never conflated
// with the monitoring health axis.
type SlurmClusterState struct {
	Controllers []SlurmController
	Partitions  []SlurmPartition
	Nodes       []SlurmNode
}

// SlurmController is one slurmctld host as reported by scontrol/slurmrestd ping, in failover
// order. The first controller is the primary; the rest are ordered standbys.
type SlurmController struct {
	Hostname string
	// Primary is true for the active/first controller in the SlurmctldHost order.
	Primary bool
	// Status is "up", "down", or "unknown" (ping did not report it). This is the
	// controller RPC liveness, not the host's monitoring health.
	Status string
}

// SlurmPartition is a scheduling partition and its configured node set.
type SlurmPartition struct {
	Name string
	// State is the partition's state (for example "UP"), normalized to lower case.
	State string
	// NodeSpec is Slurm's configured node expression for the partition (for example
	// "compute[1-4]"); empty when the API did not report it.
	NodeSpec string
	// TotalNodes is how many nodes the partition contains; zero when unreported.
	TotalNodes int
}

// SlurmNode is one compute node's live scheduler state, richer than the membership Member.
type SlurmNode struct {
	Name string
	// State is the collapsed Slurm node state (idle, allocated, mixed, down, drain, ...),
	// normalized to lower case. This is scheduler state, not monitoring health.
	State         string
	CPUs          int
	RealMemoryMiB int64
	// Gres is the node's configured generic resources (for example "gpu:8"); empty when none.
	Gres       string
	Partitions []string
	Address    string
}

// SlurmClusterReader reads a Slurm platform's live cluster state on demand. It is a superset
// of PlatformReader used only by the Slurm-specific view; the concrete Slurm reader
// implements both, so membership sync keeps using ListMembers unchanged.
type SlurmClusterReader interface {
	GetClusterState(ctx context.Context) (*SlurmClusterState, error)
}

var (
	ErrPlatformNotFound  = errors.New("platform not found")
	ErrPlatformNameTaken = errors.New("a platform with this name already exists at this site")
	// ErrNoPlatformIntegration means the platform has no integration to read through,
	// so its membership cannot be refreshed.
	ErrNoPlatformIntegration = errors.New("platform has no integration configured")
	// ErrUnsupportedPlatformType means no reader is implemented for the platform's type.
	ErrUnsupportedPlatformType = errors.New("unsupported platform type")
)

// ReaderErrorKind classifies a platform API failure.
type ReaderErrorKind string

const (
	ReaderErrorUnavailable ReaderErrorKind = "unavailable"
	ReaderErrorAuth        ReaderErrorKind = "auth"
	ReaderErrorRejected    ReaderErrorKind = "rejected"
)

// ReaderError is a failure reported by, or while reaching, a platform API.
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

// PlatformRepository persists platform registrations.
type PlatformRepository interface {
	Create(ctx context.Context, platform *Platform) error
	FindByID(ctx context.Context, id string) (*Platform, error)
	List(ctx context.Context, siteID string) ([]*Platform, error)
	Update(ctx context.Context, platform *Platform) error
	// UpdateSyncState writes only while the Platform still references integrationID,
	// preventing an in-flight membership read from restoring state after uninstall.
	UpdateSyncState(ctx context.Context, id, integrationID string, state SyncState) error
	Delete(ctx context.Context, id string) error
}
