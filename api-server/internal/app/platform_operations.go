package app

import (
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	platformdelivery "github.com/maple52046/swallow/internal/core/platform/delivery"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	"github.com/maple52046/swallow/internal/core/store"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
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

func buildPlatformOperations(deps platformOperationDeps) platformOperations {
	launcher := platformDeploymentLauncher{
		operations: deps.operations, orchestrations: deps.orchestrations, deployments: deps.deployments, servers: deps.servers,
		bootMedia: deps.bootMedia, software: deps.software, ntp: deps.ntp,
	}
	deploy := platformapp.NewDeployService(deps.platformService, deps.platforms, deps.servers, launcher, deps.protection)
	deploy.AttachPlatformClaims(deps.claims)
	deploy.AttachMachinePreparationValidator(platformMachinePreparationValidator{deployments: deps.deployments})
	deploy.AttachNodeSubnetResolver(platformNodeSubnetResolver{networks: deps.networks})
	deploy.AttachDeploymentRequirementReader(deps.requirements)
	deploy.AttachDeploymentKeyChecker(deps.keys)
	deploy.AttachGPUOperatorRecorder(deps.platforms)
	deploy.AttachOmnistatRecorder(deps.platforms)
	deploy.AttachDeviceMetricsExporterRecorder(deps.platforms)
	// The GPU Operator and the Device Metrics Exporter are the AMD Accelerator module's Platform
	// features, so they serve its GPUs.
	deploy.AttachAccelerator(deps.modules.accelerators[amd.ID])
	deploy.AttachRequestReader(platformdelivery.DeployInputFromRequest)
	uninstall := platformapp.NewUninstallService(deps.platforms, deps.servers, deps.lifecycle, launcher, deps.protection)
	// GPU Operator changes after deployment (decision 062) run as their own durable Workflow.
	gpuOperators := platformapp.NewGPUOperatorService(
		deps.transactor, deps.platforms, deps.platforms, deps.servers, deps.lifecycle,
		gpuOperatorLauncher{operations: deps.operations, orchestrations: deps.orchestrations},
		deps.keys,
	)
	gpuOperators.AttachAccelerator(deps.modules.accelerators[amd.ID])
	return platformOperations{
		deploy: deploy, uninstall: uninstall, gpuOperators: gpuOperators,
		planner: platformapp.NewPlatformPlanner(deps.platforms, deps.lifecycle, deploy, uninstall, gpuOperators, deps.modules.platforms),
	}
}
