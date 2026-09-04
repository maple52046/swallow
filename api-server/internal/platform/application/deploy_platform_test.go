package application

import (
	"context"
	"errors"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// The deploy validation is the load-bearing part of platform deployment: a topology or
// network mistake produces a platform that cannot form, so it is checked before anything is
// created. These tests exercise it against in-memory fakes.

type deployFakePlatformRepo struct {
	platforms         map[string]*platformdomain.Platform
	syncPlatformID    string
	syncIntegrationID string
	syncState         platformdomain.SyncState
}

func (r *deployFakePlatformRepo) Create(_ context.Context, platform *platformdomain.Platform) error {
	for _, existing := range r.platforms {
		if existing.SiteID == platform.SiteID && existing.Name == platform.Name {
			return platformdomain.ErrPlatformNameTaken
		}
	}
	r.platforms[platform.ID] = platform
	return nil
}
func (r *deployFakePlatformRepo) FindByID(_ context.Context, id string) (*platformdomain.Platform, error) {
	if c, ok := r.platforms[id]; ok {
		return c, nil
	}
	return nil, platformdomain.ErrPlatformNotFound
}
func (r *deployFakePlatformRepo) List(_ context.Context, siteID string) ([]*platformdomain.Platform, error) {
	platforms := make([]*platformdomain.Platform, 0, len(r.platforms))
	for _, platform := range r.platforms {
		if siteID == "" || platform.SiteID == siteID {
			platforms = append(platforms, platform)
		}
	}
	return platforms, nil
}
func (r *deployFakePlatformRepo) Update(_ context.Context, platform *platformdomain.Platform) error {
	r.platforms[platform.ID] = platform
	return nil
}
func (r *deployFakePlatformRepo) UpdateSyncState(
	_ context.Context, id, integrationID string, state platformdomain.SyncState,
) error {
	platform, ok := r.platforms[id]
	if !ok || platform.IntegrationID != integrationID {
		return platformdomain.ErrPlatformNotFound
	}
	platform.Sync = state
	r.syncPlatformID = id
	r.syncIntegrationID = integrationID
	r.syncState = state
	return nil
}
func (r *deployFakePlatformRepo) Delete(_ context.Context, id string) error {
	delete(r.platforms, id)
	return nil
}

type deployFakeLifecycleReader struct {
	snapshots map[string]platformdomain.LifecycleSnapshot
}

func (r *deployFakeLifecycleReader) Read(
	_ context.Context,
	platformIDs []string,
) (map[string]platformdomain.LifecycleSnapshot, error) {
	result := make(map[string]platformdomain.LifecycleSnapshot, len(platformIDs))
	for _, platformID := range platformIDs {
		snapshot, ok := r.snapshots[platformID]
		if !ok {
			snapshot = platformdomain.LifecycleSnapshot{
				Origin: platformdomain.PlatformOriginRegistered,
				State:  platformdomain.PlatformLifecycleRegistered,
			}
		}
		result[platformID] = snapshot
	}
	return result, nil
}

type deployFakeServerRepo struct {
	servers map[string]*serverdomain.Server
}

func (r *deployFakeServerRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	if s, ok := r.servers[id]; ok {
		return s, nil
	}
	return nil, serverdomain.ErrServerNotFound
}
func (r *deployFakeServerRepo) FindBySource(context.Context, serverdomain.Source) (*serverdomain.Server, error) {
	return nil, serverdomain.ErrServerNotFound
}
func (r *deployFakeServerRepo) FindByHardware(context.Context, serverdomain.Hardware) ([]*serverdomain.Server, error) {
	return nil, nil
}
func (r *deployFakeServerRepo) List(context.Context, serverdomain.ListFilter) (serverdomain.ListResult, error) {
	return serverdomain.ListResult{}, nil
}
func (r *deployFakeServerRepo) Upsert(context.Context, *serverdomain.Server) error { return nil }
func (r *deployFakeServerRepo) MarkAbsent(context.Context, string, time.Time) (int, error) {
	return 0, nil
}
func (r *deployFakeServerRepo) SetMembership(context.Context, string, *serverdomain.MembershipStatus) error {
	return nil
}
func (r *deployFakeServerRepo) SetGPUs(context.Context, string, []serverdomain.GPU) error {
	return nil
}
func (r *deployFakeServerRepo) CountByIntegration(context.Context, string) (int, error) {
	return 0, nil
}
func (r *deployFakeServerRepo) Delete(context.Context, string) error { return nil }

type deployFakeSiteRepo struct{ ids map[string]bool }

func (r *deployFakeSiteRepo) Create(context.Context, *sitedomain.Site) error { return nil }
func (r *deployFakeSiteRepo) FindByID(_ context.Context, id string) (*sitedomain.Site, error) {
	if r.ids[id] {
		return &sitedomain.Site{ID: id, Name: id}, nil
	}
	return nil, sitedomain.ErrSiteNotFound
}
func (r *deployFakeSiteRepo) List(context.Context) ([]*sitedomain.Site, error) { return nil, nil }
func (r *deployFakeSiteRepo) Update(context.Context, *sitedomain.Site) error   { return nil }
func (r *deployFakeSiteRepo) Delete(context.Context, string) error             { return nil }

type recordingLauncher struct {
	launched *platformdomain.DeploymentLaunch
}

func (l *recordingLauncher) Launch(_ context.Context, launch platformdomain.DeploymentLaunch) (string, error) {
	l.launched = &launch
	return "operation-1", nil
}

func deployedServer(id, hostname, siteID string, addresses ...string) *serverdomain.Server {
	return &serverdomain.Server{
		ID:       id,
		Source:   serverdomain.Source{SiteID: siteID},
		Observed: serverdomain.Observed{Hostname: hostname, Addresses: addresses},
		Provisioning: &serverdomain.ProvisioningStatus{
			State: "deployed",
		},
	}
}

func newDeployHarness(servers ...*serverdomain.Server) (*DeployService, *recordingLauncher, *deployFakePlatformRepo) {
	return newDeployHarnessWithLifecycle(
		&deployFakeLifecycleReader{snapshots: map[string]platformdomain.LifecycleSnapshot{}},
		servers...,
	)
}

func newDeployHarnessWithLifecycle(
	lifecycle platformdomain.LifecycleReader,
	servers ...*serverdomain.Server,
) (*DeployService, *recordingLauncher, *deployFakePlatformRepo) {
	platformRepo := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{}}
	serverRepo := &deployFakeServerRepo{servers: map[string]*serverdomain.Server{}}
	for _, s := range servers {
		serverRepo.servers[s.ID] = s
	}
	siteRepo := &deployFakeSiteRepo{ids: map[string]bool{"site-1": true}}
	platformService := NewPlatformService(platformRepo, siteRepo, serverRepo, nil, nil)
	launcher := &recordingLauncher{}
	return NewDeployService(platformService, platformRepo, serverRepo, lifecycle, launcher), launcher, platformRepo
}

