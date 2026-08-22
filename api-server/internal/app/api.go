// Package app provides top-level runtime bootstrap functions.
// Each function accepts a fully assembled config object so that no application
// code needs to parse environment variables or CLI flags directly.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"go.mongodb.org/mongo-driver/mongo"
	mongoopts "go.mongodb.org/mongo-driver/mongo/options"

	"github.com/AFDEAPAC/swallow/bootstrap"
	"github.com/AFDEAPAC/swallow/config"
	authapp "github.com/AFDEAPAC/swallow/internal/auth/application"
	authdelivery "github.com/AFDEAPAC/swallow/internal/auth/delivery"
	authinfra "github.com/AFDEAPAC/swallow/internal/auth/infra"
	clusterapp "github.com/AFDEAPAC/swallow/internal/cluster/application"
	clusterdelivery "github.com/AFDEAPAC/swallow/internal/cluster/delivery"
	clusterinfra "github.com/AFDEAPAC/swallow/internal/cluster/infra"
	discoveryapp "github.com/AFDEAPAC/swallow/internal/discovery/application"
	discoverydelivery "github.com/AFDEAPAC/swallow/internal/discovery/delivery"
	monitoringapp "github.com/AFDEAPAC/swallow/internal/monitoring/application"
	monitoringdelivery "github.com/AFDEAPAC/swallow/internal/monitoring/delivery"
	monitoringinfra "github.com/AFDEAPAC/swallow/internal/monitoring/infra"
	operationapp "github.com/AFDEAPAC/swallow/internal/operation/application"
	operationdelivery "github.com/AFDEAPAC/swallow/internal/operation/delivery"
	operationinfra "github.com/AFDEAPAC/swallow/internal/operation/infra"
	provisioningapp "github.com/AFDEAPAC/swallow/internal/provisioning/application"
	provisioningdelivery "github.com/AFDEAPAC/swallow/internal/provisioning/delivery"
	provisioninginfra "github.com/AFDEAPAC/swallow/internal/provisioning/infra"
	serverapp "github.com/AFDEAPAC/swallow/internal/server/application"
	serverdelivery "github.com/AFDEAPAC/swallow/internal/server/delivery"
	serverinfra "github.com/AFDEAPAC/swallow/internal/server/infra"
	"github.com/AFDEAPAC/swallow/internal/shared/jwt"
	"github.com/AFDEAPAC/swallow/internal/shared/middleware"
	"github.com/AFDEAPAC/swallow/internal/shared/secret"
	siteapp "github.com/AFDEAPAC/swallow/internal/site/application"
	sitedelivery "github.com/AFDEAPAC/swallow/internal/site/delivery"
	siteinfra "github.com/AFDEAPAC/swallow/internal/site/infra"
)

// shutdownTimeout bounds how long in-flight requests get to finish on shutdown.
const shutdownTimeout = 10 * time.Second

