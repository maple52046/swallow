package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
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
	varK0sPlatformName          = "swallow_k0s_platform_name"
	varK0sRoles                 = "swallow_k0s_roles"
	varK0sControllerIDs         = "swallow_k0s_controller_ids"
	varK0sWorkerIDs             = "swallow_k0s_worker_ids"
	varK0sWorkloadIDs           = "swallow_k0s_workload_ids"
	varK0sWorkloadControllerIDs = "swallow_k0s_workload_controller_ids"
	varK0sInitialController     = "swallow_k0s_initial_controller_id"
	varK0sEphemeralRoot         = "swallow_k0s_ephemeral_root"
	// varK0sVRRPAuthPass is sealed at rest and materialised only for an HA run.
	varK0sVRRPAuthPass = "swallow_k0s_vrrp_auth_pass"
)

// Trusted extra-var names the deploy operation passes to the Slurm playbook. Like the k0s
// vars they use the swallow_ prefix so a client cannot forge them. Node roles are expressed
// as ordered id lists (per-daemon), not a single role map, because a Server may run both
// slurmctld and slurmd.
const (
	varSlurmClusterName         = "swallow_slurm_cluster_name"
	varSlurmPlatformName        = "swallow_slurm_platform_name"
	varSlurmControllerIDs       = "swallow_slurm_controller_ids"
	varSlurmComputeIDs          = "swallow_slurm_compute_ids"
	varSlurmLoginIDs            = "swallow_slurm_login_ids"
	varSlurmPrimaryController   = "swallow_slurm_primary_controller_id"
	varSlurmHighAvailability    = "swallow_slurm_high_availability"
	varSlurmStateSaveLocation   = "swallow_slurm_state_save_location"
	varSlurmControllerStateMode = "swallow_slurm_controller_state_mode"
	varSlurmStateServer         = "swallow_slurm_state_server_id"
	varSlurmStateExport         = "swallow_slurm_state_export"
	varSlurmLoginConfigMode     = "swallow_slurm_login_config_mode"
	varSlurmLoginUseSackd       = "swallow_slurm_login_use_sackd"
	varSlurmWorkloadEnabled     = "swallow_slurm_workload_enabled"
	varSlurmWorkloadMode        = "swallow_slurm_workload_mode"
	varSlurmWorkloadMountPath   = "swallow_slurm_workload_mount_path"
	varSlurmWorkloadFstype      = "swallow_slurm_workload_fstype"
	varSlurmWorkloadMountOpts   = "swallow_slurm_workload_mount_options"
	varSlurmWorkloadServer      = "swallow_slurm_workload_server_id"
	varSlurmWorkloadExport      = "swallow_slurm_workload_export"
	varSlurmWorkloadSource      = "swallow_slurm_workload_source"
	varSlurmAPIVersion          = "swallow_slurm_api_version"
)

// DeployService owns the intent to build a platform: it validates the topology, creates the
// platform with no integration yet, and delegates execution to the operation context.
type DeployService struct {
	platformService    *PlatformService
	platforms          platformdomain.PlatformRepository
	servers            serverdomain.ServerRepository
	lifecycle          platformdomain.LifecycleReader
	launcher           platformdomain.DeploymentLauncher
	protection         serverdomain.MutationGuard
	machinePreparation platformdomain.MachinePreparationValidator
	requirements       platformdomain.DeploymentRequirementReader
	deploymentKeys     platformdomain.DeploymentKeyChecker
}

// NewDeployService constructs deployment preflight with both Server observations and the
// batch lifecycle reader. The lifecycle dependency must preserve original target snapshots
// so partial deployments cannot be assigned to a second Platform.
func NewDeployService(
	platformService *PlatformService,
	platforms platformdomain.PlatformRepository,
	servers serverdomain.ServerRepository,
	lifecycle platformdomain.LifecycleReader,
	launcher platformdomain.DeploymentLauncher,
	protection ...serverdomain.MutationGuard,
) *DeployService {
	service := &DeployService{
		platformService: platformService, platforms: platforms,
		servers: servers, lifecycle: lifecycle, launcher: launcher,
	}
	if len(protection) > 0 {
		service.protection = protection[0]
	}
	return service
}

// DeployPlatformInput is accepted by POST /platforms/deploy.
//
// Type selects which platform is built and therefore which spec is read: Spec for
// PlatformTypeKubernetes (k0s) and SlurmSpec for PlatformTypeSlurm. An empty Type defaults
// to Kubernetes so existing callers that predate the type field keep working. The other
// spec is ignored for the type that is not selected.
type DeployPlatformInput struct {
	SiteID             string
	Name               string
	Type               platformdomain.PlatformType
	GPUStackOwner      string
	Spec               platformdomain.DeploymentSpec
	SlurmSpec          platformdomain.SlurmDeploymentSpec
	RequestedBy        string
	RequestCorrelation string
	MachinePreparation platformdomain.MachinePreparation
}

// DeployPlatformResult is the accepted deployment: the created platform and the operation
// that is building it.
type DeployPlatformResult struct {
	PlatformID  string `json:"platformId"`
	OperationID string `json:"operationId"`
}