func threeControllersFourWorkers() []platformdomain.RoleAssignment {
	return []platformdomain.RoleAssignment{
		{ServerID: "c1", Role: platformdomain.NodeRoleControlPlane},
		{ServerID: "c2", Role: platformdomain.NodeRoleControlPlane},
		{ServerID: "c3", Role: platformdomain.NodeRoleControlPlane},
		{ServerID: "w1", Role: platformdomain.NodeRoleWorker},
	}
}

func haServers() []*serverdomain.Server {
	return []*serverdomain.Server{
		deployedServer("c1", "lab-control-1", "site-1", "192.168.100.2"),
		deployedServer("c2", "lab-control-2", "site-1", "192.168.100.3"),
		deployedServer("c3", "lab-control-3", "site-1", "192.168.100.8"),
		deployedServer("w1", "lab-compute-1", "site-1", "192.168.100.4"),
	}
}

func validDeployInput() DeployPlatformInput {
	return DeployPlatformInput{
		SiteID:        "site-1",
		Name:          "lab-k0s",
		GPUStackOwner: "provisioning",
		Spec: platformdomain.DeploymentSpec{
			K0sVersion:      "v1.36.3+k0s.2",
			APIVIP:          "192.168.100.200",
			RoleAssignments: threeControllersFourWorkers(),
		},
		RequestedBy: "admin",
	}
}

