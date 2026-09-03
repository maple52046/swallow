package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// Deployment defaults use the k0s/Kubernetes service networks. The use case still
// refuses ranges that overlap any selected Server address.
const (
	defaultPodCIDR      = "10.244.0.0/16"
	defaultServiceCIDR  = "10.96.0.0/12"
	defaultAPIVIPPrefix = 24
	minHAControllers    = 3
)

// Trusted extra-var names the deploy operation passes to the k0s playbook. They use the
// swallow_ prefix and are supplied as trusted vars, so a client cannot forge them.
const (
	varK0sVersion               = "swallow_k0s_version"
	varK0sPodCIDR               = "swallow_k0s_pod_cidr"
	varK0sServiceCIDR           = "swallow_k0s_service_cidr"
	varK0sHighAvailability      = "swallow_k0s_high_availability"
	varK0sAPIAddress            = "swallow_k0s_api_address"
	varK0sAPIVIP                = "swallow_k0s_api_vip"
	varK0sAPIVIPPrefix          = "swallow_k0s_api_vip_prefix"
	varK0sClusterName           = "swallow_k0s_cluster_name"
	varK0sRoles                 = "swallow_k0s_roles"
	varK0sControllerIDs         = "swallow_k0s_controller_ids"
	varK0sWorkerIDs             = "swallow_k0s_worker_ids"
	varK0sWorkloadIDs           = "swallow_k0s_workload_ids"
	varK0sWorkloadControllerIDs = "swallow_k0s_workload_controller_ids"
	varK0sInitialController     = "swallow_k0s_initial_controller_id"
	// varK0sVRRPAuthPass is sealed at rest and materialised only for an HA run.
	varK0sVRRPAuthPass = "swallow_k0s_vrrp_auth_pass"
)

// DeployService owns the intent to build a cluster: it validates the topology, creates the
// cluster with no integration yet, and delegates execution to the operation context.
type DeployService struct {
	clusterService *ClusterService
	clusters       clusterdomain.ClusterRepository
	servers        serverdomain.ServerRepository
	lifecycle      clusterdomain.LifecycleReader
	launcher       clusterdomain.DeploymentLauncher
	protection     serverdomain.MutationGuard
}

