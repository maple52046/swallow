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

	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	"github.com/maple52046/swallow/internal/adapters/hostautomation/ansible"
	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	"github.com/maple52046/swallow/internal/adapters/store/mongodb/migration"
	platformstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/platform"
	serverstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/server"
	sitestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/site"
	softwarestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/software"
	workflowstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/workflow"
	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
	"github.com/maple52046/swallow/modules/platform/platformapi"
)

// RunAnsibleExecutor owns every ansible-runner process: it is the Temporal worker of the Ansible
// task queue (decision 072). Stopping it ends its single-attempt activities, whose Tasks become
// requires_attention instead of guessing an outcome.
func RunAnsibleExecutor(cfg config.APIConfig) error {
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
	integrations, err := sitestore.NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		return err
	}
	servers, err := serverstore.NewMongoServerRepo(db)
	if err != nil {
		return err
	}
	ansibleEvents, err := workflowstore.NewMongoAnsibleEventRepo(db)
	if err != nil {
		return err
	}
	legacyOperations, err := workflowstore.NewMongoExecutionRepo(db, sealer)
	if err != nil {
		return err
	}
	orchestrations, err := workflowstore.NewMongoWorkflowRepo(db)
	if err != nil {
		return err
	}
	operationSecrets, err := workflowstore.NewMongoOperationSecretRepo(db, sealer)
	if err != nil {
		return err
	}
	catalog, err := ansible.LoadManifestCatalog(cfg.PlaybookManifest, cfg.PlaybookDir)
	if err != nil {
		return err
	}
	providers := maas.NewProviderFactory(integrations)
	// Every run authenticates with the Deployment Key, the only automation key (decision 041),
	// through the effective-credential decorator built below.
	sshKeys, err := newSSHKeyService(db, sealer, integrations, providers)
	if err != nil {
		return err
	}
	configurations := workflowapp.NewEffectiveAutomationConfigurations(
		workflowstore.NewMongoAutomationConfigurationRepo(db, sealer), deploymentKeySource{keys: sshKeys})
	runner := ansible.NewLocalRunner(cfg.AnsibleRunnerCommand, catalog.ProjectRoot(), cfg.JobRuntimeDir, cfg.JobArtifactDir)
	// Resolve each host's SSH login user per host (images use different default users), so a fleet
	// mixing image families deploys and is managed under one Site without hand-switching sshUser.
	runner.AttachUserProber(sshprobe.DefaultProber{})
	protection := providerServerMutationGuard{servers: servers, providers: providers}
	discovery := discoveryapp.NewDiscoveryUseCase(servers)
	sites, err := sitestore.NewMongoSiteRepo(db)
	if err != nil {
		return err
	}
	platforms, err := platformstore.NewMongoPlatformRepo(db)
	if err != nil {
		return err
	}
	membership := platformapp.NewMembershipSyncUseCase(platforms, servers, platformapi.NewReaderFactory(integrations))
	lifecycles := platformLifecycleReader{operations: legacyOperations, orchestrations: orchestrations}
	platformService := platformapp.NewPlatformService(transactor, platforms, sites, servers, lifecycles, managedPlatformIntegrationCleaner{integrations: integrations})
	platformService.AttachGPUOperatorRecorder(platforms)
	platformService.AttachOmnistatRecorder(platforms)
	platformService.AttachDeviceMetricsExporterRecorder(platforms)
	// An uninstall completed by its ansible Step frees its Servers (decision 067) and, for Slurm,
	// retires their Device Metrics Exporter records (decision 068).
	platformService.AttachPlatformClaims(servers)
	softwareAssignments, err := softwarestore.NewMongoAssignmentRepo(db)
	if err != nil {
		return err
	}
	platformService.AttachDeviceMetricsExporterAssignments(deviceMetricsExporterAssignments{assignments: softwareAssignments})
	credentials := platformapp.NewDeploymentCredentialService(transactor, platforms, integrations, membership)
	durable := workflowapp.NewWorkflowService(transactor, orchestrations, workflowPlanCatalog(), operationSecrets)
	operationService := workflowapp.NewExecutionService(legacyOperations, servers, configurations, catalog, runner, ansibleEvents, platformapp.NewPolicyChecker(platforms, servers), protection)
	operationService.AttachWorkflow(durable)
	results := workflowResults(platformDeploymentObserver{credentials: credentials, platforms: platformService, operations: operationService, servers: servers})
	// Ansible Tasks arrive as Temporal activities on the executor's own task queue (decision 072).
	temporalClient, err := dialTemporal(ctx, client.Options{HostPort: cfg.TemporalAddress, Namespace: cfg.TemporalNamespace}, temporalDialTimeout)
	if err != nil {
		return fmt.Errorf("temporal connect: %w", err)
	}
	defer temporalClient.Close()
	ownership, err := workflowstore.NewMongoOwnershipRepo(db)
	if err != nil {
		return fmt.Errorf("resource ownership repo: %w", err)
	}
	engine, err := workflowapp.NewEngine(transactor, orchestrations, ownership, workflowPlanCatalog(), workflowapp.CompletionHandlers{
		Projections: workflowProjections(servers, softwareAssignments, platforms, platforms, platforms), Results: results,
	}, cfg.OperationMaxParallelism)
	if err != nil {
		return err
	}
	ansibleTasks, err := workflowapp.NewAnsibleTaskRunner(configurations, catalog, runner, ansibleEvents,
		executionInventoryAdapter{discovery: discovery}, protection, operationSecrets,
		temporalworkflow.NewSSHKeyscanHostKeyScanner(), engine, cfg.JobArtifactDir)
	if err != nil {
		return err
	}
	tasks := temporalworkflow.NewTaskActivities(engine, map[workflowdomain.RunnerKind]temporalworkflow.StepLifecycleExecutor{
		workflowdomain.RunnerKindAnsible: ansibleTaskExecutor{runner: ansibleTasks},
	}, nil)
	ansibleWorker := worker.New(temporalClient, cfg.AnsibleTaskQueue, worker.Options{
		MaxConcurrentActivityExecutionSize: cfg.OperationMaxParallelism, WorkerStopTimeout: 10 * time.Second,
	})
	ansibleWorker.RegisterActivityWithOptions(tasks.ExecuteTask, activity.RegisterOptions{Name: temporalworkflow.ActivityExecuteTask})
	ansibleWorker.RegisterActivityWithOptions(tasks.Compensate, activity.RegisterOptions{Name: temporalworkflow.ActivityCompensate})
	if err := ansibleWorker.Start(); err != nil {
		return fmt.Errorf("start the Ansible task worker: %w", err)
	}
	defer ansibleWorker.Stop()
	slog.Info("Ansible executor started", "parallelism", cfg.OperationMaxParallelism, "artifactDir", cfg.JobArtifactDir,
		"taskQueue", cfg.AnsibleTaskQueue)
	<-ctx.Done()
	return nil
}

// ansibleTaskExecutor adapts the core Ansible Task runner to the executor adapter's Runner port.
type ansibleTaskExecutor struct {
	runner *workflowapp.AnsibleTaskRunner
}

// Execute implements temporalworkflow.StepLifecycleExecutor.
func (e ansibleTaskExecutor) Execute(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	outcome := e.runner.Run(ctx, workflowapp.AnsibleTask{
		WorkflowID: input.OperationID, Kind: input.Kind, PlatformID: input.PlatformID, SiteID: input.SiteID,
		Generation: input.Generation, Task: input.Step, Resumed: input.Resumed,
	})
	return temporalworkflow.StepExecutionResult{
		Status: outcome.Status, Progress: outcome.Progress, WaitingReason: outcome.WaitingReason, Error: outcome.Error,
		ExternalExecution: outcome.ExternalExecution, Artifacts: outcome.Artifacts, Data: outcome.Data,
	}
}
