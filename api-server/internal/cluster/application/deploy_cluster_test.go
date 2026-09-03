package application

import (
	"context"
	"errors"
	"testing"
	"time"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// The deploy validation is the load-bearing part of cluster deployment: a topology or
// network mistake produces a cluster that cannot form, so it is checked before anything is
// created. These tests exercise it against in-memory fakes.

type deployFakeClusterRepo struct {
	clusters          map[string]*clusterdomain.Cluster
	syncClusterID     string
	syncIntegrationID string
	syncState         clusterdomain.SyncState
}

func (r *deployFakeClusterRepo) Create(_ context.Context, cluster *clusterdomain.Cluster) error {
	for _, existing := range r.clusters {
		if existing.SiteID == cluster.SiteID && existing.Name == cluster.Name {
			return clusterdomain.ErrClusterNameTaken
		}
	}
	r.clusters[cluster.ID] = cluster
	return nil
}
func (r *deployFakeClusterRepo) FindByID(_ context.Context, id string) (*clusterdomain.Cluster, error) {
	if c, ok := r.clusters[id]; ok {
		return c, nil
	}
	return nil, clusterdomain.ErrClusterNotFound
}
func (r *deployFakeClusterRepo) List(_ context.Context, siteID string) ([]*clusterdomain.Cluster, error) {
	clusters := make([]*clusterdomain.Cluster, 0, len(r.clusters))
	for _, cluster := range r.clusters {
		if siteID == "" || cluster.SiteID == siteID {
			clusters = append(clusters, cluster)
		}
	}
	return clusters, nil
}
func (r *deployFakeClusterRepo) Update(_ context.Context, cluster *clusterdomain.Cluster) error {
	r.clusters[cluster.ID] = cluster
	return nil
}
func (r *deployFakeClusterRepo) UpdateSyncState(
	_ context.Context, id, integrationID string, state clusterdomain.SyncState,
) error {
	cluster, ok := r.clusters[id]
	if !ok || cluster.IntegrationID != integrationID {
		return clusterdomain.ErrClusterNotFound
	}
	cluster.Sync = state
	r.syncClusterID = id
	r.syncIntegrationID = integrationID
	r.syncState = state
	return nil
}
func (r *deployFakeClusterRepo) Delete(_ context.Context, id string) error {
	delete(r.clusters, id)
	return nil
}

type deployFakeLifecycleReader struct {
	snapshots map[string]clusterdomain.LifecycleSnapshot
}

func (r *deployFakeLifecycleReader) Read(
	_ context.Context,
	clusterIDs []string,
) (map[string]clusterdomain.LifecycleSnapshot, error) {
	result := make(map[string]clusterdomain.LifecycleSnapshot, len(clusterIDs))
	for _, clusterID := range clusterIDs {
		snapshot, ok := r.snapshots[clusterID]
		if !ok {
			snapshot = clusterdomain.LifecycleSnapshot{
				Origin: clusterdomain.ClusterOriginRegistered,
				State:  clusterdomain.ClusterLifecycleRegistered,
			}
		}
		result[clusterID] = snapshot
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
	launched *clusterdomain.DeploymentLaunch
}

func (l *recordingLauncher) Launch(_ context.Context, launch clusterdomain.DeploymentLaunch) (string, error) {
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

func newDeployHarness(servers ...*serverdomain.Server) (*DeployService, *recordingLauncher, *deployFakeClusterRepo) {
	return newDeployHarnessWithLifecycle(
		&deployFakeLifecycleReader{snapshots: map[string]clusterdomain.LifecycleSnapshot{}},
		servers...,
	)
}

func newDeployHarnessWithLifecycle(
	lifecycle clusterdomain.LifecycleReader,
	servers ...*serverdomain.Server,
) (*DeployService, *recordingLauncher, *deployFakeClusterRepo) {
	clusterRepo := &deployFakeClusterRepo{clusters: map[string]*clusterdomain.Cluster{}}
	serverRepo := &deployFakeServerRepo{servers: map[string]*serverdomain.Server{}}
	for _, s := range servers {
		serverRepo.servers[s.ID] = s
	}
	siteRepo := &deployFakeSiteRepo{ids: map[string]bool{"site-1": true}}
	clusterService := NewClusterService(clusterRepo, siteRepo, serverRepo, nil, nil)
	launcher := &recordingLauncher{}
	return NewDeployService(clusterService, clusterRepo, serverRepo, lifecycle, launcher), launcher, clusterRepo
}

func threeControllersFourWorkers() []clusterdomain.RoleAssignment {
	return []clusterdomain.RoleAssignment{
		{ServerID: "c1", Role: clusterdomain.NodeRoleControlPlane},
		{ServerID: "c2", Role: clusterdomain.NodeRoleControlPlane},
		{ServerID: "c3", Role: clusterdomain.NodeRoleControlPlane},
		{ServerID: "w1", Role: clusterdomain.NodeRoleWorker},
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

func validDeployInput() DeployClusterInput {
	return DeployClusterInput{
		SiteID:        "site-1",
		Name:          "lab-k0s",
		GPUStackOwner: "provisioning",
		Spec: clusterdomain.DeploymentSpec{
			K0sVersion:      "v1.36.3+k0s.2",
			APIVIP:          "192.168.100.200",
			RoleAssignments: threeControllersFourWorkers(),
		},
		RequestedBy: "admin",
	}
}

func TestDeploySucceedsAndBuildsTrustedVars(t *testing.T) {
	service, launcher, clusters := newDeployHarness(haServers()...)

	result, err := service.Deploy(context.Background(), validDeployInput())
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if result.OperationID != "operation-1" {
		t.Errorf("expected operation id from launcher, got %q", result.OperationID)
	}
	if _, ok := clusters.clusters[result.ClusterID]; !ok {
		t.Errorf("cluster was not created")
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
	service, _, clusters := newDeployHarness(haServers()...)
	input := validDeployInput()
	input.Spec.RoleAssignments = []clusterdomain.RoleAssignment{
		{ServerID: "c1", Role: clusterdomain.NodeRoleControlPlane},
		{ServerID: "c2", Role: clusterdomain.NodeRoleControlPlane},
		{ServerID: "w1", Role: clusterdomain.NodeRoleWorker},
	}

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, clusterdomain.ErrInvalidDeployment) {
		t.Fatalf("expected invalid deployment, got %v", err)
	}
	if len(clusters.clusters) != 0 {
		t.Errorf("no cluster should be created on validation failure, got %d", len(clusters.clusters))
	}
}

func TestDeployRejectsPodCIDRCoveringNode(t *testing.T) {
	service, _, _ := newDeployHarness(haServers()...)
	input := validDeployInput()
	// A podCIDR that swallows the 192.168.100.0/24 node subnet is the knowledge-base trap.
	input.Spec.PodCIDR = "192.168.0.0/16"

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, clusterdomain.ErrInvalidDeployment) {
		t.Fatalf("expected invalid deployment for overlapping podCidr, got %v", err)
	}
}

func TestDeployRejectsVIPThatIsANodeAddress(t *testing.T) {
	service, _, _ := newDeployHarness(haServers()...)
	input := validDeployInput()
	input.Spec.APIVIP = "192.168.100.2" // c1's address

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, clusterdomain.ErrInvalidDeployment) {
		t.Fatalf("expected invalid deployment for VIP colliding with a node, got %v", err)
	}
}

func TestDeployRejectsUndeployedTarget(t *testing.T) {
	servers := haServers()
	servers[3].Provisioning.State = "ready" // worker not yet deployed
	service, _, _ := newDeployHarness(servers...)

	_, err := service.Deploy(context.Background(), validDeployInput())
	if !errors.Is(err, clusterdomain.ErrInvalidDeployment) {
		t.Fatalf("expected invalid deployment for undeployed target, got %v", err)
	}
}

func TestDeployRejectsTargetWithObservedMembership(t *testing.T) {
	servers := haServers()
	servers[0].Membership = &serverdomain.MembershipStatus{
		ClusterID: "registered-k8s",
		NodeName:  "lab-control-1",
		Role:      "control-plane",
		State:     "ready",
	}
	service, launcher, clusters := newDeployHarness(servers...)
	clusters.clusters["registered-k8s"] = &clusterdomain.Cluster{
		ID: "registered-k8s", SiteID: "site-1", Name: "existing-k8s",
		Type: clusterdomain.ClusterTypeKubernetes,
	}

	_, err := service.Deploy(context.Background(), validDeployInput())
	if !errors.Is(err, clusterdomain.ErrInvalidDeployment) {
		t.Fatalf("Deploy() error = %v, want ErrInvalidDeployment", err)
	}
	if launcher.launched != nil {
		t.Error("Deploy() launched automation for a Server with observed membership")
	}
}

func TestDeployReservesTargetsUntilSuccessfulUninstall(t *testing.T) {
	tests := []struct {
		name       string
		state      clusterdomain.ClusterLifecycleState
		wantReject bool
	}{
		{name: "deploying", state: clusterdomain.ClusterLifecycleDeploying, wantReject: true},
		{name: "deployment failed", state: clusterdomain.ClusterLifecycleDeployFailed, wantReject: true},
		{name: "active", state: clusterdomain.ClusterLifecycleActive, wantReject: true},
		{name: "uninstalling", state: clusterdomain.ClusterLifecycleUninstalling, wantReject: true},
		{name: "uninstall failed", state: clusterdomain.ClusterLifecycleUninstallFailed, wantReject: true},
		{name: "uninstalled", state: clusterdomain.ClusterLifecycleUninstalled, wantReject: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			existing := &clusterdomain.Cluster{
				ID: "existing-cluster", SiteID: "site-1", Name: "existing-k8s",
				Type: clusterdomain.ClusterTypeKubernetes,
			}
			lifecycle := &deployFakeLifecycleReader{
				snapshots: map[string]clusterdomain.LifecycleSnapshot{
					existing.ID: {
						Origin: clusterdomain.ClusterOriginDeployed,
						State:  test.state,
						Deployment: &clusterdomain.LifecycleOperation{
							ID:              "existing-deployment",
							Status:          "failed",
							TargetServerIDs: []string{"c1"},
						},
					},
				},
			}
			service, launcher, clusters := newDeployHarnessWithLifecycle(lifecycle, haServers()...)
			clusters.clusters[existing.ID] = existing

			_, err := service.Deploy(context.Background(), validDeployInput())
			if test.wantReject {
				if !errors.Is(err, clusterdomain.ErrInvalidDeployment) {
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

func TestDeployLiveLockGuardRunsBeforeClusterOrOperationCreation(t *testing.T) {
	service, launcher, clusters := newDeployHarness(haServers()...)
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
	if len(clusters.clusters) != 0 {
		t.Fatalf("live lock conflict created %d Cluster records", len(clusters.clusters))
	}
	if launcher.launched != nil {
		t.Fatal("live lock conflict reached the Operation launcher")
	}
}
