package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/adapters/localrepo/pulp"
	"github.com/maple52046/swallow/internal/adapters/metrics/promstack"
	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	accessapp "github.com/maple52046/swallow/internal/core/access/application"
	accessdelivery "github.com/maple52046/swallow/internal/core/access/delivery"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/core/convergence/convergencetest"
	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	discoverydelivery "github.com/maple52046/swallow/internal/core/discovery/delivery"
	monitoringapp "github.com/maple52046/swallow/internal/core/monitoring/application"
	monitoringdelivery "github.com/maple52046/swallow/internal/core/monitoring/delivery"
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	platformdelivery "github.com/maple52046/swallow/internal/core/platform/delivery"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdelivery "github.com/maple52046/swallow/internal/core/provisioning/delivery"
	serverapp "github.com/maple52046/swallow/internal/core/server/application"
	serverdelivery "github.com/maple52046/swallow/internal/core/server/delivery"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	siteapp "github.com/maple52046/swallow/internal/core/site/application"
	sitedelivery "github.com/maple52046/swallow/internal/core/site/delivery"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/apicontract"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/modules/accelerator/amd"
	kubernetesmodule "github.com/maple52046/swallow/modules/platform/kubernetes"
	"github.com/maple52046/swallow/modules/platform/kubernetes/kubeapi"
	slurmmodule "github.com/maple52046/swallow/modules/platform/slurm"
	"github.com/maple52046/swallow/modules/platform/slurm/slurmapi"
)

// fakeSiteAutomation stands in for the Sites' Automation Configurations that the Node Exporter
// policy reads: enabled says whether a Site's automation is on, and a Site it does not list has no
// configuration. err, when set, fails every read; reads records the Site of each read.
type fakeSiteAutomation struct {
	enabled map[string]bool
	err     error
	reads   []string
}

func (a *fakeSiteAutomation) FindBySiteID(_ context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error) {
	a.reads = append(a.reads, siteID)
	if a.err != nil {
		return nil, a.err
	}
	enabled, ok := a.enabled[siteID]
	if !ok {
		return nil, workflowdomain.ErrAutomationConfigNotFound
	}
	return &workflowdomain.AutomationConfiguration{SiteID: siteID, Enabled: enabled}, nil
}

type platformFixture struct {
	app          *fiber.App
	jwtSvc       *jwt.Service
	servers      *fakeServerRepo
	sites        *fakeSiteRepo
	integrations *fakeIntegrationRepo
	templates    *fakeDeploymentTemplateRepo
	provider     *fakeProvider
	factory      *fakeProviderFactory
	automation   *fakeSiteAutomation
	activeWork   *fakeActiveServerWorkReader

	launcher          *executingLauncher
	platformLifecycle *fakeLifecycleReader
	uninstallLauncher *fakeUninstallLauncher
	monitoring        *fakeMonitoringFactory
	metricScopes      *fakeScopeResolver
	platformRepo      *fakePlatformRepo
	platformReader    *fakeReaderFactory
	kubernetesClient  *fakeKubernetesClientFactory
	requirementRepo   *fakeDeploymentRequirementRepo
	integrationDeps   *fakeIntegrationDependents
	exporters         *fakeExporterInventory
	imageUploads      *fakeOSImageUploadRepo
	// users and memberships decide what userToken's alice may reach (decision 082).
	users       *fakeUserRepo
	memberships *fakeMemberships
}

// testImageUploadMaxBytes is the installation upload limit the HTTP tests run with; small, so a
// test can declare an image over it without sending one.
const testImageUploadMaxBytes = 1 << 20

// fakeExporterInventory reports the exporters a test says are installed, narrowed to the filter as
// the real inventory is. err, when set, fails every listing.
type fakeExporterInventory struct {
	installed []discoveryapp.InstalledExporter
	err       error
}

