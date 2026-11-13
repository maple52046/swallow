// Package domain defines Managed Software: single-software deployment intent and the
// swallow-owned Software Assignment record that tracks what software is installed on which
// Server. It is deliberately distinct from the Platform aggregate: the deploy target here is one
// software and its variants, not a multi-component runtime (docs/decisions/038-software-deployment.md).
//
// This package owns no orchestration or I/O. Workflow assembly lives in the app layer, and
// persistence is a repository interface implemented by the store adapter.
package domain

import (
	"context"
	"errors"
	"time"
)

// Kind is a single installable software, the id of the Software module that provides it. Each Kind
// maps to a pair of release-manifest playbooks (deploy-<kind> and uninstall-<kind>) and, when the
// software has variants, a set of Roles.
type Kind string

const (
	KindDockerCE Kind = "docker-ce"
	KindPodman   Kind = "podman"
	KindNFS      Kind = "nfs"
	// KindPrometheus and KindGrafana install as pinned containers or natively (decision 063).
	KindPrometheus Kind = "prometheus"
	KindGrafana    Kind = "grafana"
	// KindDeviceMetricsExporter is AMD's per-GPU exporter, installed natively (decision 068).
	KindDeviceMetricsExporter Kind = "device-metrics-exporter"
)

// Role is a variant of one software (for NFS, whether a Server is a server, a client, or both).
// Role-less software (a container runtime) carries no Role.
type Role string

const (
	RoleServer Role = "server"
	RoleClient Role = "client"
)

// AssignmentState is the lifecycle of one Software Assignment. It is swallow-owned intent plus a
// last-applied fact, never a Server status axis. `absent` means swallow no longer considers the
// software present (for example the Server left `deployed`).
type AssignmentState string

const (
	StatePending      AssignmentState = "pending"
	StateInstalled    AssignmentState = "installed"
	StateFailed       AssignmentState = "failed"
	StateUninstalling AssignmentState = "uninstalling"
	StateAbsent       AssignmentState = "absent"
)

