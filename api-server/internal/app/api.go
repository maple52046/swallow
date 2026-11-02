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

	"github.com/maple52046/swallow/bootstrap"
	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/adapters/bmc/redfish"
	"github.com/maple52046/swallow/internal/adapters/bootiso/genfsimg"
	"github.com/maple52046/swallow/internal/adapters/events/memory"
	"github.com/maple52046/swallow/internal/adapters/hostaccess/sshaccess"
	"github.com/maple52046/swallow/internal/adapters/hostautomation/ansible"
	"github.com/maple52046/swallow/internal/adapters/hypervisor/libvirt"
	"github.com/maple52046/swallow/internal/adapters/metrics/promstack"
	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	apikeystore "github.com/maple52046/swallow/internal/adapters/store/mongodb/apikey"
	authstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/auth"
	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	infrastructurestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/infrastructure"
	"github.com/maple52046/swallow/internal/adapters/store/mongodb/migration"
	platformstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/platform"
	provisioningstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/provisioning"
	serverstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/server"
	sitestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/site"
	softwarestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/software"
	workflowstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/workflow"
	apikeyapp "github.com/maple52046/swallow/internal/core/apikey/application"
	apikeydelivery "github.com/maple52046/swallow/internal/core/apikey/delivery"
	authapp "github.com/maple52046/swallow/internal/core/auth/application"
	authdelivery "github.com/maple52046/swallow/internal/core/auth/delivery"
	convergencedelivery "github.com/maple52046/swallow/internal/core/convergence/delivery"
	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	discoverydelivery "github.com/maple52046/swallow/internal/core/discovery/delivery"
	infrastructureapp "github.com/maple52046/swallow/internal/core/infrastructure/application"
	infrastructuredelivery "github.com/maple52046/swallow/internal/core/infrastructure/delivery"
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
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdelivery "github.com/maple52046/swallow/internal/core/workflow/delivery"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/shared/secret"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
	"github.com/maple52046/swallow/internal/shared/version"
	"github.com/maple52046/swallow/modules/platform/kubernetes/kubeapi"
	"github.com/maple52046/swallow/modules/software/docker-ce/dockerengine"
	"github.com/maple52046/swallow/modules/software/docker-ce/explorer"
	"github.com/maple52046/swallow/modules/software/docker-ce/explorerhttp"
)

// shutdownTimeout bounds how long in-flight requests get to finish on shutdown.
const shutdownTimeout = 10 * time.Second