func (f *fakeExporterInventory) InstalledExporters(_ context.Context, filter discoveryapp.ExporterFilter) ([]discoveryapp.InstalledExporter, error) {
	if f.err != nil {
		return nil, f.err
	}
	var kept []discoveryapp.InstalledExporter
	for _, exporter := range f.installed {
		if (filter.Name == "" || exporter.Name == filter.Name) && (filter.ServerID == "" || exporter.ServerID == filter.ServerID) {
			kept = append(kept, exporter)
		}
	}
	return kept, nil
}

// installNodeExporter records the Node Exporter as installed on the Servers, which makes each one
// with an address a health target.
func (f *fakeExporterInventory) installNodeExporter(serverIDs ...string) {
	for _, id := range serverIDs {
		f.installed = append(f.installed, discoveryapp.InstalledExporter{ServerID: id, Name: "node-exporter", Port: 9100})
	}
}

// setupPlatform wires the routes exactly as internal/app does, so that route shape and
// middleware are covered rather than only the use cases.
func setupPlatform(t *testing.T) *platformFixture {
	t.Helper()

	servers := newFakeServerRepo()
	sites := newFakeSiteRepo()
	integrations := newFakeIntegrationRepo()
	templates := newFakeDeploymentTemplateRepo()
	osImageMetadata := newFakeOSImageMetadataRepo()
	osImageVerifications := newFakeOSImageVerificationRepo()
	osImageUploads := newFakeOSImageUploadRepo()
	provider := newFakeProvider()
	factory := newFakeProviderFactory()
	factory.providers[testIntegrationID] = provider
	// No Site has an Automation Configuration until a test gives it one, so no Server is covered
	// by the Node Exporter policy by default.
	automation := &fakeSiteAutomation{enabled: map[string]bool{}}
	activeWork := &fakeActiveServerWorkReader{work: map[string]provisioningapp.ActiveServerWork{}}

	jwtSvc := jwt.NewService("test-secret", time.Hour)

	integrationDeps := &fakeIntegrationDependents{}
	siteHandler := sitedelivery.NewSiteHandler(
		siteapp.NewSiteService(sites, integrations),
		siteapp.NewIntegrationService(integrations, sites, servers, templates, sitedomain.IntegrationProviders{
			sitedomain.IntegrationKindProvisioner: {maas.ProviderKind},
			sitedomain.IntegrationKindMetrics:     {promstack.ProviderKind},
			sitedomain.IntegrationKindPlatform:    {kubeapi.ProviderKind, slurmapi.ProviderKind},
			sitedomain.IntegrationKindRepository:  {pulp.ProviderKind},
		}, &storetest.Transactor{}, integrationDeps),
	)
	// The reconcile use case is also the DeployedImageNameRefresher, so the metadata edit and the
	// handler share one instance and a rename propagates to server projections through the same
	// server repo the reconcile pass uses.
	reconcileUC := provisioningapp.NewReconcileUseCase(integrations, servers, factory, osImageMetadata, &recordingInstalledSoftware{})
	deployments := provisioningapp.NewDeployServersUseCase(servers, templates, factory, osImageVerifications, presentDeploymentKey{})
	launcher := &executingLauncher{
		deployments: deployments,
		release:     provisioningapp.NewReleaseServerUseCase(servers, factory, nil),
	}
	provisioningHandler := provisioningdelivery.NewProvisioningHandler(
		deployments,
		provisioningapp.NewDeploymentTargetPreflightService(servers, factory),
		provisioningapp.NewDeploymentTemplateService(
			templates,
			provisioningapp.NewIntegrationReader(integrations, sites),
			factory,
		),
		nil,
		nil,
		provisioningapp.NewRefreshServerUseCase(servers, factory, osImageMetadata),
		provisioningapp.NewListOSImagesUseCase(factory, osImageMetadata, osImageVerifications),
		reconcileUC,
		provisioningapp.NewGetProvisionerDetailUseCase(servers, factory),
		provisioningapp.NewGetProviderEventsUseCase(servers, factory),
		provisioningapp.NewMachineActionsUseCase(servers, factory, activeWork),
		provisioningapp.NewDeleteServerUseCase(servers, factory),
		provisioningapp.NewDeleteOSImageUseCase(factory, osImageMetadata, osImageVerifications),
		provisioningapp.NewOSImageUploadService(factory, osImageUploads, osImageMetadata, testImageUploadMaxBytes),
		provisioningapp.NewSetOSImageMetadataUseCase(osImageMetadata, reconcileUC),
		provisioningapp.NewListServerTagsUseCase(integrations, factory, servers),
		provisioningapp.NewEditServerTagsUseCase(&storetest.Transactor{}, servers, servers, factory),
		provisioningapp.NewDeleteServerTagUseCase(&storetest.Transactor{}, integrations, factory, servers, servers),
		provisioningapp.NewRenameServerTagUseCase(&storetest.Transactor{}, integrations, factory, servers, servers),
	)
	provisioningHandler.AttachDurableOperations(launcher)
	exporters := &fakeExporterInventory{}
	exporterDiscovery := discoveryapp.NewExporterDiscovery(servers, exporters)
	discoveryHandler := discoverydelivery.NewDiscoveryHandler(
		discoveryapp.NewDiscoveryUseCase(servers),
		exporterDiscovery,
	)

	monitoringFactory := newFakeMonitoringFactory()
	// Health is classified by the real rule over the same exporter discovery and the real Node
	// Exporter policy, as internal/app composes it.
	health := monitoringapp.NewHealthResolver(monitoringFactory, exporterDiscovery, softwareapp.NewEnsureCoverage(automation))
	serverHandler := serverdelivery.NewServerHandler(
		serverapp.NewListServersUseCase(servers, health),
		serverapp.NewGetServerUseCase(servers, health),
	)
	scopeResolver := &fakeScopeResolver{members: map[string][]string{}}
	monitoringHandler := monitoringdelivery.NewMonitoringHandler(
		monitoringapp.NewAlertService(monitoringFactory, fakeSiteLookup{sites: sites}),
		monitoringapp.NewServerMetricsService(monitoringFactory, everyServerInSite{siteID: testSiteID}),
		monitoringapp.NewMetricRangeService(monitoringFactory, scopeResolver, everyServerInSite{siteID: testSiteID}).
			WithClock(func() time.Time { return testMetricsNow }),
	)

	platformRepo := newFakePlatformRepo()
	requirementRepo := &fakeDeploymentRequirementRepo{}
	readerFactory := newFakeReaderFactory()
	lifecycle := newFakeLifecycleReader()
	uninstallLauncher := &fakeUninstallLauncher{}
	membershipSync := platformapp.NewMembershipSyncUseCase(platformRepo, servers, readerFactory)
	claims := fakePlatformClaims{servers: servers}
	features := fakeFeatureRecorder{platforms: platformRepo}
	lockGuard := storedLockGuard{servers: servers}
	platformCleanup := &recordingPlatformCleanup{}
	platformService, err := platformapp.NewPlatformService(platformapp.PlatformServiceDeps{
		Transactor: &storetest.Transactor{}, Platforms: platformRepo, Sites: sites, Servers: servers,
		Lifecycle: lifecycle, Integrations: platformCleanup, Canceler: platformCleanup, Claims: claims,
		GPUOperators: features, Omnistats: features, DeviceMetricsExporters: features,
		ExporterAssignments: platformCleanup, OwnedMonitoringLinks: []string{"Platform Grafana"},
	})
	if err != nil {
		t.Fatalf("NewPlatformService() error = %v", err)
	}
	deployService, err := platformapp.NewDeployService(platformapp.DeployServiceDeps{
		PlatformService: platformService, Platforms: platformRepo, Servers: servers, Launcher: &fakeDeploymentLauncher{},
		Protection: lockGuard, Claims: claims, MachinePreparation: acceptingMachinePreparation{},
		NodeSubnets: noNodeSubnets{}, Requirements: requirementRepo, DeploymentKeys: presentDeploymentKey{},
		GPUOperators: features, Omnistats: features, DeviceMetricsExporters: features,
		Accelerator: amd.Accelerator{}, ReadRequest: platformdelivery.DeployInputFromRequest,
	})
	if err != nil {
		t.Fatalf("NewDeployService() error = %v", err)
	}
	uninstallService := platformapp.NewUninstallService(
		platformRepo, servers, lifecycle, uninstallLauncher, lockGuard)
	slurmClusterRead := platformapp.NewGetSlurmClusterUseCase(platformRepo, readerFactory)
	kubernetesFactory := newFakeKubernetesClientFactory()
	kubernetesExplorer := platformapp.NewKubernetesExplorerUseCase(platformRepo, lifecycle, kubernetesFactory, servers)
	requirementService := platformapp.NewDeploymentRequirementService(requirementRepo)
	platformHandler := platformdelivery.NewPlatformHandler(
		platformService, membershipSync, slurmClusterRead, kubernetesExplorer, requirementService)
	platformModules := map[platformdomain.PlatformType]platformdomain.PlatformModule{
		platformdomain.PlatformTypeKubernetes: kubernetesmodule.Platform{}, platformdomain.PlatformTypeSlurm: slurmmodule.Platform{},
	}
	changePlans, err := convergence.NewChangePlanService(convergence.ChangePlanServiceDeps{
		Transactor: &storetest.Transactor{}, Plans: &fakeChangePlanRepo{}, Policies: &convergencetest.Policies{},
		Planners: []convergence.Planner{platformapp.NewPlatformPlanner(platformRepo, lifecycle, deployService, uninstallService, nil, platformModules)},
	})
	if err != nil {
		t.Fatalf("NewChangePlanService() error = %v", err)
	}
	platformHandler.AttachSpecService(platformapp.NewPlatformSpecService(&storetest.Transactor{}, platformRepo, platformRepo, claims,
		deployService, platformService, changePlans, nil, platformModules))

	// Access (decision 082): admin-1 is an admin and user-1 (alice) a user with no Pool Membership
	// until a test gives her one through f.memberships.
	users := newFakeUserRepo()
	users.users["admin-1"] = &authdomain.User{ID: "admin-1", Username: "admin", Role: authdomain.RoleAdmin, UIDNumber: 10000}
	users.users["user-1"] = &authdomain.User{ID: "user-1", Username: "alice", Role: authdomain.RoleUser, UIDNumber: 10001}
	memberships := newFakeMemberships()
	authorizer, err := accessapp.NewAuthorizer(accessapp.AuthorizerDeps{
		Users: users, Memberships: memberships, Servers: fakeServerPools{servers: servers},
		Platforms: platformapp.NewPoolResolver(platformRepo, claims, servers), Workflows: noWorkflowTargets{},
	})
	if err != nil {
		t.Fatalf("NewAuthorizer() error = %v", err)
	}
	g := accessdelivery.NewGuard(authorizer)

	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	authed := []fiber.Handler{middleware.Auth(jwtSvc), g.Scope()}
	with := func(extra ...fiber.Handler) []fiber.Handler { return append(slices.Clone(authed), extra...) }
	admin := with(g.Admin())
	route := func(h fiber.Handler, extra ...fiber.Handler) []fiber.Handler { return append(with(extra...), h) }
	adminRoute := func(h fiber.Handler) []fiber.Handler { return append(slices.Clone(admin), h) }
	serverRead := g.Server(accessdomain.Read, "id")
	serverOperate := g.Server(accessdomain.Operate, "id")
	platformRead := g.Platform(accessdomain.Read, "id")
	platformOperate := g.Platform(accessdomain.Operate, "id")
	bodyServers := g.ServersInBody(accessdomain.Operate, func(body []byte) []string {
		var request struct {
			ServerIDs []string `json:"serverIds"`
		}
		_ = json.Unmarshal(body, &request)
		return request.ServerIDs
	})
	platformMembers := g.PlatformMembersInBody(func(body []byte) []string {
		var request struct {
			Spec struct {
				Members []struct {
					ServerID string `json:"serverId"`
				} `json:"members"`
			} `json:"spec"`
		}
		_ = json.Unmarshal(body, &request)
		var ids []string
		for _, member := range request.Spec.Members {
			ids = append(ids, member.ServerID)
		}
		return ids
	})
	v1 := app.Group("/api/v1")

	siteGroup := v1.Group("/sites")
	siteGroup.Post("/", adminRoute(siteHandler.CreateSite)...)
	siteGroup.Get("/", route(siteHandler.ListSites)...)
	siteGroup.Get("/:id", route(siteHandler.GetSite)...)
	siteGroup.Patch("/:id", adminRoute(siteHandler.UpdateSite)...)
	siteGroup.Delete("/:id", adminRoute(siteHandler.DeleteSite)...)

	integrationGroup := v1.Group("/integrations")
	integrationGroup.Post("/", adminRoute(siteHandler.CreateIntegration)...)
	integrationGroup.Get("/", adminRoute(siteHandler.ListIntegrations)...)
	integrationGroup.Get("/:id", adminRoute(siteHandler.GetIntegration)...)
	integrationGroup.Patch("/:id", adminRoute(siteHandler.UpdateIntegration)...)
	integrationGroup.Put("/:id/credential", adminRoute(siteHandler.ReplaceCredential)...)
	integrationGroup.Delete("/:id", adminRoute(siteHandler.DeleteIntegration)...)

	serverGroup := v1.Group("/servers")
	serverGroup.Get("/", route(serverHandler.List)...)
	serverGroup.Get("/:id", route(serverHandler.Get, serverRead)...)
	serverGroup.Post("/:id/refresh", route(provisioningHandler.RefreshServer, serverOperate)...)
	serverGroup.Delete("/:id", adminRoute(provisioningHandler.DeleteServer)...)
	serverGroup.Get("/:id/provisioner-detail", route(provisioningHandler.ProvisionerDetail, serverOperate)...)
	serverGroup.Get("/:id/events", route(provisioningHandler.ProviderEvents, serverRead)...)
	serverGroup.Post("/:id/power-on", route(provisioningHandler.PowerOn, serverOperate)...)
	serverGroup.Post("/:id/power-off", route(provisioningHandler.PowerOff, serverOperate)...)
	serverGroup.Get("/:id/power-state", route(provisioningHandler.PowerState, serverRead)...)
	serverGroup.Post("/:id/inspect", route(provisioningHandler.Inspect, serverOperate)...)
	serverGroup.Post("/:id/test", route(provisioningHandler.Test, serverOperate)...)
	serverGroup.Post("/:id/abort", route(provisioningHandler.Abort, serverOperate)...)
	serverGroup.Post("/:id/override-failed-testing", route(provisioningHandler.OverrideFailedTesting, serverOperate)...)
	serverGroup.Post("/:id/lock", route(provisioningHandler.Lock, serverOperate)...)
	serverGroup.Post("/:id/unlock", route(provisioningHandler.Unlock, serverOperate)...)
	serverGroup.Post("/:id/mark-broken", route(provisioningHandler.MarkBroken, serverOperate)...)
	serverGroup.Post("/:id/mark-fixed", route(provisioningHandler.MarkFixed, serverOperate)...)
	serverGroup.Post("/:id/rescue-mode", route(provisioningHandler.RescueMode, serverOperate)...)
	serverGroup.Post("/:id/exit-rescue-mode", route(provisioningHandler.ExitRescueMode, serverOperate)...)

	provisioningGroup := v1.Group("/provisioning")
	provisioningGroup.Get("/images", route(provisioningHandler.ListImages)...)
	provisioningGroup.Delete("/images", adminRoute(provisioningHandler.DeleteImage)...)
	provisioningGroup.Post("/image-uploads", adminRoute(provisioningHandler.CreateImageUpload)...)
	provisioningGroup.Put("/image-uploads/:id/content", adminRoute(provisioningHandler.SendImageUploadContent)...)
	provisioningGroup.Put("/images/metadata", adminRoute(provisioningHandler.SetImageMetadata)...)
	provisioningGroup.Delete("/images/metadata", adminRoute(provisioningHandler.ClearImageMetadata)...)
	provisioningGroup.Get("/tags", route(provisioningHandler.ListServerTags)...)
	provisioningGroup.Post("/tags", route(provisioningHandler.EditServerTags, bodyServers)...)
	provisioningGroup.Delete("/tags/:name", adminRoute(provisioningHandler.DeleteServerTag)...)
	provisioningGroup.Patch("/tags/:name", adminRoute(provisioningHandler.RenameServerTag)...)
	provisioningGroup.Get("/templates", route(provisioningHandler.ListTemplates)...)
	provisioningGroup.Post("/templates", adminRoute(provisioningHandler.CreateTemplate)...)
	provisioningGroup.Get("/templates/:id", route(provisioningHandler.GetTemplate)...)
	provisioningGroup.Patch("/templates/:id", adminRoute(provisioningHandler.UpdateTemplate)...)
	provisioningGroup.Delete("/templates/:id", adminRoute(provisioningHandler.DeleteTemplate)...)
	provisioningGroup.Put("/templates/:id/user-data", adminRoute(provisioningHandler.ReplaceTemplateUserData)...)
	provisioningGroup.Delete("/templates/:id/user-data", adminRoute(provisioningHandler.ClearTemplateUserData)...)
	provisioningGroup.Post("/deployments/preflight", route(provisioningHandler.PreflightDeployServers, bodyServers)...)
	provisioningGroup.Post("/deployment-operations", route(provisioningHandler.CreateDeploymentOperation, bodyServers)...)
	provisioningGroup.Post("/release-operations", route(provisioningHandler.CreateReleaseOperation, bodyServers)...)
	provisioningGroup.Post("/reconcile", adminRoute(provisioningHandler.ReconcileAll)...)
	provisioningGroup.Post("/integrations/:id/reconcile", adminRoute(provisioningHandler.Reconcile)...)

	platformGroup := v1.Group("/platforms")
	platformGroup.Get("/deployment-requirements/slurm", route(platformHandler.GetSlurmDeploymentRequirement)...)
	platformGroup.Put("/deployment-requirements/slurm", adminRoute(platformHandler.PutSlurmDeploymentRequirement)...)
	platformGroup.Get("/", route(platformHandler.List)...)
	platformGroup.Post("/", route(platformHandler.Create, platformMembers)...)
	platformGroup.Post("/sync", adminRoute(platformHandler.SyncAllMembership)...)
	platformGroup.Get("/:id", route(platformHandler.Get, platformRead)...)
	platformGroup.Patch("/:id", route(platformHandler.Update, platformOperate)...)
	platformGroup.Put("/:id/spec", route(platformHandler.PutSpec, platformOperate, platformMembers)...)
	platformGroup.Delete("/:id", route(platformHandler.Delete, platformOperate)...)
	platformGroup.Post("/:id/sync", route(platformHandler.SyncMembership, platformOperate)...)
	platformGroup.Get("/:id/slurm", route(platformHandler.GetSlurmCluster, platformRead)...)
	explorer := with(platformOperate)
	k8s := func(h fiber.Handler) []fiber.Handler { return append(slices.Clone(explorer), h) }
	platformGroup.Get("/:id/kubernetes", k8s(platformHandler.GetKubernetesCluster)...)
	platformGroup.Get("/:id/kubernetes/nodes", k8s(platformHandler.ListKubernetesNodes)...)
	platformGroup.Post("/:id/kubernetes/nodes/:node/cordon", k8s(platformHandler.CordonKubernetesNode)...)
	platformGroup.Post("/:id/kubernetes/nodes/:node/uncordon", k8s(platformHandler.UncordonKubernetesNode)...)
	platformGroup.Get("/:id/kubernetes/namespaces", k8s(platformHandler.ListKubernetesNamespaces)...)
	platformGroup.Post("/:id/kubernetes/namespaces", k8s(platformHandler.CreateKubernetesNamespace)...)
	platformGroup.Delete("/:id/kubernetes/namespaces/:namespace", k8s(platformHandler.DeleteKubernetesNamespace)...)
	platformGroup.Get("/:id/kubernetes/applications", k8s(platformHandler.ListKubernetesApplications)...)
	platformGroup.Get("/:id/kubernetes/applications/:namespace/:kind/:name", k8s(platformHandler.GetKubernetesApplication)...)
	platformGroup.Delete("/:id/kubernetes/applications/:namespace/:kind/:name", k8s(platformHandler.DeleteKubernetesApplication)...)
	platformGroup.Post("/:id/kubernetes/applications/:namespace/:kind/:name/scale", k8s(platformHandler.ScaleKubernetesApplication)...)
	platformGroup.Post("/:id/kubernetes/applications/:namespace/:kind/:name/restart", k8s(platformHandler.RestartKubernetesApplication)...)
	platformGroup.Get("/:id/kubernetes/pods", k8s(platformHandler.ListKubernetesPods)...)
	platformGroup.Get("/:id/kubernetes/pods/:namespace/:name/logs", k8s(platformHandler.GetKubernetesPodLogs)...)
	platformGroup.Delete("/:id/kubernetes/pods/:namespace/:name", k8s(platformHandler.DeleteKubernetesPod)...)
	platformGroup.Get("/:id/kubernetes/services", k8s(platformHandler.ListKubernetesServices)...)
	platformGroup.Get("/:id/kubernetes/ingresses", k8s(platformHandler.ListKubernetesIngresses)...)
	platformGroup.Get("/:id/kubernetes/configmaps", k8s(platformHandler.ListKubernetesConfigMaps)...)
	platformGroup.Get("/:id/kubernetes/secrets", k8s(platformHandler.ListKubernetesSecrets)...)
	platformGroup.Get("/:id/kubernetes/persistentvolumeclaims", k8s(platformHandler.ListKubernetesPersistentVolumeClaims)...)
	platformGroup.Post("/:id/kubernetes/apply", k8s(platformHandler.ApplyKubernetesManifest)...)

	monitoringGroup := v1.Group("/monitoring")
	monitoringGroup.Get("/alerts", adminRoute(monitoringHandler.ListAlerts)...)
	monitoringGroup.Post("/alerts/:fingerprint/acknowledge", adminRoute(monitoringHandler.Acknowledge)...)
	monitoringGroup.Get("/metrics", adminRoute(monitoringHandler.ServerMetrics)...)
	monitoringGroup.Get("/metrics/range", adminRoute(monitoringHandler.MetricRange)...)
	monitoringGroup.Get("/metrics/names", route(monitoringHandler.MetricNames)...)

	machine := middleware.MachineAuth(jwtSvc, testMachineToken)

	discoveryGroup := v1.Group("/discovery", machine)
	discoveryGroup.Get("/prometheus", discoveryHandler.PrometheusTargets)
	discoveryGroup.Get("/prometheus/exporters", discoveryHandler.ExporterTargets)
	discoveryGroup.Get("/ansible", discoveryHandler.AnsibleInventory)

	return &platformFixture{
		exporters:         exporters,
		imageUploads:      osImageUploads,
		app:               app,
		jwtSvc:            jwtSvc,
		servers:           servers,
		sites:             sites,
		integrations:      integrations,
		templates:         templates,
		provider:          provider,
		factory:           factory,
		automation:        automation,
		activeWork:        activeWork,
		launcher:          launcher,
		monitoring:        monitoringFactory,
		metricScopes:      scopeResolver,
		platformRepo:      platformRepo,
		platformReader:    readerFactory,
		kubernetesClient:  kubernetesFactory,
		requirementRepo:   requirementRepo,
		integrationDeps:   integrationDeps,
		platformLifecycle: lifecycle,
		uninstallLauncher: uninstallLauncher,
		users:             users,
		memberships:       memberships,
	}
}

