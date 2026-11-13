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
	"slices"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/maple52046/swallow/bootstrap"
	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/adapters/bmc/redfish"
	"github.com/maple52046/swallow/internal/adapters/bootiso/genfsimg"
	"github.com/maple52046/swallow/internal/adapters/events/memory"
	"github.com/maple52046/swallow/internal/adapters/hostaccess/sshaccess"
	"github.com/maple52046/swallow/internal/adapters/hostautomation/ansible"
	"github.com/maple52046/swallow/internal/adapters/hypervisor/libvirt"
	"github.com/maple52046/swallow/internal/adapters/localrepo/pulp"
	"github.com/maple52046/swallow/internal/adapters/metrics/promstack"
	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	apikeystore "github.com/maple52046/swallow/internal/adapters/store/mongodb/apikey"
	authstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/auth"
	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	infrastructurestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/infrastructure"
	localrepostore "github.com/maple52046/swallow/internal/adapters/store/mongodb/localrepo"
	"github.com/maple52046/swallow/internal/adapters/store/mongodb/migration"
	platformstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/platform"
	provisioningstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/provisioning"
	serverstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/server"
	sitestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/site"
	softwarestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/software"
	sshkeystore "github.com/maple52046/swallow/internal/adapters/store/mongodb/sshkey"
	workflowstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/workflow"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	apikeyapp "github.com/maple52046/swallow/internal/core/apikey/application"
	apikeydelivery "github.com/maple52046/swallow/internal/core/apikey/delivery"
	authapp "github.com/maple52046/swallow/internal/core/auth/application"
	authdelivery "github.com/maple52046/swallow/internal/core/auth/delivery"
	convergencedelivery "github.com/maple52046/swallow/internal/core/convergence/delivery"
	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	discoverydelivery "github.com/maple52046/swallow/internal/core/discovery/delivery"
	infrastructureapp "github.com/maple52046/swallow/internal/core/infrastructure/application"
	infrastructuredelivery "github.com/maple52046/swallow/internal/core/infrastructure/delivery"
	localrepoapp "github.com/maple52046/swallow/internal/core/localrepo/application"
	localrepodelivery "github.com/maple52046/swallow/internal/core/localrepo/delivery"
	moduledelivery "github.com/maple52046/swallow/internal/core/module/delivery"
	monitoringapp "github.com/maple52046/swallow/internal/core/monitoring/application"
	monitoringdelivery "github.com/maple52046/swallow/internal/core/monitoring/delivery"
	overviewapp "github.com/maple52046/swallow/internal/core/overview/application"
	overviewdelivery "github.com/maple52046/swallow/internal/core/overview/delivery"
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	platformdelivery "github.com/maple52046/swallow/internal/core/platform/delivery"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdelivery "github.com/maple52046/swallow/internal/core/provisioning/delivery"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverapp "github.com/maple52046/swallow/internal/core/server/application"
	serverdelivery "github.com/maple52046/swallow/internal/core/server/delivery"
	siteapp "github.com/maple52046/swallow/internal/core/site/application"
	sitedelivery "github.com/maple52046/swallow/internal/core/site/delivery"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	softwaredelivery "github.com/maple52046/swallow/internal/core/software/delivery"
	sshkeydelivery "github.com/maple52046/swallow/internal/core/sshkey/delivery"
	validationdelivery "github.com/maple52046/swallow/internal/core/validation/delivery"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdelivery "github.com/maple52046/swallow/internal/core/workflow/delivery"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/shared/secret"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
	"github.com/maple52046/swallow/internal/shared/version"
	"github.com/maple52046/swallow/modules/accelerator/amd"
	"github.com/maple52046/swallow/modules/platform/kubernetes/kubeapi"
	"github.com/maple52046/swallow/modules/software/docker-ce/dockerengine"
	"github.com/maple52046/swallow/modules/software/docker-ce/explorer"
	"github.com/maple52046/swallow/modules/software/docker-ce/explorerhttp"
)

// shutdownTimeout bounds how long in-flight requests get to finish on shutdown.
const shutdownTimeout = 10 * time.Second

var httpRequestCount atomic.Uint64

// requestLog counts every request and logs it once, after it was answered, with its request ID,
// method, path, status, and duration. It must be registered after requestid (so the ID is set) and
// before every other middleware and route, so it sees each of their errors.
//
// A handler or middleware that returns an error has not written its response yet: Fiber's error
// handler does that only after the whole chain returned. So when the chain fails, requestLog runs
// the app's error handler itself — the status it logs is the one the client receives (a 404 for no
// route, 405, 413, 500) instead of the 200 the response held before — and returns nil, so the error
// handler does not answer the request a second time. When the error handler itself fails, the
// request is answered and logged 500, as Fiber does.
func requestLog(logger *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		started := time.Now()
		if err := c.Next(); err != nil {
			if handlerErr := c.App().ErrorHandler(c, err); handlerErr != nil {
				// SendStatus fails only when the response cannot be written at all, which leaves
				// nothing else to answer with; the 500 is still what gets logged.
				_ = c.SendStatus(fiber.StatusInternalServerError)
			}
		}
		httpRequestCount.Add(1)
		logger.Info("http request",
			"requestId", c.GetRespHeader(fiber.HeaderXRequestID),
			"method", c.Method(), "path", c.Path(), "status", c.Response().StatusCode(),
			"durationMs", time.Since(started).Milliseconds())
		return nil
	}
}

