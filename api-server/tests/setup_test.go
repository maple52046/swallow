package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	discoveryapp "github.com/maple52046/swallow/internal/discovery/application"
	discoverydelivery "github.com/maple52046/swallow/internal/discovery/delivery"
	monitoringapp "github.com/maple52046/swallow/internal/monitoring/application"
	monitoringdelivery "github.com/maple52046/swallow/internal/monitoring/delivery"
	platformapp "github.com/maple52046/swallow/internal/platform/application"
	platformdelivery "github.com/maple52046/swallow/internal/platform/delivery"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdelivery "github.com/maple52046/swallow/internal/provisioning/delivery"
	provisioninginfra "github.com/maple52046/swallow/internal/provisioning/infra"
	serverapp "github.com/maple52046/swallow/internal/server/application"
	serverdelivery "github.com/maple52046/swallow/internal/server/delivery"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
	siteapp "github.com/maple52046/swallow/internal/site/application"
	sitedelivery "github.com/maple52046/swallow/internal/site/delivery"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// testHealthResolver stands in for the metrics integration.
type testHealthResolver struct {
	health map[string]*serverdomain.HealthStatus
	err    error
}

func (r *testHealthResolver) ResolveHealth(_ context.Context, serverIDs []string) (map[string]*serverdomain.HealthStatus, error) {
	if r.err != nil {
		return nil, r.err
	}
	resolved := map[string]*serverdomain.HealthStatus{}
	for _, id := range serverIDs {
		if h, ok := r.health[id]; ok {
			resolved[id] = h
		}
	}
	return resolved, nil
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
	health       *testHealthResolver
	activeWork   *fakeActiveServerWorkReader

	platformLifecycle *fakeLifecycleReader
	uninstallLauncher *fakeUninstallLauncher
	monitoring        *fakeMonitoringFactory
	platformRepo      *fakePlatformRepo
	platformReader    *fakeReaderFactory
	requirementRepo   *fakeDeploymentRequirementRepo
}

