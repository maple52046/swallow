package application

import (
	"context"
	"errors"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

type fakeLifecycleReader struct {
	snapshots map[string]platformdomain.LifecycleSnapshot
}

func (r fakeLifecycleReader) Read(
	_ context.Context,
	platformIDs []string,
) (map[string]platformdomain.LifecycleSnapshot, error) {
	result := make(map[string]platformdomain.LifecycleSnapshot, len(platformIDs))
	for _, id := range platformIDs {
		if snapshot, ok := r.snapshots[id]; ok {
			result[id] = snapshot
		}
	}
	return result, nil
}

type recordingUninstallLauncher struct {
	launch *platformdomain.UninstallLaunch
	err    error
}

func (l *recordingUninstallLauncher) LaunchUninstall(
	_ context.Context,
	launch platformdomain.UninstallLaunch,
) (string, error) {
	l.launch = &launch
	return "uninstall-operation", l.err
}

type recordingIntegrationCleaner struct {
	platformID  string
	allowLegacy bool
}

func (c *recordingIntegrationCleaner) DeleteForPlatform(
	_ context.Context,
	platform *platformdomain.Platform,
	allowLegacy bool,
) error {
	c.platformID = platform.ID
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
	ids := r.memberships[filter.PlatformID]
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
	state platformdomain.PlatformLifecycleState,
) platformdomain.LifecycleSnapshot {
	deploymentTime := time.Now().UTC().Add(-time.Hour)
	snapshot := platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed,
		State:  state,
		Deployment: &platformdomain.LifecycleOperation{
			ID: "deploy-operation", Status: "succeeded",
			TargetServerIDs: []string{"server-1", "server-2"},
			RequestedAt:     deploymentTime,
		},
		OperationID: "deploy-operation",
	}
	if state == platformdomain.PlatformLifecycleUninstallFailed ||
		state == platformdomain.PlatformLifecycleUninstalling ||
		state == platformdomain.PlatformLifecycleUninstalled {
		snapshot.Uninstall = &platformdomain.LifecycleOperation{
			ID: "failed-uninstall", Status: "failed",
			TargetServerIDs: []string{"server-1", "server-2"},
			RequestedAt:     deploymentTime.Add(time.Minute),
		}
		snapshot.OperationID = snapshot.Uninstall.ID
	}
	return snapshot
}

func newUninstallHarness(
	state platformdomain.PlatformLifecycleState,
) (*UninstallService, *recordingUninstallLauncher, *deployFakeServerRepo, *platformdomain.Platform) {
	platform := &platformdomain.Platform{
		ID: "platform-1", SiteID: "site-1", Name: "lab",
		Type:          platformdomain.PlatformTypeKubernetes,
		ExporterOwner: platformdomain.ExporterOwnerK8s,
	}
	platforms := &deployFakePlatformRepo{
		platforms: map[string]*platformdomain.Platform{platform.ID: platform},
	}
	servers := &deployFakeServerRepo{servers: map[string]*serverdomain.Server{
		"server-1": deployedServer("server-1", "node-1", "site-1"),
		"server-2": deployedServer("server-2", "node-2", "site-1"),
	}}
	lifecycle := fakeLifecycleReader{snapshots: map[string]platformdomain.LifecycleSnapshot{
		platform.ID: deployedLifecycle(state),
	}}
	launcher := &recordingUninstallLauncher{}
	return NewUninstallService(platforms, servers, lifecycle, launcher), launcher, servers, platform
}