// RunAPI starts the HTTP API server and blocks until interrupted. It runs no background loop:
// the worker observes external systems, and only the Server event relay runs beside the HTTP server.
func RunAPI(cfg config.APIConfig) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	modules, err := composeModules()
	if err != nil {
		return err
	}
	releaseVersion := cfg.ReleaseVersion
	if releaseVersion == "" {
		releaseVersion = version.Version
	}
	slog.Info("starting swallow API",
		"version", releaseVersion, "commit", version.Commit, "builtAt", version.BuiltAt,
		"addr", cfg.Addr, "mongoDatabase", cfg.MongoDB,
		"playbookManifest", cfg.PlaybookManifest, "jobArtifactDir", cfg.JobArtifactDir)
	// Cancelled on SIGINT or SIGTERM, which stops the event relay and triggers a
	// graceful HTTP shutdown instead of dropping in-flight requests.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sealer, err := secret.NewSealer(cfg.CredentialKey)
	if err != nil {
		return fmt.Errorf("credential key: %w", err)
	}

	connectCtx, cancelConnect := context.WithTimeout(ctx, 15*time.Second)
	defer cancelConnect()

	client, err := storemongo.Connect(connectCtx, cfg.MongoURI)
	if err != nil {
		return err
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

	process, err := assembleAPI(ctx, cfg, modules, client, db, sealer, releaseVersion)
	if err != nil {
		return err
	}
	fiberApp := process.app

	// Each API process relays the Server changes of every process to its own stream subscribers.
	go process.relay.Run(ctx)

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

// apiProcess is the API role assembled over one store: the HTTP app with every route registered,
// and the relay that feeds its Server stream from the store's change feed. Nothing in it has
// started yet; RunAPI starts the relay and serves the app.
type apiProcess struct {
	app   *fiber.App
	relay *serverapp.ServerEventRelay
}

// assembleAPI builds the API role over db: every repository, use case, and handler, and the routes
// that serve them. It is the only place the API's dependencies are wired, so the assembly test
// (process_assembly_test.go) builds exactly what RunAPI serves. It writes to the store only to
// ensure the bootstrap admin user; it opens no listener and starts no goroutine. client must be the
// one db belongs to (transactions and the readiness check use it).
func assembleAPI(ctx context.Context, cfg config.APIConfig, modules composedModules, client *mongo.Client,
	db *mongo.Database, sealer *secret.Sealer, releaseVersion string,
) (*apiProcess, error) {
	// The one Transactor of this process: every use case that keeps a cross-document invariant
	// receives it through its constructor (decision 069).
	transactor := storemongo.NewTransactor(client)
	userRepo := authstore.NewMongoUserRepo(db)
	if err := authstore.EnsureUserIndexes(ctx, db); err != nil {
		return nil, err
	}
	accessRepos, err := newAccessStores(ctx, db, cfg.POSIXIDStart)
	if err != nil {
		return nil, fmt.Errorf("access stores init: %w", err)
	}
	siteRepo, err := sitestore.NewMongoSiteRepo(db)
	if err != nil {
		return nil, fmt.Errorf("site repo init: %w", err)
	}
	integrationRepo, err := sitestore.NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		return nil, fmt.Errorf("integration repo init: %w", err)
	}
	mongoServerRepo, err := serverstore.NewMongoServerRepo(db)
	if err != nil {
		return nil, fmt.Errorf("server repo init: %w", err)
	}
	// One in-process broker fans Server changes out to SSE subscribers. The relay started below
	// feeds it from the store's change feed, so it carries every process's Server writes.
	serverEventBroker := memory.NewServerEventBroker()
	serverRepo := mongoServerRepo
	templateRepo, err := provisioningstore.NewMongoDeploymentTemplateRepo(db, sealer)
	if err != nil {
		return nil, fmt.Errorf("deployment template repo init: %w", err)
	}
	// OS Image metadata (decision 071): the names, OS and release labels, tags, and default user
	// swallow keeps for provider images, merged onto the live catalog. Desired state that needs no
	// provider realization, so no controller and no sealing apply.
	osImageMetadataRepo, err := provisioningstore.NewMongoOSImageMetadataRepo(db)
	if err != nil {
		return nil, fmt.Errorf("os image metadata repo init: %w", err)
	}
	// OS Image uploads (decision 027): the single-use intent recorded between the create request
	// and the content request, which may reach different API processes. Expired records are
	// discarded by the store.
	osImageUploadRepo, err := provisioningstore.NewMongoOSImageUploadRepo(db)
	if err != nil {
		return nil, fmt.Errorf("os image upload repo init: %w", err)
	}
	// swallow-owned OS Image verification attestations: which deploy targets (disk/ram) a real
	// verification deploy has proven for an image. Owned data keyed by the provider image identity,
	// kept separate from the display metadata so pruning one never erases the other.
	osImageVerificationRepo, err := provisioningstore.NewMongoOSImageVerificationRepo(db)
	if err != nil {
		return nil, fmt.Errorf("os image verification repo init: %w", err)
	}
	taskRepo, err := provisioningstore.NewMongoProvisioningTaskRepo(db)
	if err != nil {
		return nil, fmt.Errorf("provisioning task repo init: %w", err)
	}
	orchestrationRepo, err := workflowstore.NewMongoWorkflowRepo(db)
	if err != nil {
		return nil, fmt.Errorf("orchestration repo init: %w", err)
	}
	operationSecretRepo, err := workflowstore.NewMongoOperationSecretRepo(db, sealer)
	if err != nil {
		return nil, fmt.Errorf("operation secret repo init: %w", err)
	}
	ownership, err := workflowstore.NewMongoOwnershipRepo(db)
	if err != nil {
		return nil, fmt.Errorf("resource ownership repo init: %w", err)
	}

	if err := bootstrap.EnsureAdminUser(
		ctx, transactor, userRepo, accessRepos.posixIDs,
		cfg.BootstrapAdminUsername, cfg.BootstrapAdminPassword,
	); err != nil {
		return nil, fmt.Errorf("bootstrap admin: %w", err)
	}

	if cfg.JWTExpiryHours > 0 {
		slog.Warn("api.jwtExpiryHours is deprecated and ignored; access tokens now live accessTokenTTL",
			"jwtExpiryHours", cfg.JWTExpiryHours, "accessTokenTTL", cfg.AccessTokenTTL.String())
	}
	jwtSvc := jwt.NewService(cfg.JWTSecret, cfg.AccessTokenTTL)
	sessionRepo, err := authstore.NewMongoSessionRepo(db)
	if err != nil {
		return nil, fmt.Errorf("session repo init: %w", err)
	}
	apiKeyRepo, err := apikeystore.NewMongoKeyRepo(db)
	if err != nil {
		return nil, fmt.Errorf("api key repo init: %w", err)
	}
	apiKeyService := apikeyapp.NewService(apiKeyRepo, apiKeyOwners{users: userRepo})
	apiKeyHandler := apikeydelivery.NewHandler(apiKeyService)
	authenticator := middleware.NewAuthenticator(jwtSvc, apiKeyService)

	monitoringFactory := promstack.NewMonitoringFactory(integrationRepo)
	alertService := monitoringapp.NewAlertService(monitoringFactory, siteExistence{sites: siteRepo})
	metricSites := serverSites{servers: serverRepo}

	// The SSE stream pushes Server projection changes so the dashboard patches rows live
	// instead of re-reading the whole list.
	serverStreamHandler := serverdelivery.NewServerStreamHandler(serverEventBroker)

	providerFactory := maas.NewProviderFactory(integrationRepo)
	serverProtection := providerServerMutationGuard{
		servers: serverRepo, providers: providerFactory,
	}
	// SSH Keys (decision 039). The Deployment Key is created by the installation step
	// (`swallow-api deployment-key ensure`), never here: API startup neither creates nor requires
	// it, and only OS and Platform deployment check that it exists. Every key is realized in
	// key-capable provisioners by the SSH Key controller in the worker (decision 071), which sees key
	// and Integration changes on the change feed; POST /ssh-keys/sync asks it through the store.
	sshKeyService, err := newSSHKeyService(db, sealer, integrationRepo, providerFactory)
	if err != nil {
		return nil, err
	}
	sshKeyHandler := sshkeydelivery.NewSSHKeyHandler(sshKeyService)
	deploymentKeys := deploymentKeySource{keys: sshKeyService}
	activeWork := activeServerWorkReader{orchestrations: orchestrationRepo}
	integrationReader := provisioningapp.NewIntegrationReader(integrationRepo, siteRepo)
	templateService := provisioningapp.NewDeploymentTemplateService(
		templateRepo, integrationReader, providerFactory)
	networkService := provisioningapp.NewNetworkConfigurationService(serverRepo, providerFactory)
	taskService := provisioningapp.NewProvisioningTaskService(taskRepo, serverRepo, releaseCleanupRetrier{
		orchestrations: orchestrationRepo,
		workflows:      workflowapp.NewWorkflowService(transactor, orchestrationRepo, workflowPlanCatalog(modules.workflowKinds, modules.software), ownership),
	}, serverProtection)
	deploymentsUC := provisioningapp.NewDeployServersUseCase(serverRepo, templateRepo, providerFactory, osImageVerificationRepo, sshKeyService)
	softwareAssignmentRepo, err := softwarestore.NewMongoAssignmentRepo(db)
	if err != nil {
		return nil, fmt.Errorf("software assignment repo init: %w", err)
	}
	// Reconcile clears the Software Assignments of a machine it observes without an OS, whoever
	// removed the OS (decision 038).
	reconcileUC := provisioningapp.NewReconcileUseCase(integrationRepo, serverRepo, providerFactory, osImageMetadataRepo,
		installedSoftwareRecords{assignments: softwareAssignmentRepo})
	// One RefreshServer use case is shared by the HTTP handler and the durable launcher so the
	// Release/Recover acceptance gate can live-sync a target's provisioning state before deciding.
	refreshServerUC := provisioningapp.NewRefreshServerUseCase(serverRepo, providerFactory, osImageMetadataRepo)
	// Delete names the Platform that claims a refused Server; its name reader is attached once the
	// platform repository exists below.
	deleteServerUC := provisioningapp.NewDeleteServerUseCase(serverRepo, providerFactory)
	provisioningHandler := provisioningdelivery.NewProvisioningHandler(
		deploymentsUC,
		provisioningapp.NewDeploymentTargetPreflightService(serverRepo, providerFactory),
		templateService,
		networkService,
		taskService,
		refreshServerUC,
		provisioningapp.NewListOSImagesUseCase(providerFactory, osImageMetadataRepo, osImageVerificationRepo),
		reconcileUC,
		provisioningapp.NewGetProvisionerDetailUseCase(serverRepo, providerFactory),
		provisioningapp.NewGetProviderEventsUseCase(serverRepo, providerFactory),
		provisioningapp.NewMachineActionsUseCase(serverRepo, providerFactory, activeWork),
		deleteServerUC,
		provisioningapp.NewDeleteOSImageUseCase(providerFactory, osImageMetadataRepo, osImageVerificationRepo),
		provisioningapp.NewOSImageUploadService(providerFactory, osImageUploadRepo, osImageMetadataRepo, cfg.ImageUploadMaxBytes),
		provisioningapp.NewSetOSImageMetadataUseCase(osImageMetadataRepo, reconcileUC),
		provisioningapp.NewListServerTagsUseCase(integrationRepo, providerFactory, serverRepo),
		provisioningapp.NewEditServerTagsUseCase(transactor, serverRepo, serverRepo, providerFactory),
		provisioningapp.NewDeleteServerTagUseCase(transactor, integrationRepo, providerFactory, serverRepo, serverRepo),
		provisioningapp.NewRenameServerTagUseCase(transactor, integrationRepo, providerFactory, serverRepo, serverRepo),
	)

	// Infrastructure: swallow-owned Zones (decision 071) and installation-wide Pools (decision 082).
	// The API writes them and the Servers' placement spec; the worker's catalog and Server controllers
	// realize the Zones. The provider factory is used here only to delete a Zone from the
	// provisioners that hold it.
	zoneRepo, err := infrastructurestore.NewMongoZoneRepo(db)
	if err != nil {
		return nil, fmt.Errorf("zone repo init: %w", err)
	}
	poolRepo, err := infrastructurestore.NewMongoPoolRepo(db)
	if err != nil {
		return nil, fmt.Errorf("pool repo init: %w", err)
	}
	platformRepo, err := platformstore.NewMongoPlatformRepo(db, sealer)
	if err != nil {
		return nil, fmt.Errorf("platform repo init: %w", err)
	}
	// Access (decision 082): the Authorizer every route below is checked with, Users, Pool
	// Memberships, and the Directory's status. A Platform's Pool is derived by one resolver, which the
	// Platform responses, authorization, and the placement guard share.
	platformPools := platformapp.NewPoolResolver(platformRepo, serverRepo, serverRepo)
	sshKeyRepo, err := sshkeystore.NewMongoKeyRepo(db, sealer)
	if err != nil {
		return nil, fmt.Errorf("ssh key repo init: %w", err)
	}
	access, err := newAccessAPI(ctx, accessAPIDeps{
		cfg: cfg, db: db, transactor: transactor, stores: accessRepos, users: userRepo, sessions: sessionRepo,
		apiKeys: apiKeyRepo, sshKeys: sshKeyRepo, servers: serverRepo, pools: poolRepo, platforms: platformPools,
		orchestrator: orchestrationRepo,
	})
	if err != nil {
		return nil, err
	}
	authHandler := authdelivery.NewAuthHandler(
		authapp.NewSessionService(userRepo, sessionRepo, jwtSvc, authapp.SessionConfig{
			RefreshTokenTTL: cfg.RefreshTokenTTL,
			MaxAge:          cfg.SessionMaxAge,
		}),
		authapp.NewMeUseCase(userRepo, meMemberships{memberships: access.memberships}),
	)
	infrastructureHandler := infrastructuredelivery.NewInfrastructureHandler(
		infrastructureapp.NewGroupingService(
			zoneRepo,
			poolRepo,
			NewSiteReader(siteRepo),
			serverPlacements{servers: serverRepo, specs: serverRepo},
			placementReferences{specs: serverRepo},
			providerFactory,
			platformPools,
			access.memberships,
			transactor,
		),
	)
	platformNames := platformNameReader{platforms: platformRepo}
	deleteServerUC.AttachPlatformNames(platformNames)

	// Monitoring reads (alerts, current values, usage history) need the grouping repositories
	// above: a usage-history scope names a Zone, Pool, or Platform, and metrics carry only
	// server_id, so the scope is resolved to Servers here at query time.
	monitoringHandler := monitoringdelivery.NewMonitoringHandler(
		alertService,
		monitoringapp.NewServerMetricsService(monitoringFactory, metricSites),
		monitoringapp.NewMetricRangeService(monitoringFactory, metricScopeResolver{
			zones: zoneRepo, pools: poolRepo, platforms: platformRepo, servers: serverRepo,
		}, metricSites),
	)
	requirementRepo := platformstore.NewMongoDeploymentRequirementRepo(db)
	requirementService := platformapp.NewDeploymentRequirementService(requirementRepo)
	platformReaderFactory, err := platformapp.NewReaderFactory(integrationRepo, modules.platformReaders...)
	if err != nil {
		return nil, err
	}
	membershipSync := platformapp.NewMembershipSyncUseCase(
		platformRepo, serverRepo, platformReaderFactory)
	// On-demand Slurm cluster read: reuses the same reader factory but never writes the
	// membership axis or sync counters. It powers the Slurm-specific management view.
	slurmClusterRead := platformapp.NewGetSlurmClusterUseCase(platformRepo, platformReaderFactory)
	// Live Kubernetes cluster explorer: a read/write client factory for a deployed Kubernetes
	// Platform's own API. Like the Slurm read it never writes swallow state; eligibility
	// (deployed Kubernetes with a recorded credential) is enforced by the use case below.
	kubernetesClientFactory := kubeapi.NewClientFactory(platformReaderFactory)

	catalog, err := ansible.LoadManifestCatalog(cfg.PlaybookManifest, cfg.PlaybookDir)
	if err != nil {
		return nil, fmt.Errorf("playbook catalog: %w", err)
	}
	lifecycleReader := platformLifecycleReader{orchestrations: orchestrationRepo}
	orchestrationService := workflowapp.NewWorkflowService(transactor, orchestrationRepo,
		workflowPlanCatalog(modules.workflowKinds, modules.software), ownership, operationSecretRepo)
	// Deleting a platform cancels its in-flight durable operations so their leases are
	// released and the member servers are freed rather than left blocked by orphaned work.
	platformService, err := newPlatformService(transactor, platformRepo, siteRepo, serverRepo, orchestrationRepo,
		integrationRepo, softwareAssignmentRepo, platformOperationCanceler{orchestrations: orchestrationService})
	if err != nil {
		return nil, err
	}
	automationRepo := workflowstore.NewMongoAutomationConfigurationRepo(db, sealer)
	// Automation readers get the effective credential (the Deployment Key, decision 039) and the
	// derived credential source; the configuration service reads the plain repository and derives
	// the same source itself, because it reports what is stored and never needs key material.
	effectiveAutomation := workflowapp.NewEffectiveAutomationConfigurations(automationRepo, deploymentKeys)
	// Server Default User (decision 039): setting one logs in to the host directly — once with an
	// optional one-time password to install the Deployment Key, then with the key to verify — on the
	// Site's SSH port, behind the live Server Lock guard.
	hostAccess := sshaccess.NewSSHHostAccess(deploymentKeys, siteSSHPorts{configurations: automationRepo})
	defaultUserHandler := serverdelivery.NewDefaultUserHandler(serverapp.NewDefaultUserUseCase(
		serverRepo, serverProtection, hostAccess,
	))
	// Hypervisors (decision 055): swallow reads a deployed Server's libvirt domains and gives its
	// virtual machines libvirt Boot Media over the same Deployment Key logins.
	libvirtHosts := libvirt.NewHost(hostAccess)
	virtualMachineHandler := serverdelivery.NewVirtualMachineHandler(serverapp.NewVirtualMachineUseCase(serverRepo, libvirtHosts))
	// Boot Media (decision 047): this process builds and serves Boot ISOs, probes each
	// Server's BMC for Redfish capability, and applies a Server's chosen Boot ISO through Redfish
	// with the BMC connection read live from the provisioner. The setting and capability are
	// written straight to the Mongo repository (the event broker only carries projection changes).
	bootISORepo, err := provisioningstore.NewMongoBootISORepo(db)
	if err != nil {
		return nil, fmt.Errorf("boot ISO repo init: %w", err)
	}
	// Deleting an Integration is refused while a Boot ISO or a Platform depends on it, and removes
	// its OS Image metadata and verification records with it.
	integrationService := siteapp.NewIntegrationService(integrationRepo, siteRepo, serverRepo, templateRepo, integrationProviders(),
		transactor, integrationDependents{
			bootISOs: bootISORepo, platforms: platformRepo, metadata: osImageMetadataRepo, verifications: osImageVerificationRepo,
		})
	siteHandler := sitedelivery.NewSiteHandler(
		siteapp.NewSiteService(siteRepo, integrationRepo),
		integrationService,
	)
	bootISOFiles := genfsimg.NewGenfsimgBuilder(cfg.BootMedia.Dir, cfg.BootMedia.IPXEDir, cfg.BootMedia.BaseURL)
	bootISOs := newBootISOCatalog(bootISORepo, bootISOFiles, cfg.BootMedia.BaseURL)
	bootISOHandler := provisioningdelivery.NewBootISOHandler(provisioningapp.NewBootISOService(
		bootISORepo, integrationReader, bootISOFiles, bootISOUsage{servers: mongoServerRepo}, bootChainRenderers()))
	bootMediaUC := serverapp.NewBootMediaUseCase(serverRepo, mongoServerRepo, serverProtection,
		newBootMediaEndpointSource(providerFactory, serverRepo), redfish.NewController(), libvirtHosts, bootISOs)
	bootMediaHandler := serverdelivery.NewBootMediaHandler(bootMediaUC)
	bootMediaPlan := bootMediaPlanner{servers: serverRepo, isos: bootISOs}
	runner := ansible.NewLocalRunner(
		cfg.AnsibleRunnerCommand, catalog.ProjectRoot(), cfg.JobRuntimeDir, cfg.JobArtifactDir)
	// Per-host SSH-user resolution: images use different default login users, so the runner probes
	// candidate users per host with the automation key instead of forcing one Site sshUser.
	runner.AttachUserProber(sshprobe.DefaultProber{})
	// Live Ansible task events, streamed by the executor and read here to serve a durable Step's
	// task list during a run rather than only after it finishes (decision: live progress).
	ansibleEventRepo, err := workflowstore.NewMongoAnsibleEventRepo(db)
	if err != nil {
		return nil, fmt.Errorf("ansible event repo init: %w", err)
	}
	// Local Repository: the API reads and writes each Site's spec, browses content live through the
	// Site's repository Integration, and resolves at acceptance where a deployment's hosts fetch
	// their artifacts (the Local Repository or the upstreams); the worker's controller converges the
	// provider.
	localRepositories := localrepostore.NewMongoLocalRepositoryRepo(db)
	localRepositoryProviders := pulp.NewRepositoryFactory(integrationRepo, nil)
	localRepositoryHandler := localrepodelivery.NewHandler(localrepoapp.NewService(
		siteRepo, integrationRepo, localRepositories, localRepositoryProviders, convergencestore.NewMongoRequester(db), modules.artifacts))
	operationService := workflowapp.NewExecutionService(
		serverRepo, effectiveAutomation, catalog, modules.workflowKinds, runner, ansibleEventRepo,
		platformapp.NewPolicyChecker(platformRepo, serverRepo),
		serverProtection, orchestrationService,
		localrepoapp.NewResolver(integrationRepo, localRepositories, modules.artifacts),
	)
	automationService := workflowapp.NewAutomationConfigurationService(
		automationRepo, siteRepo, catalog, modules.workflowKinds, deploymentKeys,
	)
	// Both deployment launchers point every Server they provision at the installation's NTP server
	// after SSH readiness, and Platform deployments check time sync (decision 065); the Tasks are
	// prepared through the same ansible acceptance checks as any other.
	ntpPlan := ntpStepPlanner{server: cfg.NTPServer, operations: operationService}
	// Software deployment (Managed Software, decision 038): install/uninstall a single piece of
	// host software on deployed Servers, tracked by a swallow-owned Software Assignment. It reuses
	// the operation orchestration (PrepareAnsibleStep + WorkflowService) through a launcher, so it
	// shares the platform deployment engine rather than owning a second one. It is built before the
	// deployment launchers because they compose software: the Node Exporter after every OS they
	// provision, and a Slurm deployment's features (decision 063).
	softwareService := softwareapp.NewSoftwareService(
		transactor, modules.software, modules.accelerators, softwareAssignmentRepo, serverRepo,
		kubernetesMembershipChecker{servers: serverRepo, platforms: platformRepo},
		newDirectoryClientSettings(cfg, db, accessRepos),
	)
	softwareService.AttachLauncher(softwareDeploymentLauncher{
		operations: operationService, orchestrations: orchestrationService, modules: modules.registry,
	})
	softwareService.AttachPlatformNames(platformNames)

	nodeExporterPlan := nodeExporterPlanner{software: softwareService, operations: operationService}
	provisioningHandler.AttachDurableOperations(durableProvisioningLauncher{
		deployments: deploymentsUC, operations: orchestrationService, servers: serverRepo,
		protection: serverProtection, refresh: refreshServerUC, bootMedia: bootMediaPlan, ntp: ntpPlan,
		nodeExporter: nodeExporterPlan, platformNames: platformNames,
	})
	// Server Enrollment (decision 053): Inspect and the automatic sweep both run hardware
	// inspection as an inspect-hardware Workflow; existing-OS hosts get their bundle and script
	// from the provisioner adapter.
	inspectionLauncher := hardwareInspectionLauncher{
		workflows: orchestrationService, history: orchestrationRepo, servers: serverRepo,
		protection: serverProtection, refresh: refreshServerUC,
	}
	provisioningHandler.AttachHardwareInspection(inspectionLauncher)
	provisioningHandler.AttachHostEnrollment(provisioningapp.NewHostEnrollmentUseCase(providerFactory))
	provisioningHandler.AttachPowerConfiguration(provisioningapp.NewPowerConfigurationUseCase(
		serverRepo, providerFactory, provisioningdomain.DefaultPowerAdapters()))
	subnetDNSRepo, err := provisioningstore.NewMongoSubnetDNSRepo(db)
	if err != nil {
		return nil, fmt.Errorf("subnet DNS repo init: %w", err)
	}
	provisioningHandler.AttachHTTPProxy(provisioningapp.NewHTTPProxyUseCase(providerFactory, provisioningstore.NewMongoHTTPProxyRepo(db)))
	provisioningHandler.AttachSubnetDNS(provisioningapp.NewSubnetDNSUseCase(providerFactory, subnetDNSRepo))
	// Virtual-machine enrollment (decision 055): libvirt domains of a Hypervisor are registered with
	// the provisioner by an enroll-virtual-machines Workflow run by the worker.
	provisioningHandler.AttachVirtualMachineEnrollment(virtualMachineEnrollmentLauncher{
		workflows: orchestrationService, integrations: integrationRepo, providers: providerFactory,
		servers: serverRepo, protection: serverProtection, isos: bootISOs, keys: sshKeyService,
	})
	operationHandler := workflowdelivery.NewExecutionHandler(operationService, automationService, orchestrationService)

	// Validation (decision 081): the suite catalog, user suites, runs, and schedules. Runs are
	// accepted here as unattended Workflows; Platform deployments compose gate suites through the
	// same service.
	validationService, err := newValidationService(validationDeps{
		db: db, transactor: transactor, modules: modules, operations: operationService, orchestrations: orchestrationService,
		servers: serverRepo, platforms: platformRepo, lifecycle: lifecycleReader, retention: cfg.ValidationRunRetention,
		ntpServer: cfg.NTPServer,
	})
	if err != nil {
		return nil, fmt.Errorf("validation service: %w", err)
	}

	// Platform lifecycle adapters keep durable operations outside the platform context. A Platform's
	// monitoring token is issued by the deployments here and verified by the metrics gateway below,
	// over the same store.
	platformAccess := newPlatformTokenIssuer(db)
	platformOps, err := buildPlatformOperations(platformOperationDeps{
		transactor: transactor, modules: modules, platformService: platformService, platforms: platformRepo,
		servers: serverRepo, claims: serverRepo, lifecycle: lifecycleReader,
		operations: operationService, orchestrations: orchestrationService, deployments: deploymentsUC,
		bootMedia: bootMediaPlan, software: softwareService, ntp: ntpPlan, nodeExporter: nodeExporterPlan,
		protection: serverProtection,
		networks:   networkService, requirements: requirementRepo, keys: sshKeyService,
		monitoring: newPlatformMonitoringAccess(cfg), monitoringTokens: platformAccess, validation: validationService,
	})
	if err != nil {
		return nil, err
	}
	deployService, gpuOperatorService := platformOps.deploy, platformOps.gpuOperators
	changePlans, err := newChangePlanService(ctx, db, transactor, cfg.ChangePlanTTL, platformOps.planner)
	if err != nil {
		return nil, err
	}
	kubernetesExplorer := platformapp.NewKubernetesExplorerUseCase(
		platformRepo, lifecycleReader, kubernetesClientFactory, serverRepo)
	platformHandler := platformdelivery.NewPlatformHandler(
		platformService, membershipSync, slurmClusterRead, kubernetesExplorer, requirementService)
	platformHandler.AttachGPUOperatorService(gpuOperatorService)
	// Platforms are written as specs (desired-state-model §5.2): a destructive change is proposed as
	// a Change Plan with the spec, and the worker's platform controller converges the rest.
	platformHandler.AttachSpecService(platformapp.NewPlatformSpecService(
		transactor, platformRepo, platformRepo, serverRepo, deployService, platformService, changePlans,
		convergencestore.NewMongoRequester(db), modules.platforms))

	softwareHandler := softwaredelivery.NewSoftwareHandler(softwareService)
	// Docker Host Explorer (decision 043): live management of the Docker Engine on a Server where
	// swallow installed Docker CE with enableApi. Eligibility reads the same assignment repository;
	// writes go through the provider-backed lock guard; Docker objects are never persisted.
	dockerExplorer := explorer.NewDockerExplorerUseCase(
		softwareAssignmentRepo, serverRepo, dockerengine.NewFactory(), serverProtection,
	)
	// Registry Credentials (decision 044): installation-wide, sealed with the credential key, and
	// attached to an explorer pull whose image reference resolves to their registry.
	registryCredentialRepo, err := softwarestore.NewMongoRegistryCredentialRepo(db, sealer)
	if err != nil {
		return nil, fmt.Errorf("registry credential repo init: %w", err)
	}
	dockerExplorer.AttachRegistryCredentials(registryCredentialRepo)
	dockerHandler := explorerhttp.NewDockerHandler(dockerExplorer)
	registryCredentialHandler := softwaredelivery.NewRegistryCredentialHandler(
		softwareapp.NewRegistryCredentialService(registryCredentialRepo))

	discoveryUseCase := discoveryapp.NewDiscoveryUseCase(serverRepo)
	exporterDiscovery := discoveryapp.NewExporterDiscovery(serverRepo,
		installedExporters{catalog: modules.software, assignments: softwareAssignmentRepo, platforms: platformRepo,
			servers: serverRepo, accelerator: modules.accelerators[amd.ID]})
	discoveryHandler := discoverydelivery.NewDiscoveryHandler(discoveryUseCase, exporterDiscovery)

	// The health axis is classified each time a Server is read and never persisted
	// (servers-list.md, Health). The monitoring context owns the rule; the Node Exporter targets
	// come from the same exporter discovery the Site Prometheus scrapes, and the coverage from the
	// same policy the worker's Node Exporter controller applies (whether or not the worker runs it),
	// so a Server is monitored, and read, by one definition. The Server reads and the overview
	// share the one resolver.
	healthResolver := monitoringapp.NewHealthResolver(monitoringFactory, exporterDiscovery,
		softwareapp.NewEnsureCoverage(automationRepo))
	serverHandler := serverdelivery.NewServerHandler(
		serverapp.NewListServersUseCase(serverRepo, healthResolver),
		serverapp.NewGetServerUseCase(serverRepo, healthResolver),
	)
	overviewReader := NewOverviewReader(
		siteRepo, integrationRepo, serverRepo, healthResolver, platformRepo, orchestrationRepo, alertService,
	)
	overviewHandler := overviewdelivery.NewHandler(
		overviewapp.NewService(overviewReader, overviewapp.SystemClock{}))

	fiberApp := fiber.New(fiber.Config{
		// The OS image upload content (PUT /provisioning/image-uploads/{id}/content) is a
		// potentially multi-gigabyte raw body, so request bodies are streamed: the handler reads
		// the stream as the provider adapter forwards it, and nothing is spooled to disk or held
		// whole in memory. In stream mode fasthttp does not refuse a body over BodyLimit, so the
		// limits are enforced by the app: middleware.BodyLimit below bounds every other route, and
		// the upload's own check (Content-Length must equal its declared size, at most
		// api.imageUploadMaxBytes) bounds the content.
		StreamRequestBody: true,
		// fasthttp otherwise parses a multipart/form-data body before any handler or middleware
		// runs — authentication included — spooling its file parts to the temp directory, and in
		// stream mode without checking BodyLimit. No route accepts multipart, so the pre-parse is
		// off: an unauthenticated request must not be able to fill the API's /tmp (a tmpfs, so
		// host memory, in the production installation).
		DisablePreParseMultipartForm: true,
		BodyLimit:                    requestBodyLimit,
		ErrorHandler:                 httpErrorHandler,
	})
	fiberApp.Use(requestid.New())
	fiberApp.Use(requestLog(slog.Default()))
	// Before authentication and every handler, so an oversized body is refused before anything
	// reads it; registered after the request ID and the request log so the 413 carries an ID and
	// is logged like any other response.
	fiberApp.Use(middleware.BodyLimit(requestBodyLimit, isImageUploadContent))
	if cfg.AllowedOrigins != "" {
		// Credentials are allowed so a dashboard served from a listed development origin can
		// send the refresh cookie to /api/v1/auth. That is only safe with explicit origins,
		// which config validation enforces (no "*").
		fiberApp.Use(cors.New(cors.Config{
			AllowOrigins:     cfg.AllowedOrigins,
			AllowMethods:     "GET,POST,PATCH,PUT,DELETE,OPTIONS",
			AllowHeaders:     "Content-Type,Authorization",
			AllowCredentials: true,
		}))
	}

	registerRoutes(fiberApp, routeDeps{
		jwtSvc:          jwtSvc,
		authenticator:   authenticator,
		machineToken:    cfg.MachineToken,
		auth:            authHandler,
		overview:        overviewHandler,
		sites:           siteHandler,
		servers:         serverHandler,
		defaultUsers:    defaultUserHandler,
		virtualMachines: virtualMachineHandler,
		bootMedia:       bootMediaHandler,
		bootMediaISO:    serveBootISO(bootISOFiles),
		cliBinary:       serveCLIBinary(cfg.CLIBinary),
		bootISOs:        bootISOHandler,
		serverStream:    serverStreamHandler,
		provisioning:    provisioningHandler,
		operations:      operationHandler,
		monitoring:      monitoringHandler,
		gateway: monitoringdelivery.NewGatewayHandler(platformAccess,
			platformMetricsUpstream{platforms: platformRepo, integrations: integrationRepo}),
		platforms:       platformHandler,
		software:        softwareHandler,
		docker:          dockerHandler,
		registryCreds:   registryCredentialHandler,
		infrastructure:  infrastructureHandler,
		discovery:       discoveryHandler,
		sshKeys:         sshKeyHandler,
		apiKeys:         apiKeyHandler,
		changePlans:     convergencedelivery.NewChangePlanHandler(changePlans, changePlanAccess{}),
		access:          access,
		moduleList:      moduledelivery.NewModuleHandler(modules.registry),
		localRepository: localRepositoryHandler,
		validation:      validationdelivery.NewHandler(validationService),
		validationRuns:  validationRunAccess{runs: validationService},
		releaseVersion:  releaseVersion,
		readiness: func(ctx context.Context) error {
			if err := client.Ping(ctx, nil); err != nil {
				return err
			}
			return migration.Check(ctx, db)
		},
	})

	return &apiProcess{
		app:   fiberApp,
		relay: serverapp.NewServerEventRelay(storemongo.NewChangeFeed(db), mongoServerRepo, serverEventBroker),
	}, nil
}

