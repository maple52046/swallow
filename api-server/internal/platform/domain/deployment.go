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