// setupPlatform wires the routes exactly as internal/app does, so that route shape and
// middleware are covered rather than only the use cases.
func setupPlatform(t *testing.T) *platformFixture {
	t.Helper()

	servers := newFakeServerRepo()
	sites := newFakeSiteRepo()
	integrations := newFakeIntegrationRepo()
	templates := newFakeDeploymentTemplateRepo()
	osImageOverlays := newFakeOSImageOverlayRepo()
	provider := newFakeProvider()
	factory := newFakeProviderFactory()
	factory.providers[testIntegrationID] = provider
	health := &testHealthResolver{health: map[string]*serverdomain.HealthStatus{}}
	activeWork := &fakeActiveServerWorkReader{work: map[string]provisioningapp.ActiveServerWork{}}

	jwtSvc := jwt.NewService("test-secret", time.Hour)

	siteHandler := sitedelivery.NewSiteHandler(
		siteapp.NewSiteService(sites, integrations),
		siteapp.NewIntegrationService(integrations, sites, servers, templates),
	)
	serverHandler := serverdelivery.NewServerHandler(
		serverapp.NewListServersUseCase(servers, health),
		serverapp.NewGetServerUseCase(servers, health),
	)
	provisioningHandler := provisioningdelivery.NewProvisioningHandler(
		provisioningapp.NewDeployServerUseCase(servers, factory),
		provisioningapp.NewDeployServersUseCase(servers, templates, factory),
		provisioningapp.NewDeploymentTargetPreflightService(servers, factory),
		provisioningapp.NewDeploymentTemplateService(
			templates,
			provisioninginfra.NewIntegrationReader(integrations, sites),
			factory,
		),
		nil,
		nil,
		provisioningapp.NewReleaseServerUseCase(servers, factory, nil),
		provisioningapp.NewRefreshServerUseCase(servers, factory),
		provisioningapp.NewListOSImagesUseCase(factory, osImageOverlays),
		provisioningapp.NewReconcileUseCase(integrations, servers, factory, osImageOverlays),
		provisioningapp.NewGetProvisionerDetailUseCase(servers, factory),
		provisioningapp.NewGetProviderEventsUseCase(servers, factory),
		provisioningapp.NewMachineActionsUseCase(servers, factory, activeWork),
		provisioningapp.NewDeleteServerUseCase(servers, factory),
		provisioningapp.NewDeleteOSImageUseCase(factory, osImageOverlays),
		provisioningapp.NewSetOSImageOverlayUseCase(osImageOverlays),
	)
	discoveryHandler := discoverydelivery.NewDiscoveryHandler(
		discoveryapp.NewDiscoveryUseCase(servers),
	)

	monitoringFactory := newFakeMonitoringFactory()
	monitoringHandler := monitoringdelivery.NewMonitoringHandler(
		monitoringapp.NewAlertService(monitoringFactory),
		monitoringapp.NewServerMetricsService(monitoringFactory),
	)

	platformRepo := newFakePlatformRepo()
	requirementRepo := &fakeDeploymentRequirementRepo{}
	readerFactory := newFakeReaderFactory()
	lifecycle := newFakeLifecycleReader()
	uninstallLauncher := &fakeUninstallLauncher{}
	membershipSync := platformapp.NewMembershipSyncUseCase(platformRepo, servers, readerFactory)
	platformService := platformapp.NewPlatformService(platformRepo, sites, servers, lifecycle, nil)
	deployService := platformapp.NewDeployService(
		platformService, platformRepo, servers, lifecycle, &fakeDeploymentLauncher{})
	deployService.AttachDeploymentRequirementReader(requirementRepo)
	uninstallService := platformapp.NewUninstallService(
		platformRepo, servers, lifecycle, uninstallLauncher)
	slurmClusterRead := platformapp.NewGetSlurmClusterUseCase(platformRepo, readerFactory)
	requirementService := platformapp.NewDeploymentRequirementService(requirementRepo)
	platformHandler := platformdelivery.NewPlatformHandler(
		platformService, membershipSync, deployService, uninstallService, slurmClusterRead, requirementService)

	app := fiber.New()
	admin := []fiber.Handler{middleware.Auth(jwtSvc), middleware.AdminOnly()}
	v1 := app.Group("/api/v1")

	siteGroup := v1.Group("/sites", admin...)
	siteGroup.Post("/", siteHandler.CreateSite)
	siteGroup.Get("/", siteHandler.ListSites)
	siteGroup.Get("/:id", siteHandler.GetSite)
	siteGroup.Patch("/:id", siteHandler.UpdateSite)
	siteGroup.Delete("/:id", siteHandler.DeleteSite)

	integrationGroup := v1.Group("/integrations", admin...)
	integrationGroup.Post("/", siteHandler.CreateIntegration)
	integrationGroup.Get("/", siteHandler.ListIntegrations)
	integrationGroup.Get("/:id", siteHandler.GetIntegration)
	integrationGroup.Patch("/:id", siteHandler.UpdateIntegration)
	integrationGroup.Put("/:id/credential", siteHandler.ReplaceCredential)
	integrationGroup.Delete("/:id", siteHandler.DeleteIntegration)

	serverGroup := v1.Group("/servers", admin...)
	serverGroup.Get("/", serverHandler.List)
	serverGroup.Get("/:id", serverHandler.Get)
	serverGroup.Post("/:id/refresh", provisioningHandler.RefreshServer)
	serverGroup.Delete("/:id", provisioningHandler.DeleteServer)
	serverGroup.Get("/:id/provisioner-detail", provisioningHandler.ProvisionerDetail)
	serverGroup.Get("/:id/events", provisioningHandler.ProviderEvents)
	serverGroup.Post("/:id/deploy", provisioningHandler.Deploy)
	serverGroup.Post("/:id/release", provisioningHandler.Release)
	serverGroup.Post("/:id/power-on", provisioningHandler.PowerOn)
	serverGroup.Post("/:id/power-off", provisioningHandler.PowerOff)
	serverGroup.Get("/:id/power-state", provisioningHandler.PowerState)
	serverGroup.Post("/:id/commission", provisioningHandler.Commission)
	serverGroup.Post("/:id/test", provisioningHandler.Test)
	serverGroup.Post("/:id/abort", provisioningHandler.Abort)
	serverGroup.Post("/:id/override-failed-testing", provisioningHandler.OverrideFailedTesting)
	serverGroup.Post("/:id/lock", provisioningHandler.Lock)
	serverGroup.Post("/:id/unlock", provisioningHandler.Unlock)
	serverGroup.Post("/:id/mark-broken", provisioningHandler.MarkBroken)
	serverGroup.Post("/:id/mark-fixed", provisioningHandler.MarkFixed)
	serverGroup.Post("/:id/rescue-mode", provisioningHandler.RescueMode)
	serverGroup.Post("/:id/exit-rescue-mode", provisioningHandler.ExitRescueMode)

	provisioningGroup := v1.Group("/provisioning", admin...)
	provisioningGroup.Get("/images", provisioningHandler.ListImages)
	provisioningGroup.Delete("/images", provisioningHandler.DeleteImage)
	provisioningGroup.Patch("/images/overlay", provisioningHandler.SetImageOverlay)
	provisioningGroup.Delete("/images/overlay", provisioningHandler.ClearImageOverlay)
	provisioningGroup.Get("/templates", provisioningHandler.ListTemplates)
	provisioningGroup.Post("/templates", provisioningHandler.CreateTemplate)
	provisioningGroup.Get("/templates/:id", provisioningHandler.GetTemplate)
	provisioningGroup.Patch("/templates/:id", provisioningHandler.UpdateTemplate)
	provisioningGroup.Delete("/templates/:id", provisioningHandler.DeleteTemplate)
	provisioningGroup.Put("/templates/:id/user-data", provisioningHandler.ReplaceTemplateUserData)
	provisioningGroup.Delete("/templates/:id/user-data", provisioningHandler.ClearTemplateUserData)
	provisioningGroup.Post("/deployments/preflight", provisioningHandler.PreflightDeployServers)
	provisioningGroup.Post("/deployments", provisioningHandler.DeployServers)
	provisioningGroup.Post("/reconcile", provisioningHandler.ReconcileAll)
	provisioningGroup.Post("/integrations/:id/reconcile", provisioningHandler.Reconcile)

	platformGroup := v1.Group("/platforms", admin...)
	platformGroup.Get("/deployment-requirements/slurm", platformHandler.GetSlurmDeploymentRequirement)
	platformGroup.Put("/deployment-requirements/slurm", platformHandler.PutSlurmDeploymentRequirement)
	platformGroup.Post("/", platformHandler.Create)
	platformGroup.Get("/", platformHandler.List)
	platformGroup.Post("/deploy", platformHandler.Deploy)
	platformGroup.Post("/sync", platformHandler.SyncAllMembership)
	platformGroup.Get("/:id", platformHandler.Get)
	platformGroup.Patch("/:id", platformHandler.Update)
	platformGroup.Delete("/:id", platformHandler.Delete)
	platformGroup.Post("/:id/sync", platformHandler.SyncMembership)
	platformGroup.Post("/:id/uninstall", platformHandler.Uninstall)

	// Deprecated one-release /clusters alias mirrors the Platform routes with a
	// Deprecation header, matching internal/app route registration.
	legacyPlatformGroup := v1.Group("/clusters", admin...)
	legacyPlatformGroup.Use(func(c *fiber.Ctx) error {
		c.Set("Deprecation", "true")
		c.Set("Link", "</api/v1/platforms>; rel=\"successor-version\"")
		return c.Next()
	})
	legacyPlatformGroup.Post("/", platformHandler.Create)
	legacyPlatformGroup.Get("/", platformHandler.List)
	legacyPlatformGroup.Post("/deploy", platformHandler.Deploy)
	legacyPlatformGroup.Post("/sync", platformHandler.SyncAllMembership)
	legacyPlatformGroup.Get("/:id", platformHandler.Get)
	legacyPlatformGroup.Patch("/:id", platformHandler.Update)
	legacyPlatformGroup.Delete("/:id", platformHandler.Delete)
	legacyPlatformGroup.Post("/:id/sync", platformHandler.SyncMembership)
	legacyPlatformGroup.Post("/:id/uninstall", platformHandler.Uninstall)

	monitoringGroup := v1.Group("/monitoring", admin...)
	monitoringGroup.Get("/alerts", monitoringHandler.ListAlerts)
	monitoringGroup.Post("/alerts/:fingerprint/acknowledge", monitoringHandler.Acknowledge)
	monitoringGroup.Get("/metrics", monitoringHandler.ServerMetrics)
	monitoringGroup.Get("/metrics/names", monitoringHandler.MetricNames)

	machine := middleware.MachineAuth(jwtSvc, testMachineToken)

	discoveryGroup := v1.Group("/discovery", machine)
	discoveryGroup.Get("/prometheus", discoveryHandler.PrometheusTargets)
	discoveryGroup.Get("/ansible", discoveryHandler.AnsibleInventory)

	return &platformFixture{
		app:               app,
		jwtSvc:            jwtSvc,
		servers:           servers,
		sites:             sites,
		integrations:      integrations,
		templates:         templates,
		provider:          provider,
		factory:           factory,
		health:            health,
		activeWork:        activeWork,
		monitoring:        monitoringFactory,
		platformRepo:      platformRepo,
		platformReader:    readerFactory,
		requirementRepo:   requirementRepo,
		platformLifecycle: lifecycle,
		uninstallLauncher: uninstallLauncher,
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
		ProviderKind: sitedomain.ProviderKindPrometheus,
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
		ProviderKind: sitedomain.ProviderKindMAAS,
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