type routeDeps struct {
	jwtSvc *jwt.Service
	// authenticator verifies Session access tokens and API Keys for every authenticated route;
	// jwtSvc remains only for the machine-token routes' admin fallback.
	authenticator *middleware.Authenticator
	machineToken  string
	auth          *authdelivery.AuthHandler
	overview      *overviewdelivery.Handler
	sites         *sitedelivery.SiteHandler
	servers       *serverdelivery.ServerHandler
	defaultUsers  *serverdelivery.DefaultUserHandler
	// virtualMachines lists a Hypervisor's libvirt domains (decision 055).
	virtualMachines *serverdelivery.VirtualMachineHandler
	bootMedia       *serverdelivery.BootMediaHandler
	// bootMediaISO serves Boot ISO files without authentication (decision 047).
	bootMediaISO fiber.Handler
	// cliBinary serves the swallow CLI without authentication for Server Enrollment (decision 053).
	cliBinary fiber.Handler
	// bootISOs builds, lists, and deletes Boot ISOs (decision 047).
	bootISOs       *provisioningdelivery.BootISOHandler
	serverStream   *serverdelivery.ServerStreamHandler
	provisioning   *provisioningdelivery.ProvisioningHandler
	operations     *workflowdelivery.ExecutionHandler
	monitoring     *monitoringdelivery.MonitoringHandler
	gateway        *monitoringdelivery.GatewayHandler
	platforms      *platformdelivery.PlatformHandler
	software       *softwaredelivery.SoftwareHandler
	docker         *explorerhttp.DockerHandler
	registryCreds  *softwaredelivery.RegistryCredentialHandler
	infrastructure *infrastructuredelivery.InfrastructureHandler
	discovery      *discoverydelivery.DiscoveryHandler
	sshKeys        *sshkeydelivery.SSHKeyHandler
	apiKeys        *apikeydelivery.Handler
	changePlans    *convergencedelivery.ChangePlanHandler
	// moduleList lists the modules of the build and their spec schemas (contract modules.md).
	moduleList *moduledelivery.ModuleHandler
	// localRepository serves each Site's Local Repository (contract local-repository.md).
	localRepository *localrepodelivery.Handler
	// validation serves Validation Suites, Runs, and Schedules (contract validation.md).
	validation *validationdelivery.Handler
	// access is the authorization every route is checked with, and the users, pool-access, and
	// access contracts' handler (decision 082).
	access *accessAPI
	// validationRuns finds a Validation Run's targets for the run routes' access checks.
	validationRuns validationRunAccess
	readiness      func(context.Context) error
	releaseVersion string
}