// RunAPI starts the HTTP API server and the reconciler, and blocks until interrupted.
func RunAPI(cfg config.APIConfig) error {
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
	log.Println("connected to mongodb")
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = client.Disconnect(disconnectCtx)
	}()

	db := client.Database(cfg.MongoDB)

	userRepo := authinfra.NewMongoUserRepo(db)
	siteRepo, err := siteinfra.NewMongoSiteRepo(db)
	if err != nil {
		return fmt.Errorf("site repo init: %w", err)
	}
	integrationRepo, err := siteinfra.NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("integration repo init: %w", err)
	}
	serverRepo, err := serverinfra.NewMongoServerRepo(db)
	if err != nil {
		return fmt.Errorf("server repo init: %w", err)
	}

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
		siteapp.NewIntegrationService(integrationRepo, siteRepo, serverRepo),
	)

	// The health axis is resolved from the metrics store at query time and never
	// persisted. With no metrics integration registered the resolver returns nothing,
	// which reads as "not known" rather than as anything about the machine.
	monitoringFactory := monitoringinfra.NewMonitoringFactory(integrationRepo)
	monitoringHandler := monitoringdelivery.NewMonitoringHandler(
		monitoringapp.NewAlertService(monitoringFactory),
		monitoringapp.NewServerMetricsService(monitoringFactory),
	)
	var healthResolver serverapp.HealthResolver = monitoringapp.NewHealthResolver(monitoringFactory)

	serverHandler := serverdelivery.NewServerHandler(
		serverapp.NewListServersUseCase(serverRepo, healthResolver),
		serverapp.NewGetServerUseCase(serverRepo, healthResolver),
	)

	providerFactory := provisioninginfra.NewProviderFactory(integrationRepo)
	reconcileUC := provisioningapp.NewReconcileUseCase(integrationRepo, serverRepo, providerFactory)
	inventorySweepUC := provisioningapp.NewInventorySweepUseCase(integrationRepo, serverRepo, providerFactory)
	provisioningHandler := provisioningdelivery.NewProvisioningHandler(
		provisioningapp.NewDeployServerUseCase(serverRepo, providerFactory),
		provisioningapp.NewReleaseServerUseCase(serverRepo, providerFactory),
		provisioningapp.NewListOSImagesUseCase(providerFactory),
		reconcileUC,
		provisioningapp.NewGetProvisionerDetailUseCase(serverRepo, providerFactory),
		provisioningapp.NewMachineActionsUseCase(serverRepo, providerFactory),
	)

	clusterRepo, err := clusterinfra.NewMongoClusterRepo(db)
	if err != nil {
		return fmt.Errorf("cluster repo init: %w", err)
	}
	membershipSync := clusterapp.NewMembershipSyncUseCase(
		clusterRepo, serverRepo, clusterinfra.NewReaderFactory(integrationRepo))
	clusterHandler := clusterdelivery.NewClusterHandler(
		clusterapp.NewClusterService(clusterRepo, siteRepo, serverRepo),
		membershipSync,
	)

	operationRepo, err := operationinfra.NewMongoOperationRepo(db)
	if err != nil {
		return fmt.Errorf("operation repo init: %w", err)
	}
	// The cluster context enforces the gpuStackOwner policy, so that an operation
	// never has to know what a GPU operator is in order to refuse fighting one.
	operationService := operationapp.NewOperationService(
		operationRepo, serverRepo, integrationRepo,
		operationinfra.NewControllerFactory(integrationRepo),
		clusterapp.NewPolicyChecker(clusterRepo, serverRepo),
	)
	operationHandler := operationdelivery.NewOperationHandler(operationService)

	discoveryHandler := discoverydelivery.NewDiscoveryHandler(
		discoveryapp.NewDiscoveryUseCase(serverRepo),
	)

	fiberApp := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			log.Printf("unhandled error: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    "internal_error",
					"message": "An unexpected error occurred.",
				},
			})
		},
	})

	// Allow cross-origin requests so browser preflight OPTIONS is handled before
	// any route matching occurs. AllowOrigins "*" is appropriate for an internal
	// management platform; restrict to a specific origin if needed.
	fiberApp.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PATCH,PUT,DELETE,OPTIONS",
		AllowHeaders: "Content-Type,Authorization",
	}))

	registerRoutes(fiberApp, routeDeps{
		jwtSvc:       jwtSvc,
		machineToken: cfg.MachineToken,
		auth:         authHandler,
		sites:        siteHandler,
		servers:      serverHandler,
		provisioning: provisioningHandler,
		operations:   operationHandler,
		monitoring:   monitoringHandler,
		clusters:     clusterHandler,
		discovery:    discoveryHandler,
	})

	go runReconciler(ctx, reconcileUC, cfg.ReconcileInterval)
	go runInventorySweep(ctx, inventorySweepUC, cfg.InventoryInterval)
	go runMembershipSync(ctx, membershipSync, cfg.ReconcileInterval)
	go runOperationPoller(ctx, operationService, cfg.OperationPollInterval)

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("starting HTTP server on %s", cfg.Addr)
		serverErr <- fiberApp.Listen(cfg.Addr)
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		log.Println("shutdown requested; draining in-flight requests")
		if err := fiberApp.ShutdownWithTimeout(shutdownTimeout); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

type routeDeps struct {
	jwtSvc       *jwt.Service
	machineToken string
	auth         *authdelivery.AuthHandler
	sites        *sitedelivery.SiteHandler
	servers      *serverdelivery.ServerHandler
	provisioning *provisioningdelivery.ProvisioningHandler
	operations   *operationdelivery.OperationHandler
	monitoring   *monitoringdelivery.MonitoringHandler
	clusters     *clusterdelivery.ClusterHandler
	discovery    *discoverydelivery.DiscoveryHandler
}

