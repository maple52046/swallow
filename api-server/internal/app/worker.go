package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	workflowregister "go.temporal.io/sdk/workflow"

	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/adapters/bmc/redfish"
	"github.com/maple52046/swallow/internal/adapters/bootiso/genfsimg"
	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	"github.com/maple52046/swallow/internal/adapters/hostaccess/sshaccess"
	"github.com/maple52046/swallow/internal/adapters/hypervisor/libvirt"
	"github.com/maple52046/swallow/internal/adapters/localrepo/pulp"
	"github.com/maple52046/swallow/internal/adapters/metrics/promstack"
	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	infrastructurestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/infrastructure"
	localrepostore "github.com/maple52046/swallow/internal/adapters/store/mongodb/localrepo"
	"github.com/maple52046/swallow/internal/adapters/store/mongodb/migration"
	platformstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/platform"
	provisioningstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/provisioning"
	serverstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/server"
	sitestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/site"
	softwarestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/software"
	workflowstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/workflow"
	"github.com/maple52046/swallow/internal/core/convergence"
	infrastructureapp "github.com/maple52046/swallow/internal/core/infrastructure/application"
	localrepoapp "github.com/maple52046/swallow/internal/core/localrepo/application"
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	serverapp "github.com/maple52046/swallow/internal/core/server/application"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