// platformGuards are the handler chains the Platform routes run before their handler
// (platforms.md, Authorization): reading a Platform needs any role in its Pool, changing it an owner
// of that Pool, and creating one or writing its spec also that every member is placed in one Pool the
// caller owns.
type platformGuards struct {
	authed, admin, read, operate, create, spec []fiber.Handler
}

// registerPlatformRoutes registers the Platform routes on routes.
func registerPlatformRoutes(routes fiber.Router, handler *platformdelivery.PlatformHandler, guards platformGuards) {
	chain := func(guard []fiber.Handler, h fiber.Handler) []fiber.Handler { return append(slices.Clone(guard), h) }
	// A Platform is created from its spec, and its install waits on a Change Plan (decision 076);
	// registering an existing one was removed (decision 032).
	routes.Get("/", chain(guards.authed, handler.List)...)
	routes.Post("/", chain(guards.create, handler.Create)...)
	routes.Post("/sync", chain(guards.admin, handler.SyncAllMembership)...)
	routes.Get("/:id", chain(guards.read, handler.Get)...)
	routes.Patch("/:id", chain(guards.operate, handler.Update)...)
	routes.Put("/:id/spec", chain(guards.spec, handler.PutSpec)...)
	routes.Delete("/:id", chain(guards.operate, handler.Delete)...)
	routes.Post("/:id/sync", chain(guards.operate, handler.SyncMembership)...)
	routes.Get("/:id/slurm", chain(guards.read, handler.GetSlurmCluster)...)
	// The live Kubernetes explorer acts on the cluster with swallow's credentials, so even its reads
	// need an owner of the Platform's Pool.
	registerKubernetesExplorerRoutes(guardedRouter{Router: routes, handlers: guards.operate}, handler)
}