// NewDeployService constructs deployment preflight with both Server observations and the
// batch lifecycle reader. The lifecycle dependency must preserve original target snapshots
// so partial deployments cannot be assigned to a second Cluster.
func NewDeployService(
	clusterService *ClusterService,
	clusters clusterdomain.ClusterRepository,
	servers serverdomain.ServerRepository,
	lifecycle clusterdomain.LifecycleReader,
	launcher clusterdomain.DeploymentLauncher,
	protection ...serverdomain.MutationGuard,
) *DeployService {
	service := &DeployService{
		clusterService: clusterService, clusters: clusters,
		servers: servers, lifecycle: lifecycle, launcher: launcher,
	}
	if len(protection) > 0 {
		service.protection = protection[0]
	}
	return service
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

// validatedDeployment keeps provider observations out of the domain DeploymentSpec while
// carrying the resolved API address from preflight into trusted playbook variables.
type validatedDeployment struct {
	spec       clusterdomain.DeploymentSpec
	targets    []*serverdomain.Server
	apiAddress string
}

// deploymentTargetAvailability combines intent-owned deployment claims with observed
// membership. The former protects partial deployments before a cluster API is reachable;
// the latter protects externally registered clusters that have no deployment provenance.
type deploymentTargetAvailability struct {
	claims       map[string]*clusterdomain.Cluster
	clustersByID map[string]*clusterdomain.Cluster
}

// Deploy validates the topology, creates the cluster, and launches the build operation.
//
// The cluster is created before the operation so the operation can carry its id, and is
// deleted again if the launch is rejected, so a failed request leaves no orphan cluster.
func (s *DeployService) Deploy(ctx context.Context, input DeployClusterInput) (*DeployClusterResult, error) {
	validated, err := s.validate(ctx, input)
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

	secretVars := map[string]any{}
	if validated.spec.HighlyAvailable() {
		authPass, err := generateVRRPPassword()
		if err != nil {
			_ = s.clusters.Delete(ctx, cluster.ID)
			return nil, err
		}
		secretVars[varK0sVRRPAuthPass] = authPass
	}

	launch := clusterdomain.DeploymentLaunch{
		Cluster:         cluster,
		TargetServerIDs: serverIDs(validated.targets),
		TrustedVars:     buildDeploymentVars(cluster, validated.spec, validated.apiAddress),
		SecretVars:      secretVars,
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

// validate resolves and checks topology, targets, and networks before any record exists.
// It returns a normalized spec and an API address derived from either the HA VIP or the
// initial control-plane Server observation.
func (s *DeployService) validate(ctx context.Context, input DeployClusterInput) (validatedDeployment, error) {
	spec := input.Spec
	var invalid validatedDeployment

	if len(spec.RoleAssignments) == 0 {
		return invalid, fmt.Errorf("%w: at least one role assignment is required", clusterdomain.ErrInvalidDeployment)
	}

	availability, err := s.targetAvailability(ctx, input.SiteID)
	if err != nil {
		return invalid, err
	}

	targets := make([]*serverdomain.Server, 0, len(spec.RoleAssignments))
	seenServer := map[string]bool{}
	controllers := 0
	for _, assignment := range spec.RoleAssignments {
		if !assignment.Role.Valid() {
			return invalid, fmt.Errorf("%w: role must be control-plane or worker", clusterdomain.ErrInvalidDeployment)
		}
		if assignment.Role == clusterdomain.NodeRoleWorker && assignment.RunWorkloads {
			return invalid, fmt.Errorf("%w: runWorkloads is only valid for a control-plane assignment", clusterdomain.ErrInvalidDeployment)
		}
		if seenServer[assignment.ServerID] {
			return invalid, fmt.Errorf("%w: server %s is assigned more than once",
				clusterdomain.ErrInvalidDeployment, assignment.ServerID)
		}
		seenServer[assignment.ServerID] = true

		server, err := s.servers.FindByID(ctx, assignment.ServerID)
		if err != nil {
			return invalid, err
		}
		if server.Source.SiteID != input.SiteID {
			return invalid, fmt.Errorf("%w: server %s is not at site %s",
				clusterdomain.ErrInvalidDeployment, server.DisplayName(), input.SiteID)
		}
		if server.Provisioning == nil || server.Provisioning.State != "deployed" {
			return invalid, fmt.Errorf("%w: server %s must be deployed before it can join a cluster",
				clusterdomain.ErrInvalidDeployment, server.DisplayName())
		}
		if s.protection == nil && server.Provisioning.Locked {
			return invalid, &serverdomain.ServerLockedError{Name: server.DisplayName()}
		}
		if claimedBy := availability.claims[server.ID]; claimedBy != nil {
			return invalid, fmt.Errorf(
				"%w: server %s is already claimed by Kubernetes Cluster %s",
				clusterdomain.ErrInvalidDeployment, server.DisplayName(), claimedBy.Name,
			)
		}
		if server.Membership != nil {
			clusterName := server.Membership.ClusterID
			clusterType := "unknown"
			if existing := availability.clustersByID[server.Membership.ClusterID]; existing != nil {
				clusterName = existing.Name
				clusterType = string(existing.Type)
			}
			return invalid, fmt.Errorf(
				"%w: server %s already reports membership in %s Cluster %s as %s",
				clusterdomain.ErrInvalidDeployment,
				server.DisplayName(),
				clusterType,
				clusterName,
				server.Membership.Role,
			)
		}
		if assignment.Role == clusterdomain.NodeRoleControlPlane {
			controllers++
		}
		targets = append(targets, server)
	}

	if s.protection != nil {
		if err := s.protection.RequireUnlocked(ctx, serverIDs(targets)); err != nil {
			return invalid, err
		}
	}

	if controllers != 1 && (controllers < minHAControllers || controllers%2 == 0) {
		return invalid, fmt.Errorf("%w: control-plane count must be one or an odd number of at least %d, got %d",
			clusterdomain.ErrInvalidDeployment, minHAControllers, controllers)
	}
	if len(spec.WorkloadServerIDs()) == 0 {
		return invalid, fmt.Errorf("%w: at least one Server must run workloads", clusterdomain.ErrInvalidDeployment)
	}

	if strings.TrimSpace(spec.K0sVersion) == "" {
		return invalid, fmt.Errorf("%w: k0sVersion is required", clusterdomain.ErrInvalidDeployment)
	}
	spec.K0sVersion = strings.TrimSpace(spec.K0sVersion)
	if spec.PodCIDR == "" {
		spec.PodCIDR = defaultPodCIDR
	}
	if spec.ServiceCIDR == "" {
		spec.ServiceCIDR = defaultServiceCIDR
	}

	podNet, err := netip.ParsePrefix(spec.PodCIDR)
	if err != nil {
		return invalid, fmt.Errorf("%w: podCidr is not a valid CIDR: %v", clusterdomain.ErrInvalidDeployment, err)
	}
	serviceNet, err := netip.ParsePrefix(spec.ServiceCIDR)
	if err != nil {
		return invalid, fmt.Errorf("%w: serviceCidr is not a valid CIDR: %v", clusterdomain.ErrInvalidDeployment, err)
	}

	if spec.HighlyAvailable() {
		spec.APIVIP = strings.TrimSpace(spec.APIVIP)
		if spec.APIVIPPrefix == 0 {
			spec.APIVIPPrefix = defaultAPIVIPPrefix
		}
	}

	apiAddress, vip, err := deploymentAPIAddress(spec, targets)
	if err != nil {
		return invalid, err
	}

	// Node addresses must remain outside cluster networks. In HA, the VIP must also be
	// unassigned so keepalived can claim it without conflicting with a Server address.
	for _, server := range targets {
		for _, raw := range server.Observed.Addresses {
			addr, err := netip.ParseAddr(raw)
			if err != nil {
				continue
			}
			if podNet.Contains(addr) {
				return invalid, fmt.Errorf("%w: podCidr %s covers node address %s (%s)",
					clusterdomain.ErrInvalidDeployment, spec.PodCIDR, raw, server.DisplayName())
			}
			if serviceNet.Contains(addr) {
				return invalid, fmt.Errorf("%w: serviceCidr %s covers node address %s (%s)",
					clusterdomain.ErrInvalidDeployment, spec.ServiceCIDR, raw, server.DisplayName())
			}
			if vip.IsValid() && addr == vip {
				return invalid, fmt.Errorf("%w: apiVip %s is already a node address (%s)",
					clusterdomain.ErrInvalidDeployment, spec.APIVIP, server.DisplayName())
			}
		}
	}

	return validatedDeployment{spec: spec, targets: targets, apiAddress: apiAddress}, nil
}

// targetAvailability resolves all claims in one lifecycle batch before any Cluster record
// is created. Non-uninstalled deployment claims deliberately use the original target
// snapshot rather than current membership, which may be absent after a partial failure.
func (s *DeployService) targetAvailability(
	ctx context.Context,
	siteID string,
) (deploymentTargetAvailability, error) {
	availability := deploymentTargetAvailability{
		claims:       map[string]*clusterdomain.Cluster{},
		clustersByID: map[string]*clusterdomain.Cluster{},
	}
	clusters, err := s.clusters.List(ctx, siteID)
	if err != nil {
		return availability, err
	}
	clusterIDs := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		availability.clustersByID[cluster.ID] = cluster
		clusterIDs = append(clusterIDs, cluster.ID)
	}
	snapshots, err := s.lifecycle.Read(ctx, clusterIDs)
	if err != nil {
		return availability, err
	}
	for _, cluster := range clusters {
		snapshot := snapshots[cluster.ID]
		if snapshot.Origin != clusterdomain.ClusterOriginDeployed ||
			snapshot.State == clusterdomain.ClusterLifecycleUninstalled ||
			snapshot.Deployment == nil {
			continue
		}
		for _, serverID := range snapshot.Deployment.TargetServerIDs {
			availability.claims[serverID] = cluster
		}
	}
	return availability, nil
}

// deploymentAPIAddress enforces the conditional VIP contract and resolves the endpoint
// used by the playbook credential. Non-HA deployments use the initial control-plane
// Server's first valid observed address and therefore cannot start from an addressless host.
func deploymentAPIAddress(spec clusterdomain.DeploymentSpec, targets []*serverdomain.Server) (string, netip.Addr, error) {
	if spec.HighlyAvailable() {
		if spec.APIVIPPrefix == 0 {
			spec.APIVIPPrefix = defaultAPIVIPPrefix
		}
		if spec.APIVIPPrefix < 1 || spec.APIVIPPrefix > 32 {
			return "", netip.Addr{}, fmt.Errorf("%w: apiVipPrefix must be between 1 and 32", clusterdomain.ErrInvalidDeployment)
		}
		vip, err := netip.ParseAddr(strings.TrimSpace(spec.APIVIP))
		if err != nil {
			return "", netip.Addr{}, fmt.Errorf("%w: apiVip is required and must be a valid IP address for high availability", clusterdomain.ErrInvalidDeployment)
		}
		return vip.String(), vip, nil
	}

	if strings.TrimSpace(spec.APIVIP) != "" || spec.APIVIPPrefix != 0 {
		return "", netip.Addr{}, fmt.Errorf("%w: apiVip and apiVipPrefix are only valid for high availability", clusterdomain.ErrInvalidDeployment)
	}
	initialID := spec.ControlPlaneServerIDs()[0]
	for _, server := range targets {
		if server.ID != initialID {
			continue
		}
		for _, raw := range server.Observed.Addresses {
			if addr, err := netip.ParseAddr(raw); err == nil {
				return addr.String(), netip.Addr{}, nil
			}
		}
		return "", netip.Addr{}, fmt.Errorf("%w: initial control-plane Server %s has no valid address",
			clusterdomain.ErrInvalidDeployment, server.DisplayName())
	}
	return "", netip.Addr{}, fmt.Errorf("%w: initial control-plane Server was not resolved", clusterdomain.ErrInvalidDeployment)
}

// buildDeploymentVars translates validated intent into the trusted variables read by the
// release playbook. The role map is keyed by serverId, matching dynamic inventory.
func buildDeploymentVars(cluster *clusterdomain.Cluster, spec clusterdomain.DeploymentSpec, apiAddress string) map[string]any {
	roles := make(map[string]any, len(spec.RoleAssignments))
	workloadControllers := make([]string, 0, len(spec.RoleAssignments))
	for _, assignment := range spec.RoleAssignments {
		roles[assignment.ServerID] = string(assignment.Role)
		if assignment.Role == clusterdomain.NodeRoleControlPlane && assignment.RunWorkloads {
			workloadControllers = append(workloadControllers, assignment.ServerID)
		}
	}
	controllers := spec.ControlPlaneServerIDs()
	return map[string]any{
		varK0sVersion:               spec.K0sVersion,
		varK0sPodCIDR:               spec.PodCIDR,
		varK0sServiceCIDR:           spec.ServiceCIDR,
		varK0sHighAvailability:      spec.HighlyAvailable(),
		varK0sAPIAddress:            apiAddress,
		varK0sAPIVIP:                spec.APIVIP,
		varK0sAPIVIPPrefix:          spec.APIVIPPrefix,
		varK0sClusterName:           cluster.Name,
		varK0sRoles:                 roles,
		varK0sControllerIDs:         controllers,
		varK0sWorkerIDs:             spec.WorkerServerIDs(),
		varK0sWorkloadIDs:           spec.WorkloadServerIDs(),
		varK0sWorkloadControllerIDs: workloadControllers,
		varK0sInitialController:     controllers[0],
	}
}

func serverIDs(servers []*serverdomain.Server) []string {
	ids := make([]string, len(servers))
	for i, server := range servers {
		ids[i] = server.ID
	}
	return ids
}

// generateVRRPPassword returns an eight-character password for an HA control-plane VIP.
// keepalived ignores characters after the first eight, so four random bytes are encoded.
func generateVRRPPassword() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate vrrp password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