func TestDeploySucceedsAndBuildsTrustedVars(t *testing.T) {
	service, launcher, platforms := newDeployHarness(haServers()...)

	result, err := service.Deploy(context.Background(), validDeployInput())
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if result.OperationID != "operation-1" {
		t.Errorf("expected operation id from launcher, got %q", result.OperationID)
	}
	if _, ok := platforms.platforms[result.PlatformID]; !ok {
		t.Errorf("platform was not created")
	}
	if launcher.launched == nil {
		t.Fatal("launcher was not called")
	}

	vars := launcher.launched.TrustedVars
	if vars[varK0sInitialController] != "c1" {
		t.Errorf("expected first controller as initial controller, got %v", vars[varK0sInitialController])
	}
	roles, ok := vars[varK0sRoles].(map[string]any)
	if !ok || roles["c1"] != "control-plane" || roles["w1"] != "worker" {
		t.Errorf("unexpected role map: %v", vars[varK0sRoles])
	}
	if vars[varK0sPodCIDR] != defaultPodCIDR || vars[varK0sServiceCIDR] != defaultServiceCIDR {
		t.Errorf("expected default CIDRs, got pod=%v service=%v", vars[varK0sPodCIDR], vars[varK0sServiceCIDR])
	}
	if vars[varK0sHighAvailability] != true || vars[varK0sAPIAddress] != "192.168.100.200" {
		t.Errorf("expected HA VIP endpoint vars, got ha=%v address=%v", vars[varK0sHighAvailability], vars[varK0sAPIAddress])
	}
	if vars[varK0sAPIVIPPrefix] != defaultAPIVIPPrefix {
		t.Errorf("expected default VIP prefix %d, got %v", defaultAPIVIPPrefix, vars[varK0sAPIVIPPrefix])
	}
	if _, ok := launcher.launched.SecretVars[varK0sVRRPAuthPass].(string); !ok {
		t.Errorf("expected a generated VRRP password in secret vars")
	}
}

func TestDeployRejectsEvenControllerCount(t *testing.T) {
	service, _, platforms := newDeployHarness(haServers()...)
	input := validDeployInput()
	input.Spec.RoleAssignments = []platformdomain.RoleAssignment{
		{ServerID: "c1", Role: platformdomain.NodeRoleControlPlane},
		{ServerID: "c2", Role: platformdomain.NodeRoleControlPlane},
		{ServerID: "w1", Role: platformdomain.NodeRoleWorker},
	}

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("expected invalid deployment, got %v", err)
	}
	if len(platforms.platforms) != 0 {
		t.Errorf("no platform should be created on validation failure, got %d", len(platforms.platforms))
	}
}

func TestDeployRejectsPodCIDRCoveringNode(t *testing.T) {
	service, _, _ := newDeployHarness(haServers()...)
	input := validDeployInput()
	// A podCIDR that swallows the 192.168.100.0/24 node subnet is the knowledge-base trap.
	input.Spec.PodCIDR = "192.168.0.0/16"

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("expected invalid deployment for overlapping podCidr, got %v", err)
	}
}

func TestDeployRejectsVIPThatIsANodeAddress(t *testing.T) {
	service, _, _ := newDeployHarness(haServers()...)
	input := validDeployInput()
	input.Spec.APIVIP = "192.168.100.2" // c1's address

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("expected invalid deployment for VIP colliding with a node, got %v", err)
	}
}

func TestDeployRejectsUndeployedTarget(t *testing.T) {
	servers := haServers()
	servers[3].Provisioning.State = "ready" // worker not yet deployed
	service, _, _ := newDeployHarness(servers...)

	_, err := service.Deploy(context.Background(), validDeployInput())
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("expected invalid deployment for undeployed target, got %v", err)
	}
}

