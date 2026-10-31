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
	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	infrastructurestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/infrastructure"
	"github.com/maple52046/swallow/internal/adapters/store/mongodb/migration"
	platformstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/platform"
	provisioningstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/provisioning"
	serverstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/server"
	sitestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/site"
	softwarestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/software"
	workflowstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/workflow"
	"github.com/maple52046/swallow/internal/core/convergence"
	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	infrastructureapp "github.com/maple52046/swallow/internal/core/infrastructure/application"
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	serverapp "github.com/maple52046/swallow/internal/core/server/application"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
	"github.com/maple52046/swallow/modules/platform/platformapi"
)

// RunWorker starts the Temporal workflow/activity worker. It owns orchestration only;
// HTTP and periodic inventory reconciliation remain in the API process.
func RunWorker(cfg config.APIConfig) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
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
	leases := workflowstore.NewMongoResourceLeaseRepo(db)
	ansibleExecutions, err := workflowstore.NewMongoAnsibleExecutionRepo(db)
	if err != nil {
		return fmt.Errorf("Ansible execution repo: %w", err)
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
	inventory := executionInventoryAdapter{discovery: discoveryapp.NewDiscoveryUseCase(servers)}
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
	platforms, err := platformstore.NewMongoPlatformRepo(db)
	if err != nil {
		return err
	}
	membership := platformapp.NewMembershipSyncUseCase(platforms, servers, platformapi.NewReaderFactory(integrations))
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
	deployments := provisioningapp.NewDeployServersUseCase(servers, templates, providers, osImageVerifications)
	deployments.AttachDeploymentKeys(sshKeys)
	providerExecutor := providerStepExecutor{
		deployments: deployments,
		release:     provisioningapp.NewReleaseServerUseCase(servers, providers, tasks),
		refresh:     provisioningapp.NewRefreshServerUseCase(servers, providers, osImageMetadata),
		servers:     servers, providers: providers, secrets: operationSecrets, tasks: tasks, poll: 5 * time.Second,
		configurations: automationConfigurations,
		// The executor re-checks the provider-owned Server Lock before every host
		// mutation; an acceptance-time check alone can go stale in the durable queue.
		protection: providerServerMutationGuard{servers: servers, providers: providers},
	}
	projectionCtx, cancelProjection := context.WithTimeout(ctx, 30*time.Second)
	if err := reconcileLegacyDeploymentProjections(projectionCtx, operations, providerExecutor); err != nil {
		slog.Warn("legacy Server deployment projection reconciliation incomplete", "error", err)
	}
	cancelProjection()
	taskWorker := provisioningapp.NewProvisioningTaskWorker(tasks, servers, providers, 5*time.Second, 30*time.Second)

	temporalClient, err := dialTemporal(ctx, client.Options{
		HostPort: cfg.TemporalAddress, Namespace: cfg.TemporalNamespace,
	}, temporalDialTimeout)
	if err != nil {
		return fmt.Errorf("temporal connect: %w", err)
	}
	defer temporalClient.Close()

	// Finalizes the release-and-uninstall path's complete-uninstall step. Only CompleteUninstall
	// is used, so sites and lifecycle are nil; it needs the platform, server, and owned-credential
	// integration repositories the worker already has.
	platformFinalizer := platformapp.NewPlatformService(transactor, platforms, nil, servers, nil,
		managedPlatformIntegrationCleaner{integrations: integrations},
	)
	platformFinalizer.AttachGPUOperatorRecorder(platforms)
	platformFinalizer.AttachOmnistatRecorder(platforms)
	platformFinalizer.AttachDeviceMetricsExporterRecorder(platforms)
	// A completed uninstall frees its Servers (decision 067) and, for Slurm, retires their Device
	// Metrics Exporter records (decision 068).
	platformFinalizer.AttachPlatformClaims(servers)
	platformFinalizer.AttachDeviceMetricsExporterAssignments(deviceMetricsExporterAssignments{assignments: softwareAssignments})
	// Boot Media's ensure-boot-media Task (decisions 047 and 049) drives the BMC over Redfish with
	// the connection read live from the provisioner. An OS deployment's Task carries the Boot ISO
	// URL frozen at acceptance; an inspect-hardware Task resolves the Server's Boot ISO when it
	// runs (decision 053), so the worker's catalog needs the Boot Media directory and base URL like
	// the API's. The enable and view paths of the use case are never called here, and the guard is
	// unused.
	bootISORepo, err := provisioningstore.NewMongoBootISORepo(db)
	if err != nil {
		return fmt.Errorf("boot ISO repo init: %w", err)
	}
	bootISOs := newBootISOCatalog(bootISORepo,
		genfsimg.NewGenfsimgBuilder(cfg.BootMedia.Dir, cfg.BootMedia.IPXEDir, cfg.BootMedia.BaseURL),
		cfg.BootMedia.BaseURL)
	bootMedia := serverapp.NewBootMediaUseCase(servers, servers, nil,
		newBootMediaEndpointSource(providers, servers), redfish.NewController(), libvirtHosts, bootISOs)
	providerExecutor.bootMedia = bootMedia
	providerExecutor.virtualMachines = &virtualMachineEnroller{
		libvirt: libvirtHosts, isos: bootISOs, bootMedia: bootMedia, integrations: integrations,
	}
	runners := map[workflowdomain.RunnerKind]temporalworkflow.StepLifecycleExecutor{
		workflowdomain.RunnerKindInternal: platformWorkflowStepExecutor{
			servers: servers, configurations: automationConfigurations, membership: membership,
			finalizer: platformFinalizer, imageVerifications: osImageVerifications,
			software: softwareAssignments, bootMedia: bootMedia, bootISOs: bootISOs, poll: 5 * time.Second,
		},
		workflowdomain.RunnerKindAnsible:     temporalworkflow.NewAnsibleStepExecutor(ansibleExecutions, automationConfigurations, inventory, temporalworkflow.NewSSHKeyscanHostKeyScanner(), operations, cfg.JobArtifactDir, 2*time.Second),
		workflowdomain.RunnerKindProvisioner: providerExecutor,
	}
	observers := []temporalworkflow.StepProjectionObserver{
		serverDeploymentStepObserver{servers: servers}, softwareAssignmentObserver{assignments: softwareAssignments},
		gpuOperatorStepObserver{recorder: platforms}, omnistatStepObserver{recorder: platforms},
		deviceMetricsExporterStepObserver{recorder: platforms},
	}
	activities := temporalworkflow.NewActivities(operations, leases, runners, observers...)
	// The store-driven engine (decision 072) runs every Workflow accepted from now on; the
	// activities above keep running Workflows accepted before it.
	ownership, err := workflowstore.NewMongoOwnershipRepo(db)
	if err != nil {
		return fmt.Errorf("resource ownership repo: %w", err)
	}
	// The worker's Runners return no result data, so it registers only projections; the Ansible
	// executor records results.
	engine, err := workflowapp.NewEngine(transactor, operations, ownership, workflowPlanCatalog(),
		workflowapp.CompletionHandlers{Projections: workflowProjections(servers, softwareAssignments, platforms)},
		cfg.OperationMaxParallelism)
	if err != nil {
		return err
	}
	driverActivities := temporalworkflow.NewDriverActivities(engine)
	taskActivities := temporalworkflow.NewTaskActivities(engine, runners, map[string]temporalworkflow.CompensationExecutor{
		compensationReturnCanceledDeployment: canceledDeploymentCompensation{runner: providerExecutor},
	})
	temporalWorker := worker.New(temporalClient, cfg.TemporalTaskQueue, worker.Options{WorkerStopTimeout: 10 * time.Second})
	temporalWorker.RegisterWorkflowWithOptions(temporalworkflow.OperationWorkflowV1,
		workflowregister.RegisterOptions{Name: temporalworkflow.WorkflowNameV1})
	// Reusable Job child workflow (ADR 017): the parent Operation runs each Job through it.
	temporalWorker.RegisterWorkflowWithOptions(temporalworkflow.JobWorkflowV1,
		workflowregister.RegisterOptions{Name: temporalworkflow.JobWorkflowName})
	temporalWorker.RegisterActivityWithOptions(activities.AcquireLeases,
		activity.RegisterOptions{Name: temporalworkflow.ActivityAcquireLeases})
	temporalWorker.RegisterActivityWithOptions(activities.RenewLeases,
		activity.RegisterOptions{Name: temporalworkflow.ActivityRenewLeases})
	temporalWorker.RegisterActivityWithOptions(activities.ReleaseLeases,
		activity.RegisterOptions{Name: temporalworkflow.ActivityReleaseLeases})
	temporalWorker.RegisterActivityWithOptions(activities.UpdateState,
		activity.RegisterOptions{Name: temporalworkflow.ActivityUpdateState})
	temporalWorker.RegisterActivityWithOptions(activities.UpdateStep,
		activity.RegisterOptions{Name: temporalworkflow.ActivityUpdateStep})
	temporalWorker.RegisterActivityWithOptions(activities.ExecuteStep,
		activity.RegisterOptions{Name: temporalworkflow.ActivityExecuteStep})
	temporalWorker.RegisterActivityWithOptions(activities.AppendEvent,
		activity.RegisterOptions{Name: temporalworkflow.ActivityAppendEvent})
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
		string(workflowdomain.RunnerKindAnsible):     workerRoute,
	})

	go taskWorker.Run(ctx)
	if err := temporalWorker.Start(); err != nil {
		return fmt.Errorf("start temporal worker: %w", err)
	}
	// Run the start reconciler here as well as in the API process. A deployment that
	// runs the worker without the API would otherwise never turn a persisted
	// startState=pending Operation into a Temporal workflow. Duplicate starts are safe:
	// the stable Workflow ID rejects duplicates and the record is reconciled as started.
	starter := temporalworkflow.NewStarter(
		temporalClient, operations, cfg.TemporalTaskQueue, cfg.TemporalStartInterval,
		cfg.OperationLeaseDuration, cfg.OperationMaxParallelism,
	)
	go starter.Run(ctx)
	// Run the lost-execution reconciler here as well as in the API process so a worker-only
	// deployment still surfaces Operations whose execution was lost as repairable. Duplicate
	// sweeps are safe: each write is an idempotent status update.
	reconciler := temporalworkflow.NewReconciler(temporalClient, operations, cfg.ReconcileInterval)
	reconciler.AttachLeaseReleaser(leases)
	go reconciler.Run(ctx)
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
	runControllers := controllerRuntime(db, convergence.RuntimeOptions{ResyncInterval: cfg.ControllerResyncInterval},
		workflowapp.NewDispatcher(operations, engine, executor, cfg.WorkflowResyncInterval, false),
		provisioningapp.NewServerController(servers, servers, providers, serverWorkflowHolds{leases: ownership},
			serverGroupCatalog{zones: zones, pools: pools}, convergenceEvents),
		infrastructureapp.NewCatalogController(zones, pools, groupingTargets{integrations: integrations}, providers, convergenceEvents),
		provisioningapp.NewHTTPProxyController(provisionerIntegrations{integrations: integrations}, providers,
			provisioningstore.NewMongoHTTPProxyRepo(db), convergenceEvents),
		provisioningapp.NewSubnetDNSController(provisionerIntegrations{integrations: integrations}, providers,
			subnetDNS, convergenceEvents),
		sshKeyController(sshKeys, convergenceEvents),
	)
	controllersDone := make(chan struct{})
	go func() {
		defer close(controllersDone)
		runControllers(ctx)
	}()
	<-ctx.Done()
	temporalWorker.Stop()
	<-controllersDone
	return nil
}
