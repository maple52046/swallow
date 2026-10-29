package application

import (
	"context"
	"slices"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// claimServer returns a Server claimed by platformID (unclaimed when platformID is empty).
func claimServer(id, platformID string) *serverdomain.Server {
	server := &serverdomain.Server{ID: id}
	if platformID != "" {
		server.PlatformClaim = &serverdomain.PlatformClaim{PlatformID: platformID}
	}
	return server
}

func newClaimPlatformService(
	lifecycle platformdomain.LifecycleReader,
	platforms []*platformdomain.Platform,
	servers ...*serverdomain.Server,
) (*PlatformService, *lifecycleServerRepo) {
	platformRepo := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{}}
	for _, platform := range platforms {
		platformRepo.platforms[platform.ID] = platform
	}
	serverRepo := &lifecycleServerRepo{
		deployFakeServerRepo: &deployFakeServerRepo{servers: map[string]*serverdomain.Server{}},
		memberships:          map[string][]string{},
	}
	for _, server := range servers {
		serverRepo.servers[server.ID] = server
	}
	service := NewPlatformService(platformRepo, &deployFakeSiteRepo{}, serverRepo, lifecycle, &recordingIntegrationCleaner{})
	service.AttachPlatformClaims(serverRepo)
	return service, serverRepo
}

func claimOf(repo *lifecycleServerRepo, id string) string {
	if claim := repo.servers[id].PlatformClaim; claim != nil {
		return claim.PlatformID
	}
	return ""
}

// Deleting the record frees its Servers, and only its own: hosts are not changed, so nothing can
// uninstall through the gone record any more.
func TestDeleteReleasesThePlatformsClaims(t *testing.T) {
	platform := &platformdomain.Platform{ID: "platform-1", SiteID: "site-1", Name: "lab", Type: platformdomain.PlatformTypeKubernetes}
	lifecycle := fakeLifecycleReader{snapshots: map[string]platformdomain.LifecycleSnapshot{
		platform.ID: deployedLifecycle(platformdomain.PlatformLifecycleActive),
	}}
	service, servers := newClaimPlatformService(lifecycle, []*platformdomain.Platform{platform},
		claimServer("server-1", platform.ID), claimServer("server-9", "platform-9"))

	if err := service.Delete(context.Background(), platform.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := claimOf(servers, "server-1"); got != "" {
		t.Errorf("server-1 claim = %q after Delete, want none", got)
	}
	if got := claimOf(servers, "server-9"); got != "platform-9" {
		t.Errorf("server-9 claim = %q after deleting another Platform, want platform-9", got)
	}
}

// A successful uninstall frees the Servers, with or without deleting the record.
func TestCompleteUninstallReleasesThePlatformsClaims(t *testing.T) {
	for _, deleteRecord := range []bool{false, true} {
		platform := &platformdomain.Platform{ID: "platform-1", SiteID: "site-1", Name: "lab", Type: platformdomain.PlatformTypeKubernetes}
		service, servers := newClaimPlatformService(nil, []*platformdomain.Platform{platform}, claimServer("server-1", platform.ID))

		if err := service.CompleteUninstall(context.Background(), platform.ID, deleteRecord); err != nil {
			t.Fatalf("CompleteUninstall(deleteRecord=%v) error = %v", deleteRecord, err)
		}
		if got := claimOf(servers, "server-1"); got != "" {
			t.Errorf("CompleteUninstall(deleteRecord=%v) left claim %q, want none", deleteRecord, got)
		}
	}
}

// The startup reconciliation claims the targets of every Platform that is not uninstalled and has
// a deployment (Platforms deployed before claims existed), drops claims of uninstalled or deleted
// Platforms, keeps a fresh claim whose deployment Operation does not exist yet, and never takes a
// Server another Platform already claims.
func TestReconcilePlatformClaims(t *testing.T) {
	kubernetes := platformdomain.PlatformTypeKubernetes
	active := &platformdomain.Platform{ID: "active", SiteID: "site-1", Name: "active", Type: kubernetes}
	uninstalled := &platformdomain.Platform{ID: "uninstalled", SiteID: "site-1", Name: "gone", Type: kubernetes}
	fresh := &platformdomain.Platform{ID: "fresh", SiteID: "site-1", Name: "fresh", Type: kubernetes}
	activeLifecycle := deployedLifecycle(platformdomain.PlatformLifecycleDeployFailed)
	activeLifecycle.Deployment.TargetServerIDs = []string{"server-1", "server-2", "server-contested", "server-deleted"}
	lifecycle := fakeLifecycleReader{snapshots: map[string]platformdomain.LifecycleSnapshot{
		active.ID:      activeLifecycle,
		uninstalled.ID: deployedLifecycle(platformdomain.PlatformLifecycleUninstalled),
		// fresh was accepted a moment ago: its claims exist but its lifecycle has no deployment yet.
		fresh.ID: {Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleDeploying},
	}}
	service, servers := newClaimPlatformService(lifecycle, []*platformdomain.Platform{active, uninstalled, fresh},
		claimServer("server-1", ""),
		claimServer("server-2", active.ID),
		claimServer("server-contested", fresh.ID),
		claimServer("server-old", uninstalled.ID),
		claimServer("server-orphan", "deleted-platform"),
		claimServer("server-fresh", fresh.ID),
	)

	if err := service.ReconcilePlatformClaims(context.Background()); err != nil {
		t.Fatalf("ReconcilePlatformClaims() error = %v", err)
	}
	want := map[string]string{
		"server-1":         active.ID,
		"server-2":         active.ID,
		"server-contested": fresh.ID,
		"server-old":       "",
		"server-orphan":    "",
		"server-fresh":     fresh.ID,
	}
	for id, platformID := range want {
		if got := claimOf(servers, id); got != platformID {
			t.Errorf("%s claim = %q, want %q", id, got, platformID)
		}
	}
}

// recordingExporterAssignments records the Servers whose exporter records were marked removed.
type recordingExporterAssignments struct{ removed []string }

func (r *recordingExporterAssignments) MarkDeviceMetricsExporterRemoved(_ context.Context, serverIDs []string) error {
	r.removed = append(r.removed, serverIDs...)
	return nil
}

// uninstall-slurm removes the Device Metrics Exporter from every target, so a successful Slurm
// uninstall retires those Servers' records (decision 068), named by the claims it is about to
// release. A Kubernetes uninstall leaves a standalone exporter alone.
func TestCompleteUninstallRetiresASlurmPlatformsExporterRecords(t *testing.T) {
	for _, platformType := range []platformdomain.PlatformType{platformdomain.PlatformTypeSlurm, platformdomain.PlatformTypeKubernetes} {
		platform := &platformdomain.Platform{ID: "platform-1", SiteID: "site-1", Name: "lab", Type: platformType}
		service, _ := newClaimPlatformService(nil, []*platformdomain.Platform{platform},
			claimServer("server-1", platform.ID), claimServer("server-2", platform.ID), claimServer("server-9", "platform-9"))
		exporters := &recordingExporterAssignments{}
		service.AttachDeviceMetricsExporterAssignments(exporters)

		if err := service.CompleteUninstall(context.Background(), platform.ID, false); err != nil {
			t.Fatalf("CompleteUninstall(%s) error = %v", platformType, err)
		}
		want := []string{"server-1", "server-2"}
		if platformType != platformdomain.PlatformTypeSlurm {
			want = nil
		}
		if !slices.Equal(exporters.removed, want) {
			t.Errorf("%s uninstall retired exporter records of %v, want %v", platformType, exporters.removed, want)
		}
	}
}
