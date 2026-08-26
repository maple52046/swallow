package domain

import (
	"context"
	"errors"
)

// NodeRole is the part a server plays in a cluster: running the control plane or running
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

// RoleAssignment assigns one server a role in a deployment.
type RoleAssignment struct {
	ServerID string
	Role     NodeRole
}

// DeploymentSpec is the desired shape of a cluster to build. The CIDRs, version, and VIP
// prefix are optional at the API and defaulted by the deploy use case; role assignments
// are required.
type DeploymentSpec struct {
	K0sVersion      string
	PodCIDR         string
	ServiceCIDR     string
	APIVIP          string
	APIVIPPrefix    int
	RoleAssignments []RoleAssignment
}

// ControlPlaneServerIDs and WorkerServerIDs split the assignments by role, preserving the
// order they were given.
func (s DeploymentSpec) ControlPlaneServerIDs() []string {
	return s.serverIDsWithRole(NodeRoleControlPlane)
}

func (s DeploymentSpec) WorkerServerIDs() []string {
	return s.serverIDsWithRole(NodeRoleWorker)
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
// cluster. The trusted and secret vars are assembled by the deploy use case, which owns
// the translation from a DeploymentSpec to the playbook's variables; the launcher only
// forwards them to the operation context.
type DeploymentLaunch struct {
	Cluster         *Cluster
	TargetServerIDs []string
	TrustedVars     map[string]any
	SecretVars      map[string]any
	RequestedBy     string
}

// DeploymentLauncher starts the embedded operation that builds a cluster and returns its
// operation id. It is a port so the cluster context does not depend on the operation
// context; the composition root supplies an adapter over the operation service.
type DeploymentLauncher interface {
	Launch(ctx context.Context, launch DeploymentLaunch) (operationID string, err error)
}

var (
	// ErrInvalidDeployment covers topology and network validation failures.
	ErrInvalidDeployment = errors.New("invalid cluster deployment")
)
