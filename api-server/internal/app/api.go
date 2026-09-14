// Package app provides top-level runtime bootstrap functions.
// Each function accepts a fully assembled config object so that no application
// code needs to parse environment variables or CLI flags directly.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"go.mongodb.org/mongo-driver/mongo"
	mongoopts "go.mongodb.org/mongo-driver/mongo/options"
	temporalclient "go.temporal.io/sdk/client"

	"github.com/maple52046/swallow/bootstrap"
	"github.com/maple52046/swallow/config"
	authapp "github.com/maple52046/swallow/internal/auth/application"
	authdelivery "github.com/maple52046/swallow/internal/auth/delivery"
	authinfra "github.com/maple52046/swallow/internal/auth/infra"
	discoveryapp "github.com/maple52046/swallow/internal/discovery/application"
	discoverydelivery "github.com/maple52046/swallow/internal/discovery/delivery"
	infrastructureapp "github.com/maple52046/swallow/internal/infrastructure/application"
	infrastructuredelivery "github.com/maple52046/swallow/internal/infrastructure/delivery"
	infrastructureinfra "github.com/maple52046/swallow/internal/infrastructure/infra"
	"github.com/maple52046/swallow/internal/migration"
	monitoringapp "github.com/maple52046/swallow/internal/monitoring/application"
	monitoringdelivery "github.com/maple52046/swallow/internal/monitoring/delivery"
	monitoringinfra "github.com/maple52046/swallow/internal/monitoring/infra"
	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdelivery "github.com/maple52046/swallow/internal/operation/delivery"
	operationinfra "github.com/maple52046/swallow/internal/operation/infra"
	temporalworkflow "github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	overviewapp "github.com/maple52046/swallow/internal/overview/application"
	overviewdelivery "github.com/maple52046/swallow/internal/overview/delivery"
	overviewinfra "github.com/maple52046/swallow/internal/overview/infra"
	platformapp "github.com/maple52046/swallow/internal/platform/application"
	platformdelivery "github.com/maple52046/swallow/internal/platform/delivery"
	platforminfra "github.com/maple52046/swallow/internal/platform/infra"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdelivery "github.com/maple52046/swallow/internal/provisioning/delivery"
	provisioninginfra "github.com/maple52046/swallow/internal/provisioning/infra"
	serverapp "github.com/maple52046/swallow/internal/server/application"
	serverdelivery "github.com/maple52046/swallow/internal/server/delivery"
	serverinfra "github.com/maple52046/swallow/internal/server/infra"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/shared/secret"
	siteapp "github.com/maple52046/swallow/internal/site/application"
	sitedelivery "github.com/maple52046/swallow/internal/site/delivery"
	siteinfra "github.com/maple52046/swallow/internal/site/infra"
	"github.com/maple52046/swallow/internal/version"
)

// shutdownTimeout bounds how long in-flight requests get to finish on shutdown.
const shutdownTimeout = 10 * time.Second

var httpRequestCount atomic.Uint64

