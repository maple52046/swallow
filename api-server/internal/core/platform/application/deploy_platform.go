package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
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
	// varSlurmGPUComputeIDs lists the compute Servers whose GPU Inventory has AMD compute GPUs.
	// The playbook loads amdgpu on them and configures their GPU GRES (decision 063); a
	// listed host whose KFD topology shows no GPU fails the run instead of joining without GRES.
	varSlurmGPUComputeIDs = "swallow_slurm_gpu_compute_ids"
	// varSlurmGPUGresType names the GRES type of every GPU node; empty derives it per node from the
	// GPU architecture. Always sent so a rerun converges both ways.
	varSlurmGPUGresType = "swallow_slurm_gpu_gres_type"
	// varSlurmOmnistatEnabled makes deploy-slurm render the Slurm side of Omnistat (Prolog/Epilog,
	// PrologFlags=Alloc, exclusive partition), so a redeploy re-applies it (decision 063).
	varSlurmOmnistatEnabled = "swallow_slurm_omnistat_enabled"
	// varSlurmDeviceMetricsExporterEnabled makes deploy-slurm install the Device Metrics Exporter's
	// job hook (decision 066), so a redeploy re-applies it.
	varSlurmDeviceMetricsExporterEnabled = "swallow_slurm_device_metrics_exporter_enabled"
	// varSlurmOCIContainers and varSlurmPMIx switch the native OCI container and host MPI features;
	// always sent so a rerun converges both ways.
	varSlurmOCIContainers = "swallow_slurm_oci_containers"
	varSlurmPMIx          = "swallow_slurm_pmix"
	// varGPUDriverModuleOptions is the operator's amdgpu module options ({"name": "value"}), read by
	// the shared amdgpu loader of every Workflow that loads the driver (decision 063). It is always
	// sent, possibly empty, so dropping an option converges on the next run.
	varGPUDriverModuleOptions = "swallow_gpu_driver_module_options"
)

// Trusted extra-var names of the configure-omnistat playbook (decision 063). The Prometheus and
// Grafana ports (swallow_omnistat_prometheus_port / _grafana_port) are added by the launcher from
// the software domain, which owns them.
const (
	varOmnistatVersion         = "swallow_omnistat_version"
	varOmnistatPort            = "swallow_omnistat_port"
	varOmnistatComputeIDs      = "swallow_omnistat_compute_ids"
	varOmnistatObservabilityID = "swallow_omnistat_observability_id"
	varOmnistatSystemName      = "swallow_omnistat_system_name"
)

// Trusted extra-var names the Slurm side lays over the composed device-metrics-exporter install
// (decisions 066 and 068), read by the deploy-device-metrics-exporter playbook. The Prometheus and
// Grafana ports are added by the launcher from the software domain, which owns them. The record id
// is the Workflow the Platform's record names, which the install's completion fences the GPU
// identity on; it survives a rerun that copies the Task under a new Workflow id.
const (
	varDeviceMetricsExporterVersion = "swallow_device_metrics_exporter_version"
	varDeviceMetricsExporterPort    = "swallow_device_metrics_exporter_port"
	// varDeviceMetricsExporterJobAttribution turns on Slurm job attribution; a standalone install
	// sends none.
	varDeviceMetricsExporterJobAttribution  = "swallow_device_metrics_exporter_job_attribution"
	varDeviceMetricsExporterComputeIDs      = "swallow_device_metrics_exporter_compute_ids"
	varDeviceMetricsExporterObservabilityID = "swallow_device_metrics_exporter_observability_id"
	varDeviceMetricsExporterClusterName     = "swallow_device_metrics_exporter_cluster_name"
	// VarDeviceMetricsExporterRecordID is read back by the install Task's completion.
	VarDeviceMetricsExporterRecordID = "swallow_device_metrics_exporter_record_id"
)

// DeployService owns the intent to build a platform: it validates the topology, creates the
// platform with no integration yet, and delegates execution to the operation context.
type DeployService struct {
	platformService    *PlatformService
	platforms          platformdomain.PlatformRepository
	servers            serverdomain.ServerRepository
	launcher           platformdomain.DeploymentLauncher
	protection         serverdomain.MutationGuard
	machinePreparation platformdomain.MachinePreparationValidator
	nodeSubnets        platformdomain.NodeSubnetResolver
	requirements       platformdomain.DeploymentRequirementReader
	deploymentKeys     platformdomain.DeploymentKeyChecker
	gpuOperators       platformdomain.GPUOperatorRecorder
	omnistats          platformdomain.OmnistatRecorder
	// deviceMetricsExporters is required to accept a deploy that requests the exporter.
	deviceMetricsExporters platformdomain.DeviceMetricsExporterRecorder
	// claims writes the accepted deployment's Platform Claims (decision 067); nil in tests.
	claims serverdomain.PlatformClaimStore
}

