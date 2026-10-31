package application

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// Trusted extra-var names the configure-amd-gpu-operator playbook reads (platform-deployment.md
// §4.2). They use the swallow_ prefix so a client cannot forge them, and are the same for the
// deployment Job and the standalone change Workflow.
const (
	varGPUOperatorState           = "swallow_amd_gpu_operator_state"
	varGPUOperatorVersion         = "swallow_amd_gpu_operator_version"
	varGPUOperatorAllocationMode  = "swallow_amd_gpu_operator_allocation_mode"
	varGPUOperatorResourceNaming  = "swallow_amd_gpu_operator_resource_naming"
	varGPUOperatorNodeLabeller    = "swallow_amd_gpu_operator_node_labeller"
	varGPUOperatorMetricsExporter = "swallow_amd_gpu_operator_metrics_exporter"
	varGPUOperatorControllerID    = "swallow_amd_gpu_operator_controller_id"
	varGPUOperatorImages          = "swallow_amd_gpu_operator_images"
)

// buildGPUOperatorVars translates normalized settings into the playbook's trusted variables.
// controllerID is the control-plane Server helm runs on. A disabled setting produces the
// uninstall intent (state absent); the release is still passed so the playbook can name it. No
// node list is passed: the playbook takes the managed nodes from the operator's Node Feature
// Discovery (decision 062).
func buildGPUOperatorVars(settings platformdomain.GPUOperatorSettings, controllerID string, driver platformdomain.GPUDriverSettings) map[string]any {
	state := "present"
	if !settings.Enabled {
		state = "absent"
	}
	vars := map[string]any{
		varGPUDriverModuleOptions:     gpuDriverModuleOptionsVar(driver),
		varGPUOperatorState:           state,
		varGPUOperatorVersion:         settings.Version,
		varGPUOperatorAllocationMode:  string(settings.AllocationMode),
		varGPUOperatorResourceNaming:  string(settings.ResourceNaming),
		varGPUOperatorNodeLabeller:    settings.NodeLabeller,
		varGPUOperatorMetricsExporter: settings.MetricsExporter,
		varGPUOperatorControllerID:    controllerID,
	}
	if release, ok := platformdomain.LookupGPUOperatorRelease(settings.Version); ok {
		images := map[string]any{
			"devicePlugin":    release.Images.DevicePlugin,
			"nodeLabeller":    release.Images.NodeLabeller,
			"utils":           release.Images.Utils,
			"metricsExporter": release.Images.MetricsExporter,
			"draDriver":       release.DRADriverImages[settings.DRADriverVersion],
		}
		vars[varGPUOperatorImages] = images
	}
	return vars
}

// hasAMDGPUWorkload reports whether any of the workload Servers has an AMD compute GPU in its
// current provider inventory, which is what makes the GPU Operator worth enabling. It does not
// choose nodes; the operator's Node Feature Discovery does.
func hasAMDGPUWorkload(
	ctx context.Context,
	servers serverdomain.ServerRepository,
	workloadIDs []string,
) (bool, error) {
	for _, id := range workloadIDs {
		server, err := servers.FindByID(ctx, id)
		if err != nil {
			return false, err
		}
		if serverdomain.HasAMDComputeGPU(server.Observed.GPUs) {
			return true, nil
		}
	}
	return false, nil
}

// GPUOperatorReleaseItem is the wire shape of one release catalog entry
// (GET /platforms/gpu-operator/releases).
type GPUOperatorReleaseItem struct {
	Version                 string   `json:"version"`
	Default                 bool     `json:"default"`
	MinKubernetesVersion    string   `json:"minKubernetesVersion"`
	DRAMinKubernetesVersion string   `json:"draMinKubernetesVersion"`
	DRADriverVersions       []string `json:"draDriverVersions"`
	DefaultDRADriverVersion string   `json:"defaultDraDriverVersion"`
}

// GPUOperatorReleaseItems projects the release catalog, newest first. Images stay internal: they
// are pinned by the release and are not a client choice.
func GPUOperatorReleaseItems() []GPUOperatorReleaseItem {
	releases := platformdomain.GPUOperatorReleases()
	items := make([]GPUOperatorReleaseItem, len(releases))
	for i, release := range releases {
		items[i] = GPUOperatorReleaseItem{
			Version: release.Version, Default: release.Default,
			MinKubernetesVersion:    release.MinKubernetesVersion.String(),
			DRAMinKubernetesVersion: release.DRAMinKubernetesVersion.String(),
			DRADriverVersions:       append([]string{}, release.DRADriverVersions...),
			DefaultDRADriverVersion: release.DefaultDRADriverVersion,
		}
	}
	return items
}

// GPUOperatorService changes the Swallow-managed GPU Operator of a deployed Kubernetes Platform
// (PUT /platforms/{id}/gpu-operator). It owns the preconditions and the settings record; the
// launcher owns Workflow composition and the operation context's busy and lock checks. The
// deploy-time install is owned by DeployService, which reuses the same normalization and vars.
type GPUOperatorService struct {
	transactor     store.Transactor
	platforms      platformdomain.PlatformRepository
	recorder       platformdomain.GPUOperatorRecorder
	servers        serverdomain.ServerRepository
	lifecycle      platformdomain.LifecycleReader
	launcher       platformdomain.GPUOperatorLauncher
	deploymentKeys platformdomain.DeploymentKeyChecker
}