// RunAPI starts the HTTP API server and the reconciler, and blocks until interrupted.
func RunAPI(cfg config.APIConfig) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	releaseVersion := cfg.ReleaseVersion
	if releaseVersion == "" {
		releaseVersion = version.Version
	}
	slog.Info("starting swallow API",
		"version", releaseVersion, "commit", version.Commit, "builtAt", version.BuiltAt,
		"addr", cfg.Addr, "mongoDatabase", cfg.MongoDB,
		"playbookManifest", cfg.PlaybookManifest, "jobArtifactDir", cfg.JobArtifactDir)
	// Cancelled on SIGINT or SIGTERM, which stops the reconciler and triggers a
	// graceful HTTP shutdown instead of dropping in-flight requests.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sealer, err := secret.NewSealer(cfg.CredentialKey)
	if err != nil {
		return fmt.Errorf("credential key: %w", err)
	}

	connectCtx, cancelConnect := context.WithTimeout(ctx, 15*time.Second)
	defer cancelConnect()

	client, err := mongo.Connect(connectCtx, mongoopts.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(connectCtx, nil); err != nil {
		return fmt.Errorf("mongo ping: %w", err)
	}
	slog.Info("connected to mongodb")
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = client.Disconnect(disconnectCtx)
	}()

	db := client.Database(cfg.MongoDB)
	if err := migration.EnsureInitialized(connectCtx, db); err != nil {
		return fmt.Errorf("database schema: %w", err)
	}

	userRepo := authinfra.NewMongoUserRepo(db)
	siteRepo, err := siteinfra.NewMongoSiteRepo(db)
	if err != nil {
		return fmt.Errorf("site repo init: %w", err)
	}
	integrationRepo, err := siteinfra.NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("integration repo init: %w", err)
	}
	mongoServerRepo, err := serverinfra.NewMongoServerRepo(db)
	if err != nil {
		return fmt.Errorf("server repo init: %w", err)
	}
	// One in-process broker fans Server changes out to SSE subscribers. Wrapping the sole
	// persistence adapter here makes every write in this process (reconcile, refresh,
	// delete) publish; cross-process writes from the worker (deployment/membership) surface
	// through the reconcile pump within its interval, deduplicated by the broker.
	serverEventBroker := serverinfra.NewServerEventBroker()
	serverRepo := serverinfra.NewEventingServerRepository(mongoServerRepo, serverEventBroker)
	templateRepo, err := provisioninginfra.NewMongoDeploymentTemplateRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("deployment template repo init: %w", err)
	}
	// Swallow-owned OS Image name overlays merged onto the provider catalog at read
	// (docs/decisions/025). Owned data, so no sealing or staleness applies.
	osImageOverlayRepo, err := provisioninginfra.NewMongoOSImageOverlayRepo(db)
	if err != nil {
		return fmt.Errorf("os image overlay repo init: %w", err)
	}
	taskRepo, err := provisioninginfra.NewMongoProvisioningTaskRepo(db)
	if err != nil {
		return fmt.Errorf("provisioning task repo init: %w", err)
	}
	operationRepo, err := operationinfra.NewMongoExecutionRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("operation repo init: %w", err)
	}
	orchestrationRepo, err := operationinfra.NewMongoWorkflowRepo(db)
	if err != nil {
		return fmt.Errorf("orchestration repo init: %w", err)
	}
	operationSecretRepo, err := operationinfra.NewMongoOperationSecretRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("operation secret repo init: %w", err)
	}
	temporalClient, err := temporalclient.NewLazyClient(temporalclient.Options{
		HostPort: cfg.TemporalAddress, Namespace: cfg.TemporalNamespace,
	})
	if err != nil {
		return fmt.Errorf("temporal client: %w", err)
	}
	defer temporalClient.Close()

	if err := bootstrap.EnsureAdminUser(
		ctx, userRepo,
		cfg.BootstrapAdminUsername, cfg.BootstrapAdminPassword,
	); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	jwtSvc := jwt.NewService(cfg.JWTSecret, time.Duration(cfg.JWTExpiryHours)*time.Hour)

	authHandler := authdelivery.NewAuthHandler(
		authapp.NewLoginUseCase(userRepo, jwtSvc),
		authapp.NewMeUseCase(userRepo),
	)

	siteHandler := sitedelivery.NewSiteHandler(
		siteapp.NewSiteService(siteRepo, integrationRepo),
		siteapp.NewIntegrationService(integrationRepo, siteRepo, serverRepo, templateRepo),
	)

	// The health axis is resolved from the metrics store at query time and never
	// persisted. With no metrics integration registered the resolver returns nothing,
	// which reads as "not known" rather than as anything about the machine.
	monitoringFactory := monitoringinfra.NewMonitoringFactory(integrationRepo)
	alertService := monitoringapp.NewAlertService(monitoringFactory)
	monitoringHandler := monitoringdelivery.NewMonitoringHandler(
		alertService,
		monitoringapp.NewServerMetricsService(monitoringFactory),
	)
	healthResolver := monitoringapp.NewHealthResolver(monitoringFactory)

	serverHandler := serverdelivery.NewServerHandler(
		serverapp.NewListServersUseCase(serverRepo, healthResolver),
		serverapp.NewGetServerUseCase(serverRepo, healthResolver),
	)
	// The SSE stream pushes Server projection changes so the dashboard patches rows live
	// instead of re-reading the whole list.
	serverStreamHandler := serverdelivery.NewServerStreamHandler(serverEventBroker)

	providerFactory := provisioninginfra.NewProviderFactory(integrationRepo)
	serverProtection := providerServerMutationGuard{
		servers: serverRepo, providers: providerFactory,
	}
	activeWork := activeServerWorkReader{operations: operationRepo, orchestrations: orchestrationRepo, tasks: taskRepo}
	integrationReader := provisioninginfra.NewIntegrationReader(integrationRepo, siteRepo)
	templateService := provisioningapp.NewDeploymentTemplateService(
		templateRepo, integrationReader, providerFactory)
	networkService := provisioningapp.NewNetworkConfigurationService(serverRepo, providerFactory)
	taskService := provisioningapp.NewProvisioningTaskService(taskRepo, serverRepo, serverProtection)
	deploymentsUC := provisioningapp.NewDeployServersUseCase(serverRepo, templateRepo, providerFactory)
	taskWorker := provisioningapp.NewProvisioningTaskWorker(
		taskRepo, serverRepo, providerFactory, 5*time.Second, 30*time.Second)
	reconcileUC := provisioningapp.NewReconcileUseCase(integrationRepo, serverRepo, providerFactory, osImageOverlayRepo)
	inventorySweepUC := provisioningapp.NewInventorySweepUseCase(integrationRepo, serverRepo, providerFactory)
	provisioningHandler := provisioningdelivery.NewProvisioningHandler(
		provisioningapp.NewDeployServerUseCase(serverRepo, providerFactory),
		deploymentsUC,
		provisioningapp.NewDeploymentTargetPreflightService(serverRepo, providerFactory),
		templateService,
		networkService,
		taskService,
		provisioningapp.NewReleaseServerUseCase(serverRepo, providerFactory, taskRepo),
		provisioningapp.NewRefreshServerUseCase(serverRepo, providerFactory),
		provisioningapp.NewListOSImagesUseCase(providerFactory, osImageOverlayRepo),
		reconcileUC,
		provisioningapp.NewGetProvisionerDetailUseCase(serverRepo, providerFactory),
		provisioningapp.NewGetProviderEventsUseCase(serverRepo, providerFactory),
		provisioningapp.NewMachineActionsUseCase(serverRepo, providerFactory, activeWork),
		provisioningapp.NewDeleteServerUseCase(serverRepo, providerFactory),
		provisioningapp.NewDeleteOSImageUseCase(providerFactory, osImageOverlayRepo),
		provisioningapp.NewUploadOSImageUseCase(providerFactory),
		provisioningapp.NewSetOSImageOverlayUseCase(osImageOverlayRepo),
	)

	// Infrastructure: swallow-owned Zones and Pools (decision 029). The grouping realizer reuses
	// the provisioning ProviderFactory so there is a single MAAS transport, and reads Sites and
	// Servers through narrow adapters. Provider realization is optional per Site capability.
	zoneRepo, err := infrastructureinfra.NewMongoZoneRepo(db)
	if err != nil {
		return fmt.Errorf("zone repo init: %w", err)
	}
	poolRepo, err := infrastructureinfra.NewMongoPoolRepo(db)
	if err != nil {
		return fmt.Errorf("pool repo init: %w", err)
	}
	infrastructureHandler := infrastructuredelivery.NewInfrastructureHandler(
		infrastructureapp.NewGroupingService(
			zoneRepo,
			poolRepo,
			infrastructureinfra.NewSiteReader(siteRepo),
			infrastructureinfra.NewServerLocator(serverRepo),
			infrastructureinfra.NewGroupingRealizer(integrationRepo, providerFactory),
		),
	)

	platformRepo, err := platforminfra.NewMongoPlatformRepo(db)
	if err != nil {
		return fmt.Errorf("platform repo init: %w", err)
	}
	requirementRepo := platforminfra.NewMongoDeploymentRequirementRepo(db)
	requirementService := platformapp.NewDeploymentRequirementService(requirementRepo)
	platformReaderFactory := platforminfra.NewReaderFactory(integrationRepo)
	membershipSync := platformapp.NewMembershipSyncUseCase(
		platformRepo, serverRepo, platformReaderFactory)
	// On-demand Slurm cluster read: reuses the same reader factory but never writes the
	// membership axis or sync counters. It powers the Slurm-specific management view.
	slurmClusterRead := platformapp.NewGetSlurmClusterUseCase(platformRepo, platformReaderFactory)

	catalog, err := operationinfra.LoadManifestCatalog(cfg.PlaybookManifest, cfg.PlaybookDir)
	if err != nil {
		return fmt.Errorf("playbook catalog: %w", err)
	}
	lifecycleReader := platformLifecycleReader{operations: operationRepo, orchestrations: orchestrationRepo}
	integrationCleaner := managedPlatformIntegrationCleaner{integrations: integrationRepo}
	platformService := platformapp.NewPlatformService(
		platformRepo, siteRepo, serverRepo, lifecycleReader, integrationCleaner,
	)
	automationRepo := operationinfra.NewMongoAutomationConfigurationRepo(db, sealer)
	runner := operationinfra.NewLocalRunner(
		cfg.AnsibleRunnerCommand, catalog.ProjectRoot(), cfg.JobRuntimeDir, cfg.JobArtifactDir)
	operationService := operationapp.NewExecutionService(
		operationRepo, serverRepo, automationRepo, catalog, runner,
		platformapp.NewPolicyChecker(platformRepo, serverRepo),
		serverProtection,
	)
	automationService := operationapp.NewAutomationConfigurationService(
		automationRepo, siteRepo, catalog,
	)
	orchestrationService := operationapp.NewWorkflowService(
		orchestrationRepo, temporalworkflow.NewController(temporalClient), operationSecretRepo,
	)
	orchestrationService.AttachLeaseReader(operationinfra.NewMongoResourceLeaseRepo(db))
	operationService.AttachWorkflow(orchestrationService)
	// Deleting a platform cancels its in-flight durable operations so their leases are
	// released and the member servers are freed rather than left blocked by orphaned work.
	platformService.AttachOperationCanceler(platformOperationCanceler{orchestrations: orchestrationService})
	provisioningHandler.AttachDurableOperations(durableProvisioningLauncher{
		deployments: deploymentsUC, operations: orchestrationService, servers: serverRepo, protection: serverProtection,
	})
	operationHandler := operationdelivery.NewExecutionHandler(operationService, automationService, orchestrationService)
	orchestrationStarter := temporalworkflow.NewStarter(
		temporalClient, orchestrationRepo, cfg.TemporalTaskQueue, cfg.TemporalStartInterval,
		cfg.OperationLeaseDuration, cfg.OperationMaxParallelism,
	)
	// Surface durable Operations whose Temporal execution was lost (a host restart or
	// execution timeout) as repairable instead of leaving them stuck in a forward-progress
	// status. It only marks state; the operator triggers the actual rerun. Reusing the
	// provisioning reconcile interval keeps recovery latency in line with other sweeps.
	orchestrationReconciler := temporalworkflow.NewReconciler(
		temporalClient, orchestrationRepo, cfg.ReconcileInterval,
	)

	overviewReader := overviewinfra.NewReader(
		siteRepo, integrationRepo, serverRepo, healthResolver, platformRepo, operationRepo,
		alertService, orchestrationRepo,
	)
	overviewHandler := overviewdelivery.NewHandler(
		overviewapp.NewService(overviewReader, overviewapp.SystemClock{}))

	// Platform lifecycle adapters keep durable operations outside the platform context.
	platformLauncher := platformDeploymentLauncher{
		operations: operationService, orchestrations: orchestrationService, deployments: deploymentsUC, servers: serverRepo,
	}
	deployService := platformapp.NewDeployService(
		platformService, platformRepo, serverRepo, lifecycleReader, platformLauncher,
		serverProtection,
	)
	deployService.AttachMachinePreparationValidator(platformMachinePreparationValidator{deployments: deploymentsUC})
	deployService.AttachDeploymentRequirementReader(requirementRepo)
	uninstallService := platformapp.NewUninstallService(
		platformRepo, serverRepo, lifecycleReader, platformLauncher,
		serverProtection,
	)
	platformHandler := platformdelivery.NewPlatformHandler(
		platformService, membershipSync, deployService, uninstallService, slurmClusterRead, requirementService)

	// Auto-install exporters when a server reaches the deployed state and its effective
	// exporter owner is ansible. The resolver bridges the provisioning lock and platform
	// policy so the operation context stays free of platform types.
	autoExporterDeploy := operationapp.NewAutoExporterDeployUseCase(
		serverRepo, operationRepo, operationService,
		exporterOwnerResolver{platforms: platformRepo}, orchestrationRepo,
	)

	discoveryUseCase := discoveryapp.NewDiscoveryUseCase(serverRepo)
	discoveryHandler := discoverydelivery.NewDiscoveryHandler(discoveryUseCase)
	// The v2 embedded dispatcher and Mongo site lease were removed (ADR 016/017): Temporal
	// is the sole execution engine, and per-resource fencing leases replace the site lease.
	// Historical schema-v2 records remain readable through ExecutionService.

	fiberApp := fiber.New(fiber.Config{
		// OS image upload (POST /provisioning/images) streams a potentially multi-gigabyte
		// artifact, so the body is read as a stream and the limit is raised well past the 4 MiB
		// default. StreamRequestBody keeps the large body off the heap; fasthttp spools the
		// multipart file part to a temp file the handler then streams to the provider.
		StreamRequestBody: true,
		BodyLimit:         int(cfg.ImageUploadMaxBytes),
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			slog.Error("unhandled HTTP error",
				"requestId", c.GetRespHeader(fiber.HeaderXRequestID), "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{
					"code":      "internal_error",
					"message":   "An unexpected error occurred.",
					"requestId": c.GetRespHeader(fiber.HeaderXRequestID),
				},
			})
		},
	})
	fiberApp.Use(requestid.New())
	fiberApp.Use(func(c *fiber.Ctx) error {
		started := time.Now()
		err := c.Next()
		httpRequestCount.Add(1)
		slog.Info("http request",
			"requestId", c.GetRespHeader(fiber.HeaderXRequestID),
			"method", c.Method(), "path", c.Path(), "status", c.Response().StatusCode(),
			"durationMs", time.Since(started).Milliseconds())
		return err
	})
	if cfg.AllowedOrigins != "" {
		fiberApp.Use(cors.New(cors.Config{
			AllowOrigins: cfg.AllowedOrigins,
			AllowMethods: "GET,POST,PATCH,PUT,DELETE,OPTIONS",
			AllowHeaders: "Content-Type,Authorization",
		}))
	}

	registerRoutes(fiberApp, routeDeps{
		jwtSvc:         jwtSvc,
		machineToken:   cfg.MachineToken,
		auth:           authHandler,
		overview:       overviewHandler,
		sites:          siteHandler,
		servers:        serverHandler,
		serverStream:   serverStreamHandler,
		provisioning:   provisioningHandler,
		operations:     operationHandler,
		monitoring:     monitoringHandler,
		platforms:      platformHandler,
		infrastructure: infrastructureHandler,
		discovery:      discoveryHandler,
		releaseVersion: releaseVersion,
		readiness: func(ctx context.Context) error {
			if err := client.Ping(ctx, nil); err != nil {
				return err
			}
			return migration.Check(ctx, db)
		},
	})

	go runReconciler(ctx, reconcileUC, cfg.ReconcileInterval)
	go runInventorySweep(ctx, inventorySweepUC, cfg.InventoryInterval)
	go taskWorker.Run(ctx)
	go runMembershipSync(ctx, membershipSync, cfg.ReconcileInterval)
	go runAutoExporterDeploy(ctx, autoExporterDeploy, cfg.ReconcileInterval)
	go orchestrationStarter.Run(ctx)
	go orchestrationReconciler.Run(ctx)
	go runArtifactRetention(ctx, cfg.JobArtifactDir, cfg.JobArtifactRetention)

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP server listening", "addr", cfg.Addr)
		serverErr <- fiberApp.Listen(cfg.Addr)
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		slog.Info("shutdown requested; draining in-flight requests")
		if err := fiberApp.ShutdownWithTimeout(shutdownTimeout); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

