package application

import (
	"context"
	"errors"
	"testing"
	"time"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

type fakeLifecycleReader struct {
	snapshots map[string]clusterdomain.LifecycleSnapshot
}

func (r fakeLifecycleReader) Read(
	_ context.Context,
	clusterIDs []string,
) (map[string]clusterdomain.LifecycleSnapshot, error) {
	result := make(map[string]clusterdomain.LifecycleSnapshot, len(clusterIDs))
	for _, id := range clusterIDs {
		if snapshot, ok := r.snapshots[id]; ok {
			result[id] = snapshot
		}
	}
	return result, nil
}

type recordingUninstallLauncher struct {
	launch *clusterdomain.UninstallLaunch
	err    error
}

func (l *recordingUninstallLauncher) LaunchUninstall(
	_ context.Context,
	launch clusterdomain.UninstallLaunch,
) (string, error) {
	l.launch = &launch
	return "uninstall-operation", l.err
}

type recordingIntegrationCleaner struct {
	clusterID   string
	allowLegacy bool
}

func (c *recordingIntegrationCleaner) DeleteForCluster(
	_ context.Context,
	cluster *clusterdomain.Cluster,
	allowLegacy bool,
) error {
	c.clusterID = cluster.ID
	c.allowLegacy = allowLegacy
	return nil
}

type lifecycleServerRepo struct {
	*deployFakeServerRepo
	memberships map[string][]string
	cleared     []string
}

func (r *lifecycleServerRepo) List(
	_ context.Context,
	filter serverdomain.ListFilter,
) (serverdomain.ListResult, error) {
	ids := r.memberships[filter.ClusterID]
	servers := make([]*serverdomain.Server, 0, len(ids))
	for _, id := range ids {
		servers = append(servers, r.servers[id])
	}
	return serverdomain.ListResult{Servers: servers, Total: len(servers)}, nil
}

func (r *lifecycleServerRepo) SetMembership(
	_ context.Context,
	id string,
	membership *serverdomain.MembershipStatus,
) error {
	if membership == nil {
		r.cleared = append(r.cleared, id)
		r.servers[id].Membership = nil
	}
	return nil
}

func deployedLifecycle(
	state clusterdomain.ClusterLifecycleState,
) clusterdomain.LifecycleSnapshot {
	deploymentTime := time.Now().UTC().Add(-time.Hour)
	snapshot := clusterdomain.LifecycleSnapshot{
		Origin: clusterdomain.ClusterOriginDeployed,
		State:  state,
		Deployment: &clusterdomain.LifecycleOperation{
			ID: "deploy-operation", Status: "succeeded",
			TargetServerIDs: []string{"server-1", "server-2"},
			RequestedAt:     deploymentTime,
		},
		OperationID: "deploy-operation",
	}
	if state == clusterdomain.ClusterLifecycleUninstallFailed ||
		state == clusterdomain.ClusterLifecycleUninstalling ||
		state == clusterdomain.ClusterLifecycleUninstalled {
		snapshot.Uninstall = &clusterdomain.LifecycleOperation{
			ID: "failed-uninstall", Status: "failed",
			TargetServerIDs: []string{"server-1", "server-2"},
			RequestedAt:     deploymentTime.Add(time.Minute),
		}
		snapshot.OperationID = snapshot.Uninstall.ID
	}
	return snapshot
}

func newUninstallHarness(
	state clusterdomain.ClusterLifecycleState,
) (*UninstallService, *recordingUninstallLauncher, *deployFakeServerRepo, *clusterdomain.Cluster) {
	cluster := &clusterdomain.Cluster{
		ID: "cluster-1", SiteID: "site-1", Name: "lab",
		Type:          clusterdomain.ClusterTypeKubernetes,
		ExporterOwner: clusterdomain.ExporterOwnerK8s,
	}
	clusters := &deployFakeClusterRepo{
		clusters: map[string]*clusterdomain.Cluster{cluster.ID: cluster},
	}
	servers := &deployFakeServerRepo{servers: map[string]*serverdomain.Server{
		"server-1": deployedServer("server-1", "node-1", "site-1"),
		"server-2": deployedServer("server-2", "node-2", "site-1"),
	}}
	lifecycle := fakeLifecycleReader{snapshots: map[string]clusterdomain.LifecycleSnapshot{
		cluster.ID: deployedLifecycle(state),
	}}
	launcher := &recordingUninstallLauncher{}
	return NewUninstallService(clusters, servers, lifecycle, launcher), launcher, servers, cluster
}

func TestUninstallUsesDeploymentTargetSnapshot(t *testing.T) {
	service, launcher, _, _ := newUninstallHarness(clusterdomain.ClusterLifecycleActive)

	result, err := service.Uninstall(context.Background(), UninstallClusterInput{
		ClusterID: "cluster-1", RequestedBy: "admin",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if result.OperationID != "uninstall-operation" {
		t.Fatalf("operation id = %q", result.OperationID)
	}
	if launcher.launch == nil {
		t.Fatal("launcher was not called")
	}
	if got := launcher.launch.TargetServerIDs; len(got) != 2 ||
		got[0] != "server-1" || got[1] != "server-2" {
		t.Fatalf("targets = %v, want deployment snapshot", got)
	}
	if !launcher.launch.RestoreExporters {
		t.Error("k8s exporter owner must request independent host exporter restoration")
	}
	if launcher.launch.RetryOfOperationID != "" {
		t.Errorf("unexpected retry lineage %q", launcher.launch.RetryOfOperationID)
	}
}

func TestUninstallRetriesFailedOperationWithLineage(t *testing.T) {
	service, launcher, _, _ := newUninstallHarness(
		clusterdomain.ClusterLifecycleUninstallFailed,
	)

	if _, err := service.Uninstall(context.Background(), UninstallClusterInput{
		ClusterID: "cluster-1",
	}); err != nil {
		t.Fatalf("retry uninstall: %v", err)
	}
	if launcher.launch.RetryOfOperationID != "failed-uninstall" {
		t.Errorf("retryOfOperationId = %q", launcher.launch.RetryOfOperationID)
	}
}

func TestUninstallRejectsUnsafeTargetsAndStates(t *testing.T) {
	tests := []struct {
		name   string
		state  clusterdomain.ClusterLifecycleState
		mutate func(*deployFakeServerRepo, *clusterdomain.Cluster)
		want   error
	}{
		{
			name: "locked target", state: clusterdomain.ClusterLifecycleActive,
			mutate: func(servers *deployFakeServerRepo, _ *clusterdomain.Cluster) {
				servers.servers["server-1"].Provisioning.Locked = true
			},
			want: clusterdomain.ErrClusterUninstallConflict,
		},
		{
			name: "absent target", state: clusterdomain.ClusterLifecycleActive,
			mutate: func(servers *deployFakeServerRepo, _ *clusterdomain.Cluster) {
				servers.servers["server-1"].Absent = true
			},
			want: clusterdomain.ErrClusterUninstallConflict,
		},
		{
			name: "missing target", state: clusterdomain.ClusterLifecycleActive,
			mutate: func(servers *deployFakeServerRepo, _ *clusterdomain.Cluster) {
				delete(servers.servers, "server-2")
			},
			want: clusterdomain.ErrClusterUninstallConflict,
		},
		{
			name: "deployment running", state: clusterdomain.ClusterLifecycleDeploying,
			want: clusterdomain.ErrClusterUninstallConflict,
		},
		{
			name: "already uninstalled", state: clusterdomain.ClusterLifecycleUninstalled,
			want: clusterdomain.ErrClusterAlreadyUninstalled,
		},
		{
			name: "slurm", state: clusterdomain.ClusterLifecycleActive,
			mutate: func(_ *deployFakeServerRepo, cluster *clusterdomain.Cluster) {
				cluster.Type = clusterdomain.ClusterTypeSlurm
			},
			want: clusterdomain.ErrClusterNotDeployManaged,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, launcher, servers, cluster := newUninstallHarness(test.state)
			if test.mutate != nil {
				test.mutate(servers, cluster)
			}
			_, err := service.Uninstall(context.Background(), UninstallClusterInput{
				ClusterID: cluster.ID,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if launcher.launch != nil {
				t.Fatal("unsafe request reached launcher")
			}
		})
	}
}

func TestDeleteRemovesRecordMembershipAndOwnedIntegrationOnly(t *testing.T) {
	cluster := &clusterdomain.Cluster{
		ID: "cluster-1", SiteID: "site-1", Name: "lab",
		Type:               clusterdomain.ClusterTypeKubernetes,
		OwnedIntegrationID: "integration-owned",
	}
	clusters := &deployFakeClusterRepo{
		clusters: map[string]*clusterdomain.Cluster{cluster.ID: cluster},
	}
	servers := &lifecycleServerRepo{
		deployFakeServerRepo: &deployFakeServerRepo{servers: map[string]*serverdomain.Server{
			"server-1": {
				ID:         "server-1",
				Membership: &serverdomain.MembershipStatus{ClusterID: cluster.ID},
			},
		}},
		memberships: map[string][]string{cluster.ID: {"server-1"}},
	}
	lifecycle := fakeLifecycleReader{snapshots: map[string]clusterdomain.LifecycleSnapshot{
		cluster.ID: deployedLifecycle(clusterdomain.ClusterLifecycleUninstalling),
	}}
	cleaner := &recordingIntegrationCleaner{}
	service := NewClusterService(
		clusters, &deployFakeSiteRepo{}, servers, lifecycle, cleaner,
	)

	if err := service.Delete(context.Background(), cluster.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := clusters.clusters[cluster.ID]; ok {
		t.Error("cluster record remains")
	}
	if len(servers.cleared) != 1 || servers.cleared[0] != "server-1" {
		t.Errorf("cleared memberships = %v", servers.cleared)
	}
	if cleaner.clusterID != cluster.ID || !cleaner.allowLegacy {
		t.Errorf("cleaner call = cluster %q legacy=%v", cleaner.clusterID, cleaner.allowLegacy)
	}
}

func TestCompleteUninstallKeepsRecordAndClearsOwnedProjections(t *testing.T) {
	cluster := &clusterdomain.Cluster{
		ID: "cluster-1", SiteID: "site-1", Name: "lab",
		Type:          clusterdomain.ClusterTypeKubernetes,
		IntegrationID: "integration-owned", OwnedIntegrationID: "integration-owned",
		Sync: clusterdomain.SyncState{MemberCount: 1, MatchedCount: 1},
	}
	clusters := &deployFakeClusterRepo{
		clusters: map[string]*clusterdomain.Cluster{cluster.ID: cluster},
	}
	servers := &lifecycleServerRepo{
		deployFakeServerRepo: &deployFakeServerRepo{servers: map[string]*serverdomain.Server{
			"server-1": {
				ID:         "server-1",
				Membership: &serverdomain.MembershipStatus{ClusterID: cluster.ID},
			},
		}},
		memberships: map[string][]string{cluster.ID: {"server-1"}},
	}
	cleaner := &recordingIntegrationCleaner{}
	service := NewClusterService(
		clusters, &deployFakeSiteRepo{}, servers, nil, cleaner,
	)

	if err := service.CompleteUninstall(context.Background(), cluster.ID); err != nil {
		t.Fatalf("complete uninstall: %v", err)
	}
	persisted, ok := clusters.clusters[cluster.ID]
	if !ok {
		t.Fatal("successful uninstall deleted the cluster record")
	}
	if persisted.IntegrationID != "" || persisted.OwnedIntegrationID != "" {
		t.Fatalf("integration projection remains: %#v", persisted)
	}
	if persisted.Sync != (clusterdomain.SyncState{}) {
		t.Fatalf("sync projection remains: %#v", persisted.Sync)
	}
	if clusters.syncClusterID != cluster.ID || clusters.syncIntegrationID != "" ||
		clusters.syncState != (clusterdomain.SyncState{}) {
		t.Fatalf("sync cleanup write = cluster %q integration %q state %#v",
			clusters.syncClusterID, clusters.syncIntegrationID, clusters.syncState)
	}
	if len(servers.cleared) != 1 || servers.cleared[0] != "server-1" {
		t.Fatalf("cleared memberships = %v", servers.cleared)
	}
	if cleaner.clusterID != cluster.ID || !cleaner.allowLegacy {
		t.Fatalf("cleaner call = cluster %q legacy=%v", cleaner.clusterID, cleaner.allowLegacy)
	}
}

func TestCompleteUninstallAfterDeleteIsIdempotent(t *testing.T) {
	service := NewClusterService(
		&deployFakeClusterRepo{clusters: map[string]*clusterdomain.Cluster{}},
		&deployFakeSiteRepo{},
		&deployFakeServerRepo{servers: map[string]*serverdomain.Server{}},
		nil,
		nil,
	)
	if err := service.CompleteUninstall(context.Background(), "deleted-cluster"); err != nil {
		t.Fatalf("completion after delete: %v", err)
	}
}