func TestDeployRejectsTargetWithObservedMembership(t *testing.T) {
	servers := haServers()
	servers[0].Membership = &serverdomain.MembershipStatus{
		PlatformID: "registered-k8s",
		NodeName:   "lab-control-1",
		Role:       "control-plane",
		State:      "ready",
	}
	service, launcher, platforms := newDeployHarness(servers...)
	platforms.platforms["registered-k8s"] = &platformdomain.Platform{
		ID: "registered-k8s", SiteID: "site-1", Name: "existing-k8s",
		Type: platformdomain.PlatformTypeKubernetes,
	}

	_, err := service.Deploy(context.Background(), validDeployInput())
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("Deploy() error = %v, want ErrInvalidDeployment", err)
	}
	if launcher.launched != nil {
		t.Error("Deploy() launched automation for a Server with observed membership")
	}
}

func TestDeployReservesTargetsUntilSuccessfulUninstall(t *testing.T) {
	tests := []struct {
		name       string
		state      platformdomain.PlatformLifecycleState
		wantReject bool
	}{
		{name: "deploying", state: platformdomain.PlatformLifecycleDeploying, wantReject: true},
		{name: "deployment failed", state: platformdomain.PlatformLifecycleDeployFailed, wantReject: true},
		{name: "active", state: platformdomain.PlatformLifecycleActive, wantReject: true},
		{name: "uninstalling", state: platformdomain.PlatformLifecycleUninstalling, wantReject: true},
		{name: "uninstall failed", state: platformdomain.PlatformLifecycleUninstallFailed, wantReject: true},
		{name: "uninstalled", state: platformdomain.PlatformLifecycleUninstalled, wantReject: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			existing := &platformdomain.Platform{
				ID: "existing-platform", SiteID: "site-1", Name: "existing-k8s",
				Type: platformdomain.PlatformTypeKubernetes,
			}
			lifecycle := &deployFakeLifecycleReader{
				snapshots: map[string]platformdomain.LifecycleSnapshot{
					existing.ID: {
						Origin: platformdomain.PlatformOriginDeployed,
						State:  test.state,
						Deployment: &platformdomain.LifecycleOperation{
							ID:              "existing-deployment",
							Status:          "failed",
							TargetServerIDs: []string{"c1"},
						},
					},
				},
			}
			service, launcher, platforms := newDeployHarnessWithLifecycle(lifecycle, haServers()...)
			platforms.platforms[existing.ID] = existing

			_, err := service.Deploy(context.Background(), validDeployInput())
			if test.wantReject {
				if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
					t.Fatalf("Deploy() error = %v, want ErrInvalidDeployment", err)
				}
				if launcher.launched != nil {
					t.Error("Deploy() launched automation for a claimed Server")
				}
				return
			}
			if err != nil {
				t.Fatalf("Deploy() error = %v, want nil after successful uninstall", err)
			}
			if launcher.launched == nil {
				t.Error("Deploy() did not release targets after successful uninstall")
			}
		})
	}
}

type recordingMutationGuard struct {
	err       error
	serverIDs []string
}

func (g *recordingMutationGuard) RequireUnlocked(_ context.Context, serverIDs []string) error {
	g.serverIDs = append([]string(nil), serverIDs...)
	return g.err
}

func TestDeployLiveLockGuardRunsBeforePlatformOrOperationCreation(t *testing.T) {
	service, launcher, platforms := newDeployHarness(haServers()...)
	guard := &recordingMutationGuard{
		err: &serverdomain.ServerLockedError{Name: "lab-control-2"},
	}
	service.protection = guard

	_, err := service.Deploy(context.Background(), validDeployInput())
	if !errors.Is(err, serverdomain.ErrServerLocked) {
		t.Fatalf("Deploy() error = %v, want ErrServerLocked", err)
	}
	if len(guard.serverIDs) != 4 {
		t.Fatalf("guard targets = %v, want complete target set", guard.serverIDs)
	}
	if len(platforms.platforms) != 0 {
		t.Fatalf("live lock conflict created %d Platform records", len(platforms.platforms))
	}
	if launcher.launched != nil {
		t.Fatal("live lock conflict reached the Operation launcher")
	}
}