// registerKubernetesExplorerRoutes mounts the live Kubernetes cluster explorer under a
// deployed Platform. These routes read and write the deployed cluster's own API on demand and
// persist nothing (decision 032); the contract is platforms-kubernetes.md.
func registerKubernetesExplorerRoutes(routes fiber.Router, handler *platformdelivery.PlatformHandler) {
	routes.Get("/:id/kubernetes", handler.GetKubernetesCluster)

	routes.Get("/:id/kubernetes/nodes", handler.ListKubernetesNodes)
	routes.Post("/:id/kubernetes/nodes/:node/cordon", handler.CordonKubernetesNode)
	routes.Post("/:id/kubernetes/nodes/:node/uncordon", handler.UncordonKubernetesNode)

	routes.Get("/:id/kubernetes/namespaces", handler.ListKubernetesNamespaces)
	routes.Post("/:id/kubernetes/namespaces", handler.CreateKubernetesNamespace)
	routes.Delete("/:id/kubernetes/namespaces/:namespace", handler.DeleteKubernetesNamespace)

	routes.Get("/:id/kubernetes/applications", handler.ListKubernetesApplications)
	routes.Get("/:id/kubernetes/applications/:namespace/:kind/:name", handler.GetKubernetesApplication)
	routes.Delete("/:id/kubernetes/applications/:namespace/:kind/:name", handler.DeleteKubernetesApplication)
	routes.Post("/:id/kubernetes/applications/:namespace/:kind/:name/scale", handler.ScaleKubernetesApplication)
	routes.Post("/:id/kubernetes/applications/:namespace/:kind/:name/restart", handler.RestartKubernetesApplication)

	routes.Get("/:id/kubernetes/pods", handler.ListKubernetesPods)
	routes.Get("/:id/kubernetes/pods/:namespace/:name/logs", handler.GetKubernetesPodLogs)
	routes.Delete("/:id/kubernetes/pods/:namespace/:name", handler.DeleteKubernetesPod)

	routes.Get("/:id/kubernetes/services", handler.ListKubernetesServices)
	routes.Get("/:id/kubernetes/ingresses", handler.ListKubernetesIngresses)
	routes.Get("/:id/kubernetes/configmaps", handler.ListKubernetesConfigMaps)
	routes.Get("/:id/kubernetes/secrets", handler.ListKubernetesSecrets)
	routes.Get("/:id/kubernetes/persistentvolumeclaims", handler.ListKubernetesPersistentVolumeClaims)

	routes.Post("/:id/kubernetes/apply", handler.ApplyKubernetesManifest)
}