type routeDeps struct {
	jwtSvc         *jwt.Service
	machineToken   string
	auth           *authdelivery.AuthHandler
	overview       *overviewdelivery.Handler
	sites          *sitedelivery.SiteHandler
	servers        *serverdelivery.ServerHandler
	serverStream   *serverdelivery.ServerStreamHandler
	provisioning   *provisioningdelivery.ProvisioningHandler
	operations     *operationdelivery.ExecutionHandler
	monitoring     *monitoringdelivery.MonitoringHandler
	platforms      *platformdelivery.PlatformHandler
	infrastructure *infrastructuredelivery.InfrastructureHandler
	discovery      *discoverydelivery.DiscoveryHandler
	readiness      func(context.Context) error
	releaseVersion string
}

// registerPlatformRoutes keeps the canonical and one-release compatibility routes on
// the same handlers, so deprecated URLs cannot drift from Platform behavior.
func registerPlatformRoutes(routes fiber.Router, handler *platformdelivery.PlatformHandler) {
	routes.Post("/", handler.Create)
	routes.Get("/", handler.List)
	routes.Post("/deploy", handler.Deploy)
	routes.Post("/sync", handler.SyncAllMembership)
	routes.Get("/:id", handler.Get)
	routes.Patch("/:id", handler.Update)
	routes.Delete("/:id", handler.Delete)
	routes.Post("/:id/uninstall", handler.Uninstall)
	routes.Post("/:id/sync", handler.SyncMembership)
	routes.Get("/:id/slurm", handler.GetSlurmCluster)
}