// NewDeployService constructs deployment preflight over Server observations. Which Platform
// already holds a target comes from the Servers' stored Platform Claims (AttachPlatformClaims),
// so a partial deployment cannot be assigned to a second Platform even before its platform API
// reports membership.
func NewDeployService(
	platformService *PlatformService,
	platforms platformdomain.PlatformRepository,
	servers serverdomain.ServerRepository,
	launcher platformdomain.DeploymentLauncher,
	protection ...serverdomain.MutationGuard,
) *DeployService {
	service := &DeployService{
		platformService: platformService, platforms: platforms,
		servers: servers, launcher: launcher,
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
//
// GPUOperator, when non-nil and enabled, installs the AMD GPU Operator in the same Workflow
// (Kubernetes only). GPUStackOwner may then be empty and is derived from it. ContainerRuntime,
// when set, installs that container runtime on every compute and login node, and Omnistat, when
// non-nil and enabled, installs Omnistat with Prometheus and Grafana, both in the same Workflow
// (Slurm only, decision 063). DeviceMetricsExporter, when non-nil and enabled, installs AMD's Device
// Metrics Exporter the same way instead (Slurm only, decision 066); it and Omnistat are mutually
// exclusive. GPUDriver, when non-nil, carries the operator's amdgpu module options for the
// Platform's GPU nodes (either type).
type DeployPlatformInput struct {
	SiteID           string
	Name             string
	Type             platformdomain.PlatformType
	GPUStackOwner    string
	GPUOperator      *platformdomain.GPUOperatorSettings
	GPUDriver        *platformdomain.GPUDriverSettings
	ContainerRuntime string
	Omnistat         *platformdomain.OmnistatSettings
	// DeviceMetricsExporter is the request's deviceMetricsExporter intent, nil when omitted.
	DeviceMetricsExporter *platformdomain.DeviceMetricsExporterSettings
	Spec                  platformdomain.DeploymentSpec
	SlurmSpec             platformdomain.SlurmDeploymentSpec
	RequestedBy           string
	RequestCorrelation    string
	MachinePreparation    platformdomain.MachinePreparation
	// TimeSyncCheck is the request's timeSyncCheck as sent; Deploy normalizes it (empty is enforce)
	// before any record is created and hands it to the launcher.
	TimeSyncCheck platformdomain.TimeSyncCheck
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

// deploymentTargetAvailability names the Platforms a target can be taken by: the one its stored
// Platform Claim names, which protects a partial deployment before its platform API is reachable,
// and the one it reports observed membership in, which protects an externally registered platform
// that has no deployment provenance.
type deploymentTargetAvailability struct {
	platformsByID map[string]*platformdomain.Platform
}

// AttachMachinePreparationValidator adds provisioner preflight without coupling Platform to provisioning models.
func (s *DeployService) AttachMachinePreparationValidator(validator platformdomain.MachinePreparationValidator) {
	s.machinePreparation = validator
}

// AttachNodeSubnetResolver lets an HA deploy take the API VIP prefix from the control plane's
// subnet and refuse a VIP outside it. Without it (tests, a provisioner without network reads)
// the requested prefix is used as given and defaults to 24.
func (s *DeployService) AttachNodeSubnetResolver(resolver platformdomain.NodeSubnetResolver) {
	s.nodeSubnets = resolver
}

// AttachDeploymentKeyChecker makes every deploy, existing_os or provision_os, require the
// installation's Deployment Key before any validation, Platform record, or Workflow is created.
// Without it (tests) the check is skipped.
func (s *DeployService) AttachDeploymentKeyChecker(checker platformdomain.DeploymentKeyChecker) {
	s.deploymentKeys = checker
}

// AttachGPUOperatorRecorder enables installing the GPU Operator as part of a Kubernetes deploy.
// Without it a deploy that requests the operator is refused instead of silently skipping it.
func (s *DeployService) AttachGPUOperatorRecorder(recorder platformdomain.GPUOperatorRecorder) {
	s.gpuOperators = recorder
}

// AttachOmnistatRecorder enables installing Omnistat as part of a Slurm deploy. Without it a
// deploy that requests Omnistat is refused instead of silently skipping it.
func (s *DeployService) AttachOmnistatRecorder(recorder platformdomain.OmnistatRecorder) {
	s.omnistats = recorder
}

// AttachDeviceMetricsExporterRecorder enables installing the Device Metrics Exporter as part of a
// Slurm deploy. Without it a deploy that requests the exporter is refused instead of silently
// skipping it.
func (s *DeployService) AttachDeviceMetricsExporterRecorder(recorder platformdomain.DeviceMetricsExporterRecorder) {
	s.deviceMetricsExporters = recorder
}

// AttachDeploymentRequirementReader enables authoritative resource eligibility checks for
// Slurm. Repository failures are returned before any Platform or Workflow is created.
func (s *DeployService) AttachDeploymentRequirementReader(reader platformdomain.DeploymentRequirementReader) {
	s.requirements = reader
}

// Deploy validates the topology, plans the build Workflow, and accepts the deployment. It
// dispatches on the requested platform type. An empty type defaults to Kubernetes so callers
// that predate the type field are unaffected; an unknown type is a domain error the delivery
// layer turns into a 400.
//
// Acceptance has two phases (see accept). Everything that reads a provider happens first and
// writes nothing: validation, and the launcher's PrepareDeployment against an unsaved Platform
// record. Then one store transaction claims the targets, stores the Platform and its feature
// records, and persists the Workflow, so a refusal at any point — including losing a claim to a
// concurrent deployment — leaves no Platform, claim, or Workflow behind (decisions 067, 069).
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
	timeSyncCheck, err := platformdomain.ParseTimeSyncCheck(string(input.TimeSyncCheck))
	if err != nil {
		return nil, err
	}
	input.TimeSyncCheck = timeSyncCheck
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
	if input.Omnistat != nil && input.Omnistat.Enabled {
		return nil, fmt.Errorf("%w: Omnistat is a Slurm Platform feature", platformdomain.ErrInvalidOmnistat)
	}
	if input.DeviceMetricsExporter != nil && input.DeviceMetricsExporter.Enabled {
		return nil, fmt.Errorf("%w: on Kubernetes the GPU Operator runs the Device Metrics Exporter (gpuOperator.metricsExporter)",
			platformdomain.ErrInvalidDeviceMetricsExporter)
	}
	if strings.TrimSpace(input.ContainerRuntime) != "" {
		return nil, fmt.Errorf("%w: a Kubernetes Platform runs containers on its own containerd", platformdomain.ErrInvalidContainerRuntime)
	}
	gpuDriver, err := normalizeDeployGPUDriver(input.GPUDriver)
	if err != nil {
		return nil, err
	}
	validated, err := s.validate(ctx, input)
	if err != nil {
		return nil, err
	}
	gpuOperator, gpuStackOwner, err := s.validateDeployGPUOperator(input, validated)
	if err != nil {
		return nil, err
	}

	platform, err := s.platformService.newPlatform(ctx, CreatePlatformInput{
		SiteID:        input.SiteID,
		Name:          input.Name,
		Type:          string(platformdomain.PlatformTypeKubernetes),
		GPUStackOwner: gpuStackOwner,
		GPUDriver:     gpuDriver,
	})
	if err != nil {
		return nil, err
	}
	targetIDs := serverIDs(validated.targets)
	// The Workflow id is assigned before planning, so a feature record can name the deploy
	// Workflow before it exists and the observer of that feature's Task matches from its first
	// transition.
	operationID := uuid.NewString()

	var records []featureRecord
	var gpuOperatorVars map[string]any
	if gpuOperator != nil {
		record := &platformdomain.GPUOperator{
			Settings: *gpuOperator, State: platformdomain.GPUOperatorInstalling,
			LastWorkflowID: operationID, UpdatedAt: time.Now().UTC(),
		}
		records = append(records, func(ctx context.Context) error {
			return s.gpuOperators.SetGPUOperator(ctx, platform.ID, record, platform.GPUStackOwner)
		})
		gpuOperatorVars = buildGPUOperatorVars(*gpuOperator, validated.spec.ControlPlaneServerIDs()[0], platform.GPUDriver)
	}

	secretVars := map[string]any{}
	if validated.spec.HighlyAvailable() {
		authPass, err := generateVRRPPassword()
		if err != nil {
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
		OperationID:     operationID,
		TargetServerIDs: targetIDs,
		TrustedVars:     trustedVars,
		SecretVars:      secretVars,
		GPUOperatorVars: gpuOperatorVars,
		RequestedBy:     input.RequestedBy, RequestCorrelation: input.RequestCorrelation,
		MachinePreparation: validated.machinePreparation,
		TimeSyncCheck:      input.TimeSyncCheck,
	}
	prepared, err := s.launcher.PrepareDeployment(ctx, launch)
	if err != nil {
		return nil, err
	}
	launchedID, err := s.accept(ctx, platform, validated.targets, targetIDs, records, prepared)
	if err != nil {
		return nil, err
	}
	return &DeployPlatformResult{PlatformID: platform.ID, OperationID: launchedID}, nil
}

// featureRecord writes one record a deployment carries besides the Platform itself (its GPU
// Operator, Omnistat, or Device Metrics Exporter state). It runs inside accept's transaction.
type featureRecord func(ctx context.Context) error

// accept is the second phase of Deploy: one store transaction that claims the targets, stores the
// Platform and its feature records, and persists the prepared Workflow. The claims come first, so
// a target lost to a concurrent deployment (whose transaction committed first, or conflicts with
// this one and wins on the retry) fails the unit before anything else is written. The caller has
// already done every provider read; nothing in here calls out of the store.
func (s *DeployService) accept(
	ctx context.Context,
	platform *platformdomain.Platform,
	targets []*serverdomain.Server,
	targetIDs []string,
	records []featureRecord,
	prepared platformdomain.PreparedWorkflow,
) (string, error) {
	var operationID string
	err := s.platformService.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.claimTargets(ctx, platform, targetIDs, targets); err != nil {
			return err
		}
		if err := s.platforms.Create(ctx, platform); err != nil {
			return err
		}
		for _, record := range records {
			if err := record(ctx); err != nil {
				return err
			}
		}
		id, err := s.launcher.PersistDeployment(ctx, prepared)
		operationID = id
		return err
	})
	if err != nil {
		return "", err
	}
	return operationID, nil
}

// validateDeployGPUOperator normalizes an optional GPU Operator request against the validated
// topology, before any record exists. It returns the normalized settings (nil when no operator
// is requested) and the effective gpuStackOwner: gpu-operator for an enabled operator, the
// requested owner otherwise (defaulting to provisioning). A provisioning owner with an enabled
// operator is contradictory and rejected. Whether any workload target has an AMD GPU is read from
// the targets' current GPU inventory.
func (s *DeployService) validateDeployGPUOperator(
	input DeployPlatformInput,
	validated validatedDeployment,
) (*platformdomain.GPUOperatorSettings, string, error) {
	owner := strings.TrimSpace(input.GPUStackOwner)
	if input.GPUOperator == nil || !input.GPUOperator.Enabled {
		if owner == "" {
			owner = string(platformdomain.GPUStackOwnerProvisioning)
		}
		return nil, owner, nil
	}
	if owner == string(platformdomain.GPUStackOwnerProvisioning) {
		return nil, "", fmt.Errorf("%w: gpuStackOwner provisioning contradicts an enabled gpuOperator", platformdomain.ErrInvalidGPUOperator)
	}
	if s.gpuOperators == nil {
		return nil, "", fmt.Errorf("%w: GPU Operator installation is unavailable", platformdomain.ErrInvalidGPUOperator)
	}
	kubernetes, ok := platformdomain.ParseKubernetesMinorVersion(validated.spec.K0sVersion)
	if !ok {
		return nil, "", fmt.Errorf("%w: k0sVersion %q does not name a Kubernetes version", platformdomain.ErrInvalidGPUOperator, validated.spec.K0sVersion)
	}
	byID := make(map[string]*serverdomain.Server, len(validated.targets))
	for _, server := range validated.targets {
		byID[server.ID] = server
	}
	hasGPU := false
	for _, id := range validated.spec.WorkloadServerIDs() {
		if server := byID[id]; server != nil && serverdomain.HasAMDComputeGPU(server.Observed.GPUs) {
			hasGPU = true
			break
		}
	}
	settings, err := platformdomain.NormalizeGPUOperatorSettings(*input.GPUOperator, kubernetes, hasGPU)
	if err != nil {
		return nil, "", err
	}
	return &settings, string(platformdomain.GPUStackOwnerGPUOperator), nil
}

// deploySlurm validates the Slurm topology, plans the build operation, and accepts the
// deployment in one transaction like deployKubernetes, so a rejected request leaves no record.
// Slurm needs no launch-time secret: the playbook
// mints the shared MUNGE key and the slurmrestd JWT on the primary controller itself, and the
// resulting reader credential is recorded from the run's result file.
func (s *DeployService) deploySlurm(ctx context.Context, input DeployPlatformInput) (*DeployPlatformResult, error) {
	if input.GPUOperator != nil && input.GPUOperator.Enabled {
		return nil, fmt.Errorf("%w: a Slurm Platform has no GPU Operator", platformdomain.ErrInvalidGPUOperator)
	}
	containerRuntime, err := platformdomain.ParseContainerRuntime(input.ContainerRuntime)
	if err != nil {
		return nil, err
	}
	gpuDriver, err := normalizeDeployGPUDriver(input.GPUDriver)
	if err != nil {
		return nil, err
	}
	omnistatEnabled := input.Omnistat != nil && input.Omnistat.Enabled
	var omnistat platformdomain.OmnistatSettings
	if omnistatEnabled {
		if s.omnistats == nil {
			return nil, fmt.Errorf("%w: Omnistat installation is unavailable", platformdomain.ErrInvalidOmnistat)
		}
		if omnistat, err = platformdomain.NormalizeOmnistatSettings(*input.Omnistat); err != nil {
			return nil, err
		}
	}
	exporterEnabled := input.DeviceMetricsExporter != nil && input.DeviceMetricsExporter.Enabled
	var exporter platformdomain.DeviceMetricsExporterSettings
	if exporterEnabled {
		if omnistatEnabled {
			// Omnistat attributes whole nodes (one job per node) and the exporter attributes GPUs on
			// shared nodes; both would also take the same hardware profiler. Whether they may run
			// together is not decided, so a Platform gets one.
			return nil, fmt.Errorf("%w: deviceMetricsExporter and omnistat cannot both be enabled", platformdomain.ErrInvalidDeviceMetricsExporter)
		}
		if s.deviceMetricsExporters == nil {
			return nil, fmt.Errorf("%w: Device Metrics Exporter installation is unavailable", platformdomain.ErrInvalidDeviceMetricsExporter)
		}
		if exporter, err = platformdomain.NormalizeDeviceMetricsExporterSettings(*input.DeviceMetricsExporter); err != nil {
			return nil, err
		}
	}
	validated, err := s.validateSlurm(ctx, input)
	if err != nil {
		return nil, err
	}
	gpuComputeIDs := slurmGPUComputeIDs(validated.slurmSpec, validated.targets)
	if exporterEnabled && len(gpuComputeIDs) == 0 {
		return nil, fmt.Errorf("%w: no compute Server has AMD compute GPUs in its GPU Inventory", platformdomain.ErrInvalidDeviceMetricsExporter)
	}

	gpuStackOwner := strings.TrimSpace(input.GPUStackOwner)
	if gpuStackOwner == "" {
		// Slurm has no in-platform GPU operator, so drivers come from the OS image or
		// provisioning. Default the owner rather than forcing a Kubernetes-shaped choice.
		gpuStackOwner = string(platformdomain.GPUStackOwnerProvisioning)
	}

	platform, err := s.platformService.newPlatform(ctx, CreatePlatformInput{
		SiteID:        input.SiteID,
		Name:          input.Name,
		Type:          string(platformdomain.PlatformTypeSlurm),
		GPUStackOwner: gpuStackOwner, ContainerRuntime: containerRuntime, GPUDriver: gpuDriver,
		GPUGresType:   validated.slurmSpec.GPUGresType,
		OCIContainers: validated.slurmSpec.OCIContainers, PMIx: validated.slurmSpec.PMIx,
	})
	if err != nil {
		return nil, err
	}
	targetIDs := validated.slurmSpec.ServerIDs()

	// Preassigned so the Omnistat or Device Metrics Exporter record names the deploy Workflow
	// before it exists: the observer of that Task, and the install's completion, are fenced on it.
	launch := platformdomain.DeploymentLaunch{
		Platform:           platform,
		OperationID:        uuid.NewString(),
		TargetServerIDs:    targetIDs,
		TrustedVars:        buildSlurmVars(platform, validated.slurmSpec, gpuComputeIDs),
		RequestedBy:        input.RequestedBy,
		RequestCorrelation: input.RequestCorrelation,
		MachinePreparation: validated.machinePreparation,
		TimeSyncCheck:      input.TimeSyncCheck,
	}
	if containerRuntime != "" {
		launch.ContainerRuntime = &platformdomain.ContainerRuntimeLaunch{
			Kind:      containerRuntime,
			ServerIDs: slurmContainerRuntimeIDs(validated.slurmSpec),
		}
	}
	var records []featureRecord
	if omnistatEnabled {
		observability := validated.slurmSpec.PrimaryControllerID()
		record := &platformdomain.Omnistat{
			Enabled: true, Version: platformdomain.OmnistatVersion, InstallMethod: omnistat.InstallMethod,
			ObservabilityServerID: observability, State: platformdomain.OmnistatInstalling,
			LastWorkflowID: launch.OperationID, UpdatedAt: time.Now().UTC(),
		}
		records = append(records, func(ctx context.Context) error {
			return s.omnistats.SetOmnistat(ctx, platform.ID, record)
		})
		launch.TrustedVars[varSlurmOmnistatEnabled] = true
		launch.Omnistat = &platformdomain.OmnistatLaunch{
			ObservabilityServerID: observability,
			ComputeServerIDs:      validated.slurmSpec.ComputeServerIDs(),
			InstallMethod:         omnistat.InstallMethod,
			TrustedVars:           buildOmnistatVars(platform, validated.slurmSpec, observability, gpuComputeIDs),
		}
	}
	if exporterEnabled {
		observability := validated.slurmSpec.PrimaryControllerID()
		record := &platformdomain.DeviceMetricsExporter{
			Enabled: true, Version: platformdomain.DeviceMetricsExporterVersion, InstallMethod: exporter.InstallMethod,
			ObservabilityServerID: observability, State: platformdomain.DeviceMetricsExporterInstalling,
			LastWorkflowID: launch.OperationID, UpdatedAt: time.Now().UTC(),
		}
		records = append(records, func(ctx context.Context) error {
			return s.deviceMetricsExporters.SetDeviceMetricsExporter(ctx, platform.ID, record)
		})
		launch.TrustedVars[varSlurmDeviceMetricsExporterEnabled] = true
		launch.DeviceMetricsExporter = &platformdomain.DeviceMetricsExporterLaunch{
			ObservabilityServerID: observability,
			GPUComputeServerIDs:   gpuComputeIDs,
			InstallMethod:         exporter.InstallMethod,
			TrustedVars: buildDeviceMetricsExporterVars(platform, validated.slurmSpec, observability,
				gpuComputeIDs, launch.OperationID),
		}
	}
	prepared, err := s.launcher.PrepareDeployment(ctx, launch)
	if err != nil {
		return nil, err
	}
	operationID, err := s.accept(ctx, platform, validated.targets, targetIDs, records, prepared)
	if err != nil {
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
	spec.GPUGresType = strings.TrimSpace(spec.GPUGresType)
	if err := platformdomain.ValidateSlurmGRESType(spec.GPUGresType); err != nil {
		return invalid, err
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
		if server.PlatformClaim != nil {
			return invalid, fmt.Errorf("%w: server %s is already claimed by %s",
				platformdomain.ErrInvalidDeployment, server.DisplayName(), claimedBy(server, availability.platformsByID))
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
		if server.PlatformClaim != nil {
			return invalid, fmt.Errorf("%w: server %s is already claimed by %s",
				platformdomain.ErrInvalidDeployment, server.DisplayName(), claimedBy(server, availability.platformsByID))
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
		prefix, err := s.apiVIPPrefix(ctx, spec, preparation)
		if err != nil {
			return invalid, err
		}
		spec.APIVIPPrefix = prefix
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

// targetAvailability loads the Site's Platforms once before any Platform record is created, so
// a refusal can name the Platform that claims a target (its stored Platform Claim, decision 067)
// or that a target reports membership in.
func (s *DeployService) targetAvailability(
	ctx context.Context,
	siteID string,
) (deploymentTargetAvailability, error) {
	availability := deploymentTargetAvailability{platformsByID: map[string]*platformdomain.Platform{}}
	platforms, err := s.platforms.List(ctx, siteID)
	if err != nil {
		return availability, err
	}
	for _, platform := range platforms {
		availability.platformsByID[platform.ID] = platform
	}
	return availability, nil
}

// apiVIPPrefix resolves the HA API VIP prefix against the control plane's subnet. The VIP must lie
// in that subnet, an omitted prefix takes the subnet's, and a different prefix is refused: with a
// longer one the VIP holder adds a narrower route whose source is the VIP, so its traffic to part
// of the subnet leaves from the VIP and etcd rejects it as a peer (the certificate names the node
// address). When no control-plane subnet can be read, the requested prefix is used, defaulting to
// 24, and the deploy playbook checks it against the host instead.
func (s *DeployService) apiVIPPrefix(
	ctx context.Context,
	spec platformdomain.DeploymentSpec,
	preparation platformdomain.MachinePreparation,
) (int, error) {
	requested := spec.APIVIPPrefix
	fallback := requested
	if fallback == 0 {
		fallback = defaultAPIVIPPrefix
	}
	vip, err := netip.ParseAddr(spec.APIVIP)
	if s.nodeSubnets == nil || err != nil || !vip.Is4() {
		// deploymentAPIAddress reports an unparseable VIP.
		return fallback, nil
	}
	controllers := spec.ControlPlaneServerIDs()
	subnets, err := s.nodeSubnets.NodeSubnets(ctx, controllers, preparation)
	if err != nil || len(subnets) == 0 {
		return fallback, nil
	}
	var subnet netip.Prefix
	for _, id := range controllers {
		current, ok := subnets[id]
		if !ok {
			continue
		}
		if subnet.IsValid() && current != subnet {
			return 0, fmt.Errorf("%w: the control-plane Servers are on different subnets (%s and %s); an HA API VIP needs them on one subnet",
				platformdomain.ErrInvalidDeployment, subnet, current)
		}
		subnet = current
	}
	if !subnet.Contains(vip) {
		return 0, fmt.Errorf("%w: apiVip %s is outside the control-plane subnet %s",
			platformdomain.ErrInvalidDeployment, vip, subnet)
	}
	if requested != 0 && requested != subnet.Bits() {
		return 0, fmt.Errorf("%w: apiVipPrefix %d must be the control-plane subnet's prefix %d (%s); a longer prefix makes the VIP holder send traffic to part of the subnet from the VIP, which etcd rejects",
			platformdomain.ErrInvalidDeployment, requested, subnet.Bits(), subnet)
	}
	return subnet.Bits(), nil
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
// gpuComputeIDs is always sent (possibly empty) so a rerun converges GPU GRES both ways.
func buildSlurmVars(platform *platformdomain.Platform, spec platformdomain.SlurmDeploymentSpec, gpuComputeIDs []string) map[string]any {
	vars := map[string]any{
		varSlurmClusterName:       slurmClusterName(spec.ClusterName, platform.Name),
		varSlurmPlatformName:      platform.Name,
		varSlurmControllerIDs:     spec.ControllerServerIDs(),
		varSlurmComputeIDs:        spec.ComputeServerIDs(),
		varSlurmLoginIDs:          spec.LoginServerIDs(),
		varSlurmPrimaryController: spec.PrimaryControllerID(),
		varSlurmHighAvailability:  spec.HighlyAvailable(),
		varSlurmGPUComputeIDs:     gpuComputeIDs,
		varSlurmGPUGresType:       spec.GPUGresType,
		varSlurmOCIContainers:     spec.OCIContainers,
		varSlurmPMIx:              spec.PMIx,
		varGPUDriverModuleOptions: gpuDriverModuleOptionsVar(platform.GPUDriver),
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

// buildOmnistatVars translates the Slurm topology into the configure-omnistat playbook's trusted
// variables: every compute node runs the exporter, and the observability Server (the primary
// controller, which runs Prometheus and Grafana) is the only remote address the exporter answers
// and the source of the result URLs. GPU compute nodes collect through AMD SMI.
func buildOmnistatVars(
	platform *platformdomain.Platform,
	spec platformdomain.SlurmDeploymentSpec,
	observabilityID string,
	gpuComputeIDs []string,
) map[string]any {
	return map[string]any{
		varOmnistatVersion:         platformdomain.OmnistatVersion,
		varOmnistatPort:            platformdomain.OmnistatExporterPort,
		varOmnistatComputeIDs:      spec.ComputeServerIDs(),
		varOmnistatObservabilityID: observabilityID,
		varOmnistatSystemName:      slurmClusterName(spec.ClusterName, platform.Name),
		varSlurmGPUComputeIDs:      gpuComputeIDs,
	}
}

// buildDeviceMetricsExporterVars translates the Slurm topology into the variables the Slurm side
// lays over the composed device-metrics-exporter install: Slurm job attribution, every GPU compute
// node running the exporter (it reports only GPUs), the observability Server (the primary
// controller) that runs Prometheus and Grafana and is the source of the result URL, and the Slurm
// cluster name as the exporter's cluster_name label the dashboards select on.
func buildDeviceMetricsExporterVars(
	platform *platformdomain.Platform,
	spec platformdomain.SlurmDeploymentSpec,
	observabilityID string,
	gpuComputeIDs []string,
	recordID string,
) map[string]any {
	return map[string]any{
		varDeviceMetricsExporterVersion:         platformdomain.DeviceMetricsExporterVersion,
		varDeviceMetricsExporterPort:            platformdomain.DeviceMetricsExporterPort,
		varDeviceMetricsExporterJobAttribution:  "slurm",
		varDeviceMetricsExporterComputeIDs:      gpuComputeIDs,
		varDeviceMetricsExporterObservabilityID: observabilityID,
		varDeviceMetricsExporterClusterName:     slurmClusterName(spec.ClusterName, platform.Name),
		VarDeviceMetricsExporterRecordID:        recordID,
	}
}

// slurmGPUComputeIDs returns, in request order, the compute Servers whose GPU Inventory has AMD
// compute GPUs; those become Slurm GPU nodes (decision 063). The selection reads Swallow's
// provider-observed inventory rather than probing hosts, like the GPU Operator's eligibility
// check, and the playbook then proves each selected host really exposes GPUs through KFD. A
// controller-only or login-only Server never gets GRES because it runs no slurmd. The result is
// never nil so the trusted variable is always a list.
func slurmGPUComputeIDs(spec platformdomain.SlurmDeploymentSpec, targets []*serverdomain.Server) []string {
	byID := make(map[string]*serverdomain.Server, len(targets))
	for _, server := range targets {
		byID[server.ID] = server
	}
	ids := []string{}
	for _, id := range spec.ComputeServerIDs() {
		if server := byID[id]; server != nil && serverdomain.HasAMDComputeGPU(server.Observed.GPUs) {
			ids = append(ids, id)
		}
	}
	return ids
}

// normalizeDeployGPUDriver validates an optional GPU driver request; nil requests nothing.
func normalizeDeployGPUDriver(settings *platformdomain.GPUDriverSettings) (platformdomain.GPUDriverSettings, error) {
	if settings == nil {
		return platformdomain.GPUDriverSettings{}, nil
	}
	return platformdomain.NormalizeGPUDriverSettings(*settings)
}

// gpuDriverModuleOptionsVar is the trusted module-options value: never nil, so an empty request
// still clears options an earlier run applied.
func gpuDriverModuleOptionsVar(settings platformdomain.GPUDriverSettings) map[string]string {
	options := make(map[string]string, len(settings.ModuleOptions))
	for name, value := range settings.ModuleOptions {
		options[name] = value
	}
	return options
}

// slurmContainerRuntimeIDs selects the Servers the container runtime feature installs on: every
// compute node (where jobs run) and login node (where users build and test images), in request
// order. A controller-only Server runs no workload and gets no runtime.
func slurmContainerRuntimeIDs(spec platformdomain.SlurmDeploymentSpec) []string {
	seen := map[string]bool{}
	ids := []string{}
	for _, assignment := range spec.NodeAssignments {
		if (assignment.Compute || assignment.Login) && !seen[assignment.ServerID] {
			seen[assignment.ServerID] = true
			ids = append(ids, assignment.ServerID)
		}
	}
	return ids
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
	// Automatic and DHCP addresses are learned after MAAS completes. The playbook resolves the
	// initial control-plane inventory address at execution time.
	return "", netip.Addr{}, nil
}
