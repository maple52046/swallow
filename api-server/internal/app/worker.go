package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	mongoopts "go.mongodb.org/mongo-driver/mongo/options"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	workflowregister "go.temporal.io/sdk/workflow"

	"github.com/maple52046/swallow/config"
	discoveryapp "github.com/maple52046/swallow/internal/discovery/application"
	"github.com/maple52046/swallow/internal/migration"
	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	operationinfra "github.com/maple52046/swallow/internal/operation/infra"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	platformapp "github.com/maple52046/swallow/internal/platform/application"
	platforminfra "github.com/maple52046/swallow/internal/platform/infra"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioninginfra "github.com/maple52046/swallow/internal/provisioning/infra"
	serverapp "github.com/maple52046/swallow/internal/server/application"
	serverinfra "github.com/maple52046/swallow/internal/server/infra"
	"github.com/maple52046/swallow/internal/server/infra/redfish"
	"github.com/maple52046/swallow/internal/shared/secret"
	siteinfra "github.com/maple52046/swallow/internal/site/infra"
	softwareinfra "github.com/maple52046/swallow/internal/software/infra"
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
	mongoClient, err := mongo.Connect(connectCtx, mongoopts.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return fmt.Errorf("mongo connect: %w", err)
	}
	defer mongoClient.Disconnect(context.Background())
	if err := mongoClient.Ping(connectCtx, nil); err != nil {
		return fmt.Errorf("mongo ping: %w", err)
	}
	db := mongoClient.Database(cfg.MongoDB)
	if err := migration.Check(connectCtx, db); err != nil {
		return fmt.Errorf("database schema: %w", err)
	}
	operations, err := operationinfra.NewMongoWorkflowRepo(db)
	if err != nil {
		return fmt.Errorf("orchestration repo: %w", err)
	}
	leases := operationinfra.NewMongoResourceLeaseRepo(db)
	ansibleExecutions, err := operationinfra.NewMongoAnsibleExecutionRepo(db)
	if err != nil {
		return fmt.Errorf("Ansible execution repo: %w", err)
	}
	operationSecrets, err := operationinfra.NewMongoOperationSecretRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("operation secret repo: %w", err)
	}
	integrations, err := siteinfra.NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		return err
	}
	servers, err := serverinfra.NewMongoServerRepo(db)
	if err != nil {
		return err
	}
	inventory := executionInventoryAdapter{discovery: discoveryapp.NewDiscoveryUseCase(servers)}
	providers := provisioninginfra.NewProviderFactory(integrations)
	// The worker reads the Deployment Key for wait-for-ssh and ensures provisioner key registration
	// before every OS deployment it drives (decision 039). It does not run the periodic sync loop.
	sshKeys, err := newSSHKeyService(db, sealer, integrations, providers)
	if err != nil {
		return err
	}
	automationConfigurations := operationapp.NewEffectiveAutomationConfigurations(
		operationinfra.NewMongoAutomationConfigurationRepo(db, sealer), deploymentKeySource{keys: sshKeys})
	platforms, err := platforminfra.NewMongoPlatformRepo(db)
	if err != nil {
		return err
	}
	membership := platformapp.NewMembershipSyncUseCase(platforms, servers, platforminfra.NewReaderFactory(integrations))
	templates, err := provisioninginfra.NewMongoDeploymentTemplateRepo(db, sealer)
	if err != nil {
		return err
	}
	tasks, err := provisioninginfra.NewMongoProvisioningTaskRepo(db)
	if err != nil {
		return err
	}
	osImageVerifications, err := provisioninginfra.NewMongoOSImageVerificationRepo(db)
	if err != nil {
		return err
	}
	// Software Assignments back the software Workflow's internal record step (installed/absent) and
	// the step observer's failure marking (decision 038).
	softwareAssignments, err := softwareinfra.NewMongoAssignmentRepo(db)
	if err != nil {
		return err
	}
	// The overlay repo lets a deploy completion fill the mirrored OS image display name at once
	// (RefreshServer), so the fleet list shows the friendly name instead of the raw id while the
	// worker observes the deploy, rather than waiting for the next reconcile pass.
	osImageOverlays, err := provisioninginfra.NewMongoOSImageOverlayRepo(db)
	if err != nil {
		return err
	}
	deployments := provisioningapp.NewDeployServersUseCase(servers, templates, providers, osImageVerifications)
	deployments.AttachDeploymentKeys(sshKeys)
	providerExecutor := providerStepExecutor{
		deployments: deployments,
		release:     provisioningapp.NewReleaseServerUseCase(servers, providers, tasks),
		refresh:     provisioningapp.NewRefreshServerUseCase(servers, providers, osImageOverlays),
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

	temporalClient, err := client.Dial(client.Options{
		HostPort: cfg.TemporalAddress, Namespace: cfg.TemporalNamespace,
	})
	if err != nil {
		return fmt.Errorf("temporal connect: %w", err)
	}
	defer temporalClient.Close()

	// Finalizes the release-and-uninstall path's complete-uninstall step. Only CompleteUninstall
	// is used, so sites and lifecycle are nil; it needs the platform, server, and owned-credential
	// integration repositories the worker already has.
	platformFinalizer := platformapp.NewPlatformService(
		platforms, nil, servers, nil,
		managedPlatformIntegrationCleaner{integrations: integrations},
	)
	// Boot Media's ensure-boot-media Task (decisions 047 and 049) drives the BMC over Redfish with
	// the connection read live from the provisioner. An OS deployment's Task carries the Boot ISO
	// URL frozen at acceptance; an inspect-hardware Task resolves the Server's Boot ISO when it
	// runs (decision 053), so the worker's catalog needs the Boot Media directory and base URL like
	// the API's. The enable and view paths of the use case are never called here, and the guard is
	// unused.
	bootISORepo, err := provisioninginfra.NewMongoBootISORepo(db)
	if err != nil {
		return fmt.Errorf("boot ISO repo init: %w", err)
	}
	bootISOs := newBootISOCatalog(bootISORepo,
		provisioninginfra.NewGenfsimgBuilder(cfg.BootMedia.Dir, cfg.BootMedia.IPXEDir, cfg.BootMedia.BaseURL),
		cfg.BootMedia.BaseURL)
	bootMedia := serverapp.NewBootMediaUseCase(servers, servers, nil,
		bmcEndpointSource{providers: providers}, redfish.NewController(), bootISOs)
	providerExecutor.bootMedia = bootMedia
	activities := temporalworkflow.NewActivities(operations, leases, map[operationdomain.RunnerKind]temporalworkflow.StepLifecycleExecutor{
		operationdomain.RunnerKindInternal: platformWorkflowStepExecutor{
			servers: servers, configurations: automationConfigurations, membership: membership,
			finalizer: platformFinalizer, imageVerifications: osImageVerifications,
			software: softwareAssignments, bootMedia: bootMedia, bootISOs: bootISOs, poll: 5 * time.Second,
		},
		operationdomain.RunnerKindAnsible:     temporalworkflow.NewAnsibleStepExecutor(ansibleExecutions, automationConfigurations, inventory, temporalworkflow.NewSSHKeyscanHostKeyScanner(), operations, cfg.JobArtifactDir, 2*time.Second),
		operationdomain.RunnerKindProvisioner: providerExecutor,
	}, serverDeploymentStepObserver{servers: servers}, softwareAssignmentObserver{assignments: softwareAssignments})
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
	go reconciler.Run(ctx)
	slog.Info("Temporal worker started", "address", cfg.TemporalAddress,
		"namespace", cfg.TemporalNamespace, "taskQueue", cfg.TemporalTaskQueue)
	<-ctx.Done()
	temporalWorker.Stop()
	return nil
}