// markDeprecatedPlatformRoute identifies the former Cluster resource without changing
// its response body. Clients have one release to follow the canonical successor link.
func markDeprecatedPlatformRoute(c *fiber.Ctx) error {
	c.Set("Deprecation", "true")
	c.Set("Link", "</api/v1/platforms>; rel=\"successor-version\"")
	return c.Next()
}

// markDeprecatedProvisioningCommand preserves legacy wire behavior while pointing clients at durable Operations.
func markDeprecatedProvisioningCommand(successor string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("Deprecation", "true")
		c.Set("Link", "<"+successor+">; rel=\"successor-version\"")
		return c.Next()
	}
}

func registerRoutes(app *fiber.App, deps routeDeps) {
	app.Get("/livez", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "alive", "version": deps.releaseVersion})
	})
	ready := func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := deps.readiness(ctx); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "not-ready", "reason": "mongodb",
			})
		}
		return c.JSON(fiber.Map{"status": "ready", "schemaVersion": migration.CurrentSchemaVersion})
	}
	app.Get("/readyz", ready)
	app.Get("/healthz", ready)
	metricsHandler := func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "text/plain; version=0.0.4")
		return c.SendString(
			"# HELP swallow_http_requests_total Total HTTP requests.\n" +
				"# TYPE swallow_http_requests_total counter\n" +
				"swallow_http_requests_total " + strconv.FormatUint(httpRequestCount.Load(), 10) + "\n" +
				"# HELP swallow_build_info Build metadata.\n" +
				"# TYPE swallow_build_info gauge\n" +
				"swallow_build_info{version=\"" + deps.releaseVersion + "\",commit=\"" + version.Commit + "\"} 1\n")
	}

	v1 := app.Group("/api/v1")

	auth := v1.Group("/auth")
	auth.Post("/login", deps.auth.Login)
	auth.Get("/me", middleware.Auth(deps.jwtSvc), deps.auth.Me)

	admin := []fiber.Handler{middleware.Auth(deps.jwtSvc), middleware.AdminOnly()}

	overview := v1.Group("/overview", admin...)
	overview.Get("/", deps.overview.Get)

	sites := v1.Group("/sites", admin...)
	sites.Post("/", deps.sites.CreateSite)
	sites.Get("/", deps.sites.ListSites)
	sites.Get("/:id", deps.sites.GetSite)
	sites.Patch("/:id", deps.sites.UpdateSite)
	sites.Delete("/:id", deps.sites.DeleteSite)
	sites.Get("/:id/automation", deps.operations.GetAutomation)
	sites.Put("/:id/automation", deps.operations.PutAutomation)
	sites.Put("/:id/automation/credential", deps.operations.PutAutomationCredential)

	integrations := v1.Group("/integrations", admin...)
	integrations.Post("/", deps.sites.CreateIntegration)
	integrations.Get("/", deps.sites.ListIntegrations)
	integrations.Get("/:id", deps.sites.GetIntegration)
	integrations.Patch("/:id", deps.sites.UpdateIntegration)
	integrations.Put("/:id/credential", deps.sites.ReplaceCredential)
	integrations.Delete("/:id", deps.sites.DeleteIntegration)

	// Servers cannot be created directly: reconciliation projects provisioner inventory.
	// Explicit deletion is provider-backed so the next pass cannot recreate the record.
	// Lifecycle actions are addressed by server because that is the identifier callers hold.
	// Live Server change stream (SSE). Registered before the "/servers" group so its static
	// path is unambiguous next to "/servers/:id", and with its own middleware order because a
	// browser EventSource cannot send an Authorization header: the access token arrives in a
	// query parameter, which BearerTokenFromQuery promotes to a Bearer header before Auth.
	v1.Get("/servers/stream",
		middleware.BearerTokenFromQuery("access_token"),
		middleware.Auth(deps.jwtSvc),
		middleware.AdminOnly(),
		deps.serverStream.Stream)

	servers := v1.Group("/servers", admin...)
	servers.Get("/", deps.servers.List)
	servers.Get("/:id", deps.servers.Get)
	servers.Post("/:id/refresh", deps.provisioning.RefreshServer)
	servers.Delete("/:id", deps.provisioning.DeleteServer)
	// The provisioner detail is a live proxy read one machine at a time, distinct from
	// the mirrored projection the list and get return.
	servers.Get("/:id/provisioner-detail", deps.provisioning.ProvisionerDetail)
	servers.Get("/:id/events", deps.provisioning.ProviderEvents)
	servers.Get("/:id/network", deps.provisioning.GetNetwork)
	servers.Post("/:id/network/interfaces/:interfaceId/links", deps.provisioning.CreateNetworkLink)
	servers.Put("/:id/network/interfaces/:interfaceId/links/:linkId", deps.provisioning.ReplaceNetworkLink)
	servers.Delete("/:id/network/interfaces/:interfaceId/links/:linkId", deps.provisioning.DeleteNetworkLink)
	servers.Get("/:id/provisioning-tasks", deps.provisioning.ListProvisioningTasks)
	servers.Post("/:id/deploy", markDeprecatedProvisioningCommand("/api/v1/provisioning/deployment-operations"), deps.provisioning.Deploy)
	servers.Post("/:id/release", markDeprecatedProvisioningCommand("/api/v1/provisioning/release-operations"), deps.provisioning.Release)
	// Power, hardware validation, and operator state are the provisioner actions beyond
	// deploy and release. Each is refused by a provisioner that does not offer it.
	servers.Post("/:id/power-on", deps.provisioning.PowerOn)
	servers.Post("/:id/power-off", deps.provisioning.PowerOff)
	servers.Get("/:id/power-state", deps.provisioning.PowerState)
	servers.Post("/:id/commission", deps.provisioning.Commission)
	servers.Post("/:id/test", deps.provisioning.Test)
	servers.Post("/:id/abort", deps.provisioning.Abort)
	servers.Post("/:id/override-failed-testing", deps.provisioning.OverrideFailedTesting)
	servers.Post("/:id/lock", deps.provisioning.Lock)
	servers.Post("/:id/unlock", deps.provisioning.Unlock)
	servers.Post("/:id/mark-broken", deps.provisioning.MarkBroken)
	servers.Post("/:id/mark-fixed", deps.provisioning.MarkFixed)
	servers.Post("/:id/rescue-mode", deps.provisioning.RescueMode)
	servers.Post("/:id/exit-rescue-mode", deps.provisioning.ExitRescueMode)
	// Placement assigns a Server to a swallow-owned Zone and/or Pool, realized in the
	// provisioner (decision 029). Mounted here because clients address a Server, but handled by
	// the infrastructure feature that owns the Zone/Pool catalog.
	servers.Put("/:id/placement", deps.infrastructure.AssignServerPlacement)

	provisioning := v1.Group("/provisioning", admin...)
	provisioning.Get("/images", deps.provisioning.ListImages)
	// Multipart upload of a provider-owned OS image. Streamed and spooled by the handler; the
	// provider decides whether it becomes a custom image (docs/decisions/027).
	provisioning.Post("/images", deps.provisioning.UploadImage)
	provisioning.Delete("/images", deps.provisioning.DeleteImage)
	// Swallow-owned OS Image overlay: set or clear the display overrides (name, OS, release)
	// merged over the provider values. Same query-parameter identity as DELETE /images
	// (docs/decisions/025).
	provisioning.Patch("/images/overlay", deps.provisioning.SetImageOverlay)
	provisioning.Delete("/images/overlay", deps.provisioning.ClearImageOverlay)
	provisioning.Get("/templates", deps.provisioning.ListTemplates)
	provisioning.Post("/templates", deps.provisioning.CreateTemplate)
	provisioning.Get("/templates/:id", deps.provisioning.GetTemplate)
	provisioning.Patch("/templates/:id", deps.provisioning.UpdateTemplate)
	provisioning.Delete("/templates/:id", deps.provisioning.DeleteTemplate)
	provisioning.Put("/templates/:id/user-data", deps.provisioning.ReplaceTemplateUserData)
	provisioning.Delete("/templates/:id/user-data", deps.provisioning.ClearTemplateUserData)
	provisioning.Post("/deployments/preflight", deps.provisioning.PreflightDeployServers)
	provisioning.Post("/deployments", markDeprecatedProvisioningCommand("/api/v1/provisioning/deployment-operations"), deps.provisioning.DeployServers)
	provisioning.Post("/deployment-operations", deps.provisioning.CreateDeploymentOperation)
	provisioning.Post("/release-operations", deps.provisioning.CreateReleaseOperation)
	provisioning.Post("/networks/inspect", deps.provisioning.InspectNetworks)
	provisioning.Get("/tasks/:id", deps.provisioning.GetProvisioningTask)
	provisioning.Post("/tasks/:id/retry", deps.provisioning.RetryProvisioningTask)
	provisioning.Post("/reconcile", deps.provisioning.ReconcileAll)
	provisioning.Post("/integrations/:id/reconcile", deps.provisioning.Reconcile)

	// Swallow owns Platform registration and policy; membership is observed from the
	// runtime API. The former Cluster route is a delivery-only compatibility alias.
	platforms := v1.Group("/platforms", admin...)
	platforms.Get("/deployment-requirements/slurm", deps.platforms.GetSlurmDeploymentRequirement)
	platforms.Put("/deployment-requirements/slurm", deps.platforms.PutSlurmDeploymentRequirement)
	registerPlatformRoutes(platforms, deps.platforms)
	legacyPlatforms := v1.Group("/clusters", admin...)
	legacyPlatforms.Use(markDeprecatedPlatformRoute)
	registerPlatformRoutes(legacyPlatforms, deps.platforms)

	// Swallow-owned Zones and Pools (the dashboard's Infrastructure area). Each write is also
	// realized in the Site's provisioner when it is grouping-capable (decision 029).
	infrastructure := v1.Group("/infrastructure", admin...)
	infrastructure.Get("/zones", deps.infrastructure.ListZones)
	infrastructure.Post("/zones", deps.infrastructure.CreateZone)
	infrastructure.Get("/zones/:id", deps.infrastructure.GetZone)
	infrastructure.Patch("/zones/:id", deps.infrastructure.UpdateZone)
	infrastructure.Delete("/zones/:id", deps.infrastructure.DeleteZone)
	infrastructure.Get("/pools", deps.infrastructure.ListPools)
	infrastructure.Post("/pools", deps.infrastructure.CreatePool)
	infrastructure.Get("/pools/:id", deps.infrastructure.GetPool)
	infrastructure.Patch("/pools/:id", deps.infrastructure.UpdatePool)
	infrastructure.Delete("/pools/:id", deps.infrastructure.DeletePool)

	// Alerts and metrics are read straight from the monitoring stack: swallow stores
	// neither, and acknowledging an alert creates a silence in Alertmanager.
	monitoring := v1.Group("/monitoring", admin...)
	monitoring.Get("/alerts", deps.monitoring.ListAlerts)
	monitoring.Post("/alerts/:fingerprint/acknowledge", deps.monitoring.Acknowledge)
	monitoring.Get("/metrics", deps.monitoring.ServerMetrics)
	monitoring.Get("/metrics/names", deps.monitoring.MetricNames)

	// Canonical Workflow surface (ADR 017). A Workflow is a DAG of Tasks.
	workflows := v1.Group("/workflows", admin...)
	workflows.Post("/", deps.operations.Create)
	workflows.Get("/", deps.operations.List)
	workflows.Get("/:id", deps.operations.Get)
	workflows.Get("/:id/timeline", deps.operations.Timeline)
	workflows.Post("/:id/cancel", deps.operations.Cancel)
	workflows.Post("/:id/rerun", deps.operations.Rerun)
	workflows.Post("/:id/tasks/:taskId/retry", deps.operations.RetryStep)
	workflows.Get("/:id/tasks/:taskId/logs", deps.operations.StepLogs)
	workflows.Get("/:id/tasks/:taskId/stderr", deps.operations.StepStderr)
	workflows.Get("/:id/tasks/:taskId/events", deps.operations.StepEvents)
	workflows.Get("/:id/tasks/:taskId/artifacts", deps.operations.StepArtifacts)

	// Deprecated /operations alias for one release (ADR 017). Same handlers; the
	// Deprecation header points clients at the canonical /workflows surface. The v2
	// operation-level logs/events/retry remain only on this legacy path.
	operations := v1.Group("/operations", admin...)
	operations.Use(func(c *fiber.Ctx) error {
		c.Set("Deprecation", "true")
		c.Set("Link", `</api/v1/workflows>; rel="successor-version"`)
		return c.Next()
	})
	operations.Post("/", deps.operations.Create)
	operations.Get("/", deps.operations.List)
	operations.Get("/:id", deps.operations.Get)
	operations.Get("/:id/timeline", deps.operations.Timeline)
	operations.Post("/:id/cancel", deps.operations.Cancel)
	operations.Post("/:id/rerun", deps.operations.Rerun)
	operations.Post("/:id/steps/:stepId/retry", deps.operations.RetryStep)
	operations.Get("/:id/steps/:stepId/logs", deps.operations.StepLogs)
	operations.Get("/:id/steps/:stepId/stderr", deps.operations.StepStderr)
	operations.Get("/:id/steps/:stepId/events", deps.operations.StepEvents)
	operations.Get("/:id/steps/:stepId/artifacts", deps.operations.StepArtifacts)
	operations.Get("/:id/logs", deps.operations.Logs)
	operations.Get("/:id/events", deps.operations.Events)
	operations.Post("/:id/retry", deps.operations.Retry)

	// Machine-to-machine discovery remains pull-based for Prometheus and external
	// Ansible diagnostics. The embedded runner calls the same use case directly.
	machine := middleware.MachineAuth(deps.jwtSvc, deps.machineToken)
	app.Get("/metrics", machine, metricsHandler)

	discovery := v1.Group("/discovery", machine)
	discovery.Get("/prometheus", deps.discovery.PrometheusTargets)
	discovery.Get("/ansible", deps.discovery.AnsibleInventory)
}