// RunWorker starts the worker process: the Temporal workflow/activity worker, the desired-state
// controllers, and the periodic observation passes (reconcile, inventory sweep, membership sync,
// Redfish capability sweep). The API process runs none of them.
func RunWorker(cfg config.APIConfig) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	modules, err := composeModules()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	sealer, err := secret.NewSealer(cfg.CredentialKey)
	if err != nil {
		return fmt.Errorf("credential key: %w", err)
	}

	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	mongoClient, err := storemongo.Connect(connectCtx, cfg.MongoURI)
	if err != nil {
		return err
	}
	defer mongoClient.Disconnect(context.Background())
	// The one Transactor of this process, shared by every use case it composes (decision 069).
	transactor := storemongo.NewTransactor(mongoClient)
	db := mongoClient.Database(cfg.MongoDB)
	if err := migration.Check(connectCtx, db); err != nil {
		return fmt.Errorf("database schema: %w", err)
	}
	operations, err := workflowstore.NewMongoWorkflowRepo(db)
	if err != nil {
		return fmt.Errorf("orchestration repo: %w", err)
	}
	operationSecrets, err := workflowstore.NewMongoOperationSecretRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("operation secret repo: %w", err)
	}
	integrations, err := sitestore.NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		return err
	}
	servers, err := serverstore.NewMongoServerRepo(db)
	if err != nil {
		return err
	}
	providers := maas.NewProviderFactory(integrations)
	// The worker reads the Deployment Key for wait-for-ssh, ensures provisioner key registration
	// before every OS deployment it drives (decision 039), and runs the SSH Key controller on the
	// same service so the two never interleave a realization pass (decision 071).
	sshKeys, err := newSSHKeyService(db, sealer, integrations, providers)
	if err != nil {
		return err
	}
	automationRepo := workflowstore.NewMongoAutomationConfigurationRepo(db, sealer)
	automationConfigurations := workflowapp.NewEffectiveAutomationConfigurations(
		automationRepo, deploymentKeySource{keys: sshKeys})
	// Hypervisor access (decision 055): libvirt Boot Media ensures and virtual-machine enrollment
	// log in to a Hypervisor with the Deployment Key on the Site's SSH port.
	libvirtHosts := libvirt.NewHost(sshaccess.NewSSHHostAccess(deploymentKeySource{keys: sshKeys}, siteSSHPorts{configurations: automationRepo}))
	platforms, err := platformstore.NewMongoPlatformRepo(db, sealer)
	if err != nil {
		return err
	}
	readers, err := platformapp.NewReaderFactory(integrations, modules.platformReaders...)
	if err != nil {
		return err
	}
	membership := platformapp.NewMembershipSyncUseCase(platforms, servers, readers)
	templates, err := provisioningstore.NewMongoDeploymentTemplateRepo(db, sealer)
	if err != nil {
		return err
	}
	tasks, err := provisioningstore.NewMongoProvisioningTaskRepo(db)
	if err != nil {
		return err
	}
	osImageVerifications, err := provisioningstore.NewMongoOSImageVerificationRepo(db)
	if err != nil {
		return err
	}
	// Software Assignments back the software Workflow's internal record step (installed/absent) and
	// the step observer's failure marking (decision 038).
	softwareAssignments, err := softwarestore.NewMongoAssignmentRepo(db)
	if err != nil {
		return err
	}
	// The metadata repo lets a deploy completion fill the mirrored OS image display name at once
	// (RefreshServer), so the fleet list shows the friendly name instead of the raw id while the
	// worker observes the deploy, rather than waiting for the next reconcile pass.
	osImageMetadata, err := provisioningstore.NewMongoOSImageMetadataRepo(db)
	if err != nil {
		return err
	}
	deployments := provisioningapp.NewDeployServersUseCase(servers, templates, providers, osImageVerifications, sshKeys)
	// The live Server Lock check every host mutation of this process passes.
	protection := providerServerMutationGuard{servers: servers, providers: providers}
	providerExecutor := providerStepExecutor{
		deployments: deployments,
		release:     provisioningapp.NewReleaseServerUseCase(servers, providers, tasks),
		refresh:     provisioningapp.NewRefreshServerUseCase(servers, providers, osImageMetadata),
		servers:     servers, providers: providers, secrets: operationSecrets, tasks: tasks, poll: 5 * time.Second,
		cleanup:        provisioningapp.NewReleaseNetworkCleanup(tasks, servers, providers),
		configurations: automationConfigurations,
		// The executor re-checks the provider-owned Server Lock before every host
		// mutation; an acceptance-time check alone can go stale in the durable queue.
		protection: protection,
	}

	// Boot Media's ensure-boot-media Task (decision 047) drives the BMC over Redfish with
	// the connection read live from the provisioner. An OS deployment's Task carries the Boot ISO
	// URL frozen at acceptance; an inspect-hardware Task resolves the Server's Boot ISO when it
	// runs (decision 053), so the worker's catalog needs the Boot Media directory and base URL like
	// the API's. The Redfish capability pass probes BMCs through it too. The use case gets the same
	// live lock guard as in the API, so any of its paths that changes a host's boot checks the lock.
	bootISORepo, err := provisioningstore.NewMongoBootISORepo(db)
	if err != nil {
		return fmt.Errorf("boot ISO repo init: %w", err)
	}
	bootISOs := newBootISOCatalog(bootISORepo,
		genfsimg.NewGenfsimgBuilder(cfg.BootMedia.Dir, cfg.BootMedia.IPXEDir, cfg.BootMedia.BaseURL),
		cfg.BootMedia.BaseURL)
	bootMedia := serverapp.NewBootMediaUseCase(servers, servers, protection,
		newBootMediaEndpointSource(providers, servers), redfish.NewController(), libvirtHosts, bootISOs)
	providerExecutor.bootMedia = bootMedia
	// The observation passes read external systems into swallow's observed state and need no
	// Workflow executor, so they start before the worker waits for Temporal and keep running if
	// Temporal is unavailable.
	runtime := workerRuntime(db, convergence.RuntimeOptions{ResyncInterval: cfg.ControllerResyncInterval})
	reconcile := provisioningapp.NewReconcileUseCase(integrations, servers, providers, osImageMetadata,
		installedSoftwareRecords{assignments: softwareAssignments})
	inventory := provisioningapp.NewInventorySweepUseCase(integrations, servers, providers)
	// The passes stop when the worker returns, also when it gives up on Temporal, and their leases
	// are released before it does.
	passCtx, stopPasses := context.WithCancel(ctx)
	passesDone := make(chan struct{})
	go func() {
		defer close(passesDone)
		runtime.RunPeriodic(passCtx, observationPasses(cfg, reconcile, inventory, membership, bootMedia)...)
	}()
	defer func() {
		stopPasses()
		<-passesDone
	}()

	temporalClient, err := dialTemporal(ctx, client.Options{
		HostPort: cfg.TemporalAddress, Namespace: cfg.TemporalNamespace,
	}, temporalDialTimeout)
	if err != nil {
		return fmt.Errorf("temporal connect: %w", err)
	}
	defer temporalClient.Close()

	ownership, err := workflowstore.NewMongoOwnershipRepo(db)
	if err != nil {
		return fmt.Errorf("resource ownership repo: %w", err)
	}
	// One WorkflowService accepts the Workflows this process plans: the platform controller's
	// realizations and the automatic hardware inspections.
	workflows := workflowapp.NewWorkflowService(transactor, operations, workflowPlanCatalog(modules.workflowKinds, modules.software),
		ownership, operationSecrets)

	// Finalizes the release-and-uninstall path's complete-uninstall step, and builds the Platform
	// records the platform controller deploys onto. The worker never deletes a Platform, but its
	// PlatformService still cancels through this process's WorkflowService rather than skipping
	// cancellation.
	sites, err := sitestore.NewMongoSiteRepo(db)
	if err != nil {
		return err
	}
	platformFinalizer, err := newPlatformService(transactor, platforms, sites, servers, operations, integrations,
		softwareAssignments, platformOperationCanceler{orchestrations: workflows})
	if err != nil {
		return err
	}
	providerExecutor.virtualMachines = &virtualMachineEnroller{
		libvirt: libvirtHosts, isos: bootISOs, bootMedia: bootMedia, integrations: integrations,
	}
	runners := map[workflowdomain.RunnerKind]temporalworkflow.StepLifecycleExecutor{
		workflowdomain.RunnerKindInternal: platformWorkflowStepExecutor{
			servers: servers, configurations: automationConfigurations, membership: membership,
			finalizer: platformFinalizer, imageVerifications: osImageVerifications,
			software: softwareAssignments, bootMedia: bootMedia, bootISOs: bootISOs, poll: 5 * time.Second,
		},
		workflowdomain.RunnerKindProvisioner: providerExecutor,
	}
	// The store-driven engine (decision 072) is the only writer of execution state. The worker's
	// Runners return no result data, so it registers only projections; the Ansible executor records
	// results.
	engine, err := workflowapp.NewEngine(transactor, operations, ownership, workflowPlanCatalog(modules.workflowKinds, modules.software),
		workflowapp.CompletionHandlers{Projections: workflowProjections(servers, softwareAssignments, platforms, platforms, platforms)},
		cfg.OperationMaxParallelism)
	if err != nil {
		return err
	}
	driverActivities := temporalworkflow.NewDriverActivities(engine)
	taskActivities := temporalworkflow.NewTaskActivities(engine, runners, map[string]temporalworkflow.CompensationExecutor{
		compensationReturnCanceledDeployment: canceledDeploymentCompensation{runner: providerExecutor},
	})
	temporalWorker := worker.New(temporalClient, cfg.TemporalTaskQueue, worker.Options{WorkerStopTimeout: 10 * time.Second})
	temporalWorker.RegisterWorkflowWithOptions(temporalworkflow.DriverWorkflow,
		workflowregister.RegisterOptions{Name: temporalworkflow.DriverWorkflowName})
	for name, fn := range map[string]any{
		temporalworkflow.ActivityAdvance:        driverActivities.Advance,
		temporalworkflow.ActivityRecordFailure:  driverActivities.RecordFailure,
		temporalworkflow.ActivityBeginCancel:    driverActivities.BeginCancel,
		temporalworkflow.ActivityFinishCanceled: driverActivities.FinishCanceled,
		temporalworkflow.ActivityExecuteTask:    taskActivities.ExecuteTask,
		temporalworkflow.ActivityCompensate:     taskActivities.Compensate,
	} {
		temporalWorker.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	workerRoute := temporalworkflow.RunnerRoute{TaskQueue: cfg.TemporalTaskQueue}
	executor := temporalworkflow.NewExecutor(temporalClient, cfg.TemporalTaskQueue, map[string]temporalworkflow.RunnerRoute{
		string(workflowdomain.RunnerKindProvisioner): workerRoute,
		string(workflowdomain.RunnerKindInternal):    workerRoute,
		// Ansible Tasks run in the Ansible executor, once: their outcome cannot be proven after
		// that process stopped.
		string(workflowdomain.RunnerKindHostAutomation): {TaskQueue: cfg.AnsibleTaskQueue, SingleAttempt: true},
	})

	if err := temporalWorker.Start(); err != nil {
		return fmt.Errorf("start temporal worker: %w", err)
	}
	slog.Info("Temporal worker started", "address", cfg.TemporalAddress,
		"namespace", cfg.TemporalNamespace, "taskQueue", cfg.TemporalTaskQueue)
	convergenceEvents, err := convergencestore.NewMongoEventRecorder(ctx, db)
	if err != nil {
		return fmt.Errorf("convergence event store init: %w", err)
	}
	zones, err := infrastructurestore.NewMongoZoneRepo(db)
	if err != nil {
		return fmt.Errorf("zone repo init: %w", err)
	}
	pools, err := infrastructurestore.NewMongoPoolRepo(db)
	if err != nil {
		return fmt.Errorf("pool repo init: %w", err)
	}
	subnetDNS, err := provisioningstore.NewMongoSubnetDNSRepo(db)
	if err != nil {
		return fmt.Errorf("subnet DNS repo init: %w", err)
	}
	// The Local Repository controller converges each Site's repository Integration to what the
	// modules declare and the Site's spec adds; the Workflows this process accepts read the status it
	// records to point their hosts there.
	localRepositories := localrepostore.NewMongoLocalRepositoryRepo(db)
	platformOps, err := workerPlatformOperations(cfg, db, transactor, modules, workerPlatformDeps{
		platformService: platformFinalizer, platforms: platforms, servers: servers, operations: operations,
		workflows: workflows, automation: automationConfigurations, providers: providers,
		deployments: deployments, software: softwareAssignments, bootISOs: bootISOs, keys: sshKeys,
		localRepository: localrepoapp.NewResolver(integrations, localRepositories, modules.artifacts),
	})
	if err != nil {
		return err
	}
	changePlans, err := newChangePlanService(ctx, db, transactor, cfg.ChangePlanTTL, platformOps.planner)
	if err != nil {
		return err
	}
	// The platform controller converges Platforms to their specs with the same operations the API
	// accepts deployments through (desired-state-model §5.2).
	platformController := platformapp.NewPlatformController(platforms, platforms, servers, servers,
		platformLifecycleReader{orchestrations: operations}, platformDeploymentRecords{orchestrations: operations},
		workflowPlatforms{orchestrations: operations}, platformWorkflowCanceler{orchestrations: operations, workflows: workflows},
		changePlans, platformOps.planner, modules.platforms)
	controllers := []convergence.Controller{
		workflowapp.NewDispatcher(operations, engine, executor, cfg.WorkflowResyncInterval),
		provisioningapp.NewServerController(servers, servers, providers, serverWorkflowHolds{leases: ownership},
			serverGroupCatalog{zones: zones, pools: pools}, convergenceEvents),
		infrastructureapp.NewCatalogController(zones, pools, groupingTargets{integrations: integrations}, providers, convergenceEvents),
		provisioningapp.NewHTTPProxyController(provisionerIntegrations{integrations: integrations}, providers,
			provisioningstore.NewMongoHTTPProxyRepo(db), convergenceEvents),
		provisioningapp.NewSubnetDNSController(provisionerIntegrations{integrations: integrations}, providers,
			subnetDNS, convergenceEvents),
		sshKeyController(sshKeys, convergenceEvents),
		// An enabled provisioner's autoInspect policy, realized by inspect-hardware Workflows.
		provisioningapp.NewAutoInspectController(integrations, servers, hardwareInspectionLauncher{
			workflows: workflows, history: operations, servers: servers, protection: protection,
		}),
		convergence.NewChangePlanExpiryController(changePlans),
		platformController,
		localrepoapp.NewController(sites, integrations, localRepositories,
			pulp.NewRepositoryFactory(integrations, nil), modules.artifacts),
	}
	// The Node Exporter controller starts installs on its own, so an installation sharing machines
	// it was not asked to manage turns it off (config.APIConfig.NodeExporterEnsure).
	nodeExporterController, err := newNodeExporterController(nodeExporterControllerDeps{
		catalog: modules.software, software: platformOps.software, servers: servers, assignments: softwareAssignments,
		automation: automationRepo, workflows: operations, canceler: workflows,
		metrics: promstack.NewMonitoringFactory(integrations),
	})
	if err != nil {
		return err
	}
	if cfg.NodeExporterEnsure {
		controllers = append(controllers, nodeExporterController)
		slog.Info("Node Exporter controller enabled", "controller", nodeExporterController.Name())
	} else {
		slog.Info("Node Exporter controller disabled (nodeExporterEnsure, SWALLOW_WORKER_NODE_EXPORTER_ENSURE, is false); it is not registered")
	}
	controllersDone := make(chan struct{})
	go func() {
		defer close(controllersDone)
		runtime.Run(ctx, controllers...)
	}()
	<-ctx.Done()
	temporalWorker.Stop()
	<-controllersDone
	return nil
}