// validatedDeployment keeps provider observations out of the domain DeploymentSpec while
// carrying the resolved API address from preflight into trusted playbook variables. slurmSpec
// carries the normalized Slurm intent (defaults applied) when the deployment is a Slurm one;
// spec/apiAddress are the Kubernetes equivalents.
type validatedDeployment struct {
	spec               platformdomain.DeploymentSpec
	slurmSpec          platformdomain.SlurmDeploymentSpec
	targets            []*serverdomain.Server
	apiAddress         string
	machinePreparation platformdomain.MachinePreparation
}

// deploymentTargetAvailability combines intent-owned deployment claims with observed
// membership. The former protects partial deployments before a platform API is reachable;
// the latter protects externally registered platforms that have no deployment provenance.
type deploymentTargetAvailability struct {
	claims        map[string]*platformdomain.Platform
	platformsByID map[string]*platformdomain.Platform
}

// Deploy validates the topology, creates the platform, and launches the build operation.
//
// The platform is created before the operation so the operation can carry its id, and is
// deleted again if the launch is rejected, so a failed request leaves no orphan platform.
// AttachMachinePreparationValidator adds provisioner preflight without coupling Platform to provisioning models.
func (s *DeployService) AttachMachinePreparationValidator(validator platformdomain.MachinePreparationValidator) {
	s.machinePreparation = validator
}

// AttachDeploymentKeyChecker makes every deploy, existing_os or provision_os, require the
// installation's Deployment Key before any validation, Platform record, or Workflow is created.
// Without it (tests) the check is skipped.
func (s *DeployService) AttachDeploymentKeyChecker(checker platformdomain.DeploymentKeyChecker) {
	s.deploymentKeys = checker
}

// AttachDeploymentRequirementReader enables authoritative resource eligibility checks for
// Slurm. Repository failures are returned before any Platform or Workflow is created.
func (s *DeployService) AttachDeploymentRequirementReader(reader platformdomain.DeploymentRequirementReader) {
	s.requirements = reader
}

// Deploy dispatches on the requested platform type. An empty type defaults to Kubernetes so
// callers that predate the type field are unaffected; an unknown type is a domain error the
// delivery layer turns into a 400.
func (s *DeployService) Deploy(ctx context.Context, input DeployPlatformInput) (*DeployPlatformResult, error) {
	if s.deploymentKeys != nil {
		exists, err := s.deploymentKeys.HasDeploymentKey(ctx)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, platformdomain.ErrDeploymentKeyMissing
		}
	}
	platformType := input.Type
	if platformType == "" {
		platformType = platformdomain.PlatformTypeKubernetes
	}
	switch platformType {
	case platformdomain.PlatformTypeKubernetes:
		return s.deployKubernetes(ctx, input)
	case platformdomain.PlatformTypeSlurm:
		return s.deploySlurm(ctx, input)
	default:
		return nil, fmt.Errorf("%w: %q", platformdomain.ErrUnsupportedPlatformType, platformType)
	}
}

func (s *DeployService) deployKubernetes(ctx context.Context, input DeployPlatformInput) (*DeployPlatformResult, error) {
	validated, err := s.validate(ctx, input)
	if err != nil {
		return nil, err
	}

	created, err := s.platformService.Create(ctx, CreatePlatformInput{
		SiteID:        input.SiteID,
		Name:          input.Name,
		Type:          string(platformdomain.PlatformTypeKubernetes),
		GPUStackOwner: input.GPUStackOwner,
	})
	if err != nil {
		return nil, err
	}

	platform, err := s.platforms.FindByID(ctx, created.ID)
	if err != nil {
		return nil, err
	}

	secretVars := map[string]any{}
	if validated.spec.HighlyAvailable() {
		authPass, err := generateVRRPPassword()
		if err != nil {
			_ = s.platforms.Delete(ctx, platform.ID)
			return nil, err
		}
		secretVars[varK0sVRRPAuthPass] = authPass
	}

	trustedVars := buildDeploymentVars(platform, validated.spec, validated.apiAddress)
	if validated.machinePreparation.Mode == platformdomain.MachinePreparationProvisionOS &&
		validated.machinePreparation.Ephemeral != nil && *validated.machinePreparation.Ephemeral {
		// An overlayfs containerd snapshotter cannot reliably nest inside MAAS overlayroot.
		// Pass only the derived, trusted boolean; clients cannot inject playbook variables.
		trustedVars[varK0sEphemeralRoot] = true
	}
	launch := platformdomain.DeploymentLaunch{
		Platform:        platform,
		TargetServerIDs: serverIDs(validated.targets),
		TrustedVars:     trustedVars,
		SecretVars:      secretVars,
		RequestedBy:     input.RequestedBy, RequestCorrelation: input.RequestCorrelation,
		MachinePreparation: validated.machinePreparation,
	}
	operationID, err := s.launcher.Launch(ctx, launch)
	if err != nil {
		// The operation was refused, so nothing is building this platform; remove it
		// rather than leave a platform that will never converge.
		_ = s.platforms.Delete(ctx, platform.ID)
		return nil, err
	}

	return &DeployPlatformResult{PlatformID: platform.ID, OperationID: operationID}, nil
}

