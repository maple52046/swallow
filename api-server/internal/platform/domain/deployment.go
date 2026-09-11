package domain

import (
	"context"
	"errors"
)

// NodeRole is the part a server plays in a platform: running the control plane or running
// workloads. It is the same vocabulary the membership axis reports, used here to assign a
// role when deploying. The k0s word "controller" is a playbook-side translation and never
// appears in the domain.
type NodeRole string

const (
	NodeRoleControlPlane NodeRole = "control-plane"
	NodeRoleWorker       NodeRole = "worker"
)

func (r NodeRole) Valid() bool {
	return r == NodeRoleControlPlane || r == NodeRoleWorker
}

// RoleAssignment assigns one Server a role and an optional workload placement.
//
// RunWorkloads is meaningful only for a control-plane assignment. Worker assignments
// always run workloads; keeping co-location separate preserves the observed Node Role
// vocabulary while allowing a control-plane Server to register as a Kubernetes node.
type RoleAssignment struct {
	ServerID     string
	Role         NodeRole
	RunWorkloads bool
}

// KubernetesTopology names the supported control-plane and workload placement shape.
// It is derived from role assignments rather than persisted on the Platform record.
type KubernetesTopology string

const (
	KubernetesTopologyStandalone       KubernetesTopology = "standalone"
	KubernetesTopologyMultiNode        KubernetesTopology = "multi-node"
	KubernetesTopologyHighAvailability KubernetesTopology = "high-availability"
)

// DeploymentSpec is the desired shape of a platform to build. The CIDRs and version are
// defaulted by the deploy use case. API VIP fields are required only when role assignments
// infer a highly available control plane.
type DeploymentSpec struct {
	K0sVersion      string
	PodCIDR         string
	ServiceCIDR     string
	APIVIP          string
	APIVIPPrefix    int
	RoleAssignments []RoleAssignment
}

// ControlPlaneServerIDs returns control-plane targets in request order.
func (s DeploymentSpec) ControlPlaneServerIDs() []string {
	return s.serverIDsWithRole(NodeRoleControlPlane)
}

// WorkerServerIDs returns workload-only targets in request order.
func (s DeploymentSpec) WorkerServerIDs() []string {
	return s.serverIDsWithRole(NodeRoleWorker)
}

// WorkloadServerIDs returns every target that registers as a Kubernetes node.
//
// The result includes worker assignments and control-plane assignments that explicitly
// opt into workload co-location, preserving request order for deterministic verification.
func (s DeploymentSpec) WorkloadServerIDs() []string {
	ids := make([]string, 0, len(s.RoleAssignments))
	for _, assignment := range s.RoleAssignments {
		if assignment.Role == NodeRoleWorker ||
			assignment.Role == NodeRoleControlPlane && assignment.RunWorkloads {
			ids = append(ids, assignment.ServerID)
		}
	}
	return ids
}

// HighlyAvailable reports whether the assignments require control-plane failover.
// Validation guarantees that any count above one is an odd count of at least three.
func (s DeploymentSpec) HighlyAvailable() bool {
	return len(s.ControlPlaneServerIDs()) >= 3
}

// Topology derives the operator-facing topology after deployment validation. An empty
// value means historical intent was incomplete and must not be guessed by read models.
func (s DeploymentSpec) Topology() KubernetesTopology {
	controllers := len(s.ControlPlaneServerIDs())
	if len(s.WorkloadServerIDs()) == 0 {
		return ""
	}
	if controllers >= 3 {
		return KubernetesTopologyHighAvailability
	}
	if controllers != 1 {
		return ""
	}
	if len(s.RoleAssignments) == 1 && s.RoleAssignments[0].RunWorkloads {
		return KubernetesTopologyStandalone
	}
	return KubernetesTopologyMultiNode
}

func (s DeploymentSpec) serverIDsWithRole(role NodeRole) []string {
	ids := make([]string, 0, len(s.RoleAssignments))
	for _, assignment := range s.RoleAssignments {
		if assignment.Role == role {
			ids = append(ids, assignment.ServerID)
		}
	}
	return ids
}