// Assignment is the swallow-owned record of one Managed Software kind on one Server, keyed by
// (ServerID, Kind). It is the single source of truth for what software swallow has installed
// where, driving listing, duplicate-prevention, and uninstall.
type Assignment struct {
	ServerID       string
	SiteID         string
	Kind           Kind
	Roles          []Role
	Spec           map[string]any
	State          AssignmentState
	LastWorkflowID string
	LastAppliedAt  *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CatalogEntry describes one installable software kind and the rules the deploy use case enforces
// before creating a Workflow. Each Software module provides its kind's entry (Software.Entry).
// Exporter is a Prometheus exporter a module declares. Name is the `exporter` label of its scrape
// targets and must be unique among the build's exporters; Port is where it listens on the Server's
// primary address.
type Exporter struct {
	Name string
	Port int
}

type CatalogEntry struct {
	Kind Kind
	// Label is the user-facing name.
	Label string
	// Roles are the software's variants; empty for a role-less kind.
	Roles []Role
	// MutuallyExclusiveWith lists kinds that cannot coexist with this one on one Server.
	MutuallyExclusiveWith []Kind
	// RefusedForKubernetesMembers rejects installing this kind on a Server that is already a
	// Kubernetes Platform member (Docker/Podman containerd coexisting with k0s is undefined).
	RefusedForKubernetesMembers bool
	// RefusedForClaimedServers rejects a standalone install or uninstall of this kind on a Server
	// with a Platform Claim (decision 067), because that Platform manages the kind on its Servers
	// (decision 068). A Platform's own composition of the kind is not subject to it.
	RefusedForClaimedServers bool
	// Exporter is the Prometheus exporter this kind runs on each Server it is installed on, nil for a
	// kind that runs none. The Site Prometheus scrapes it from the moment the Server's assignment is
	// installed until it is not.
	Exporter *Exporter
	// RequiredAccelerator is the id of the Accelerator module whose compute GPUs every target's GPU
	// Inventory must list, or "" when the kind needs none.
	RequiredAccelerator string
	// InstallMethods lists how this kind can be installed (spec installMethod); the first is the
	// default. Empty for a kind with one fixed install path.
	InstallMethods []InstallMethod
	// ContainerRuntimes lists the Managed Software kinds that can run this kind's container install
	// method. One of them must be installed on the Server (they are mutually exclusive); a standalone
	// install refuses a target without one, while a platform deployment that composes this kind
	// installs the runtime in an earlier Job of the same Workflow instead.
	ContainerRuntimes []Kind
	// SpecFields lists the kind-specific spec keys the API accepts, for documentation and UI.
	SpecFields []string
	// DirectoryClient marks the kind that makes a Server read its accounts, SSH keys, and sudo
	// rules from the installation's Directory (decision 082). Its install needs a configured
	// Directory and receives the Directory's connection settings as trusted variables and the
	// read-only account's password as a secret variable.
	DirectoryClient bool
}

// InstallMethod is how a Managed Software kind that supports several install paths is installed
// on a Server (decision 063). It is a variant of the same software, not a different kind.
type InstallMethod string

const (
	// InstallMethodContainer runs the software as a pinned container under one of the kind's
	// ContainerRuntimes (Docker Compose for Docker CE, a Quadlet systemd unit for Podman).
	InstallMethodContainer InstallMethod = "container"
	// InstallMethodNative installs the software on the host from the vendor's official packages or
	// release binaries, managed by systemd.
	InstallMethodNative InstallMethod = "native"
)

// Allows reports whether the entry offers the install method.
func (e CatalogEntry) Allows(method InstallMethod) bool {
	for _, allowed := range e.InstallMethods {
		if allowed == method {
			return true
		}
	}
	return false
}

// RunsContainersOn reports whether kind is one of the entry's container runtimes.
func (e CatalogEntry) RunsContainersOn(kind Kind) bool {
	for _, runtime := range e.ContainerRuntimes {
		if runtime == kind {
			return true
		}
	}
	return false
}

// SpecVersion optionally pins a kind's package or release version. It is published language of the
// software contract (software.md), so renaming it is a breaking change.
const SpecVersion = "version"

// AllowsRole reports whether a role is valid for this kind.
func (e CatalogEntry) AllowsRole(role Role) bool {
	for _, allowed := range e.Roles {
		if allowed == role {
			return true
		}
	}
	return false
}

// AssignmentFilter narrows a Software Assignment listing. IncludeAbsent adds records that have
// gone absent (for example after a Server left `deployed`), which are omitted by default.
type AssignmentFilter struct {
	ServerID      string
	Kind          Kind
	IncludeAbsent bool
}

// AssignmentRepository persists Software Assignments keyed by (ServerID, Kind).
type AssignmentRepository interface {
	// Upsert creates or replaces the assignment for (ServerID, Kind).
	Upsert(ctx context.Context, assignment *Assignment) error
	// FindByServerAndKind returns the assignment or ErrAssignmentNotFound.
	FindByServerAndKind(ctx context.Context, serverID string, kind Kind) (*Assignment, error)
	// List returns assignments matching the filter, ordered by serverId then kind.
	List(ctx context.Context, filter AssignmentFilter) ([]*Assignment, error)
	// SetState updates only the lifecycle fields of an existing assignment. appliedAt is set on
	// the last-applied field only when non-nil (a successful install/uninstall). A missing
	// record is ErrAssignmentNotFound.
	SetState(ctx context.Context, serverID string, kind Kind, state AssignmentState, workflowID string, appliedAt *time.Time) error
}

var (
	// ErrUnknownKind means the requested software kind is not in the catalog.
	ErrUnknownKind = errors.New("unknown software kind")
	// ErrInvalidRole means a requested role is not valid for the kind (or a role-less kind was
	// given a role, or a role-carrying kind was given none).
	ErrInvalidRole = errors.New("invalid software role for kind")
	// ErrSpecInvalid means a required kind-specific spec field is missing or malformed.
	ErrSpecInvalid = errors.New("invalid software spec")
	// ErrMutuallyExclusive means a conflicting software kind is already installed on a target.
	ErrMutuallyExclusive = errors.New("a mutually exclusive software kind is already installed")
	// ErrKubernetesMember means the kind refuses installation on a Kubernetes Platform member.
	ErrKubernetesMember = errors.New("software kind cannot be installed on a Kubernetes platform member")
	// ErrRequiredSoftwareMissing means a kind the requested kind requires (for example Docker CE
	// under Prometheus) is not installed on a target. It is a state conflict, not a request error.
	ErrRequiredSoftwareMissing = errors.New("a required software kind is not installed")
	// ErrAssignmentNotFound means no Software Assignment exists for (ServerID, Kind).
	ErrAssignmentNotFound = errors.New("software assignment not found")
	// ErrPlatformClaimed means the kind refuses a standalone install or uninstall on a Server a
	// Platform claims, whose Platform manages it there. It is a state conflict.
	ErrPlatformClaimed = errors.New("software kind is managed by the platform that claims the server")
	// ErrTargetIneligible means a target lacks what the kind needs on the host (for example AMD
	// compute GPUs for the Device Metrics Exporter). It is a state conflict, not a request error.
	ErrTargetIneligible = errors.New("target server cannot run this software kind")
)