var httpRequestCount atomic.Uint64

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
	// The one Transactor of this process: every use case that keeps a cross-document invariant
	// receives it through its constructor (decision 069).
	transactor := storemongo.NewTransactor(client)
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = client.Disconnect(disconnectCtx)
	}()

	db := client.Database(cfg.MongoDB)
	if err := migration.EnsureInitialized(connectCtx, db); err != nil {
		return fmt.Errorf("database schema: %w", err)
	}

	userRepo := authstore.NewMongoUserRepo(db)
	siteRepo, err := sitestore.NewMongoSiteRepo(db)
	if err != nil {
		return fmt.Errorf("site repo init: %w", err)
	}
	integrationRepo, err := sitestore.NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("integration repo init: %w", err)
	}
	mongoServerRepo, err := serverstore.NewMongoServerRepo(db)
	if err != nil {
		return fmt.Errorf("server repo init: %w", err)
	}
	// One in-process broker fans Server changes out to SSE subscribers. The relay started below
	// feeds it from the store's change feed, so it carries every process's Server writes.
	serverEventBroker := memory.NewServerEventBroker()
	serverRepo := mongoServerRepo
	templateRepo, err := provisioningstore.NewMongoDeploymentTemplateRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("deployment template repo init: %w", err)
	}
	// OS Image metadata (decision 071): the names, OS and release labels, tags, and default user
	// swallow keeps for provider images, merged onto the live catalog. Desired state that needs no
	// provider realization, so no controller and no sealing apply.
	osImageMetadataRepo, err := provisioningstore.NewMongoOSImageMetadataRepo(db)
	if err != nil {
		return fmt.Errorf("os image metadata repo init: %w", err)
	}
	// swallow-owned OS Image verification attestations: which deploy targets (disk/ram) a real
	// verification deploy has proven for an image. Owned data keyed by the provider image identity,
	// kept separate from the display metadata so pruning one never erases the other.
	osImageVerificationRepo, err := provisioningstore.NewMongoOSImageVerificationRepo(db)
	if err != nil {
		return fmt.Errorf("os image verification repo init: %w", err)
	}
	taskRepo, err := provisioningstore.NewMongoProvisioningTaskRepo(db)
	if err != nil {
		return fmt.Errorf("provisioning task repo init: %w", err)
	}
	orchestrationRepo, err := workflowstore.NewMongoWorkflowRepo(db)
	if err != nil {
		return fmt.Errorf("orchestration repo init: %w", err)
	}
	operationSecretRepo, err := workflowstore.NewMongoOperationSecretRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("operation secret repo init: %w", err)
	}
	ownership, err := workflowstore.NewMongoOwnershipRepo(db)
	if err != nil {
		return fmt.Errorf("resource ownership repo init: %w", err)
	}

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
	sessionRepo, err := authstore.NewMongoSessionRepo(db)
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
	apiKeyRepo, err := apikeystore.NewMongoKeyRepo(db)
	if err != nil {
		return fmt.Errorf("api key repo init: %w", err)
	}
	apiKeyService := apikeyapp.NewService(apiKeyRepo, apiKeyOwners{users: userRepo})
	apiKeyHandler := apikeydelivery.NewHandler(apiKeyService)
	authenticator := middleware.NewAuthenticator(jwtSvc, apiKeyService)

	// The health axis is resolved from the metrics store at query time and never
	// persisted. With no metrics integration registered the resolver returns nothing,
	// which reads as "not known" rather than as anything about the machine.
	monitoringFactory := promstack.NewMonitoringFactory(integrationRepo)
	alertService := monitoringapp.NewAlertService(monitoringFactory, siteExistence{sites: siteRepo})
	healthResolver := monitoringapp.NewHealthResolver(monitoringFactory)

	serverHandler := serverdelivery.NewServerHandler(
		serverapp.NewListServersUseCase(serverRepo, healthResolver),
		serverapp.NewGetServerUseCase(serverRepo, healthResolver),
	)
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
		return err
	}
	sshKeyService.AttachRequester(convergencestore.NewMongoRequester(db))
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
		return fmt.Errorf("software assignment repo init: %w", err)
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
		provisioningapp.NewUploadOSImageUseCase(providerFactory, osImageMetadataRepo),
		provisioningapp.NewSetOSImageMetadataUseCase(osImageMetadataRepo, reconcileUC),
		provisioningapp.NewListServerTagsUseCase(integrationRepo, providerFactory, serverRepo),
		provisioningapp.NewEditServerTagsUseCase(transactor, serverRepo, serverRepo, providerFactory),
	)

	// Infrastructure: swallow-owned Zones and Pools (decision 071). The API writes their spec and
	// the Servers' placement spec; the worker's catalog and Server controllers realize them. The
	// provider factory is used here only to delete a group from the provisioners that hold it.
	zoneRepo, err := infrastructurestore.NewMongoZoneRepo(db)
	if err != nil {
		return fmt.Errorf("zone repo init: %w", err)
	}
	poolRepo, err := infrastructurestore.NewMongoPoolRepo(db)
	if err != nil {
		return fmt.Errorf("pool repo init: %w", err)
	}
	infrastructureHandler := infrastructuredelivery.NewInfrastructureHandler(
		infrastructureapp.NewGroupingService(
			zoneRepo,
			poolRepo,
			NewSiteReader(siteRepo),
			serverPlacements{servers: serverRepo, specs: serverRepo},
			placementReferences{specs: serverRepo},
			providerFactory,
		),
	)

	platformRepo, err := platformstore.NewMongoPlatformRepo(db, sealer)
	if err != nil {
		return fmt.Errorf("platform repo init: %w", err)
	}
	platformNames := platformNameReader{platforms: platformRepo}
	deleteServerUC.AttachPlatformNames(platformNames)

	// Monitoring reads (alerts, current values, usage history) need the grouping repositories
	// above: a usage-history scope names a Zone, Pool, or Platform, and metrics carry only
	// server_id, so the scope is resolved to Servers here at query time.
	monitoringHandler := monitoringdelivery.NewMonitoringHandler(
		alertService,
		monitoringapp.NewServerMetricsService(monitoringFactory),
		monitoringapp.NewMetricRangeService(monitoringFactory, metricScopeResolver{
			zones: zoneRepo, pools: poolRepo, platforms: platformRepo, servers: serverRepo,
		}),
	)
	requirementRepo := platformstore.NewMongoDeploymentRequirementRepo(db)
	requirementService := platformapp.NewDeploymentRequirementService(requirementRepo)
	platformReaderFactory, err := platformapp.NewReaderFactory(integrationRepo, modules.platformReaders...)
	if err != nil {
		return err
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
		return fmt.Errorf("playbook catalog: %w", err)
	}
	lifecycleReader := platformLifecycleReader{orchestrations: orchestrationRepo}
	orchestrationService := workflowapp.NewWorkflowService(transactor, orchestrationRepo,
		workflowPlanCatalog(modules.workflowKinds, modules.software), ownership, operationSecretRepo)
	// Deleting a platform cancels its in-flight durable operations so their leases are
	// released and the member servers are freed rather than left blocked by orphaned work.
	platformService, err := newPlatformService(transactor, platformRepo, siteRepo, serverRepo, orchestrationRepo,
		integrationRepo, softwareAssignmentRepo, platformOperationCanceler{orchestrations: orchestrationService})
	if err != nil {
		return err
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
		return fmt.Errorf("boot ISO repo init: %w", err)
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
		return fmt.Errorf("ansible event repo init: %w", err)
	}
	operationService := workflowapp.NewExecutionService(
		serverRepo, effectiveAutomation, catalog, modules.workflowKinds, runner, ansibleEventRepo,
		platformapp.NewPolicyChecker(platformRepo, serverRepo),
		serverProtection, orchestrationService,
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
		exporterOwnerResolver{platforms: platformRepo},
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
		return fmt.Errorf("subnet DNS repo init: %w", err)
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

	overviewReader := NewOverviewReader(
		siteRepo, integrationRepo, serverRepo, healthResolver, platformRepo, orchestrationRepo, alertService,
	)
	overviewHandler := overviewdelivery.NewHandler(
		overviewapp.NewService(overviewReader, overviewapp.SystemClock{}))

	// Platform lifecycle adapters keep durable operations outside the platform context.
	platformOps, err := buildPlatformOperations(platformOperationDeps{
		transactor: transactor, modules: modules, platformService: platformService, platforms: platformRepo,
		servers: serverRepo, claims: serverRepo, lifecycle: lifecycleReader,
		operations: operationService, orchestrations: orchestrationService, deployments: deploymentsUC,
		bootMedia: bootMediaPlan, software: softwareService, ntp: ntpPlan, nodeExporter: nodeExporterPlan,
		protection: serverProtection,
		networks:   networkService, requirements: requirementRepo, keys: sshKeyService,
	})
	if err != nil {
		return err
	}
	deployService, gpuOperatorService := platformOps.deploy, platformOps.gpuOperators
	changePlans, err := newChangePlanService(ctx, db, transactor, cfg.ChangePlanTTL, platformOps.planner)
	if err != nil {
		return err
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
		return fmt.Errorf("registry credential repo init: %w", err)
	}
	dockerExplorer.AttachRegistryCredentials(registryCredentialRepo)
	dockerHandler := explorerhttp.NewDockerHandler(dockerExplorer)
	registryCredentialHandler := softwaredelivery.NewRegistryCredentialHandler(
		softwareapp.NewRegistryCredentialService(registryCredentialRepo))

	discoveryUseCase := discoveryapp.NewDiscoveryUseCase(serverRepo)
	discoveryHandler := discoverydelivery.NewDiscoveryHandler(discoveryUseCase)
	fiberApp := fiber.New(fiber.Config{
		// OS image upload (POST /provisioning/images) streams a potentially multi-gigabyte
		// artifact, so the body is read as a stream and the limit is raised well past the 4 MiB
		// default. StreamRequestBody keeps the large body off the heap; fasthttp spools the
		// multipart file part to a temp file the handler then streams to the provider.
		StreamRequestBody: true,
		BodyLimit:         int(cfg.ImageUploadMaxBytes),
		ErrorHandler:      httpErrorHandler,
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
		changePlans:     convergencedelivery.NewChangePlanHandler(changePlans),
		moduleList:      moduledelivery.NewModuleHandler(modules.registry),
		releaseVersion:  releaseVersion,
		readiness: func(ctx context.Context) error {
			if err := client.Ping(ctx, nil); err != nil {
				return err
			}
			return migration.Check(ctx, db)
		},
	})

	// Each API process relays the Server changes of every process to its own stream subscribers.
	go serverapp.NewServerEventRelay(storemongo.NewChangeFeed(db), mongoServerRepo, serverEventBroker).Run(ctx)

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
	moduleList     *moduledelivery.ModuleHandler
	readiness      func(context.Context) error
	releaseVersion string
}