func registerRoutes(app *fiber.App, deps routeDeps) {
	// Unauthenticated: a readiness probe that needs a credential is not usable by the
	// thing that needs it.
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	v1 := app.Group("/api/v1")

	auth := v1.Group("/auth")
	auth.Post("/login", deps.auth.Login)
	auth.Get("/me", middleware.Auth(deps.jwtSvc), deps.auth.Me)

	admin := []fiber.Handler{middleware.Auth(deps.jwtSvc), middleware.AdminOnly()}

	sites := v1.Group("/sites", admin...)
	sites.Post("/", deps.sites.CreateSite)
	sites.Get("/", deps.sites.ListSites)
	sites.Get("/:id", deps.sites.GetSite)
	sites.Patch("/:id", deps.sites.UpdateSite)
	sites.Delete("/:id", deps.sites.DeleteSite)

	integrations := v1.Group("/integrations", admin...)
	integrations.Post("/", deps.sites.CreateIntegration)
	integrations.Get("/", deps.sites.ListIntegrations)
	integrations.Get("/:id", deps.sites.GetIntegration)
	integrations.Patch("/:id", deps.sites.UpdateIntegration)
	integrations.Put("/:id/credential", deps.sites.ReplaceCredential)
	integrations.Delete("/:id", deps.sites.DeleteIntegration)

	// Servers are read-only: they are produced by reconciliation, so there is nothing
	// to create or delete. The lifecycle actions belong to the provisioning context
	// but are addressed by server, because that is the identifier callers hold.
	servers := v1.Group("/servers", admin...)
	servers.Get("/", deps.servers.List)
	servers.Get("/:id", deps.servers.Get)
	// The provisioner detail is a live proxy read one machine at a time, distinct from
	// the mirrored projection the list and get return.
	servers.Get("/:id/provisioner-detail", deps.provisioning.ProvisionerDetail)
	servers.Post("/:id/deploy", deps.provisioning.Deploy)
	servers.Post("/:id/release", deps.provisioning.Release)
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

	provisioning := v1.Group("/provisioning", admin...)
	provisioning.Get("/images", deps.provisioning.ListImages)
	provisioning.Post("/reconcile", deps.provisioning.ReconcileAll)
	provisioning.Post("/integrations/:id/reconcile", deps.provisioning.Reconcile)

	// gdcm owns a cluster's registration and its policy. Membership is read from the
	// cluster's own API, so there is no endpoint here to change it.
	clusters := v1.Group("/clusters", admin...)
	clusters.Post("/", deps.clusters.Create)
	clusters.Get("/", deps.clusters.List)
	clusters.Get("/:id", deps.clusters.Get)
	clusters.Patch("/:id", deps.clusters.Update)
	clusters.Delete("/:id", deps.clusters.Delete)
	clusters.Post("/:id/sync", deps.clusters.SyncMembership)
	clusters.Post("/sync", deps.clusters.SyncAllMembership)

	// Alerts and metrics are read straight from the monitoring stack: gdcm stores
	// neither, and acknowledging an alert creates a silence in Alertmanager.
	monitoring := v1.Group("/monitoring", admin...)
	monitoring.Get("/alerts", deps.monitoring.ListAlerts)
	monitoring.Post("/alerts/:fingerprint/acknowledge", deps.monitoring.Acknowledge)
	monitoring.Get("/metrics", deps.monitoring.ServerMetrics)
	monitoring.Get("/metrics/names", deps.monitoring.MetricNames)

	operations := v1.Group("/operations", admin...)
	operations.Post("/", deps.operations.Create)
	operations.Get("/", deps.operations.List)
	operations.Get("/:id", deps.operations.Get)
	operations.Get("/:id/logs", deps.operations.Logs)
	operations.Post("/:id/refresh", deps.operations.Refresh)

	// Machine-to-machine endpoints. Prometheus and AWX pull their target lists from
	// discovery rather than being pushed into, and AWX posts job notifications back
	// as a hint to re-read. See docs/decisions/003 and 004.
	machine := middleware.MachineAuth(deps.jwtSvc, deps.machineToken)

	discovery := v1.Group("/discovery", machine)
	discovery.Get("/prometheus", deps.discovery.PrometheusTargets)
	discovery.Get("/ansible", deps.discovery.AnsibleInventory)

	v1.Post("/webhooks/automation/:integrationId", machine, deps.operations.Webhook)
}
