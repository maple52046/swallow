package app

import (
	"fmt"

	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	platformdelivery "github.com/maple52046/swallow/internal/core/platform/delivery"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/internal/core/store"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/modules/accelerator/amd"
)

// platformOperations is the Platform deploy, uninstall, and GPU Operator use cases, and the
// Change Plan planner over them. The API serves them, and the worker's platform controller carries
// out spec changes and approved Change Plans with them, so both processes compose them here.
type platformOperations struct {
	deploy       *platformapp.DeployService
	uninstall    *platformapp.UninstallService
	gpuOperators *platformapp.GPUOperatorService
	planner      *platformapp.PlatformPlanner
}

// platformOperationDeps is what the Platform operations are built from.
type platformOperationDeps struct {
	transactor      store.Transactor
	modules         composedModules
	platformService *platformapp.PlatformService
	platforms       platformRecords
	servers         serverdomain.ServerRepository
	claims          serverdomain.PlatformClaimStore
	lifecycle       platformdomain.LifecycleReader
	operations      *workflowapp.ExecutionService
	orchestrations  *workflowapp.WorkflowService
	deployments     *provisioningapp.DeployServersUseCase
	bootMedia       bootMediaPlanner
	software        *softwareapp.SoftwareService
	ntp             ntpStepPlanner
	nodeExporter    nodeExporterPlanner
	protection      serverdomain.MutationGuard
	networks        *provisioningapp.NetworkConfigurationService
	requirements    platformdomain.DeploymentRequirementReader
	keys            platformdomain.DeploymentKeyChecker
}

// platformRecords is the Platform store the operations write: the repository and the feature
// records a deployment carries.
type platformRecords interface {
	platformdomain.PlatformRepository
	platformdomain.GPUOperatorRecorder
	platformdomain.OmnistatRecorder
	platformdomain.DeviceMetricsExporterRecorder
}

// claimingServers is the Server store that also holds the Servers' Platform Claims.
type claimingServers interface {
	serverdomain.ServerRepository
	serverdomain.PlatformClaimStore
}

// newPlatformService builds a process's PlatformService. The API, the worker, and the Ansible
// executor each build one and all of them complete Platform uninstalls or record feature installs
// through it, so the claims, the feature recorders, and the Device Metrics Exporter assignments are
// wired here once rather than per process. canceler is the one part that differs: it is what Delete
// cancels a Platform's Workflows through, and a process that never deletes Platforms passes one
// that refuses. A nil argument is a startup error from NewPlatformService.
func newPlatformService(
	transactor store.Transactor,
	platforms platformRecords,
	sites sitedomain.SiteRepository,
	servers claimingServers,
	workflows workflowdomain.WorkflowRepository,
	integrations sitedomain.IntegrationRepository,
	assignments softwaredomain.AssignmentRepository,
	canceler platformdomain.PlatformOperationCanceler,
) (*platformapp.PlatformService, error) {
	service, err := platformapp.NewPlatformService(platformapp.PlatformServiceDeps{
		Transactor: transactor, Platforms: platforms, Sites: sites, Servers: servers,
		Lifecycle:    platformLifecycleReader{orchestrations: workflows},
		Integrations: managedPlatformIntegrationCleaner{integrations: integrations},
		Canceler:     canceler,
		// A completed uninstall frees its Servers (decision 067) and, for Slurm, retires their
		// Device Metrics Exporter records (decision 068).
		Claims:       servers,
		GPUOperators: platforms, Omnistats: platforms, DeviceMetricsExporters: platforms,
		ExporterAssignments: deviceMetricsExporterAssignments{assignments: assignments},
	})
	if err != nil {
		return nil, fmt.Errorf("compose the platform service: %w", err)
	}
	return service, nil
}

// buildPlatformOperations composes the Platform operations once for the process that serves them.
// It fails when a required dependency is missing, so the process does not start with an
// acceptance check left out.
func buildPlatformOperations(deps platformOperationDeps) (platformOperations, error) {
	launcher := platformDeploymentLauncher{
		operations: deps.operations, orchestrations: deps.orchestrations, deployments: deps.deployments, servers: deps.servers,
		bootMedia: deps.bootMedia, software: deps.software, ntp: deps.ntp, nodeExporter: deps.nodeExporter,
	}
	// The GPU Operator and the Device Metrics Exporter are the AMD Accelerator module's Platform
	// features, so they serve its GPUs.
	accelerator := deps.modules.accelerators[amd.ID]
	deploy, err := platformapp.NewDeployService(platformapp.DeployServiceDeps{
		PlatformService: deps.platformService, Platforms: deps.platforms, Servers: deps.servers,
		Launcher: launcher, Protection: deps.protection, Claims: deps.claims,
		MachinePreparation: platformMachinePreparationValidator{deployments: deps.deployments},
		NodeSubnets:        platformNodeSubnetResolver{networks: deps.networks},
		Requirements:       deps.requirements, DeploymentKeys: deps.keys,
		GPUOperators: deps.platforms, Omnistats: deps.platforms, DeviceMetricsExporters: deps.platforms,
		Accelerator: accelerator, ReadRequest: platformdelivery.DeployInputFromRequest,
	})
	if err != nil {
		return platformOperations{}, fmt.Errorf("compose the platform deploy service: %w", err)
	}
	uninstall := platformapp.NewUninstallService(deps.platforms, deps.servers, deps.lifecycle, launcher, deps.protection)
	// GPU Operator changes after deployment (decision 062) run as their own durable Workflow.
	gpuOperators := platformapp.NewGPUOperatorService(
		deps.transactor, deps.platforms, deps.platforms, deps.servers, deps.lifecycle,
		gpuOperatorLauncher{operations: deps.operations, orchestrations: deps.orchestrations},
		deps.keys, accelerator,
	)
	return platformOperations{
		deploy: deploy, uninstall: uninstall, gpuOperators: gpuOperators,
		planner: platformapp.NewPlatformPlanner(deps.platforms, deps.lifecycle, deploy, uninstall, gpuOperators, deps.modules.platforms),
	}, nil
}