// deploySlurm validates the Slurm topology, registers the platform, and launches the build
// operation. Like deployKubernetes it deletes the platform if the launch is refused so a
// rejected request leaves no orphan record. Slurm needs no launch-time secret: the playbook
// mints the shared MUNGE key and the slurmrestd JWT on the primary controller itself, and the
// resulting reader credential is recorded from the run's result file.
func (s *DeployService) deploySlurm(ctx context.Context, input DeployPlatformInput) (*DeployPlatformResult, error) {
	validated, err := s.validateSlurm(ctx, input)
	if err != nil {
		return nil, err
	}

	gpuStackOwner := strings.TrimSpace(input.GPUStackOwner)
	if gpuStackOwner == "" {
		// Slurm has no in-platform GPU operator, so drivers come from the OS image or
		// provisioning. Default the owner rather than forcing a Kubernetes-shaped choice.
		gpuStackOwner = string(platformdomain.GPUStackOwnerProvisioning)
	}

	created, err := s.platformService.Create(ctx, CreatePlatformInput{
		SiteID:        input.SiteID,
		Name:          input.Name,
		Type:          string(platformdomain.PlatformTypeSlurm),
		GPUStackOwner: gpuStackOwner,
	})
	if err != nil {
		return nil, err
	}

	platform, err := s.platforms.FindByID(ctx, created.ID)
	if err != nil {
		return nil, err
	}

	launch := platformdomain.DeploymentLaunch{
		Platform:           platform,
		TargetServerIDs:    validated.slurmSpec.ServerIDs(),
		TrustedVars:        buildSlurmVars(platform, validated.slurmSpec),
		RequestedBy:        input.RequestedBy,
		RequestCorrelation: input.RequestCorrelation,
		MachinePreparation: validated.machinePreparation,
	}
	operationID, err := s.launcher.Launch(ctx, launch)
	if err != nil {
		// The operation was refused, so nothing is building this platform; remove it.
		_ = s.platforms.Delete(ctx, platform.ID)
		return nil, err
	}
	return &DeployPlatformResult{PlatformID: platform.ID, OperationID: operationID}, nil
}

