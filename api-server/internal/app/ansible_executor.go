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

	"github.com/maple52046/swallow/config"
	discoveryapp "github.com/maple52046/swallow/internal/discovery/application"
	"github.com/maple52046/swallow/internal/migration"
	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationinfra "github.com/maple52046/swallow/internal/operation/infra"
	platformapp "github.com/maple52046/swallow/internal/platform/application"
	platforminfra "github.com/maple52046/swallow/internal/platform/infra"
	provisioninginfra "github.com/maple52046/swallow/internal/provisioning/infra"
	serverinfra "github.com/maple52046/swallow/internal/server/infra"
	"github.com/maple52046/swallow/internal/shared/secret"
	siteinfra "github.com/maple52046/swallow/internal/site/infra"
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
	integrations, err := siteinfra.NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		return err
	}
	servers, err := serverinfra.NewMongoServerRepo(db)
	if err != nil {
		return err
	}
	executions, err := operationinfra.NewMongoAnsibleExecutionRepo(db)
	if err != nil {
		return err
	}
	legacyOperations, err := operationinfra.NewMongoExecutionRepo(db, sealer)
	if err != nil {
		return err
	}
	orchestrations, err := operationinfra.NewMongoOrchestrationRepo(db)
	if err != nil {
		return err
	}
	operationSecrets, err := operationinfra.NewMongoOperationSecretRepo(db, sealer)
	if err != nil {
		return err
	}
	catalog, err := operationinfra.LoadManifestCatalog(cfg.PlaybookManifest, cfg.PlaybookDir)
	if err != nil {
		return err
	}
	configurations := operationinfra.NewMongoAutomationConfigurationRepo(db, sealer)
	runner := operationinfra.NewLocalRunner(cfg.AnsibleRunnerCommand, catalog.ProjectRoot(), cfg.JobRuntimeDir, cfg.JobArtifactDir)
	providers := provisioninginfra.NewProviderFactory(integrations)
	protection := providerServerMutationGuard{servers: servers, providers: providers}
	discovery := discoveryapp.NewDiscoveryUseCase(servers)
	sites, err := siteinfra.NewMongoSiteRepo(db)
	if err != nil {
		return err
	}
	platforms, err := platforminfra.NewMongoPlatformRepo(db)
	if err != nil {
		return err
	}
	membership := platformapp.NewMembershipSyncUseCase(platforms, servers, platforminfra.NewReaderFactory(integrations))
	lifecycles := platformLifecycleReader{operations: legacyOperations, orchestrations: orchestrations}
	platformService := platformapp.NewPlatformService(platforms, sites, servers, lifecycles, managedPlatformIntegrationCleaner{integrations: integrations})
	credentials := platformapp.NewDeploymentCredentialService(platforms, integrations, membership)
	durable := operationapp.NewOrchestrationService(orchestrations, nil, operationSecrets)
	operationService := operationapp.NewExecutionService(legacyOperations, servers, configurations, catalog, runner, platformapp.NewPolicyChecker(platforms, servers), protection)
	operationService.AttachOrchestration(durable)
	completion := platformDeploymentObserver{credentials: credentials, platforms: platformService, operations: operationService, servers: servers}
	queue := operationapp.NewAnsibleQueueWorker(
		executions, configurations, catalog, runner,
		executionInventoryAdapter{discovery: discovery}, protection, completion,
		cfg.OperationDispatchInterval, cfg.OperationLeaseDuration, 4, operationSecrets,
	)
	slog.Info("Ansible executor started", "parallelism", 4, "artifactDir", cfg.JobArtifactDir)
	queue.Run(ctx)
	return nil
}
