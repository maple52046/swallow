// Package domain defines Managed Software: single-software deployment intent and the
// swallow-owned Software Assignment record that tracks what software is installed on which
// Server. It is deliberately distinct from the Platform aggregate: the deploy target here is one
// software and its variants, not a multi-component runtime (docs/decisions/038-software-deployment.md).
//
// This package owns no orchestration or I/O. Workflow assembly lives in the app layer, and
// persistence is a repository interface implemented under infra.
package domain

import (
	"context"
	"errors"
	"time"
)

// Kind is a single installable software. Each Kind maps to a pair of release-manifest playbooks
// (deploy-<kind> and uninstall-<kind>) and, when the software has variants, a set of Roles.
type Kind string

const (
	KindDockerCE Kind = "docker-ce"
	KindPodman   Kind = "podman"
	KindNFS      Kind = "nfs"
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
// before creating a Workflow. It is the domain source for the /software/catalog contract.
type CatalogEntry struct {
	Kind Kind
	// Label is the operator-facing name.
	Label string
	// Roles are the software's variants; empty for a role-less kind.
	Roles []Role
	// MutuallyExclusiveWith lists kinds that cannot coexist with this one on one Server.
	MutuallyExclusiveWith []Kind
	// RefusedForKubernetesMembers rejects installing this kind on a Server that is already a
	// Kubernetes Platform member (Docker/Podman containerd coexisting with k0s is undefined).
	RefusedForKubernetesMembers bool
	// SpecFields lists the kind-specific spec keys the API accepts, for documentation and UI.
	SpecFields []string
}

// catalog is the fixed first-cut Managed Software catalog. Adding an entry is a model change
// coordinated with the glossary and the software API contract.
var catalog = []CatalogEntry{
	{
		Kind:                        KindDockerCE,
		Label:                       "Docker CE",
		Roles:                       nil,
		MutuallyExclusiveWith:       []Kind{KindPodman},
		RefusedForKubernetesMembers: true,
		SpecFields:                  []string{"version"},
	},
	{
		Kind:                        KindPodman,
		Label:                       "Podman",
		Roles:                       nil,
		MutuallyExclusiveWith:       []Kind{KindDockerCE},
		RefusedForKubernetesMembers: true,
		SpecFields:                  []string{"version"},
	},
	{
		Kind:                        KindNFS,
		Label:                       "NFS",
		Roles:                       []Role{RoleServer, RoleClient},
		MutuallyExclusiveWith:       nil,
		RefusedForKubernetesMembers: false,
		SpecFields:                  []string{"exportPath", "exportOptions", "source", "mountPath", "mountOptions"},
	},
}

// Catalog returns a copy of the Managed Software catalog so callers cannot mutate the source.
func Catalog() []CatalogEntry {
	out := make([]CatalogEntry, len(catalog))
	copy(out, catalog)
	return out
}

// LookupKind returns the catalog entry for a kind, or false when the kind is unknown.
func LookupKind(kind Kind) (CatalogEntry, bool) {
	for _, entry := range catalog {
		if entry.Kind == kind {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}

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
	// ListAll returns every non-absent assignment so the OS-lifecycle sweep can reconcile them
	// against current Server provisioning state.
	ListAll(ctx context.Context) ([]*Assignment, error)
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
	// ErrAssignmentNotFound means no Software Assignment exists for (ServerID, Kind).
	ErrAssignmentNotFound = errors.New("software assignment not found")
)
