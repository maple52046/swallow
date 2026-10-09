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
	apikeyapp "github.com/maple52046/swallow/internal/apikey/application"
	apikeydelivery "github.com/maple52046/swallow/internal/apikey/delivery"
	apikeyinfra "github.com/maple52046/swallow/internal/apikey/infra"
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
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	provisioninginfra "github.com/maple52046/swallow/internal/provisioning/infra"
	serverapp "github.com/maple52046/swallow/internal/server/application"
	serverdelivery "github.com/maple52046/swallow/internal/server/delivery"
	serverinfra "github.com/maple52046/swallow/internal/server/infra"
	"github.com/maple52046/swallow/internal/server/infra/libvirt"
	"github.com/maple52046/swallow/internal/server/infra/redfish"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/shared/secret"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
	siteapp "github.com/maple52046/swallow/internal/site/application"
	sitedelivery "github.com/maple52046/swallow/internal/site/delivery"
	siteinfra "github.com/maple52046/swallow/internal/site/infra"
	softwareapp "github.com/maple52046/swallow/internal/software/application"
	softwaredelivery "github.com/maple52046/swallow/internal/software/delivery"
	softwareinfra "github.com/maple52046/swallow/internal/software/infra"
	"github.com/maple52046/swallow/internal/software/infra/dockerengine"
	sshkeydelivery "github.com/maple52046/swallow/internal/sshkey/delivery"
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
	// Swallow-owned OS Image verification attestations: which deploy targets (disk/ram) a real
	// verification deploy has proven for an image. Owned data keyed by the provider image identity,
	// kept separate from the display overlay so pruning one never erases the other.
	osImageVerificationRepo, err := provisioninginfra.NewMongoOSImageVerificationRepo(db)
	if err != nil {
		return fmt.Errorf("os image verification repo init: %w", err)
	}
	// Swallow-owned Server tag overlays: the fallback half of the capability-first tag rule
	// (docs/decisions/031), merged into Observed.Tags by reconcile only when a provisioner cannot
	// own tags. Owned data, so no sealing or staleness applies; inert while MAAS is tagging-capable.
	serverTagOverlayRepo, err := provisioninginfra.NewMongoServerTagOverlayRepo(db)
	if err != nil {
		return fmt.Errorf("server tag overlay repo init: %w", err)
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

	if cfg.JWTExpiryHours > 0 {
		slog.Warn("api.jwtExpiryHours is deprecated and ignored; access tokens now live accessTokenTTL",
			"jwtExpiryHours", cfg.JWTExpiryHours, "accessTokenTTL", cfg.AccessTokenTTL.String())
	}
	jwtSvc := jwt.NewService(cfg.JWTSecret, cfg.AccessTokenTTL)
	sessionRepo, err := authinfra.NewMongoSessionRepo(db)
	if err != nil {
		return fmt.Errorf("session repo init: %w", err)
	}
	authHandler := authdelivery.NewAuthHandler(
		authapp.NewSessionService(userRepo, sessionRepo, jwtSvc, authapp.SessionConfig{
			RefreshTokenTTL: cfg.RefreshTokenTTL,
			MaxAge:          cfg.SessionMaxAge,
		}),
		authapp.NewMeUseCase(userRepo),
	)
	apiKeyRepo, err := apikeyinfra.NewMongoKeyRepo(db)
	if err != nil {
		return fmt.Errorf("api key repo init: %w", err)
	}
	apiKeyService := apikeyapp.NewService(apiKeyRepo, apiKeyOwners{users: userRepo})
	apiKeyHandler := apikeydelivery.NewHandler(apiKeyService)
	authenticator := middleware.NewAuthenticator(jwtSvc, apiKeyService)

	integrationService := siteapp.NewIntegrationService(integrationRepo, siteRepo, serverRepo, templateRepo)
	siteHandler := sitedelivery.NewSiteHandler(
		siteapp.NewSiteService(siteRepo, integrationRepo),
		integrationService,
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
	// SSH Keys (decision 039). The Deployment Key is created by the installation step
	// (`swallow-api deployment-key ensure`), never here: API startup neither creates nor requires
	// it, and only OS and Platform deployment check that it exists. Every key is realized in
	// key-capable provisioners by runSSHKeySync below, and a provisioner Integration write requests
	// a sync so a new MAAS gets the keys promptly.
	sshKeyService, err := newSSHKeyService(db, sealer, integrationRepo, providerFactory)
	if err != nil {
		return err
	}
	integrationService.AttachProvisionerChangeListener(sshKeySyncOnProvisionerChange{keys: sshKeyService})
	sshKeyHandler := sshkeydelivery.NewSSHKeyHandler(sshKeyService)
	deploymentKeys := deploymentKeySource{keys: sshKeyService}
	activeWork := activeServerWorkReader{operations: operationRepo, orchestrations: orchestrationRepo, tasks: taskRepo}
	integrationReader := provisioninginfra.NewIntegrationReader(integrationRepo, siteRepo)
	templateService := provisioningapp.NewDeploymentTemplateService(
		templateRepo, integrationReader, providerFactory)
	networkService := provisioningapp.NewNetworkConfigurationService(serverRepo, providerFactory)
	taskService := provisioningapp.NewProvisioningTaskService(taskRepo, serverRepo, serverProtection)
	deploymentsUC := provisioningapp.NewDeployServersUseCase(serverRepo, templateRepo, providerFactory, osImageVerificationRepo)
	deploymentsUC.AttachDeploymentKeys(sshKeyService)
	deployServerUC := provisioningapp.NewDeployServerUseCase(serverRepo, providerFactory)
	deployServerUC.AttachDeploymentKeys(sshKeyService)
	taskWorker := provisioningapp.NewProvisioningTaskWorker(
		taskRepo, serverRepo, providerFactory, 5*time.Second, 30*time.Second)
	reconcileUC := provisioningapp.NewReconcileUseCase(integrationRepo, serverRepo, providerFactory, osImageOverlayRepo, serverTagOverlayRepo)
	inventorySweepUC := provisioningapp.NewInventorySweepUseCase(integrationRepo, serverRepo, providerFactory)
	// One RefreshServer use case is shared by the HTTP handler and the durable launcher so the
	// Release/Recover acceptance gate can live-sync a target's provisioning state before deciding.
	refreshServerUC := provisioningapp.NewRefreshServerUseCase(serverRepo, providerFactory, osImageOverlayRepo)
	provisioningHandler := provisioningdelivery.NewProvisioningHandler(
		deployServerUC,
		deploymentsUC,
		provisioningapp.NewDeploymentTargetPreflightService(serverRepo, providerFactory),
		templateService,
		networkService,
		taskService,
		provisioningapp.NewReleaseServerUseCase(serverRepo, providerFactory, taskRepo),
		refreshServerUC,
		provisioningapp.NewListOSImagesUseCase(providerFactory, osImageOverlayRepo, osImageVerificationRepo),
		reconcileUC,
		provisioningapp.NewGetProvisionerDetailUseCase(serverRepo, providerFactory),
		provisioningapp.NewGetProviderEventsUseCase(serverRepo, providerFactory),
		provisioningapp.NewMachineActionsUseCase(serverRepo, providerFactory, activeWork),
		provisioningapp.NewDeleteServerUseCase(serverRepo, providerFactory),
		provisioningapp.NewDeleteOSImageUseCase(providerFactory, osImageOverlayRepo, osImageVerificationRepo),
		provisioningapp.NewUploadOSImageUseCase(providerFactory, osImageOverlayRepo),
		provisioningapp.NewSetOSImageOverlayUseCase(osImageOverlayRepo, reconcileUC),
		provisioningapp.NewListServerTagsUseCase(integrationRepo, providerFactory, serverTagOverlayRepo),
		provisioningapp.NewEditServerTagsUseCase(serverRepo, providerFactory, serverTagOverlayRepo),
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
	// Live Kubernetes cluster explorer: a read/write client factory for a deployed Kubernetes
	// Platform's own API. Like the Slurm read it never writes Swallow state; eligibility
	// (deployed Kubernetes with a recorded credential) is enforced by the use case below.
	kubernetesClientFactory := platforminfra.NewKubernetesClientFactory(integrationRepo)

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
	// Automation readers get the effective credential (the Deployment Key, decision 041) and the
	// derived credential source; the configuration service reads the plain repository and derives
	// the same source itself, because it reports what is stored and never needs key material.
	effectiveAutomation := operationapp.NewEffectiveAutomationConfigurations(automationRepo, deploymentKeys)
	// Server Default User (decision 045): setting one logs in to the host directly — once with an
	// optional one-time password to install the Deployment Key, then with the key to verify — on the
	// Site's SSH port, behind the live Server Lock guard.
	hostAccess := serverinfra.NewSSHHostAccess(deploymentKeys, siteSSHPorts{configurations: automationRepo})
	defaultUserHandler := serverdelivery.NewDefaultUserHandler(serverapp.NewDefaultUserUseCase(
		serverRepo, serverProtection, hostAccess,
	))
	// Hypervisors (decision 055): swallow reads a deployed Server's libvirt domains and gives its
	// virtual machines libvirt Boot Media over the same Deployment Key logins.
	libvirtHosts := libvirt.NewHost(hostAccess)
	virtualMachineHandler := serverdelivery.NewVirtualMachineHandler(serverapp.NewVirtualMachineUseCase(serverRepo, libvirtHosts))
	// Boot Media (decisions 047 and 049): this process builds and serves Boot ISOs, probes each
	// Server's BMC for Redfish capability, and applies a Server's chosen Boot ISO through Redfish
	// with the BMC connection read live from the provisioner. The setting and capability are
	// written straight to the Mongo repository (the event broker only carries projection changes).
	bootISORepo, err := provisioninginfra.NewMongoBootISORepo(db)
	if err != nil {
		return fmt.Errorf("boot ISO repo init: %w", err)
	}
	bootISOFiles := provisioninginfra.NewGenfsimgBuilder(cfg.BootMedia.Dir, cfg.BootMedia.IPXEDir, cfg.BootMedia.BaseURL)
	bootISOs := newBootISOCatalog(bootISORepo, bootISOFiles, cfg.BootMedia.BaseURL)
	bootISOHandler := provisioningdelivery.NewBootISOHandler(provisioningapp.NewBootISOService(
		bootISORepo, integrationReader, bootISOFiles, bootISOUsage{servers: mongoServerRepo}))
	bootMediaUC := serverapp.NewBootMediaUseCase(serverRepo, mongoServerRepo, serverProtection,
		newBootMediaEndpointSource(providerFactory, serverRepo), redfish.NewController(), libvirtHosts, bootISOs)
	bootMediaHandler := serverdelivery.NewBootMediaHandler(bootMediaUC)
	bootMediaPlan := bootMediaPlanner{servers: serverRepo, isos: bootISOs}
	runner := operationinfra.NewLocalRunner(
		cfg.AnsibleRunnerCommand, catalog.ProjectRoot(), cfg.JobRuntimeDir, cfg.JobArtifactDir)
	// Per-host SSH-user resolution: images use different default login users, so the runner probes
	// candidate users per host with the automation key instead of forcing one Site sshUser.
	runner.AttachUserProber(sshprobe.DefaultProber{})
	// Live Ansible task events, streamed by the executor and read here to serve a durable Step's
	// task list during a run rather than only after it finishes (decision: live progress).
	ansibleEventRepo, err := operationinfra.NewMongoAnsibleEventRepo(db)
	if err != nil {
		return fmt.Errorf("ansible event repo init: %w", err)
	}
	operationService := operationapp.NewExecutionService(
		operationRepo, serverRepo, effectiveAutomation, catalog, runner, ansibleEventRepo,
		platformapp.NewPolicyChecker(platformRepo, serverRepo),
		serverProtection,
	)
	automationService := operationapp.NewAutomationConfigurationService(
		automationRepo, siteRepo, catalog,
	)
	automationService.AttachDeploymentKeys(deploymentKeys)
	orchestrationService := operationapp.NewWorkflowService(
		orchestrationRepo, temporalworkflow.NewController(temporalClient), operationSecretRepo,
	)
	orchestrationService.AttachLeaseReader(operationinfra.NewMongoResourceLeaseRepo(db))
	operationService.AttachWorkflow(orchestrationService)
	// Deleting a platform cancels its in-flight durable operations so their leases are
	// released and the member servers are freed rather than left blocked by orphaned work.
	platformService.AttachOperationCanceler(platformOperationCanceler{orchestrations: orchestrationService})
	provisioningHandler.AttachDurableOperations(durableProvisioningLauncher{
		deployments: deploymentsUC, operations: orchestrationService, servers: serverRepo,
		protection: serverProtection, refresh: refreshServerUC, bootMedia: bootMediaPlan,
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
	// Virtual-machine enrollment (decision 055): libvirt domains of a Hypervisor are registered with
	// the provisioner by an enroll-virtual-machines Workflow run by the worker.
	provisioningHandler.AttachVirtualMachineEnrollment(virtualMachineEnrollmentLauncher{
		workflows: orchestrationService, integrations: integrationRepo, providers: providerFactory,
		servers: serverRepo, protection: serverProtection, isos: bootISOs, keys: sshKeyService,
	})
	autoInspect := provisioningapp.NewAutoInspectUseCase(integrationRepo, serverRepo, inspectionLauncher)
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
		bootMedia: bootMediaPlan,
	}
	deployService := platformapp.NewDeployService(
		platformService, platformRepo, serverRepo, lifecycleReader, platformLauncher,
		serverProtection,
	)
	deployService.AttachMachinePreparationValidator(platformMachinePreparationValidator{deployments: deploymentsUC})
	deployService.AttachDeploymentRequirementReader(requirementRepo)
	deployService.AttachDeploymentKeyChecker(sshKeyService)
	uninstallService := platformapp.NewUninstallService(
		platformRepo, serverRepo, lifecycleReader, platformLauncher,
		serverProtection,
	)
	kubernetesExplorer := platformapp.NewKubernetesExplorerUseCase(
		platformRepo, lifecycleReader, kubernetesClientFactory, serverRepo)
	platformHandler := platformdelivery.NewPlatformHandler(
		platformService, membershipSync, deployService, uninstallService, slurmClusterRead,
		kubernetesExplorer, requirementService)

	// Software deployment (Managed Software, decision 038): install/uninstall a single piece of
	// host software on deployed Servers, tracked by a swallow-owned Software Assignment. It reuses
	// the operation orchestration (PrepareAnsibleStep + WorkflowService) through a launcher, so it
	// shares the platform deployment engine rather than owning a second one.
	softwareAssignmentRepo, err := softwareinfra.NewMongoAssignmentRepo(db)
	if err != nil {
		return fmt.Errorf("software assignment repo init: %w", err)
	}
	softwareService := softwareapp.NewSoftwareService(
		softwareAssignmentRepo, serverRepo,
		kubernetesMembershipChecker{servers: serverRepo, platforms: platformRepo},
	)
	softwareService.AttachLauncher(softwareDeploymentLauncher{
		operations: operationService, orchestrations: orchestrationService,
	})
	softwareHandler := softwaredelivery.NewSoftwareHandler(softwareService)
	softwareSweeper := softwareapp.NewSoftwareAssignmentSweeper(softwareAssignmentRepo, serverRepo)
	// Docker Host Explorer (decision 043): live management of the Docker Engine on a Server where
	// swallow installed Docker CE with enableApi. Eligibility reads the same assignment repository;
	// writes go through the provider-backed lock guard; Docker objects are never persisted.
	dockerExplorer := softwareapp.NewDockerExplorerUseCase(
		softwareAssignmentRepo, serverRepo, dockerengine.NewFactory(), serverProtection,
	)
	// Registry Credentials (decision 044): installation-wide, sealed with the credential key, and
	// attached to an explorer pull whose image reference resolves to their registry.
	registryCredentialRepo, err := softwareinfra.NewMongoRegistryCredentialRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("registry credential repo init: %w", err)
	}
	dockerExplorer.AttachRegistryCredentials(registryCredentialRepo)
	dockerHandler := softwaredelivery.NewDockerHandler(dockerExplorer)
	registryCredentialHandler := softwaredelivery.NewRegistryCredentialHandler(
		softwareapp.NewRegistryCredentialService(registryCredentialRepo))

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
		platforms:       platformHandler,
		software:        softwareHandler,
		docker:          dockerHandler,
		registryCreds:   registryCredentialHandler,
		infrastructure:  infrastructureHandler,
		discovery:       discoveryHandler,
		sshKeys:         sshKeyHandler,
		apiKeys:         apiKeyHandler,
		releaseVersion:  releaseVersion,
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
	go runAutoInspect(ctx, autoInspect, cfg.ReconcileInterval)
	go runSoftwareAssignmentSweep(ctx, softwareSweeper, cfg.ReconcileInterval)
	go runSSHKeySync(ctx, sshKeyService, cfg.SSHKeySyncInterval)
	go runRedfishCapabilitySweep(ctx, bootMediaUC, cfg.RedfishProbeInterval, cfg.RedfishProbeMaxAge)
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
	// bootMediaISO serves Boot ISO files without authentication (decisions 047 and 049).
	bootMediaISO fiber.Handler
	// cliBinary serves the swallow CLI without authentication for Server Enrollment (decision 053).
	cliBinary fiber.Handler
	// bootISOs builds, lists, and deletes Boot ISOs (decision 049).
	bootISOs       *provisioningdelivery.BootISOHandler
	serverStream   *serverdelivery.ServerStreamHandler
	provisioning   *provisioningdelivery.ProvisioningHandler
	operations     *operationdelivery.ExecutionHandler
	monitoring     *monitoringdelivery.MonitoringHandler
	platforms      *platformdelivery.PlatformHandler
	software       *softwaredelivery.SoftwareHandler
	docker         *softwaredelivery.DockerHandler
	registryCreds  *softwaredelivery.RegistryCredentialHandler
	infrastructure *infrastructuredelivery.InfrastructureHandler
	discovery      *discoverydelivery.DiscoveryHandler
	sshKeys        *sshkeydelivery.SSHKeyHandler
	apiKeys        *apikeydelivery.Handler
	readiness      func(context.Context) error
	releaseVersion string
}

// registerPlatformRoutes keeps the canonical and one-release compatibility routes on
// the same handlers, so deprecated URLs cannot drift from Platform behavior.
func registerPlatformRoutes(routes fiber.Router, handler *platformdelivery.PlatformHandler) {
	// There is no POST "/": a Platform is created only by a deployment (decision 032);
	// registering an existing one was removed.
	routes.Get("/", handler.List)
	routes.Post("/deploy", handler.Deploy)
	routes.Post("/sync", handler.SyncAllMembership)
	routes.Get("/:id", handler.Get)
	routes.Patch("/:id", handler.Update)
	routes.Delete("/:id", handler.Delete)
	routes.Post("/:id/uninstall", handler.Uninstall)
	routes.Post("/:id/sync", handler.SyncMembership)
	routes.Get("/:id/slurm", handler.GetSlurmCluster)
	registerKubernetesExplorerRoutes(routes, handler)
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

// registerDockerExplorerRoutes mounts the Docker Host Explorer under a Server (contract
// servers-docker.md, decision 043). Mounted here because clients address a Server, but handled by
// the software feature that owns the Docker CE assignment the explorer is gated on. These routes
// call the host's Engine API on demand and persist nothing.
func registerDockerExplorerRoutes(servers fiber.Router, handler *softwaredelivery.DockerHandler) {
	servers.Get("/:id/docker", handler.Summary)

	servers.Get("/:id/docker/images", handler.ListImages)
	servers.Post("/:id/docker/images/pull", handler.PullImage)
	servers.Delete("/:id/docker/images/:imageId", handler.RemoveImage)

	servers.Get("/:id/docker/containers", handler.ListContainers)
	servers.Post("/:id/docker/containers", handler.CreateContainer)
	servers.Post("/:id/docker/containers/:containerId/start", handler.StartContainer)
	servers.Post("/:id/docker/containers/:containerId/stop", handler.StopContainer)
	servers.Post("/:id/docker/containers/:containerId/restart", handler.RestartContainer)
	servers.Get("/:id/docker/containers/:containerId/logs", handler.ContainerLogs)
	servers.Delete("/:id/docker/containers/:containerId", handler.RemoveContainer)

	servers.Get("/:id/docker/volumes", handler.ListVolumes)
	servers.Post("/:id/docker/volumes", handler.CreateVolume)
	servers.Delete("/:id/docker/volumes/:volumeName", handler.RemoveVolume)

	servers.Get("/:id/docker/networks", handler.ListNetworks)
	servers.Post("/:id/docker/networks", handler.CreateNetwork)
	servers.Delete("/:id/docker/networks/:networkId", handler.RemoveNetwork)
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

	auth := v1.Group("/auth")
	auth.Post("/login", deps.auth.Login)
	auth.Post("/refresh", deps.auth.Refresh)
	auth.Post("/logout", deps.authenticator.Optional(), deps.auth.Logout)
	auth.Get("/me", deps.authenticator.Require(), deps.auth.Me)

	admin := []fiber.Handler{deps.authenticator.Require(), middleware.AdminOnly()}

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
		deps.authenticator.Require(),
		middleware.AdminOnly(),
		deps.serverStream.Stream)

	servers := v1.Group("/servers", admin...)
	servers.Get("/", deps.servers.List)
	servers.Get("/:id", deps.servers.Get)
	servers.Post("/:id/refresh", deps.provisioning.RefreshServer)
	servers.Delete("/:id", deps.provisioning.DeleteServer)
	servers.Put("/:id/default-user", deps.defaultUsers.Set)
	servers.Delete("/:id/default-user", deps.defaultUsers.Clear)
	servers.Get("/:id/boot-media", deps.bootMedia.Get)
	servers.Put("/:id/boot-media", deps.bootMedia.Set)
	servers.Post("/:id/redfish/probe", deps.bootMedia.Probe)
	servers.Get("/:id/virtual-machines", deps.virtualMachines.List)
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
	// The provisioner-owned Power Configuration (decision 054), read live and written through.
	servers.Get("/:id/power-configuration", deps.provisioning.GetPowerConfiguration)
	servers.Put("/:id/power-configuration", deps.provisioning.SetPowerConfiguration)
	servers.Post("/:id/inspect", deps.provisioning.Inspect)
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
	registerDockerExplorerRoutes(servers, deps.docker)

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
	// Server tags: list the Site's known tags for the editor, and apply a tri-state add/remove edit
	// to one or more Servers. Under /provisioning to avoid the GET /servers/:id route collision, and
	// capability-first — MAAS-driven or swallow-owned fallback (docs/decisions/031).
	provisioning.Get("/tags", deps.provisioning.ListServerTags)
	provisioning.Post("/tags", deps.provisioning.EditServerTags)
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
	// Prove a custom OS Image works for a deploy target by deploying it on a chosen ready Server,
	// recording the swallow-owned verification, and auto-releasing the Server (docs/decisions/035).
	provisioning.Post("/image-verifications", deps.provisioning.CreateImageVerification)
	provisioning.Post("/recover-operations", deps.provisioning.CreateRecoverOperation)
	provisioning.Post("/networks/inspect", deps.provisioning.InspectNetworks)
	provisioning.Get("/tasks/:id", deps.provisioning.GetProvisioningTask)
	provisioning.Post("/tasks/:id/retry", deps.provisioning.RetryProvisioningTask)
	provisioning.Post("/reconcile", deps.provisioning.ReconcileAll)
	provisioning.Post("/integrations/:id/reconcile", deps.provisioning.Reconcile)
	provisioning.Post("/integrations/:id/enroll-bundle", deps.provisioning.EnrollBundle)
	provisioning.Post("/virtual-machine-enrollments", deps.provisioning.EnrollVirtualMachines)
	// Boot ISOs (decision 049): built here, mounted by a Server's Boot Media.
	if deps.bootISOs != nil {
		provisioning.Get("/boot-isos", deps.bootISOs.List)
		provisioning.Post("/boot-isos", deps.bootISOs.Create)
		provisioning.Get("/boot-isos/:id", deps.bootISOs.Get)
		provisioning.Delete("/boot-isos/:id", deps.bootISOs.Delete)
	}

	// Swallow owns Platform registration and policy; membership is observed from the
	// runtime API. The former Cluster route is a delivery-only compatibility alias.
	platforms := v1.Group("/platforms", admin...)
	platforms.Get("/deployment-requirements/slurm", deps.platforms.GetSlurmDeploymentRequirement)
	platforms.Put("/deployment-requirements/slurm", deps.platforms.PutSlurmDeploymentRequirement)
	registerPlatformRoutes(platforms, deps.platforms)
	legacyPlatforms := v1.Group("/clusters", admin...)
	legacyPlatforms.Use(markDeprecatedPlatformRoute)
	registerPlatformRoutes(legacyPlatforms, deps.platforms)

	// Managed Software: install/uninstall a single piece of host software on deployed Servers and
	// read the swallow-owned Software Assignment records (contract: software.md, decision 038).
	software := v1.Group("/software", admin...)
	software.Get("/catalog", deps.software.Catalog)
	software.Get("/assignments", deps.software.ListAssignments)
	software.Post("/assignments", deps.software.Install)
	software.Post("/uninstall", deps.software.Uninstall)
	// Docker CE Registry Credentials for private image pulls (contract registry-credentials.md,
	// decision 044). Scoped under the kind they serve, so kind-specific settings of later software
	// kinds get their own paths instead of sharing a software-wide namespace.
	software.Get("/docker-ce/registry-credentials", deps.registryCreds.List)
	software.Post("/docker-ce/registry-credentials", deps.registryCreds.Create)
	software.Put("/docker-ce/registry-credentials/:credentialId", deps.registryCreds.Replace)
	software.Delete("/docker-ce/registry-credentials/:credentialId", deps.registryCreds.Delete)

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

	// API Keys (contract api-keys.md, decision 042): the caller's keys for non-interactive
	// clients. Creating one needs a password Session, so a leaked key cannot mint replacements.
	apiKeys := v1.Group("/api-keys", admin...)
	apiKeys.Get("/", deps.apiKeys.List)
	apiKeys.Post("/", middleware.SessionOnly(), deps.apiKeys.Create)
	apiKeys.Delete("/:keyId", deps.apiKeys.Delete)

	// SSH Keys (contract ssh-keys.md, decision 039): the Deployment Key and the caller's Access
	// Keys. Static deployment paths are registered before "/:keyId" so they are never read as ids.
	sshKeys := v1.Group("/ssh-keys", admin...)
	sshKeys.Get("/", deps.sshKeys.List)
	sshKeys.Post("/", deps.sshKeys.Import)
	sshKeys.Post("/generate", deps.sshKeys.Generate)
	sshKeys.Post("/sync", deps.sshKeys.Sync)
	sshKeys.Put("/deployment", deps.sshKeys.ReplaceDeployment)
	sshKeys.Post("/deployment/regenerate", deps.sshKeys.RegenerateDeployment)
	sshKeys.Get("/:keyId", deps.sshKeys.Get)
	sshKeys.Delete("/:keyId", deps.sshKeys.Delete)

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