// SlurmNodeAssignment assigns Slurm roles to one Server. Unlike Kubernetes, the roles are
// not mutually exclusive: Controller runs the slurmctld management daemon, Compute runs the
// slurmd job daemon, and Login is a submission/client host (no cluster daemon of its own; it
// may also host the shared-storage NFS exports). A Server may combine roles — a controller
// commonly also contributes compute — and a login-only host is valid. A Server with no role
// at all has no place in the cluster and is rejected by the deploy use case.
type SlurmNodeAssignment struct {
	ServerID   string
	Controller bool
	Compute    bool
	Login      bool
}

// SlurmStorageType is the shared-storage backend. Only NFS is supported today; the type is an
// enum so a future backend (for example CephFS) is an additive change.
type SlurmStorageType string

const SlurmStorageNFS SlurmStorageType = "nfs"

// SlurmWorkloadStorageMode selects who provides the shared workload filesystem: swallow itself
// (an NFS export on the login node) or an operator-managed external NFS server.
type SlurmWorkloadStorageMode string

const (
	SlurmWorkloadStorageSelfHosted SlurmWorkloadStorageMode = "self-hosted"
	SlurmWorkloadStorageExternal   SlurmWorkloadStorageMode = "external"
)

// SlurmWorkloadStorageSpec is the optional shared filesystem for user/job data, distinct from
// the controller StateSaveLocation. When Enabled it is mounted on every cluster node at
// MountPath (a non-overlapping path, never /home). Self-hosted mode exports NFS from the login
// node (so it requires a login node); external mode mounts an operator-supplied NFSURL and
// runs no server. Type is nfs today. NFSMountOptions overrides the default mount options.
type SlurmWorkloadStorageSpec struct {
	Enabled         bool
	Mode            SlurmWorkloadStorageMode
	Type            SlurmStorageType
	MountPath       string
	NFSURL          string
	NFSMountOptions string
}

// SlurmDeploymentSpec is the desired shape of a Slurm platform to build.
//
// Node roles are per-role flags (see SlurmNodeAssignment) rather than the single mutually
// exclusive NodeRole used by Kubernetes. ClusterName defaults to the platform name when
// empty. StateSaveLocation is the slurmctld state directory and is optional: a single
// controller uses a controller-local default from the playbook, and a highly available
// (multi-controller) deployment gets a swallow-provisioned shared directory — the deploy use
// case selects a state server (the login node when one is assigned, otherwise an off-controller
// node) and the playbook exports it over NFS and mounts it on every controller (see
// buildSlurmVars). When set, StateSaveLocation overrides that directory path. WorkloadStorage
// is the optional shared filesystem for user data. APIVersion pins the slurmrestd endpoint
// version recorded in the credential when known; empty lets the reader fall back to its default.
type SlurmDeploymentSpec struct {
	ClusterName       string
	NodeAssignments   []SlurmNodeAssignment
	APIVersion        string
	StateSaveLocation string
	WorkloadStorage   SlurmWorkloadStorageSpec
}

// ControllerServerIDs returns the slurmctld hosts in request order. The first entry is the
// primary controller: it heads the SlurmctldHost priority list, mints the shared MUNGE key,
// hosts slurmrestd, and writes the deployment credential. HA failover follows this order.
func (s SlurmDeploymentSpec) ControllerServerIDs() []string {
	ids := make([]string, 0, len(s.NodeAssignments))
	for _, assignment := range s.NodeAssignments {
		if assignment.Controller {
			ids = append(ids, assignment.ServerID)
		}
	}
	return ids
}

// ComputeServerIDs returns the slurmd hosts in request order.
func (s SlurmDeploymentSpec) ComputeServerIDs() []string {
	ids := make([]string, 0, len(s.NodeAssignments))
	for _, assignment := range s.NodeAssignments {
		if assignment.Compute {
			ids = append(ids, assignment.ServerID)
		}
	}
	return ids
}

// LoginServerIDs returns the login (submission/client) hosts in request order. A login host
// runs no cluster daemon of its own but may host the shared-storage NFS exports.
func (s SlurmDeploymentSpec) LoginServerIDs() []string {
	ids := make([]string, 0, len(s.NodeAssignments))
	for _, assignment := range s.NodeAssignments {
		if assignment.Login {
			ids = append(ids, assignment.ServerID)
		}
	}
	return ids
}