// guardedRouter prepends handlers to every route registered through it, so a feature's
// RegisterRoutes helper gets the authorization its routes need without knowing about it.
type guardedRouter struct {
	fiber.Router
	handlers []fiber.Handler
}

func (r guardedRouter) chain(h []fiber.Handler) []fiber.Handler {
	return append(slices.Clone(r.handlers), h...)
}

// Get implements fiber.Router with the guard prepended.
func (r guardedRouter) Get(path string, h ...fiber.Handler) fiber.Router {
	return r.Router.Get(path, r.chain(h)...)
}

// Post implements fiber.Router with the guard prepended.
func (r guardedRouter) Post(path string, h ...fiber.Handler) fiber.Router {
	return r.Router.Post(path, r.chain(h)...)
}

// Put implements fiber.Router with the guard prepended.
func (r guardedRouter) Put(path string, h ...fiber.Handler) fiber.Router {
	return r.Router.Put(path, r.chain(h)...)
}

// Patch implements fiber.Router with the guard prepended.
func (r guardedRouter) Patch(path string, h ...fiber.Handler) fiber.Router {
	return r.Router.Patch(path, r.chain(h)...)
}

// Delete implements fiber.Router with the guard prepended.
func (r guardedRouter) Delete(path string, h ...fiber.Handler) fiber.Router {
	return r.Router.Delete(path, r.chain(h)...)
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
	// BMCs mount this URL with no credential; Fiber's Get also answers HEAD, which BMCs send first.
	if deps.bootMediaISO != nil {
		app.Get(bootISORoute, deps.bootMediaISO)
	}
	// A host enrolling with its OS kept has no credential (decision 053): it fetches the script and
	// the CLI it downloads without one. Neither carries a secret.
	app.Get(provisioningapp.HostEnrollmentScriptPath, serveEnrollmentScript())
	if deps.cliBinary != nil {
		app.Get(provisioningapp.CLIDownloadPath, deps.cliBinary)
	}
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
	g := deps.access.guard
	// authed authenticates the caller and computes its Scope from its current User Role and Pool
	// Memberships (decision 082); every authorization check below reads that Scope. admin adds the
	// User Role check. Groups carry no middleware of their own: Fiber applies a group's middleware to
	// every route under its prefix, so each route lists what it needs.
	authed := []fiber.Handler{deps.authenticator.Require(), g.Scope()}
	admin := append(slices.Clone(authed), g.Admin())
	with := func(extra ...fiber.Handler) []fiber.Handler { return append(slices.Clone(authed), extra...) }
	route := func(h fiber.Handler, extra ...fiber.Handler) []fiber.Handler { return append(with(extra...), h) }
	adminRoute := func(h fiber.Handler) []fiber.Handler { return append(slices.Clone(admin), h) }
	serverRead := g.Server(accessdomain.Read, "id")
	serverOperate := g.Server(accessdomain.Operate, "id")
	platformRead := g.Platform(accessdomain.Read, "id")
	platformOperate := g.Platform(accessdomain.Operate, "id")
	workflowRead := g.Workflow(accessdomain.Read, "id")
	workflowOperate := g.Workflow(accessdomain.Operate, "id")

	auth := v1.Group("/auth")
	auth.Post("/login", deps.auth.Login)
	auth.Post("/refresh", deps.auth.Refresh)
	auth.Post("/logout", deps.authenticator.Optional(), deps.auth.Logout)
	auth.Get("/me", deps.authenticator.Require(), deps.auth.Me)
	auth.Put("/me/password", append(with(middleware.SessionOnly()), deps.access.handler.ChangeOwnPassword)...)

	v1.Get("/overview", adminRoute(deps.overview.Get)...)
	v1.Get("/modules", route(deps.moduleList.List)...)

	sites := v1.Group("/sites")
	sites.Post("/", adminRoute(deps.sites.CreateSite)...)
	sites.Get("/", route(deps.sites.ListSites)...)
	sites.Get("/:id", route(deps.sites.GetSite)...)
	sites.Patch("/:id", adminRoute(deps.sites.UpdateSite)...)
	sites.Delete("/:id", adminRoute(deps.sites.DeleteSite)...)
	sites.Get("/:id/automation", adminRoute(deps.operations.GetAutomation)...)
	sites.Put("/:id/automation", adminRoute(deps.operations.PutAutomation)...)
	sites.Put("/:id/automation/credential", adminRoute(deps.operations.PutAutomationCredential)...)
	// A Site's Local Repository (contract local-repository.md): the source its deployments fetch
	// every artifact from, converged by the worker's local-repository controller.
	deps.localRepository.RegisterRoutes(guardedRouter{Router: sites, handlers: admin})
	integrations := v1.Group("/integrations")
	integrations.Post("/", adminRoute(deps.sites.CreateIntegration)...)
	integrations.Get("/", adminRoute(deps.sites.ListIntegrations)...)
	integrations.Get("/:id", adminRoute(deps.sites.GetIntegration)...)
	integrations.Patch("/:id", adminRoute(deps.sites.UpdateIntegration)...)
	integrations.Put("/:id/credential", adminRoute(deps.sites.ReplaceCredential)...)
	integrations.Delete("/:id", adminRoute(deps.sites.DeleteIntegration)...)

	// Users and access (contracts users.md, access.md; decision 082).
	users := v1.Group("/users")
	users.Get("/", adminRoute(deps.access.handler.ListUsers)...)
	users.Post("/", adminRoute(deps.access.handler.CreateUser)...)
	users.Get("/:id", adminRoute(deps.access.handler.GetUser)...)
	users.Patch("/:id", adminRoute(deps.access.handler.UpdateUser)...)
	users.Delete("/:id", adminRoute(deps.access.handler.DeleteUser)...)
	accessRoutes := v1.Group("/access")
	accessRoutes.Get("/settings", adminRoute(deps.access.handler.GetAccessSettings)...)
	accessRoutes.Put("/settings", adminRoute(deps.access.handler.SetAccessSettings)...)
	accessRoutes.Get("/audit", adminRoute(deps.access.handler.ListAccessAudit)...)
	accessRoutes.Get("/directory", adminRoute(deps.access.handler.GetDirectory)...)
	accessRoutes.Post("/directory/sync", adminRoute(deps.access.handler.SyncDirectory)...)

	// Servers cannot be created directly: reconciliation projects provisioner inventory.
	// Explicit deletion is provider-backed so the next pass cannot recreate the record.
	// Lifecycle actions are addressed by server because that is the identifier callers hold.
	// Live Server change stream (SSE). Registered before the "/servers" group so its static
	// path is unambiguous next to "/servers/:id", and with its own middleware order because a
	// browser EventSource cannot send an Authorization header: the access token arrives in a
	// query parameter, which BearerTokenFromQuery promotes to a Bearer header before Auth.
	v1.Get("/servers/stream",
		middleware.BearerTokenFromQuery("access_token"),
		deps.authenticator.Require(),
		g.Scope(),
		deps.serverStream.Stream)

	servers := v1.Group("/servers")
	servers.Get("/", route(deps.servers.List)...)
	servers.Get("/:id", route(deps.servers.Get, serverRead)...)
	servers.Post("/:id/refresh", route(deps.provisioning.RefreshServer, serverOperate)...)
	servers.Delete("/:id", adminRoute(deps.provisioning.DeleteServer)...)
	servers.Put("/:id/default-user", route(deps.defaultUsers.Set, serverOperate)...)
	servers.Delete("/:id/default-user", route(deps.defaultUsers.Clear, serverOperate)...)
	servers.Get("/:id/boot-media", adminRoute(deps.bootMedia.Get)...)
	servers.Put("/:id/boot-media", adminRoute(deps.bootMedia.Set)...)
	servers.Post("/:id/redfish/probe", adminRoute(deps.bootMedia.Probe)...)
	servers.Get("/:id/virtual-machines", adminRoute(deps.virtualMachines.List)...)
	// The provisioner detail is a live proxy read one machine at a time, distinct from
	// the mirrored projection the list and get return.
	servers.Get("/:id/provisioner-detail", route(deps.provisioning.ProvisionerDetail, serverOperate)...)
	servers.Get("/:id/events", route(deps.provisioning.ProviderEvents, serverRead)...)
	servers.Get("/:id/network", route(deps.provisioning.GetNetwork, serverRead)...)
	servers.Post("/:id/network/interfaces/:interfaceId/links", route(deps.provisioning.CreateNetworkLink, serverOperate)...)
	servers.Put("/:id/network/interfaces/:interfaceId/links/:linkId", route(deps.provisioning.ReplaceNetworkLink, serverOperate)...)
	servers.Delete("/:id/network/interfaces/:interfaceId/links/:linkId", route(deps.provisioning.DeleteNetworkLink, serverOperate)...)
	servers.Get("/:id/provisioning-tasks", route(deps.provisioning.ListProvisioningTasks, serverRead)...)
	// Power, hardware validation, and state actions are the provisioner actions beyond
	// deploy and release. Each is refused by a provisioner that does not offer it.
	servers.Post("/:id/power-on", route(deps.provisioning.PowerOn, serverOperate)...)
	servers.Post("/:id/power-off", route(deps.provisioning.PowerOff, serverOperate)...)
	servers.Get("/:id/power-state", route(deps.provisioning.PowerState, serverRead)...)
	// The provisioner-owned Power Configuration (decision 054), read live and written through.
	servers.Get("/:id/power-configuration", adminRoute(deps.provisioning.GetPowerConfiguration)...)
	servers.Put("/:id/power-configuration", adminRoute(deps.provisioning.SetPowerConfiguration)...)
	servers.Post("/:id/inspect", route(deps.provisioning.Inspect, serverOperate)...)
	servers.Post("/:id/test", route(deps.provisioning.Test, serverOperate)...)
	servers.Post("/:id/abort", route(deps.provisioning.Abort, serverOperate)...)
	servers.Post("/:id/override-failed-testing", route(deps.provisioning.OverrideFailedTesting, serverOperate)...)
	servers.Post("/:id/lock", route(deps.provisioning.Lock, serverOperate)...)
	servers.Post("/:id/unlock", route(deps.provisioning.Unlock, serverOperate)...)
	servers.Post("/:id/mark-broken", route(deps.provisioning.MarkBroken, serverOperate)...)
	servers.Post("/:id/mark-fixed", route(deps.provisioning.MarkFixed, serverOperate)...)
	servers.Post("/:id/rescue-mode", route(deps.provisioning.RescueMode, serverOperate)...)
	servers.Post("/:id/exit-rescue-mode", route(deps.provisioning.ExitRescueMode, serverOperate)...)
	// Placement assigns a Server to a swallow-owned Zone and/or Pool, realized in the
	// provisioner (decision 071). Mounted here because clients address a Server, but handled by
	// the infrastructure feature that owns the Zone/Pool catalog.
	servers.Put("/:id/placement", adminRoute(deps.infrastructure.AssignServerPlacement)...)
	// The Docker Host Explorer controls a host's Docker Engine, so even its reads need an owner.
	explorerhttp.RegisterRoutes(guardedRouter{Router: servers, handlers: with(serverOperate)}, deps.docker)

	provisioning := v1.Group("/provisioning")
	provisioning.Get("/images", route(deps.provisioning.ListImages)...)
	provisioning.Delete("/images", adminRoute(deps.provisioning.DeleteImage)...)
	// OS image upload (decision 027): record the intent, then stream the raw content, which the
	// handler forwards to the provider as it arrives; the provider decides whether the result is a
	// custom image.
	provisioning.Post("/image-uploads", adminRoute(deps.provisioning.CreateImageUpload)...)
	provisioning.Put("/image-uploads/:id/content", adminRoute(deps.provisioning.SendImageUploadContent)...)
	// OS Image metadata: replace or clear the swallow-owned name, OS, release, tags, and default
	// user of one image. Same query-parameter identity as DELETE /images (decision 071).
	provisioning.Put("/images/metadata", adminRoute(deps.provisioning.SetImageMetadata)...)
	provisioning.Delete("/images/metadata", adminRoute(deps.provisioning.ClearImageMetadata)...)
	// Server tags: list the Site's known tags for the editor and the tag manager, apply a tri-state
	// add/remove edit to one or more Servers' spec, and delete or rename a tag across a Site
	// (decision 071). Under /provisioning to avoid the GET /servers/:id route collision.
	provisioning.Get("/tags", route(deps.provisioning.ListServerTags)...)
	provisioning.Post("/tags", route(deps.provisioning.EditServerTags, g.ServersInBody(accessdomain.Operate, serverIDsField))...)
	provisioning.Delete("/tags/:name", adminRoute(deps.provisioning.DeleteServerTag)...)
	provisioning.Patch("/tags/:name", adminRoute(deps.provisioning.RenameServerTag)...)
	provisioning.Get("/templates", route(deps.provisioning.ListTemplates)...)
	provisioning.Post("/templates", adminRoute(deps.provisioning.CreateTemplate)...)
	provisioning.Get("/templates/:id", route(deps.provisioning.GetTemplate)...)
	provisioning.Patch("/templates/:id", adminRoute(deps.provisioning.UpdateTemplate)...)
	provisioning.Delete("/templates/:id", adminRoute(deps.provisioning.DeleteTemplate)...)
	provisioning.Put("/templates/:id/user-data", adminRoute(deps.provisioning.ReplaceTemplateUserData)...)
	provisioning.Delete("/templates/:id/user-data", adminRoute(deps.provisioning.ClearTemplateUserData)...)
	bodyServers := g.ServersInBody(accessdomain.Operate, serverIDsField)
	provisioning.Post("/deployments/preflight", route(deps.provisioning.PreflightDeployServers, bodyServers)...)
	provisioning.Post("/deployment-operations", route(deps.provisioning.CreateDeploymentOperation, bodyServers)...)
	provisioning.Post("/release-operations", route(deps.provisioning.CreateReleaseOperation, bodyServers)...)
	// Prove a custom OS Image works for a deploy target by deploying it on a chosen ready Server,
	// recording the swallow-owned verification, and auto-releasing the Server (docs/decisions/035).
	provisioning.Post("/image-verifications", adminRoute(deps.provisioning.CreateImageVerification)...)
	provisioning.Post("/recover-operations", route(deps.provisioning.CreateRecoverOperation, bodyServers)...)
	provisioning.Post("/networks/inspect", route(deps.provisioning.InspectNetworks, bodyServers)...)
	provisioning.Get("/tasks/:id", adminRoute(deps.provisioning.GetProvisioningTask)...)
	provisioning.Post("/tasks/:id/retry", adminRoute(deps.provisioning.RetryProvisioningTask)...)
	provisioning.Post("/reconcile", adminRoute(deps.provisioning.ReconcileAll)...)
	provisioning.Post("/integrations/:id/reconcile", adminRoute(deps.provisioning.Reconcile)...)
	provisioning.Post("/integrations/:id/enroll-bundle", adminRoute(deps.provisioning.EnrollBundle)...)
	// The provisioner-owned proxy handed to deployed machines, read live and written through; not an
	// Integration setting (contract provisioner-http-proxy.md, decision 071).
	provisioning.Get("/integrations/:id/http-proxy", adminRoute(deps.provisioning.GetHTTPProxy)...)
	provisioning.Put("/integrations/:id/http-proxy", adminRoute(deps.provisioning.SetHTTPProxy)...)
	// The provisioner's subnets and their DNS servers, read live and written through (contract
	// provisioner-subnet-dns.md, decision 058).
	provisioning.Get("/integrations/:id/subnets", adminRoute(deps.provisioning.ListSubnets)...)
	provisioning.Put("/integrations/:id/subnets/:subnetId/dns-servers", adminRoute(deps.provisioning.SetSubnetDNSServers)...)
	provisioning.Post("/virtual-machine-enrollments", adminRoute(deps.provisioning.EnrollVirtualMachines)...)
	// Boot ISOs (decision 047): built here, mounted by a Server's Boot Media.
	if deps.bootISOs != nil {
		provisioning.Get("/boot-isos", adminRoute(deps.bootISOs.List)...)
		provisioning.Post("/boot-isos", adminRoute(deps.bootISOs.Create)...)
		provisioning.Get("/boot-isos/:id", adminRoute(deps.bootISOs.Get)...)
		provisioning.Post("/boot-isos/:id/rebuild", adminRoute(deps.bootISOs.Rebuild)...)
		provisioning.Delete("/boot-isos/:id", adminRoute(deps.bootISOs.Delete)...)
	}

	// swallow owns Platform registration and policy; membership is observed from the
	// runtime API. A Platform belongs to the Pool every one of its members is placed in.
	platforms := v1.Group("/platforms")
	platforms.Get("/deployment-requirements/slurm", route(deps.platforms.GetSlurmDeploymentRequirement)...)
	platforms.Put("/deployment-requirements/slurm", adminRoute(deps.platforms.PutSlurmDeploymentRequirement)...)
	// The GPU Operator routes (decision 062) exist only on the canonical surface. The static
	// releases route is registered before the /:id routes so "gpu-operator" is never read as an id.
	platforms.Get("/gpu-operator/releases", route(deps.platforms.GPUOperatorReleases)...)
	registerPlatformRoutes(platforms, deps.platforms, platformGuards{
		read: with(platformRead), operate: with(platformOperate), admin: admin, authed: authed,
		create: with(g.PlatformMembersInBody(platformSpecMembers)),
		spec:   with(platformOperate, g.PlatformMembersInBody(platformSpecMembers)),
	})

	// Managed Software: install/uninstall a single piece of host software on deployed Servers and
	// read the swallow-owned Software Assignment records (contract: software.md, decision 038).
	software := v1.Group("/software")
	software.Get("/catalog", route(deps.software.Catalog)...)
	software.Get("/assignments", route(deps.software.ListAssignments)...)
	software.Post("/assignments", route(deps.software.Install, g.ServersInBody(accessdomain.Operate, softwareInstallServers))...)
	software.Post("/uninstall", route(deps.software.Uninstall, bodyServers)...)
	// Docker CE Registry Credentials for private image pulls (contract registry-credentials.md,
	// decision 044). Scoped under the kind they serve, so kind-specific settings of later software
	// kinds get their own paths instead of sharing a software-wide namespace.
	software.Get("/docker-ce/registry-credentials", adminRoute(deps.registryCreds.List)...)
	software.Post("/docker-ce/registry-credentials", adminRoute(deps.registryCreds.Create)...)
	software.Put("/docker-ce/registry-credentials/:credentialId", adminRoute(deps.registryCreds.Replace)...)
	software.Delete("/docker-ce/registry-credentials/:credentialId", adminRoute(deps.registryCreds.Delete)...)

	// swallow-owned Zones and Pools. A Zone write is also realized in the Site's provisioner when it
	// is grouping-capable (decision 071); a Pool exists only in swallow (decision 082). A Pool's
	// members and Sudo Policy are the access context's (contract pool-access.md).
	infrastructure := v1.Group("/infrastructure")
	infrastructure.Get("/zones", route(deps.infrastructure.ListZones)...)
	infrastructure.Post("/zones", adminRoute(deps.infrastructure.CreateZone)...)
	infrastructure.Get("/zones/:id", route(deps.infrastructure.GetZone)...)
	infrastructure.Patch("/zones/:id", adminRoute(deps.infrastructure.UpdateZone)...)
	infrastructure.Delete("/zones/:id", adminRoute(deps.infrastructure.DeleteZone)...)
	infrastructure.Get("/pools", route(deps.infrastructure.ListPools)...)
	infrastructure.Post("/pools", adminRoute(deps.infrastructure.CreatePool)...)
	infrastructure.Get("/pools/:id", route(deps.infrastructure.GetPool)...)
	infrastructure.Patch("/pools/:id", adminRoute(deps.infrastructure.UpdatePool)...)
	infrastructure.Delete("/pools/:id", adminRoute(deps.infrastructure.DeletePool)...)
	infrastructure.Get("/pools/:id/members", route(deps.access.handler.ListPoolMembers)...)
	infrastructure.Post("/pools/:id/members", route(deps.access.handler.AddPoolMember)...)
	infrastructure.Patch("/pools/:id/members/:userId", route(deps.access.handler.UpdatePoolMember)...)
	infrastructure.Delete("/pools/:id/members/:userId", route(deps.access.handler.RemovePoolMember)...)
	infrastructure.Get("/pools/:id/access-policy", route(deps.access.handler.GetPoolAccessPolicy)...)
	infrastructure.Put("/pools/:id/access-policy", route(deps.access.handler.SetPoolAccessPolicy)...)

	// Change Plans (contract change-plans.md, decision 076): the approval step of a destructive
	// change, and the policy that decides whether a change approved with its write is approved at
	// once; the resource's controller applies an approved plan. Each plan route checks the plan's
	// resource itself.
	deps.changePlans.RegisterRoutes(guardedRouter{Router: v1.Group("/change-plans"), handlers: authed})
	deps.changePlans.RegisterPolicyRoutes(guardedRouter{Router: v1.Group("/change-plan-policy"), handlers: admin})

	// API Keys (contract api-keys.md, decision 042): the caller's keys for non-interactive
	// clients. Creating one needs a password Session, so a leaked key cannot mint replacements.
	apiKeys := v1.Group("/api-keys")
	apiKeys.Get("/", route(deps.apiKeys.List)...)
	apiKeys.Post("/", route(deps.apiKeys.Create, middleware.SessionOnly())...)
	apiKeys.Delete("/:keyId", route(deps.apiKeys.Delete)...)

	// SSH Keys (contract ssh-keys.md, decision 039): the Deployment Key and the caller's Access
	// Keys. Static deployment paths are registered before "/:keyId" so they are never read as ids.
	sshKeys := v1.Group("/ssh-keys")
	sshKeys.Get("/", route(deps.sshKeys.List)...)
	sshKeys.Post("/", route(deps.sshKeys.Import)...)
	sshKeys.Post("/generate", route(deps.sshKeys.Generate)...)
	sshKeys.Post("/sync", adminRoute(deps.sshKeys.Sync)...)
	sshKeys.Put("/deployment", adminRoute(deps.sshKeys.ReplaceDeployment)...)
	sshKeys.Post("/deployment/regenerate", adminRoute(deps.sshKeys.RegenerateDeployment)...)
	sshKeys.Get("/:keyId", route(deps.sshKeys.Get)...)
	sshKeys.Delete("/:keyId", route(deps.sshKeys.Delete)...)

	// Alerts and metrics are read straight from the monitoring stack: swallow stores
	// neither, and acknowledging an alert creates a silence in Alertmanager.
	monitoring := v1.Group("/monitoring")
	monitoring.Get("/alerts", adminRoute(deps.monitoring.ListAlerts)...)
	monitoring.Post("/alerts/:fingerprint/acknowledge", adminRoute(deps.monitoring.Acknowledge)...)
	metricScope := g.Custom(metricScopeCheck)
	monitoring.Get("/metrics", route(deps.monitoring.ServerMetrics, metricScope)...)
	monitoring.Get("/metrics/range", route(deps.monitoring.MetricRange, metricScope)...)
	monitoring.Get("/metrics/names", route(deps.monitoring.MetricNames)...)

	// Validation (contract validation.md, decision 081): suites, runs executed as Workflows, and
	// schedules.
	validation := v1.Group("/validation")
	validation.Get("/suites", route(deps.validation.ListSuites)...)
	validation.Post("/suites", adminRoute(deps.validation.CreateSuite)...)
	validation.Get("/suites/:suiteId", route(deps.validation.GetSuite)...)
	validation.Put("/suites/:suiteId", adminRoute(deps.validation.ReplaceSuite)...)
	validation.Delete("/suites/:suiteId", adminRoute(deps.validation.DeleteSuite)...)
	validation.Get("/runs", route(deps.validation.ListRuns)...)
	validation.Post("/runs", route(deps.validation.StartRun, g.Custom(validationStartCheck))...)
	validation.Get("/runs/:runId", route(deps.validation.GetRun, g.Custom(deps.validationRuns.check(accessdomain.Read)))...)
	validation.Post("/runs/:runId/cancel", route(deps.validation.CancelRun, g.Custom(deps.validationRuns.check(accessdomain.Operate)))...)
	validation.Get("/schedules", adminRoute(deps.validation.ListSchedules)...)
	validation.Post("/schedules", adminRoute(deps.validation.CreateSchedule)...)
	validation.Get("/schedules/:scheduleId", adminRoute(deps.validation.GetSchedule)...)
	validation.Put("/schedules/:scheduleId", adminRoute(deps.validation.ReplaceSchedule)...)
	validation.Delete("/schedules/:scheduleId", adminRoute(deps.validation.DeleteSchedule)...)

	// Canonical Workflow surface (decision 072). A Workflow is a DAG of Tasks, and belongs to its
	// targets.
	workflows := v1.Group("/workflows")
	workflows.Post("/", adminRoute(deps.operations.Create)...)
	workflows.Get("/", route(deps.operations.List)...)
	workflows.Get("/:id", route(deps.operations.Get, workflowRead)...)
	workflows.Get("/:id/timeline", route(deps.operations.Timeline, workflowRead)...)
	workflows.Post("/:id/cancel", route(deps.operations.Cancel, workflowOperate)...)
	workflows.Post("/:id/rerun", route(deps.operations.Rerun, workflowOperate)...)
	workflows.Post("/:id/tasks/:taskId/retry", route(deps.operations.RetryStep, workflowOperate)...)
	workflows.Get("/:id/tasks/:taskId/logs", route(deps.operations.StepLogs, workflowRead)...)
	workflows.Get("/:id/tasks/:taskId/stderr", route(deps.operations.StepStderr, workflowRead)...)
	workflows.Get("/:id/tasks/:taskId/events", route(deps.operations.StepEvents, workflowRead)...)
	workflows.Get("/:id/tasks/:taskId/artifacts", route(deps.operations.StepArtifacts, workflowRead)...)

	// Machine-to-machine discovery remains pull-based for Prometheus and external
	// Ansible diagnostics. The embedded runner calls the same use case directly.
	machine := middleware.MachineAuth(deps.jwtSvc, deps.machineToken)
	app.Get("/metrics", machine, metricsHandler)

	// A Platform's Grafana authenticates with its own monitoring token, not a Session, so the
	// gateway sits outside the admin-only monitoring group (platform-metrics-gateway.md).
	gateway := v1.Group("/platform-metrics/:id/prometheus")
	for _, route := range monitoringdelivery.GatewayRoutes {
		gateway.Add(route.Method, route.Path, deps.gateway.Forward(route))
	}

	discovery := v1.Group("/discovery", machine)
	discovery.Get("/prometheus", deps.discovery.PrometheusTargets)
	discovery.Get("/prometheus/exporters", deps.discovery.ExporterTargets)
	discovery.Get("/ansible", deps.discovery.AnsibleInventory)
}