const testNonProvisionerIntegrationID = "integration-metrics"

// seedNonProvisionerIntegration registers a non-provisioner integration (a metrics
// integration) so that a test can assert provisioning reconcile refuses one.
func seedNonProvisionerIntegration(t *testing.T, f *platformFixture) {
	t.Helper()
	now := time.Now().UTC()
	if err := f.integrations.Create(context.Background(), &sitedomain.Integration{
		ID:           testNonProvisionerIntegrationID,
		SiteID:       testSiteID,
		Kind:         sitedomain.IntegrationKindMetrics,
		ProviderKind: promstack.ProviderKind,
		Name:         "prometheus",
		Endpoint:     "http://prometheus.example.com",
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, "prometheus-token"); err != nil {
		t.Fatalf("seed non-provisioner integration: %v", err)
	}
}

const testMachineToken = "discovery-token-for-tests"

// testMetricsNow is the fixed clock the usage-history service ends named windows at, so a
// test can assert the exact evaluated range.
var testMetricsNow = time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)

// errTestMetricsDown stands in for an unreachable metrics store.
var errTestMetricsDown = errors.New("metrics store unreachable")

func adminToken(t *testing.T, jwtSvc *jwt.Service) string {
	t.Helper()
	token, err := jwtSvc.Sign("admin-1", "admin", "admin")
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

func userToken(t *testing.T, jwtSvc *jwt.Service) string {
	t.Helper()
	token, err := jwtSvc.Sign("user-1", "alice", "user")
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

func parseArrayBody(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	var items []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode array body: %v", err)
	}
	return items
}

// rawBody returns the response body as text, for assertions about what must not appear
// anywhere in it.
func rawBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(raw)
}

func errorCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	body := parseBody(t, resp)
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected an error envelope, got %v", body)
	}
	code, _ := errObj["code"].(string)
	return code
}

func (f *platformFixture) adminAuth(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{"Authorization": "Bearer " + adminToken(t, f.jwtSvc)}
}

func seedProvisionerIntegration(t *testing.T, f *platformFixture) {
	t.Helper()
	now := time.Now().UTC()
	if err := f.integrations.Create(context.Background(), &sitedomain.Integration{
		ID:           testIntegrationID,
		SiteID:       testSiteID,
		Kind:         sitedomain.IntegrationKindProvisioner,
		ProviderKind: maas.ProviderKind,
		Name:         "maas-east",
		Endpoint:     "http://maas:5240/MAAS",
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, "ck:tk:ts"); err != nil {
		t.Fatalf("seed provisioner: %v", err)
	}
}

// seedServer adds a projected server directly, standing in for a reconcile pass.
func (f *platformFixture) seedServer(id, hostname, address string, mutate func(*serverdomain.Server)) *serverdomain.Server {
	now := time.Now().UTC()
	server := &serverdomain.Server{
		ID: id,
		Source: serverdomain.Source{
			SiteID:            testSiteID,
			IntegrationID:     testIntegrationID,
			ProviderMachineID: "machine-" + id,
		},
		Observed: serverdomain.Observed{
			Hostname:     hostname,
			FQDN:         hostname + ".maas",
			Architecture: "amd64/generic",
			CPUCores:     32,
			MemoryMiB:    131072,
			StorageGB:    512,
		},
		Provisioning: &serverdomain.ProvisioningStatus{
			State:         "deployed",
			ProviderState: "Deployed",
			PowerState:    "on",
			OSSystem:      "ubuntu",
			DistroSeries:  "jammy",
			IntegrationID: testIntegrationID,
			ObservedAt:    now,
		},
		LastSeenAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if address != "" {
		server.Observed.Addresses = []string{address}
	}
	if mutate != nil {
		mutate(server)
	}
	f.servers.servers[id] = server
	return server
}