// PrimaryLoginID returns the first login host, or "" when none is assigned. It is the host
// swallow uses as the self-hosted NFS server (controller state in HA, and workload storage).
func (s SlurmDeploymentSpec) PrimaryLoginID() string {
	logins := s.LoginServerIDs()
	if len(logins) == 0 {
		return ""
	}
	return logins[0]
}

// PrimaryControllerID returns the first controller, or "" when none is assigned. Validation
// guarantees at least one controller, so a non-empty result is expected after validation.
func (s SlurmDeploymentSpec) PrimaryControllerID() string {
	controllers := s.ControllerServerIDs()
	if len(controllers) == 0 {
		return ""
	}
	return controllers[0]
}

// ServerIDs returns every assigned Server (any role) deduplicated in request order. This is
// the set of deployment targets, since a Server may appear once carrying several roles (for
// example controller and compute, or a login-only host).
func (s SlurmDeploymentSpec) ServerIDs() []string {
	seen := make(map[string]bool, len(s.NodeAssignments))
	ids := make([]string, 0, len(s.NodeAssignments))
	for _, assignment := range s.NodeAssignments {
		if assignment.ServerID == "" || seen[assignment.ServerID] {
			continue
		}
		seen[assignment.ServerID] = true
		ids = append(ids, assignment.ServerID)
	}
	return ids
}

// HighlyAvailable reports whether more than one controller is assigned. HA requires a shared
// StateSaveLocation so a backup slurmctld can recover controller state on takeover.
func (s SlurmDeploymentSpec) HighlyAvailable() bool {
	return len(s.ControllerServerIDs()) > 1
}

// DeploymentLaunch is a fully validated request to start the operation that builds a
// platform. The trusted and secret vars are assembled by the deploy use case, which owns
// the translation from a DeploymentSpec to the playbook's variables; the launcher only
// forwards them to the operation context.
type DeploymentLaunch struct {
	Platform           *Platform
	TargetServerIDs    []string
	TrustedVars        map[string]any
	SecretVars         map[string]any
	RequestedBy        string
	RequestCorrelation string
	MachinePreparation MachinePreparation
}

// DeploymentLauncher starts the durable Operation that builds a Platform and returns its
// operation id. It is a port so the platform context does not depend on the operation
// context; the composition root supplies an adapter over the operation service.
type DeploymentLauncher interface {
	Launch(ctx context.Context, launch DeploymentLaunch) (operationID string, err error)
}

var (
	// ErrInvalidDeployment covers topology and network validation failures.
	ErrInvalidDeployment = errors.New("invalid platform deployment")
)

// MachinePreparationMode controls whether Platform deployment uses an existing OS or
// provisions all target Servers inside the same Operation first.
type MachinePreparationMode string

const (
	MachinePreparationExistingOS  MachinePreparationMode = "existing_os"
	MachinePreparationProvisionOS MachinePreparationMode = "provision_os"
)

func (m MachinePreparationMode) Valid() bool {
	return m == MachinePreparationExistingOS || m == MachinePreparationProvisionOS
}

// MachineNetworkAssignment is target-specific network intent for OS provisioning.
type MachineNetworkAssignment struct {
	ServerID    string
	InterfaceID string
	SubnetID    string
	IPAddress   string
}

// MachinePreparation is the provider-neutral OS intent embedded in a Platform request.
// UserData is write-only and must be sealed by the launcher before persistence.
type MachinePreparation struct {
	Mode           MachinePreparationMode
	TemplateID     string
	ImageID        string
	Ephemeral      *bool
	UserDataMode   string
	UserData       string
	NetworkMode    string
	SubnetID       string
	DefaultGateway bool
	Assignments    []MachineNetworkAssignment
}

// MachinePreparationValidator performs provisioner-owned image/network preflight before
// a Platform record or Operation is created.
type MachinePreparationValidator interface {
	Validate(ctx context.Context, siteID string, serverIDs []string, preparation MachinePreparation) error
}
