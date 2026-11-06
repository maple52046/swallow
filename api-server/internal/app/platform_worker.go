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
	workflows       *workflowapp.WorkflowService
	automation      *workflowapp.EffectiveAutomationConfigurations
	providers       *maas.ProviderFactory
	deployments     *provisioningapp.DeployServersUseCase
	software        *softwarestore.MongoAssignmentRepo
	bootISOs        serverdomain.BootISOResolver
	keys            platformdomain.DeploymentKeyChecker
	// artifacts resolves the trusted vars of playbooks that fetch artifacts, as in the API.
	artifacts workflowapp.ArtifactVars
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
	orchestrations := deps.workflows
	if deps.artifacts == nil {
		return platformOperations{}, errors.New("platform operations need the artifact vars resolver")
	}
	operations := workflowapp.NewExecutionService(deps.servers, deps.automation, catalog, modules.workflowKinds, runner, events,
		platformapp.NewPolicyChecker(deps.platforms, deps.servers), protection, orchestrations, deps.artifacts)
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
		nodeExporter: nodeExporterPlanner{software: software, operations: operations},
		networks:     provisioningapp.NewNetworkConfigurationService(deps.servers, deps.providers),
		requirements: platformstore.NewMongoDeploymentRequirementRepo(db), keys: deps.keys,
		monitoring: newPlatformMonitoringAccess(cfg), monitoringTokens: newPlatformTokenIssuer(db),
	})
}

// workflowPlatforms names the Platform a Workflow belongs to for the platform controller.
type workflowPlatforms struct {
	orchestrations workflowdomain.WorkflowRepository
}

// workflowOutcomeHistory bounds how many of a Platform's newest Workflows WorkflowOutcome searches
// for reruns; a Platform's realizing Workflow is among its latest.
const workflowOutcomeHistory = 50

// WorkflowOutcome returns the status of the Platform's Workflow, or of its newest rerun, "" for a
// Workflow that is gone.
func (w workflowPlatforms) WorkflowOutcome(ctx context.Context, platformID, workflowID string) (string, error) {
	workflows, _, err := w.orchestrations.List(ctx, workflowdomain.WorkflowFilter{PlatformID: platformID, Limit: workflowOutcomeHistory})
	if err != nil {
		return "", err
	}
	outcome, chain := "", map[string]bool{workflowID: true}
	// Newest first, so walking backwards meets each Workflow before its reruns.
	for i := len(workflows) - 1; i >= 0; i-- {
		workflow := workflows[i]
		if workflow.ID == workflowID || chain[workflow.RetryOfOperationID] {
			chain[workflow.ID] = true
			outcome = string(workflow.Status)
		}
	}
	return outcome, nil
}

// supersededBySpecReason is the cancel reason of a realization a newer Platform spec replaced.
const supersededBySpecReason = "Superseded by a newer spec of its Platform."

// platformWorkflowCanceler implements platformapp.WorkflowCanceler: it follows a Platform's Workflow
// to its newest rerun, as WorkflowOutcome does, and asks that one to cancel.
type platformWorkflowCanceler struct {
	orchestrations workflowdomain.WorkflowRepository
	workflows      *workflowapp.WorkflowService
}

// CancelUnfinished requests the cancel and reports true while the Workflow has not finished; a
// Workflow that is gone, finished, or finishes while the cancel is requested is false.
func (c platformWorkflowCanceler) CancelUnfinished(ctx context.Context, platformID, workflowID string) (bool, error) {
	workflows, _, err := c.orchestrations.List(ctx, workflowdomain.WorkflowFilter{PlatformID: platformID, Limit: workflowOutcomeHistory})
	if err != nil {
		return false, err
	}
	var newest *workflowdomain.Workflow
	chain := map[string]bool{workflowID: true}
	// Newest first, so walking backwards meets each Workflow before its reruns.
	for i := len(workflows) - 1; i >= 0; i-- {
		if workflow := workflows[i]; workflow.ID == workflowID || chain[workflow.RetryOfOperationID] {
			chain[workflow.ID] = true
			newest = workflow
		}
	}
	if newest == nil || newest.Status.Terminal() {
		return false, nil
	}
	if err := c.workflows.CancelFor(ctx, newest.ID, supersededBySpecReason); err != nil {
		if errors.Is(err, workflowdomain.ErrWorkflowControlConflict) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