// NewGPUOperatorService wires the change use case. deploymentKeys may be nil (tests); production
// wiring supplies it so a change is refused before anything is written when Swallow cannot log in.
func NewGPUOperatorService(
	transactor store.Transactor,
	platforms platformdomain.PlatformRepository,
	recorder platformdomain.GPUOperatorRecorder,
	servers serverdomain.ServerRepository,
	lifecycle platformdomain.LifecycleReader,
	launcher platformdomain.GPUOperatorLauncher,
	deploymentKeys platformdomain.DeploymentKeyChecker,
) *GPUOperatorService {
	return &GPUOperatorService{
		transactor: transactor,
		platforms:  platforms, recorder: recorder, servers: servers,
		lifecycle: lifecycle, launcher: launcher, deploymentKeys: deploymentKeys,
	}
}

// ConfigureGPUOperatorInput is one PUT request. Settings.Enabled false is the uninstall intent.
type ConfigureGPUOperatorInput struct {
	PlatformID         string
	Settings           platformdomain.GPUOperatorSettings
	RequestedBy        string
	RequestCorrelation string
}

// ConfigureGPUOperatorResult is the accepted change.
type ConfigureGPUOperatorResult struct {
	PlatformID string `json:"platformId"`
	WorkflowID string `json:"workflowId"`
}

// Configure validates a change, records it, and launches the Workflow that applies it.
//
// It returns ErrPlatformNotFound for a missing Platform; ErrGPUOperatorConflict when the Platform
// is not an active Swallow-deployed Kubernetes Platform, its Kubernetes version is unknown, or an
// uninstall finds no managed operator; ErrInvalidGPUOperator for invalid settings; and
// ErrDeploymentKeyMissing without a Deployment Key. The Workflow is planned first (the lock check
// and target resolution read outside the store); then the record and the Workflow are written in
// one transaction. The record names the preassigned Workflow id, so the Task observer's fence
// matches from the first Task, and a refusal at either step (for example a locked or busy target)
// changes nothing.
func (s *GPUOperatorService) Configure(ctx context.Context, input ConfigureGPUOperatorInput) (*ConfigureGPUOperatorResult, error) {
	platform, err := s.platforms.FindByID(ctx, input.PlatformID)
	if err != nil {
		return nil, err
	}
	if platform.Type != platformdomain.PlatformTypeKubernetes {
		return nil, fmt.Errorf("%w: only a Kubernetes Platform has a GPU Operator", platformdomain.ErrGPUOperatorConflict)
	}
	snapshots, err := s.lifecycle.Read(ctx, []string{platform.ID})
	if err != nil {
		return nil, err
	}
	snapshot := snapshots[platform.ID]
	if snapshot.Origin != platformdomain.PlatformOriginDeployed || snapshot.State != platformdomain.PlatformLifecycleActive ||
		snapshot.Deployment == nil || snapshot.Deployment.Intent == nil {
		return nil, fmt.Errorf("%w: the GPU Operator can be changed only on an active Swallow-deployed Platform (lifecycle is %s)",
			platformdomain.ErrGPUOperatorConflict, snapshot.State)
	}
	if s.deploymentKeys != nil {
		exists, err := s.deploymentKeys.HasDeploymentKey(ctx)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, platformdomain.ErrDeploymentKeyMissing
		}
	}

	intent := snapshot.Deployment.Intent
	spec := platformdomain.DeploymentSpec{RoleAssignments: intent.RoleAssignments}
	previous := platform.GPUOperator
	settings := input.Settings
	if settings.Enabled {
		kubernetes, ok := platformdomain.ParseKubernetesMinorVersion(intent.K0sVersion)
		if !ok {
			return nil, fmt.Errorf("%w: the Platform's Kubernetes version is not recorded", platformdomain.ErrGPUOperatorConflict)
		}
		hasGPU, err := hasAMDGPUWorkload(ctx, s.servers, spec.WorkloadServerIDs())
		if err != nil {
			return nil, err
		}
		if settings, err = platformdomain.NormalizeGPUOperatorSettings(settings, kubernetes, hasGPU); err != nil {
			return nil, err
		}
	} else {
		if !previous.Managed() {
			return nil, fmt.Errorf("%w: Swallow manages no GPU Operator on this Platform", platformdomain.ErrGPUOperatorConflict)
		}
		settings = previous.Settings
		settings.Enabled = false
	}

	controllers := spec.ControlPlaneServerIDs()
	if len(controllers) == 0 {
		return nil, fmt.Errorf("%w: the Platform's control plane is not recorded", platformdomain.ErrGPUOperatorConflict)
	}
	state := platformdomain.GPUOperatorInstalling
	owner := platformdomain.GPUStackOwnerGPUOperator
	if !settings.Enabled {
		// The policy stays gpu-operator until the uninstall succeeds; reaching absent resets it.
		state = platformdomain.GPUOperatorUninstalling
	}
	workflowID := uuid.NewString()
	record := &platformdomain.GPUOperator{
		Settings: settings, State: state, LastWorkflowID: workflowID, UpdatedAt: time.Now().UTC(),
	}
	prepared, err := s.launcher.PrepareGPUOperator(ctx, platformdomain.GPUOperatorLaunch{
		WorkflowID: workflowID, Platform: platform,
		TargetServerIDs: append([]string(nil), snapshot.Deployment.TargetServerIDs...),
		TrustedVars:     buildGPUOperatorVars(settings, controllers[0], platform.GPUDriver),
		Uninstall:       !settings.Enabled,
		RequestedBy:     input.RequestedBy, RequestCorrelation: input.RequestCorrelation,
	})
	if err != nil {
		return nil, err
	}
	var launched string
	err = s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.recorder.SetGPUOperator(ctx, platform.ID, record, owner); err != nil {
			return err
		}
		id, err := s.launcher.PersistGPUOperator(ctx, prepared)
		launched = id
		return err
	})
	if err != nil {
		return nil, err
	}
	return &ConfigureGPUOperatorResult{PlatformID: platform.ID, WorkflowID: launched}, nil
}