// validateSlurm resolves and checks the Slurm topology and targets before any record exists.
// It returns a normalized SlurmDeploymentSpec (ClusterName defaulted, values trimmed) and the
// resolved machine-preparation mode. It reuses the same per-target site/absent/state/lock/
// claim/membership checks as the Kubernetes path. Both platform types may use an ephemeral OS;
// their platform-specific automation owns the booted host compatibility checks.
func (s *DeployService) validateSlurm(ctx context.Context, input DeployPlatformInput) (validatedDeployment, error) {
	var invalid validatedDeployment
	spec := input.SlurmSpec
	preparation := input.MachinePreparation
	if preparation.Mode == "" {
		preparation.Mode = platformdomain.MachinePreparationExistingOS
	}
	if !preparation.Mode.Valid() {
		return invalid, fmt.Errorf("%w: machinePreparation.mode must be existing_os or provision_os", platformdomain.ErrInvalidDeployment)
	}
	if len(spec.NodeAssignments) == 0 {
		return invalid, fmt.Errorf("%w: at least one node assignment is required", platformdomain.ErrInvalidDeployment)
	}

	var minimum *platformdomain.MinimumResources
	if s.requirements != nil {
		requirement, err := s.requirements.FindByPlatformType(ctx, platformdomain.PlatformTypeSlurm)
		switch {
		case err == nil && requirement != nil:
			minimum = requirement.MinimumResources
		case err == nil:
			return invalid, fmt.Errorf("read Slurm deployment requirement: repository returned nil requirement")
		case errors.Is(err, platformdomain.ErrDeploymentRequirementNotFound):
			// A missing record is the defined disabled state.
		default:
			return invalid, fmt.Errorf("read Slurm deployment requirement: %w", err)
		}
	}

	availability, err := s.targetAvailability(ctx, input.SiteID)
	if err != nil {
		return invalid, err
	}

	targets := make([]*serverdomain.Server, 0, len(spec.NodeAssignments))
	// readyServerIDs are the targets that still need an OS in provision_os mode; only they
	// are provisioned and preflighted, so a batch may mix ready and already-deployed servers.
	readyServerIDs := make([]string, 0, len(spec.NodeAssignments))
	seenServer := map[string]bool{}
	controllers := 0
	computes := 0
	for _, assignment := range spec.NodeAssignments {
		if !assignment.Controller && !assignment.Compute && !assignment.Login {
			return invalid, fmt.Errorf("%w: server %s must have a role: slurmctld (controller), slurmd (compute), or login",
				platformdomain.ErrInvalidDeployment, assignment.ServerID)
		}
		if seenServer[assignment.ServerID] {
			return invalid, fmt.Errorf("%w: server %s is assigned more than once",
				platformdomain.ErrInvalidDeployment, assignment.ServerID)
		}
		seenServer[assignment.ServerID] = true

		server, err := s.servers.FindByID(ctx, assignment.ServerID)
		if err != nil {
			return invalid, err
		}
		if minimum != nil {
			if err := validateMinimumResources(server, *minimum); err != nil {
				return invalid, err
			}
		}
		if server.Source.SiteID != input.SiteID {
			return invalid, fmt.Errorf("%w: server %s is not at site %s",
				platformdomain.ErrInvalidDeployment, server.DisplayName(), input.SiteID)
		}
		if server.Absent {
			return invalid, fmt.Errorf("%w: server %s is absent from its provisioner",
				platformdomain.ErrInvalidDeployment, server.DisplayName())
		}
		state := ""
		if server.Provisioning != nil {
			state = server.Provisioning.State
		}
		switch preparation.Mode {
		case platformdomain.MachinePreparationProvisionOS:
			if state != "ready" && state != "deployed" {
				return invalid, fmt.Errorf("%w: server %s must be ready or deployed for machine preparation mode %s",
					platformdomain.ErrInvalidDeployment, server.DisplayName(), preparation.Mode)
			}
			if state == "ready" {
				readyServerIDs = append(readyServerIDs, server.ID)
			}
		default:
			if state != "deployed" {
				return invalid, fmt.Errorf("%w: server %s must be deployed for machine preparation mode %s",
					platformdomain.ErrInvalidDeployment, server.DisplayName(), preparation.Mode)
			}
		}
		if s.protection == nil && server.Provisioning != nil && server.Provisioning.Locked {
			return invalid, &serverdomain.ServerLockedError{Name: server.DisplayName()}
		}
		if claimedBy := availability.claims[server.ID]; claimedBy != nil {
			return invalid, fmt.Errorf(
				"%w: server %s is already claimed by %s Platform %s",
				platformdomain.ErrInvalidDeployment, server.DisplayName(), claimedBy.Type, claimedBy.Name,
			)
		}
		if server.Membership != nil {
			platformName := server.Membership.PlatformID
			platformType := "unknown"
			if existing := availability.platformsByID[server.Membership.PlatformID]; existing != nil {
				platformName = existing.Name
				platformType = string(existing.Type)
			}
			return invalid, fmt.Errorf(
				"%w: server %s already reports membership in %s Platform %s as %s",
				platformdomain.ErrInvalidDeployment,
				server.DisplayName(), platformType, platformName, server.Membership.Role,
			)
		}
		if assignment.Controller {
			controllers++
		}
		if assignment.Compute {
			computes++
		}
		targets = append(targets, server)
	}

	if s.protection != nil {
		if err := s.protection.RequireUnlocked(ctx, serverIDs(targets)); err != nil {
			return invalid, err
		}
	}
	if preparation.Mode == platformdomain.MachinePreparationProvisionOS && len(readyServerIDs) > 0 {
		if s.machinePreparation == nil {
			return invalid, fmt.Errorf("%w: OS provisioning is unavailable", platformdomain.ErrInvalidDeployment)
		}
		if err := s.machinePreparation.Validate(ctx, input.SiteID, readyServerIDs, preparation); err != nil {
			return invalid, err
		}
	}

	if controllers == 0 {
		return invalid, fmt.Errorf("%w: at least one server must run slurmctld (controller)", platformdomain.ErrInvalidDeployment)
	}
	if computes == 0 {
		return invalid, fmt.Errorf("%w: at least one server must run slurmd (compute)", platformdomain.ErrInvalidDeployment)
	}

	spec.ClusterName = strings.TrimSpace(spec.ClusterName)
	spec.APIVersion = strings.TrimSpace(spec.APIVersion)
	spec.StateSaveLocation = strings.TrimSpace(spec.StateSaveLocation)
	// A highly available (multi-controller) deploy needs one shared StateSaveLocation that
	// every backup slurmctld can recover from. swallow provisions this automatically (a managed
	// NFS export on the login node when one is assigned, otherwise an off-controller node,
	// mounted on every controller by the playbook), so stateSaveLocation is no longer required
	// from the operator; when supplied it overrides the state directory path. See buildSlurmVars.

	normalized, err := validateSlurmWorkloadStorage(spec)
	if err != nil {
		return invalid, err
	}
	spec.WorkloadStorage = normalized

	return validatedDeployment{slurmSpec: spec, targets: targets, machinePreparation: preparation}, nil
}

// defaultWorkloadMountPath is the shared workload filesystem mountpoint used when the operator
// does not override it. It is deliberately not /home: mounting an (initially empty) share over
// /home would hide the automation user's ~/.ssh and break SSH on every node.
const defaultWorkloadMountPath = "/shared"

