package application

import (
	"context"
	"slices"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
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
	service := NewPlatformService(&storetest.Transactor{}, platformRepo, &deployFakeSiteRepo{}, serverRepo, lifecycle, &recordingIntegrationCleaner{})
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