func TestUninstallUsesDeploymentTargetSnapshot(t *testing.T) {
	service, launcher, _, _ := newUninstallHarness(platformdomain.PlatformLifecycleActive)

	result, err := service.Uninstall(context.Background(), UninstallPlatformInput{
		PlatformID: "platform-1", RequestedBy: "admin",
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

func TestUninstallReleaseServersForwardsOptionsAndSkipsExporterRestore(t *testing.T) {
	service, launcher, _, _ := newUninstallHarness(platformdomain.PlatformLifecycleActive)

	_, err := service.Uninstall(context.Background(), UninstallPlatformInput{
		PlatformID: "platform-1", RequestedBy: "admin",
		ReleaseServers: true,
		ReleaseOptions: platformdomain.ServerReleaseOptions{
			Erase: true, SecureErase: true, UnbindStaticIPs: true,
		},
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if launcher.launch == nil {
		t.Fatal("launcher was not called")
	}
	if !launcher.launch.ReleaseServers {
		t.Error("ReleaseServers must be propagated to the launch")
	}
	if launcher.launch.ReleaseOptions.Erase != true ||
		launcher.launch.ReleaseOptions.SecureErase != true ||
		launcher.launch.ReleaseOptions.UnbindStaticIPs != true {
		t.Errorf("release options not forwarded: %+v", launcher.launch.ReleaseOptions)
	}
	// The k8s exporter owner would normally request exporter restoration, but releasing
	// wipes the hosts, so restoration must be turned off.
	if launcher.launch.RestoreExporters {
		t.Error("releasing servers must disable host exporter restoration")
	}
}

func TestUninstallRetriesFailedOperationWithLineage(t *testing.T) {
	service, launcher, _, _ := newUninstallHarness(
		platformdomain.PlatformLifecycleUninstallFailed,
	)

	if _, err := service.Uninstall(context.Background(), UninstallPlatformInput{
		PlatformID: "platform-1",
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
		state  platformdomain.PlatformLifecycleState
		mutate func(*deployFakeServerRepo, *platformdomain.Platform)
		want   error
	}{
		{
			name: "locked target", state: platformdomain.PlatformLifecycleActive,
			mutate: func(servers *deployFakeServerRepo, _ *platformdomain.Platform) {
				servers.servers["server-1"].Provisioning.Locked = true
			},
			want: serverdomain.ErrServerLocked,
		},
		{
			name: "absent target", state: platformdomain.PlatformLifecycleActive,
			mutate: func(servers *deployFakeServerRepo, _ *platformdomain.Platform) {
				servers.servers["server-1"].Absent = true
			},
			want: platformdomain.ErrPlatformUninstallConflict,
		},
		{
			name: "missing target", state: platformdomain.PlatformLifecycleActive,
			mutate: func(servers *deployFakeServerRepo, _ *platformdomain.Platform) {
				delete(servers.servers, "server-2")
			},
			want: platformdomain.ErrPlatformUninstallConflict,
		},
		{
			name: "deployment running", state: platformdomain.PlatformLifecycleDeploying,
			want: platformdomain.ErrPlatformUninstallConflict,
		},
		{
			name: "already uninstalled", state: platformdomain.PlatformLifecycleUninstalled,
			want: platformdomain.ErrPlatformAlreadyUninstalled,
		},
		{
			name: "slurm", state: platformdomain.PlatformLifecycleActive,
			mutate: func(_ *deployFakeServerRepo, platform *platformdomain.Platform) {
				platform.Type = platformdomain.PlatformTypeSlurm
			},
			want: platformdomain.ErrPlatformNotDeployManaged,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, launcher, servers, platform := newUninstallHarness(test.state)
			if test.mutate != nil {
				test.mutate(servers, platform)
			}
			_, err := service.Uninstall(context.Background(), UninstallPlatformInput{
				PlatformID: platform.ID,
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
	platform := &platformdomain.Platform{
		ID: "platform-1", SiteID: "site-1", Name: "lab",
		Type:               platformdomain.PlatformTypeKubernetes,
		OwnedIntegrationID: "integration-owned",
	}
	platforms := &deployFakePlatformRepo{
		platforms: map[string]*platformdomain.Platform{platform.ID: platform},
	}
	servers := &lifecycleServerRepo{
		deployFakeServerRepo: &deployFakeServerRepo{servers: map[string]*serverdomain.Server{
			"server-1": {
				ID:         "server-1",
				Membership: &serverdomain.MembershipStatus{PlatformID: platform.ID},
			},
		}},
		memberships: map[string][]string{platform.ID: {"server-1"}},
	}
	lifecycle := fakeLifecycleReader{snapshots: map[string]platformdomain.LifecycleSnapshot{
		platform.ID: deployedLifecycle(platformdomain.PlatformLifecycleUninstalling),
	}}
	cleaner := &recordingIntegrationCleaner{}
	service := NewPlatformService(
		platforms, &deployFakeSiteRepo{}, servers, lifecycle, cleaner,
	)

	if err := service.Delete(context.Background(), platform.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := platforms.platforms[platform.ID]; ok {
		t.Error("platform record remains")
	}
	if len(servers.cleared) != 1 || servers.cleared[0] != "server-1" {
		t.Errorf("cleared memberships = %v", servers.cleared)
	}
	if cleaner.platformID != platform.ID || !cleaner.allowLegacy {
		t.Errorf("cleaner call = platform %q legacy=%v", cleaner.platformID, cleaner.allowLegacy)
	}
}

// recordingPlatformOperationCanceler captures the platform whose operations Delete asked to
// cancel and can inject a failure to prove the delete aborts instead of orphaning work.
type recordingPlatformOperationCanceler struct {
	platformID string
	called     bool
	err        error
}

func (c *recordingPlatformOperationCanceler) CancelActiveForPlatform(_ context.Context, platformID string) error {
	c.called = true
	c.platformID = platformID
	return c.err
}

func newDeleteHarness() (*deployFakePlatformRepo, *lifecycleServerRepo, fakeLifecycleReader, *platformdomain.Platform) {
	platform := &platformdomain.Platform{
		ID: "platform-1", SiteID: "site-1", Name: "lab",
		Type: platformdomain.PlatformTypeKubernetes,
	}
	platforms := &deployFakePlatformRepo{
		platforms: map[string]*platformdomain.Platform{platform.ID: platform},
	}
	servers := &lifecycleServerRepo{
		deployFakeServerRepo: &deployFakeServerRepo{servers: map[string]*serverdomain.Server{}},
		memberships:          map[string][]string{},
	}
	lifecycle := fakeLifecycleReader{snapshots: map[string]platformdomain.LifecycleSnapshot{
		platform.ID: deployedLifecycle(platformdomain.PlatformLifecycleUninstalling),
	}}
	return platforms, servers, lifecycle, platform
}

func TestDeleteCancelsInFlightOperationsBeforeRemovingTheRecord(t *testing.T) {
	platforms, servers, lifecycle, platform := newDeleteHarness()
	canceler := &recordingPlatformOperationCanceler{}
	service := NewPlatformService(platforms, &deployFakeSiteRepo{}, servers, lifecycle, &recordingIntegrationCleaner{})
	service.AttachOperationCanceler(canceler)

	if err := service.Delete(context.Background(), platform.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !canceler.called || canceler.platformID != platform.ID {
		t.Errorf("canceler called=%v platform=%q, want true and %q", canceler.called, canceler.platformID, platform.ID)
	}
	if _, ok := platforms.platforms[platform.ID]; ok {
		t.Error("platform record remains after delete")
	}
}

func TestDeleteAbortsWhenOperationCancellationFails(t *testing.T) {
	platforms, servers, lifecycle, platform := newDeleteHarness()
	canceler := &recordingPlatformOperationCanceler{err: errors.New("temporal unavailable")}
	service := NewPlatformService(platforms, &deployFakeSiteRepo{}, servers, lifecycle, &recordingIntegrationCleaner{})
	service.AttachOperationCanceler(canceler)

	if err := service.Delete(context.Background(), platform.ID); err == nil {
		t.Fatal("expected delete to abort when operation cancellation fails")
	}
	// The record must survive so the operator can retry once cancellation is possible; a
	// deleted platform with still-running operations is exactly the orphaned state to avoid.
	if _, ok := platforms.platforms[platform.ID]; !ok {
		t.Error("platform record deleted despite cancellation failure")
	}
}

func TestCompleteUninstallKeepsRecordAndClearsOwnedProjections(t *testing.T) {
	platform := &platformdomain.Platform{
		ID: "platform-1", SiteID: "site-1", Name: "lab",
		Type:          platformdomain.PlatformTypeKubernetes,
		IntegrationID: "integration-owned", OwnedIntegrationID: "integration-owned",
		Sync: platformdomain.SyncState{MemberCount: 1, MatchedCount: 1},
	}
	platforms := &deployFakePlatformRepo{
		platforms: map[string]*platformdomain.Platform{platform.ID: platform},
	}
	servers := &lifecycleServerRepo{
		deployFakeServerRepo: &deployFakeServerRepo{servers: map[string]*serverdomain.Server{
			"server-1": {
				ID:         "server-1",
				Membership: &serverdomain.MembershipStatus{PlatformID: platform.ID},
			},
		}},
		memberships: map[string][]string{platform.ID: {"server-1"}},
	}
	cleaner := &recordingIntegrationCleaner{}
	service := NewPlatformService(
		platforms, &deployFakeSiteRepo{}, servers, nil, cleaner,
	)

	if err := service.CompleteUninstall(context.Background(), platform.ID); err != nil {
		t.Fatalf("complete uninstall: %v", err)
	}
	persisted, ok := platforms.platforms[platform.ID]
	if !ok {
		t.Fatal("successful uninstall deleted the platform record")
	}
	if persisted.IntegrationID != "" || persisted.OwnedIntegrationID != "" {
		t.Fatalf("integration projection remains: %#v", persisted)
	}
	if persisted.Sync != (platformdomain.SyncState{}) {
		t.Fatalf("sync projection remains: %#v", persisted.Sync)
	}
	if platforms.syncPlatformID != platform.ID || platforms.syncIntegrationID != "" ||
		platforms.syncState != (platformdomain.SyncState{}) {
		t.Fatalf("sync cleanup write = platform %q integration %q state %#v",
			platforms.syncPlatformID, platforms.syncIntegrationID, platforms.syncState)
	}
	if len(servers.cleared) != 1 || servers.cleared[0] != "server-1" {
		t.Fatalf("cleared memberships = %v", servers.cleared)
	}
	if cleaner.platformID != platform.ID || !cleaner.allowLegacy {
		t.Fatalf("cleaner call = platform %q legacy=%v", cleaner.platformID, cleaner.allowLegacy)
	}
}

func TestCompleteUninstallAfterDeleteIsIdempotent(t *testing.T) {
	service := NewPlatformService(
		&deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{}},
		&deployFakeSiteRepo{},
		&deployFakeServerRepo{servers: map[string]*serverdomain.Server{}},
		nil,
		nil,
	)
	if err := service.CompleteUninstall(context.Background(), "deleted-platform"); err != nil {
		t.Fatalf("completion after delete: %v", err)
	}
}

func TestUninstallLiveLockGuardRunsBeforeOperationCreation(t *testing.T) {
	service, launcher, _, _ := newUninstallHarness(platformdomain.PlatformLifecycleActive)
	guard := &recordingMutationGuard{
		err: &serverdomain.ServerLockedError{Name: "node-2"},
	}
	service.protection = guard

	_, err := service.Uninstall(context.Background(), UninstallPlatformInput{
		PlatformID: "platform-1",
	})
	if !errors.Is(err, serverdomain.ErrServerLocked) {
		t.Fatalf("Uninstall() error = %v, want ErrServerLocked", err)
	}
	if len(guard.serverIDs) != 2 {
		t.Fatalf("guard targets = %v, want deployment snapshot", guard.serverIDs)
	}
	if launcher.launch != nil {
		t.Fatal("live lock conflict reached the Operation launcher")
	}
}