// validateSlurmWorkloadStorage normalizes and checks the optional shared workload filesystem.
// A disabled spec passes through untouched. NFS is the only supported type. Self-hosted mode
// exports from the login node, so it requires one; external mode requires an operator NFS URL
// (host:/path). The mount path must be absolute and must not overlay /home or the root.
func validateSlurmWorkloadStorage(spec platformdomain.SlurmDeploymentSpec) (platformdomain.SlurmWorkloadStorageSpec, error) {
	ws := spec.WorkloadStorage
	if !ws.Enabled {
		return platformdomain.SlurmWorkloadStorageSpec{}, nil
	}
	if ws.Type == "" {
		ws.Type = platformdomain.SlurmStorageNFS
	}
	if ws.Type != platformdomain.SlurmStorageNFS {
		return ws, fmt.Errorf("%w: workload storage type %q is not supported (only nfs)", platformdomain.ErrInvalidDeployment, ws.Type)
	}
	ws.MountPath = strings.TrimSpace(ws.MountPath)
	if ws.MountPath == "" {
		ws.MountPath = defaultWorkloadMountPath
	}
	if !strings.HasPrefix(ws.MountPath, "/") || ws.MountPath == "/" || ws.MountPath == "/home" {
		return ws, fmt.Errorf("%w: workload storage mountPath must be an absolute path other than / or /home", platformdomain.ErrInvalidDeployment)
	}
	ws.NFSMountOptions = strings.TrimSpace(ws.NFSMountOptions)
	switch ws.Mode {
	case platformdomain.SlurmWorkloadStorageSelfHosted:
		if spec.PrimaryLoginID() == "" {
			return ws, fmt.Errorf("%w: self-hosted workload storage requires a login node to export it", platformdomain.ErrInvalidDeployment)
		}
		ws.NFSURL = ""
	case platformdomain.SlurmWorkloadStorageExternal:
		ws.NFSURL = strings.TrimSpace(ws.NFSURL)
		if !isNFSURL(ws.NFSURL) {
			return ws, fmt.Errorf("%w: external workload storage requires an NFS url of the form host:/path", platformdomain.ErrInvalidDeployment)
		}
	default:
		return ws, fmt.Errorf("%w: workload storage mode must be self-hosted or external", platformdomain.ErrInvalidDeployment)
	}
	return ws, nil
}

// isNFSURL reports whether value is a host:/absolute-path NFS source, for example
// "10.0.0.9:/export/data". It is a shape check, not a reachability check.
func isNFSURL(value string) bool {
	host, path, found := strings.Cut(value, ":")
	return found && strings.TrimSpace(host) != "" && strings.HasPrefix(path, "/")
}

