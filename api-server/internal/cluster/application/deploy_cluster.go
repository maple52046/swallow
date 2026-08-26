package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// Deployment defaults. The CIDRs are k0s/k8s defaults that the HA knowledge base verified
// against a 192.168.100.0/24 node subnet; they are chosen so they do not cover a typical
// node network, and the use case still refuses any that would.
const (
	defaultPodCIDR      = "10.244.0.0/16"
	defaultServiceCIDR  = "10.96.0.0/12"
	defaultAPIVIPPrefix = 24
	minControllers      = 3
)

// Trusted extra-var names the deploy operation passes to the k0s playbook. They use the
// swallow_ prefix and are supplied as trusted vars, so a client cannot forge them.
const (
	varK0sVersion           = "swallow_k0s_version"
	varK0sPodCIDR           = "swallow_k0s_pod_cidr"
	varK0sServiceCIDR       = "swallow_k0s_service_cidr"
	varK0sAPIVIP            = "swallow_k0s_api_vip"
	varK0sAPIVIPPrefix      = "swallow_k0s_api_vip_prefix"
	varK0sClusterName       = "swallow_k0s_cluster_name"
	varK0sRoles             = "swallow_k0s_roles"
	varK0sControllerIDs     = "swallow_k0s_controller_ids"
	varK0sWorkerIDs         = "swallow_k0s_worker_ids"
	varK0sInitialController = "swallow_k0s_initial_controller_id"
	// varK0sVRRPAuthPass is the one secret var: it is sealed at rest and materialised only
	// for the run.
	varK0sVRRPAuthPass = "swallow_k0s_vrrp_auth_pass"
)

// DeployService owns the intent to build a cluster: it validates the topology, creates the
// cluster with no integration yet, and delegates execution to the operation context.
type DeployService struct {
	clusterService *ClusterService
	clusters       clusterdomain.ClusterRepository
	servers        serverdomain.ServerRepository
	launcher       clusterdomain.DeploymentLauncher
}

// NewDeployService constructs the cluster deployment use case.
func NewDeployService(
	clusterService *ClusterService,
	clusters clusterdomain.ClusterRepository,
	servers serverdomain.ServerRepository,
	launcher clusterdomain.DeploymentLauncher,
) *DeployService {
	return &DeployService{
		clusterService: clusterService, clusters: clusters,
		servers: servers, launcher: launcher,
	}
}

// DeployClusterInput is accepted by POST /clusters/deploy.
type DeployClusterInput struct {
	SiteID        string
	Name          string
	GPUStackOwner string
	Spec          clusterdomain.DeploymentSpec
	RequestedBy   string
}

// DeployClusterResult is the accepted deployment: the created cluster and the operation
// that is building it.
type DeployClusterResult struct {
	ClusterID   string `json:"clusterId"`
	OperationID string `json:"operationId"`
}

// Deploy validates the topology, creates the cluster, and launches the build operation.
//
// The cluster is created before the operation so the operation can carry its id, and is
// deleted again if the launch is rejected, so a failed request leaves no orphan cluster.
func (s *DeployService) Deploy(ctx context.Context, input DeployClusterInput) (*DeployClusterResult, error) {
	spec, targets, err := s.validate(ctx, input)
	if err != nil {
		return nil, err
	}

	created, err := s.clusterService.Create(ctx, CreateClusterInput{
		SiteID:        input.SiteID,
		Name:          input.Name,
		Type:          string(clusterdomain.ClusterTypeKubernetes),
		GPUStackOwner: input.GPUStackOwner,
	})
	if err != nil {
		return nil, err
	}

	cluster, err := s.clusters.FindByID(ctx, created.ID)
	if err != nil {
		return nil, err
	}

	authPass, err := generateVRRPPassword()
	if err != nil {
		_ = s.clusters.Delete(ctx, cluster.ID)
		return nil, err
	}

	launch := clusterdomain.DeploymentLaunch{
		Cluster:         cluster,
		TargetServerIDs: serverIDs(targets),
		TrustedVars:     buildDeploymentVars(cluster, spec),
		SecretVars:      map[string]any{varK0sVRRPAuthPass: authPass},
		RequestedBy:     input.RequestedBy,
	}
	operationID, err := s.launcher.Launch(ctx, launch)
	if err != nil {
		// The operation was refused, so nothing is building this cluster; remove it
		// rather than leave a cluster that will never converge.
		_ = s.clusters.Delete(ctx, cluster.ID)
		return nil, err
	}

	return &DeployClusterResult{ClusterID: cluster.ID, OperationID: operationID}, nil
}