// registerPlatformRoutes registers the Platform routes on routes.
func registerPlatformRoutes(routes fiber.Router, handler *platformdelivery.PlatformHandler) {
	// A Platform is created from its spec, and its install waits on a Change Plan (decision 076);
	// registering an existing one was removed (decision 032).
	routes.Get("/", handler.List)
	routes.Post("/", handler.Create)
	routes.Post("/sync", handler.SyncAllMembership)
	routes.Get("/:id", handler.Get)
	routes.Patch("/:id", handler.Update)
	routes.Put("/:id/spec", handler.PutSpec)
	routes.Delete("/:id", handler.Delete)
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

	moduleList := v1.Group("/modules", admin...)
	moduleList.Get("/", deps.moduleList.List)

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
	// provisioner (decision 071). Mounted here because clients address a Server, but handled by
	// the infrastructure feature that owns the Zone/Pool catalog.
	servers.Put("/:id/placement", deps.infrastructure.AssignServerPlacement)
	explorerhttp.RegisterRoutes(servers, deps.docker)

	provisioning := v1.Group("/provisioning", admin...)
	provisioning.Get("/images", deps.provisioning.ListImages)
	// Multipart upload of a provider-owned OS image. Streamed and spooled by the handler; the
	// provider decides whether it becomes a custom image (docs/decisions/027).
	provisioning.Post("/images", deps.provisioning.UploadImage)
	provisioning.Delete("/images", deps.provisioning.DeleteImage)
	// OS Image metadata: replace or clear the swallow-owned name, OS, release, tags, and default
	// user of one image. Same query-parameter identity as DELETE /images (decision 071).
	provisioning.Put("/images/metadata", deps.provisioning.SetImageMetadata)
	provisioning.Delete("/images/metadata", deps.provisioning.ClearImageMetadata)
	// Server tags: list the Site's known tags for the editor, and apply a tri-state add/remove edit
	// to one or more Servers' spec (decision 071). Under /provisioning to avoid the GET /servers/:id
	// route collision.
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
	// The provisioner-owned proxy handed to deployed machines, read live and written through; not an
	// Integration setting (contract provisioner-http-proxy.md, decision 071).
	provisioning.Get("/integrations/:id/http-proxy", deps.provisioning.GetHTTPProxy)
	provisioning.Put("/integrations/:id/http-proxy", deps.provisioning.SetHTTPProxy)
	// The provisioner's subnets and their DNS servers, read live and written through (contract
	// provisioner-subnet-dns.md, decision 058).
	provisioning.Get("/integrations/:id/subnets", deps.provisioning.ListSubnets)
	provisioning.Put("/integrations/:id/subnets/:subnetId/dns-servers", deps.provisioning.SetSubnetDNSServers)
	provisioning.Post("/virtual-machine-enrollments", deps.provisioning.EnrollVirtualMachines)
	// Boot ISOs (decision 047): built here, mounted by a Server's Boot Media.
	if deps.bootISOs != nil {
		provisioning.Get("/boot-isos", deps.bootISOs.List)
		provisioning.Post("/boot-isos", deps.bootISOs.Create)
		provisioning.Get("/boot-isos/:id", deps.bootISOs.Get)
		provisioning.Post("/boot-isos/:id/rebuild", deps.bootISOs.Rebuild)
		provisioning.Delete("/boot-isos/:id", deps.bootISOs.Delete)
	}

	// swallow owns Platform registration and policy; membership is observed from the
	// runtime API.
	platforms := v1.Group("/platforms", admin...)
	platforms.Get("/deployment-requirements/slurm", deps.platforms.GetSlurmDeploymentRequirement)
	platforms.Put("/deployment-requirements/slurm", deps.platforms.PutSlurmDeploymentRequirement)
	// The GPU Operator routes (decision 062) exist only on the canonical surface. The static
	// releases route is registered before the /:id routes so "gpu-operator" is never read as an id.
	platforms.Get("/gpu-operator/releases", deps.platforms.GPUOperatorReleases)
	registerPlatformRoutes(platforms, deps.platforms)

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

	// swallow-owned Zones and Pools (the dashboard's Infrastructure area). Each write is also
	// realized in the Site's provisioner when it is grouping-capable (decision 071).
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
	// Change Plans (contract change-plans.md, decision 076): the approval step of a destructive
	// change; the resource's controller applies an approved plan.
	deps.changePlans.RegisterRoutes(v1.Group("/change-plans", admin...))

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
	monitoring.Get("/metrics/range", deps.monitoring.MetricRange)
	monitoring.Get("/metrics/names", deps.monitoring.MetricNames)

	// Canonical Workflow surface (decision 072). A Workflow is a DAG of Tasks.
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

	// Machine-to-machine discovery remains pull-based for Prometheus and external
	// Ansible diagnostics. The embedded runner calls the same use case directly.
	machine := middleware.MachineAuth(deps.jwtSvc, deps.machineToken)
	app.Get("/metrics", machine, metricsHandler)

	discovery := v1.Group("/discovery", machine)
	discovery.Get("/prometheus", deps.discovery.PrometheusTargets)
	discovery.Get("/ansible", deps.discovery.AnsibleInventory)
}