// validate resolves and checks topology, targets, and networks before any record exists.
// It returns a normalized spec and an API address derived from either the HA VIP or the
// initial control-plane Server observation.
func (s *DeployService) validate(ctx context.Context, input DeployPlatformInput) (validatedDeployment, error) {
	spec := input.Spec
	preparation := input.MachinePreparation
	if preparation.Mode == "" {
		preparation.Mode = platformdomain.MachinePreparationExistingOS
	}
	if !preparation.Mode.Valid() {
		return validatedDeployment{}, fmt.Errorf("%w: machinePreparation.mode must be existing_os or provision_os", platformdomain.ErrInvalidDeployment)
	}
	var invalid validatedDeployment

	if len(spec.RoleAssignments) == 0 {
		return invalid, fmt.Errorf("%w: at least one role assignment is required", platformdomain.ErrInvalidDeployment)
	}

	availability, err := s.targetAvailability(ctx, input.SiteID)
	if err != nil {
		return invalid, err
	}

	targets := make([]*serverdomain.Server, 0, len(spec.RoleAssignments))
	// readyServerIDs are the targets that still need an OS. In provision_os mode a deploy
	// may mix these with already-deployed servers (ADR 017 convergence); only the ready
	// ones are provisioned and preflighted.
	readyServerIDs := make([]string, 0, len(spec.RoleAssignments))
	seenServer := map[string]bool{}
	controllers := 0
	for _, assignment := range spec.RoleAssignments {
		if !assignment.Role.Valid() {
			return invalid, fmt.Errorf("%w: role must be control-plane or worker", platformdomain.ErrInvalidDeployment)
		}
		if assignment.Role == platformdomain.NodeRoleWorker && assignment.RunWorkloads {
			return invalid, fmt.Errorf("%w: runWorkloads is only valid for a control-plane assignment", platformdomain.ErrInvalidDeployment)
		}
		if seenServer[assignment.ServerID] {
			return invalid, fmt.Errorf("%w: server %s is assigned more than once",
				platformdomain.ErrInvalidDeployment, assignment.ServerID)
		}
		seenServer[assignment.ServerID] = true

		server, err := s.servers.FindByID(ctx, assignment.ServerID)
		if err != nil {
			return invalid, err
		}
		if server.Source.SiteID != input.SiteID {
			return invalid, fmt.Errorf("%w: server %s is not at site %s",
				platformdomain.ErrInvalidDeployment, server.DisplayName(), input.SiteID)
		}
		if server.Absent {
			return invalid, fmt.Errorf("%w: server %s is absent from its provisioner",
				platformdomain.ErrInvalidDeployment, server.DisplayName())
		}
		state := ""
		if server.Provisioning != nil {
			state = server.Provisioning.State
		}
		switch preparation.Mode {
		case platformdomain.MachinePreparationProvisionOS:
			// Convergent deploy (ADR 017): a provision_os batch may mix already-deployed
			// servers (used as-is) with `ready` servers (provisioned to `deployed` first).
			// Any other state is rejected because swallow only converges from these two.
			if state != "ready" && state != "deployed" {
				return invalid, fmt.Errorf("%w: server %s must be ready or deployed for machine preparation mode %s",
					platformdomain.ErrInvalidDeployment, server.DisplayName(), preparation.Mode)
			}
			if state == "ready" {
				readyServerIDs = append(readyServerIDs, server.ID)
			}
		default:
			// existing_os requires every target to already carry an OS.
			if state != "deployed" {
				return invalid, fmt.Errorf("%w: server %s must be deployed for machine preparation mode %s",
					platformdomain.ErrInvalidDeployment, server.DisplayName(), preparation.Mode)
			}
		}
		if s.protection == nil && server.Provisioning.Locked {
			return invalid, &serverdomain.ServerLockedError{Name: server.DisplayName()}
		}
		if claimedBy := availability.claims[server.ID]; claimedBy != nil {
			return invalid, fmt.Errorf(
				"%w: server %s is already claimed by Kubernetes Platform %s",
				platformdomain.ErrInvalidDeployment, server.DisplayName(), claimedBy.Name,
			)
		}
		if server.Membership != nil {
			platformName := server.Membership.PlatformID
			platformType := "unknown"
			if existing := availability.platformsByID[server.Membership.PlatformID]; existing != nil {
				platformName = existing.Name
				platformType = string(existing.Type)
			}
			return invalid, fmt.Errorf(
				"%w: server %s already reports membership in %s Platform %s as %s",
				platformdomain.ErrInvalidDeployment,
				server.DisplayName(),
				platformType,
				platformName,
				server.Membership.Role,
			)
		}
		if assignment.Role == platformdomain.NodeRoleControlPlane {
			controllers++
		}
		targets = append(targets, server)
	}

	if s.protection != nil {
		if err := s.protection.RequireUnlocked(ctx, serverIDs(targets)); err != nil {
			return invalid, err
		}
	}
	if preparation.Mode == platformdomain.MachinePreparationProvisionOS && len(readyServerIDs) > 0 {
		// Only servers that actually need an OS are preflighted; already-deployed targets
		// in the same batch are skipped so a mixed deploy is not rejected for them.
		if s.machinePreparation == nil {
			return invalid, fmt.Errorf("%w: OS provisioning is unavailable", platformdomain.ErrInvalidDeployment)
		}
		if err := s.machinePreparation.Validate(ctx, input.SiteID, readyServerIDs, preparation); err != nil {
			return invalid, err
		}
	}

	if controllers != 1 && (controllers < minHAControllers || controllers%2 == 0) {
		return invalid, fmt.Errorf("%w: control-plane count must be one or an odd number of at least %d, got %d",
			platformdomain.ErrInvalidDeployment, minHAControllers, controllers)
	}
	if len(spec.WorkloadServerIDs()) == 0 {
		return invalid, fmt.Errorf("%w: at least one Server must run workloads", platformdomain.ErrInvalidDeployment)
	}

	if strings.TrimSpace(spec.K0sVersion) == "" {
		return invalid, fmt.Errorf("%w: k0sVersion is required", platformdomain.ErrInvalidDeployment)
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
		return invalid, fmt.Errorf("%w: podCidr is not a valid CIDR: %v", platformdomain.ErrInvalidDeployment, err)
	}
	serviceNet, err := netip.ParsePrefix(spec.ServiceCIDR)
	if err != nil {
		return invalid, fmt.Errorf("%w: serviceCidr is not a valid CIDR: %v", platformdomain.ErrInvalidDeployment, err)
	}

	if spec.HighlyAvailable() {
		spec.APIVIP = strings.TrimSpace(spec.APIVIP)
		if spec.APIVIPPrefix == 0 {
			spec.APIVIPPrefix = defaultAPIVIPPrefix
		}
	}

	apiAddress, vip, err := deploymentAPIAddressForPreparation(spec, targets, preparation)
	if err != nil {
		return invalid, err
	}

	// Node addresses must remain outside platform networks. In HA, the VIP must also be
	// unassigned so keepalived can claim it without conflicting with a Server address.
	for _, server := range targets {
		for _, raw := range server.Observed.Addresses {
			addr, err := netip.ParseAddr(raw)
			if err != nil {
				continue
			}
			if podNet.Contains(addr) {
				return invalid, fmt.Errorf("%w: podCidr %s covers node address %s (%s)",
					platformdomain.ErrInvalidDeployment, spec.PodCIDR, raw, server.DisplayName())
			}
			if serviceNet.Contains(addr) {
				return invalid, fmt.Errorf("%w: serviceCidr %s covers node address %s (%s)",
					platformdomain.ErrInvalidDeployment, spec.ServiceCIDR, raw, server.DisplayName())
			}
			if vip.IsValid() && addr == vip {
				return invalid, fmt.Errorf("%w: apiVip %s is already a node address (%s)",
					platformdomain.ErrInvalidDeployment, spec.APIVIP, server.DisplayName())
			}
		}
	}

	return validatedDeployment{spec: spec, targets: targets, apiAddress: apiAddress, machinePreparation: preparation}, nil
}

// targetAvailability resolves all claims in one lifecycle batch before any Platform record
// is created. Non-uninstalled deployment claims deliberately use the original target
// snapshot rather than current membership, which may be absent after a partial failure.
func (s *DeployService) targetAvailability(
	ctx context.Context,
	siteID string,
) (deploymentTargetAvailability, error) {
	availability := deploymentTargetAvailability{
		claims:        map[string]*platformdomain.Platform{},
		platformsByID: map[string]*platformdomain.Platform{},
	}
	platforms, err := s.platforms.List(ctx, siteID)
	if err != nil {
		return availability, err
	}
	platformIDs := make([]string, 0, len(platforms))
	for _, platform := range platforms {
		availability.platformsByID[platform.ID] = platform
		platformIDs = append(platformIDs, platform.ID)
	}
	snapshots, err := s.lifecycle.Read(ctx, platformIDs)
	if err != nil {
		return availability, err
	}
	for _, platform := range platforms {
		snapshot := snapshots[platform.ID]
		if snapshot.Origin != platformdomain.PlatformOriginDeployed ||
			snapshot.State == platformdomain.PlatformLifecycleUninstalled ||
			snapshot.Deployment == nil {
			continue
		}
		for _, serverID := range snapshot.Deployment.TargetServerIDs {
			availability.claims[serverID] = platform
		}
	}
	return availability, nil
}

