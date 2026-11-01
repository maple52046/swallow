package app

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"

	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/adapters/hostautomation/ansible"
	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	platformstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/platform"
	serverstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/server"
	softwarestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/software"
	workflowstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/workflow"
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	"github.com/maple52046/swallow/internal/core/store"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
)

// workerPlatformDeps are the repositories and services the worker already has that its Platform
// operations reuse.
type workerPlatformDeps struct {
	platformService *platformapp.PlatformService
	platforms       *platformstore.MongoPlatformRepo
	servers         *serverstore.MongoServerRepo
	operations      *workflowstore.MongoWorkflowRepo
	secrets         *workflowstore.MongoOperationSecretRepo
	ownership       *workflowstore.MongoOwnershipRepo
	automation      *workflowapp.EffectiveAutomationConfigurations
	providers       *maas.ProviderFactory
	deployments     *provisioningapp.DeployServersUseCase
	software        *softwarestore.MongoAssignmentRepo
	bootISOs        serverdomain.BootISOResolver
	keys            platformdomain.DeploymentKeyChecker
}

// workerPlatformOperations composes the Platform operations in the worker, as the API does, so its
// platform controller prepares and accepts Platform Workflows through the same acceptance checks.
func workerPlatformOperations(
	cfg config.APIConfig,
	db *mongo.Database,
	transactor store.Transactor,
	modules composedModules,
	deps workerPlatformDeps,
) (platformOperations, error) {
	catalog, err := ansible.LoadManifestCatalog(cfg.PlaybookManifest, cfg.PlaybookDir)
	if err != nil {
		return platformOperations{}, fmt.Errorf("playbook catalog: %w", err)
	}
	runner := ansible.NewLocalRunner(cfg.AnsibleRunnerCommand, catalog.ProjectRoot(), cfg.JobRuntimeDir, cfg.JobArtifactDir)
	runner.AttachUserProber(sshprobe.DefaultProber{})
	events, err := workflowstore.NewMongoAnsibleEventRepo(db)
	if err != nil {
		return platformOperations{}, fmt.Errorf("ansible event repo init: %w", err)
	}
	protection := providerServerMutationGuard{servers: deps.servers, providers: deps.providers}
	operations := workflowapp.NewExecutionService(deps.servers, deps.automation, catalog, modules.workflowKinds, runner, events,
		platformapp.NewPolicyChecker(deps.platforms, deps.servers), protection)
	orchestrations := workflowapp.NewWorkflowService(transactor, deps.operations, workflowPlanCatalog(modules.workflowKinds, modules.software), deps.secrets)
	orchestrations.AttachOwnershipReader(deps.ownership)
	operations.AttachWorkflow(orchestrations)
	software := softwareapp.NewSoftwareService(transactor, modules.software, modules.accelerators, deps.software, deps.servers,
		kubernetesMembershipChecker{servers: deps.servers, platforms: deps.platforms})
	software.AttachLauncher(softwareDeploymentLauncher{operations: operations, orchestrations: orchestrations, modules: modules.registry})
	software.AttachPlatformNames(platformNameReader{platforms: deps.platforms})
	return buildPlatformOperations(platformOperationDeps{
		transactor: transactor, modules: modules, platformService: deps.platformService, platforms: deps.platforms,
		servers: deps.servers, claims: deps.servers, lifecycle: platformLifecycleReader{orchestrations: deps.operations},
		operations: operations, orchestrations: orchestrations, deployments: deps.deployments,
		bootMedia: bootMediaPlanner{servers: deps.servers, isos: deps.bootISOs}, software: software,
		ntp: ntpStepPlanner{server: cfg.NTPServer, operations: operations}, protection: protection,
		networks:     provisioningapp.NewNetworkConfigurationService(deps.servers, deps.providers),
		requirements: platformstore.NewMongoDeploymentRequirementRepo(db), keys: deps.keys,
	}), nil
}

// workflowPlatforms names the Platform a Workflow belongs to for the platform controller.
type workflowPlatforms struct {
	orchestrations workflowdomain.WorkflowRepository
}

// PlatformOfWorkflow returns the Workflow's Platform id, "" for a Workflow that is gone.
func (w workflowPlatforms) PlatformOfWorkflow(ctx context.Context, workflowID string) (string, error) {
	workflow, err := w.orchestrations.FindByID(ctx, workflowID)
	if errors.Is(err, workflowdomain.ErrWorkflowNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return workflow.PlatformID, nil
}