// validate resolves and checks the targets and network, defaulting optional fields. It
// returns the normalised spec and the resolved target servers in assignment order.
func (s *DeployService) validate(ctx context.Context, input DeployClusterInput) (clusterdomain.DeploymentSpec, []*serverdomain.Server, error) {
	spec := input.Spec
	var none []*serverdomain.Server

	if len(spec.RoleAssignments) == 0 {
		return spec, none, fmt.Errorf("%w: at least one role assignment is required", clusterdomain.ErrInvalidDeployment)
	}

	targets := make([]*serverdomain.Server, 0, len(spec.RoleAssignments))
	seenServer := map[string]bool{}
	controllers, workers := 0, 0
	for _, assignment := range spec.RoleAssignments {
		if !assignment.Role.Valid() {
			return spec, none, fmt.Errorf("%w: role must be control-plane or worker", clusterdomain.ErrInvalidDeployment)
		}
		if seenServer[assignment.ServerID] {
			return spec, none, fmt.Errorf("%w: server %s is assigned more than once",
				clusterdomain.ErrInvalidDeployment, assignment.ServerID)
		}
		seenServer[assignment.ServerID] = true

		server, err := s.servers.FindByID(ctx, assignment.ServerID)
		if err != nil {
			return spec, none, err
		}
		if server.Source.SiteID != input.SiteID {
			return spec, none, fmt.Errorf("%w: server %s is not at site %s",
				clusterdomain.ErrInvalidDeployment, server.DisplayName(), input.SiteID)
		}
		if server.Provisioning == nil || server.Provisioning.State != "deployed" {
			return spec, none, fmt.Errorf("%w: server %s must be deployed before it can join a cluster",
				clusterdomain.ErrInvalidDeployment, server.DisplayName())
		}
		if assignment.Role == clusterdomain.NodeRoleControlPlane {
			controllers++
		} else {
			workers++
		}
		targets = append(targets, server)
	}

	if controllers < minControllers || controllers%2 == 0 {
		return spec, none, fmt.Errorf("%w: a highly available control plane needs an odd number of at least %d controllers, got %d",
			clusterdomain.ErrInvalidDeployment, minControllers, controllers)
	}
	if workers < 1 {
		return spec, none, fmt.Errorf("%w: at least one worker is required", clusterdomain.ErrInvalidDeployment)
	}

	if spec.K0sVersion == "" {
		return spec, none, fmt.Errorf("%w: k0sVersion is required", clusterdomain.ErrInvalidDeployment)
	}
	if spec.PodCIDR == "" {
		spec.PodCIDR = defaultPodCIDR
	}
	if spec.ServiceCIDR == "" {
		spec.ServiceCIDR = defaultServiceCIDR
	}
	if spec.APIVIPPrefix == 0 {
		spec.APIVIPPrefix = defaultAPIVIPPrefix
	}
	if spec.APIVIPPrefix < 1 || spec.APIVIPPrefix > 32 {
		return spec, none, fmt.Errorf("%w: apiVipPrefix must be between 1 and 32", clusterdomain.ErrInvalidDeployment)
	}

	podNet, err := netip.ParsePrefix(spec.PodCIDR)
	if err != nil {
		return spec, none, fmt.Errorf("%w: podCidr is not a valid CIDR: %v", clusterdomain.ErrInvalidDeployment, err)
	}
	serviceNet, err := netip.ParsePrefix(spec.ServiceCIDR)
	if err != nil {
		return spec, none, fmt.Errorf("%w: serviceCidr is not a valid CIDR: %v", clusterdomain.ErrInvalidDeployment, err)
	}

	vip, err := netip.ParseAddr(spec.APIVIP)
	if err != nil {
		return spec, none, fmt.Errorf("%w: apiVip is not a valid IP address", clusterdomain.ErrInvalidDeployment)
	}

	// The nodes reach each other and the VIP over their own subnet, so a pod or service
	// range that covers a node address would break etcd and the control plane. This is the
	// specific trap the HA knowledge base calls out.
	for _, server := range targets {
		for _, raw := range server.Observed.Addresses {
			addr, err := netip.ParseAddr(raw)
			if err != nil {
				continue
			}
			if podNet.Contains(addr) {
				return spec, none, fmt.Errorf("%w: podCidr %s covers node address %s (%s)",
					clusterdomain.ErrInvalidDeployment, spec.PodCIDR, raw, server.DisplayName())
			}
			if serviceNet.Contains(addr) {
				return spec, none, fmt.Errorf("%w: serviceCidr %s covers node address %s (%s)",
					clusterdomain.ErrInvalidDeployment, spec.ServiceCIDR, raw, server.DisplayName())
			}
			if addr == vip {
				return spec, none, fmt.Errorf("%w: apiVip %s is already a node address (%s)",
					clusterdomain.ErrInvalidDeployment, spec.APIVIP, server.DisplayName())
			}
		}
	}

	return spec, targets, nil
}

// buildDeploymentVars translates a spec into the trusted variables the k0s playbook reads.
// The role map is keyed by serverId, matching the inventory, so the playbook can group_by
// role without trusting anything the client sent.
func buildDeploymentVars(cluster *clusterdomain.Cluster, spec clusterdomain.DeploymentSpec) map[string]any {
	roles := make(map[string]any, len(spec.RoleAssignments))
	for _, assignment := range spec.RoleAssignments {
		roles[assignment.ServerID] = string(assignment.Role)
	}
	controllers := spec.ControlPlaneServerIDs()
	return map[string]any{
		varK0sVersion:           spec.K0sVersion,
		varK0sPodCIDR:           spec.PodCIDR,
		varK0sServiceCIDR:       spec.ServiceCIDR,
		varK0sAPIVIP:            spec.APIVIP,
		varK0sAPIVIPPrefix:      spec.APIVIPPrefix,
		varK0sClusterName:       cluster.Name,
		varK0sRoles:             roles,
		varK0sControllerIDs:     controllers,
		varK0sWorkerIDs:         spec.WorkerServerIDs(),
		varK0sInitialController: controllers[0],
	}
}

func serverIDs(servers []*serverdomain.Server) []string {
	ids := make([]string, len(servers))
	for i, server := range servers {
		ids[i] = server.ID
	}
	return ids
}

// generateVRRPPassword returns a random password for the control-plane VIP. It is exactly
// eight characters because keepalived only uses the first eight and k0s rejects a longer
// value ("AuthPass must be 8 characters or less"). Four random bytes give eight hex chars.
func generateVRRPPassword() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate vrrp password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