// deploymentAPIAddress enforces the conditional VIP contract and resolves the endpoint
// used by the playbook credential. Non-HA deployments use the initial control-plane
// Server's first valid observed address and therefore cannot start from an addressless host.
func deploymentAPIAddress(spec platformdomain.DeploymentSpec, targets []*serverdomain.Server) (string, netip.Addr, error) {
	if spec.HighlyAvailable() {
		if spec.APIVIPPrefix == 0 {
			spec.APIVIPPrefix = defaultAPIVIPPrefix
		}
		if spec.APIVIPPrefix < 1 || spec.APIVIPPrefix > 32 {
			return "", netip.Addr{}, fmt.Errorf("%w: apiVipPrefix must be between 1 and 32", platformdomain.ErrInvalidDeployment)
		}
		vip, err := netip.ParseAddr(strings.TrimSpace(spec.APIVIP))
		if err != nil {
			return "", netip.Addr{}, fmt.Errorf("%w: apiVip is required and must be a valid IP address for high availability", platformdomain.ErrInvalidDeployment)
		}
		return vip.String(), vip, nil
	}

	if strings.TrimSpace(spec.APIVIP) != "" || spec.APIVIPPrefix != 0 {
		return "", netip.Addr{}, fmt.Errorf("%w: apiVip and apiVipPrefix are only valid for high availability", platformdomain.ErrInvalidDeployment)
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
			platformdomain.ErrInvalidDeployment, server.DisplayName())
	}
	return "", netip.Addr{}, fmt.Errorf("%w: initial control-plane Server was not resolved", platformdomain.ErrInvalidDeployment)
}

