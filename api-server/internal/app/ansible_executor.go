package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maple52046/swallow/config"
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
	"github.com/maple52046/swallow/internal/shared/secret"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
	"github.com/maple52046/swallow/modules/platform/platformapi"
)

// RunAnsibleExecutor owns all v3 ansible-runner processes. Stopping this process leaves
// running queue leases to expire as requires_attention instead of guessing an outcome.
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
	executions, err := workflowstore.NewMongoAnsibleExecutionRepo(db)
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
	durable := workflowapp.NewWorkflowService(transactor, orchestrations, nil, operationSecrets)
	operationService := workflowapp.NewExecutionService(legacyOperations, servers, configurations, catalog, runner, ansibleEvents, platformapp.NewPolicyChecker(platforms, servers), protection)
	operationService.AttachWorkflow(durable)
	completion := platformDeploymentObserver{credentials: credentials, platforms: platformService, operations: operationService, servers: servers}
	leases := workflowstore.NewMongoResourceLeaseRepo(db)
	queue := workflowapp.NewAnsibleQueueWorker(
		executions, configurations, catalog, runner, ansibleEvents,
		executionInventoryAdapter{discovery: discovery}, protection, leases, completion,
		cfg.OperationDispatchInterval, cfg.OperationLeaseDuration, cfg.OperationMaxParallelism, operationSecrets,
	)
	slog.Info("Ansible executor started", "parallelism", cfg.OperationMaxParallelism, "artifactDir", cfg.JobArtifactDir)
	queue.Run(ctx)
	return nil
}