// buildDeploymentVars translates validated intent into the trusted variables read by the
// release playbook. The role map is keyed by serverId, matching dynamic inventory.
func buildDeploymentVars(platform *platformdomain.Platform, spec platformdomain.DeploymentSpec, apiAddress string) map[string]any {
	roles := make(map[string]any, len(spec.RoleAssignments))
	workloadControllers := make([]string, 0, len(spec.RoleAssignments))
	for _, assignment := range spec.RoleAssignments {
		roles[assignment.ServerID] = string(assignment.Role)
		if assignment.Role == platformdomain.NodeRoleControlPlane && assignment.RunWorkloads {
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
		varK0sPlatformName:          platform.Name,
		varK0sRoles:                 roles,
		varK0sControllerIDs:         controllers,
		varK0sWorkerIDs:             spec.WorkerServerIDs(),
		varK0sWorkloadIDs:           spec.WorkloadServerIDs(),
		varK0sWorkloadControllerIDs: workloadControllers,
		varK0sInitialController:     controllers[0],
	}
}

// buildSlurmVars translates validated Slurm intent into the trusted variables read by the
// deploy-slurm playbook. Node roles are ordered id lists keyed by serverId (matching dynamic
// inventory), with the first controller as the primary. ClusterName falls back to a sanitized
// platform name.
//
// For an HA (multi-controller) deploy swallow provisions the shared StateSaveLocation itself:
// it sets controller-state mode to "shared" and names a state server (see slurmStateServerID)
// that the playbook turns into a managed NFS export mounted on every controller. A single
// controller stays "local" with a controller-local StateSaveLocation. Optional vars are
// omitted when empty so the playbook applies its own defaults; an operator-supplied
// stateSaveLocation, when present, overrides the state directory path in either mode.
func buildSlurmVars(platform *platformdomain.Platform, spec platformdomain.SlurmDeploymentSpec) map[string]any {
	vars := map[string]any{
		varSlurmClusterName:       slurmClusterName(spec.ClusterName, platform.Name),
		varSlurmPlatformName:      platform.Name,
		varSlurmControllerIDs:     spec.ControllerServerIDs(),
		varSlurmComputeIDs:        spec.ComputeServerIDs(),
		varSlurmLoginIDs:          spec.LoginServerIDs(),
		varSlurmPrimaryController: spec.PrimaryControllerID(),
		varSlurmHighAvailability:  spec.HighlyAvailable(),
	}
	if len(spec.LoginServerIDs()) > 0 {
		// Login hosts are pure submission/client nodes here, so they use the cluster's
		// configless discovery with a sackd-managed cache by default.
		vars[varSlurmLoginConfigMode] = "configless"
		vars[varSlurmLoginUseSackd] = true
	}
	if spec.HighlyAvailable() {
		vars[varSlurmControllerStateMode] = "shared"
		vars[varSlurmStateServer] = slurmStateServerID(spec)
		vars[varSlurmStateExport] = "/srv/slurm-state"
	} else {
		vars[varSlurmControllerStateMode] = "local"
	}
	if spec.StateSaveLocation != "" {
		vars[varSlurmStateSaveLocation] = spec.StateSaveLocation
	}
	addSlurmWorkloadVars(vars, spec)
	if spec.APIVersion != "" {
		vars[varSlurmAPIVersion] = spec.APIVersion
	}
	return vars
}

// addSlurmWorkloadVars sets the trusted vars for the optional shared workload filesystem. When
// disabled it records only the disabled flag. Self-hosted mode names the login node as the NFS
// server and a fixed export path; external mode passes the operator NFS source through. The
// playbook mounts it on every node at the mount path with the guarded builtin mount.
func addSlurmWorkloadVars(vars map[string]any, spec platformdomain.SlurmDeploymentSpec) {
	ws := spec.WorkloadStorage
	if !ws.Enabled {
		vars[varSlurmWorkloadEnabled] = false
		return
	}
	vars[varSlurmWorkloadEnabled] = true
	vars[varSlurmWorkloadMode] = string(ws.Mode)
	vars[varSlurmWorkloadMountPath] = ws.MountPath
	switch ws.Mode {
	case platformdomain.SlurmWorkloadStorageSelfHosted:
		// Ephemeral roots need a standalone tmpfs below the OverlayFS root because overlay
		// itself is not exportable. That export is directly addressable via NFSv3; NFSv4
		// would need a separately managed pseudo-root namespace. Keep this protocol choice
		// scoped to the Swallow-managed server—external storage retains NFSv4 below.
		vars[varSlurmWorkloadFstype] = "nfs"
		if ws.NFSMountOptions == "" {
			vars[varSlurmWorkloadMountOpts] = "rw,_netdev,hard,timeo=600,retrans=2,vers=3"
		} else {
			vars[varSlurmWorkloadMountOpts] = ws.NFSMountOptions
		}
		vars[varSlurmWorkloadServer] = spec.PrimaryLoginID()
		vars[varSlurmWorkloadExport] = "/srv/slurm-workspace"
	case platformdomain.SlurmWorkloadStorageExternal:
		vars[varSlurmWorkloadFstype] = "nfs4"
		if ws.NFSMountOptions != "" {
			vars[varSlurmWorkloadMountOpts] = ws.NFSMountOptions
		}
		vars[varSlurmWorkloadSource] = ws.NFSURL
	}
}

// slurmStateServerID selects the host that exports the shared controller StateSaveLocation for
// an HA cluster. It prefers a login node when one is assigned (the login/NFS host in the
// reference HA topology), then a compute-only node (compute and not a controller) so controller
// state lives off the controllers — a controller-host loss then does not also lose state, and
// no controller runs a kernel NFS loopback mount. When every node is also a controller it falls
// back to the primary controller. The chosen host is a single storage failure domain (a
// documented lab limitation, not storage HA). Assumes at least one controller exists, which
// deploy validation guarantees.
func slurmStateServerID(spec platformdomain.SlurmDeploymentSpec) string {
	if login := spec.PrimaryLoginID(); login != "" {
		return login
	}
	controllers := make(map[string]bool, len(spec.NodeAssignments))
	for _, id := range spec.ControllerServerIDs() {
		controllers[id] = true
	}
	for _, id := range spec.ComputeServerIDs() {
		if !controllers[id] {
			return id
		}
	}
	return spec.PrimaryControllerID()
}

// slurmClusterName produces a Slurm ClusterName from the requested value, falling back to the
// platform name. Slurm stores ClusterName lower-cased and dislikes whitespace and separators,
// so the result is lower-cased with any character outside [a-z0-9_-] replaced by '-'. An empty
// result (for example an all-symbol name) becomes "slurm" so slurm.conf always has a value.
func slurmClusterName(requested, fallback string) string {
	name := strings.TrimSpace(requested)
	if name == "" {
		name = strings.TrimSpace(fallback)
	}
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	sanitized := strings.Trim(b.String(), "-")
	if sanitized == "" {
		return "slurm"
	}
	return sanitized
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

func deploymentAPIAddressForPreparation(spec platformdomain.DeploymentSpec, targets []*serverdomain.Server, preparation platformdomain.MachinePreparation) (string, netip.Addr, error) {
	if preparation.Mode != platformdomain.MachinePreparationProvisionOS || spec.HighlyAvailable() {
		return deploymentAPIAddress(spec, targets)
	}
	initialID := spec.ControlPlaneServerIDs()[0]
	if preparation.NetworkMode == "static" {
		for _, assignment := range preparation.Assignments {
			if assignment.ServerID != initialID {
				continue
			}
			address, err := netip.ParseAddr(strings.TrimSpace(assignment.IPAddress))
			if err != nil {
				return "", netip.Addr{}, fmt.Errorf("%w: initial control-plane static IP is invalid", platformdomain.ErrInvalidDeployment)
			}
			return address.String(), netip.Addr{}, nil
		}
		return "", netip.Addr{}, fmt.Errorf("%w: initial control-plane static IP is required", platformdomain.ErrInvalidDeployment)
	}
	address, vip, err := deploymentAPIAddress(spec, targets)
	if err == nil {
		return address, vip, nil
	}
	// DHCP addresses are learned after MAAS completes. The playbook resolves the initial
	// control-plane inventory address at execution time.
	return "", netip.Addr{}, nil
}
