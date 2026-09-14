package application

import (
	"context"
	"errors"
	"testing"
	"time"

	infradomain "github.com/maple52046/swallow/internal/infrastructure/domain"
)

// newTestService builds a GroupingService over in-memory fakes with a fixed clock and id, so
// created records are deterministic and no external system is needed.
func newTestService(
	zones *fakeZoneRepo,
	pools *fakePoolRepo,
	sites *fakeSiteReader,
	servers *fakeServerLocator,
	realizer *fakeRealizer,
) *GroupingService {
	svc := NewGroupingService(zones, pools, sites, servers, realizer)
	svc.now = func() time.Time { return time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) }
	svc.newID = func() string { return "generated-id" }
	return svc
}

func TestCreateZone_RealizesAndPersists(t *testing.T) {
	zones := newFakeZoneRepo()
	realizer := &fakeRealizer{realized: true}
	svc := newTestService(zones, newFakePoolRepo(), &fakeSiteReader{exists: true}, &fakeServerLocator{}, realizer)

	// A surrounding-whitespace name proves ValidateName trims before both storage and realize.
	item, err := svc.CreateZone(context.Background(), CreateGroupInput{SiteID: "site-1", Name: "  rack-a  ", Description: "d"})
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if item.Name != "rack-a" {
		t.Errorf("name: got %q, want rack-a", item.Name)
	}
	if !item.ProviderRealized {
		t.Errorf("providerRealized: got false, want true")
	}
	if realizer.ensuredZone != "rack-a" {
		t.Errorf("realizer ensured %q, want rack-a", realizer.ensuredZone)
	}
	if _, ok := zones.byID["generated-id"]; !ok {
		t.Errorf("zone was not persisted")
	}
}

func TestCreateZone_SwallowOnlyWhenProviderNotCapable(t *testing.T) {
	zones := newFakeZoneRepo()
	realizer := &fakeRealizer{realized: false}
	svc := newTestService(zones, newFakePoolRepo(), &fakeSiteReader{exists: true}, &fakeServerLocator{}, realizer)

	item, err := svc.CreateZone(context.Background(), CreateGroupInput{SiteID: "site-1", Name: "rack-a"})
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if item.ProviderRealized {
		t.Errorf("providerRealized: got true, want false for a site with no capable provisioner")
	}
	if _, ok := zones.byID["generated-id"]; !ok {
		t.Errorf("a swallow-only zone must still be persisted")
	}
}

func TestCreateZone_SiteMustExist(t *testing.T) {
	zones := newFakeZoneRepo()
	svc := newTestService(zones, newFakePoolRepo(), &fakeSiteReader{exists: false}, &fakeServerLocator{}, &fakeRealizer{})

	_, err := svc.CreateZone(context.Background(), CreateGroupInput{SiteID: "missing", Name: "rack-a"})
	if !errors.Is(err, infradomain.ErrSiteNotFound) {
		t.Fatalf("error: got %v, want ErrSiteNotFound", err)
	}
	if len(zones.byID) != 0 {
		t.Errorf("no zone should be created when the site is missing")
	}
}

func TestCreateZone_InvalidName(t *testing.T) {
	zones := newFakeZoneRepo()
	svc := newTestService(zones, newFakePoolRepo(), &fakeSiteReader{exists: true}, &fakeServerLocator{}, &fakeRealizer{})

	_, err := svc.CreateZone(context.Background(), CreateGroupInput{SiteID: "site-1", Name: "   "})
	if !errors.Is(err, infradomain.ErrInvalidGroup) {
		t.Fatalf("error: got %v, want ErrInvalidGroup", err)
	}
}

func TestCreateZone_ProviderErrorLeavesNoRecord(t *testing.T) {
	zones := newFakeZoneRepo()
	realizer := &fakeRealizer{opErr: errors.New("maas refused")}
	svc := newTestService(zones, newFakePoolRepo(), &fakeSiteReader{exists: true}, &fakeServerLocator{}, realizer)

	if _, err := svc.CreateZone(context.Background(), CreateGroupInput{SiteID: "site-1", Name: "rack-a"}); err == nil {
		t.Fatalf("CreateZone should fail when realization fails")
	}
	if len(zones.byID) != 0 {
		t.Errorf("no zone should be persisted when realization fails")
	}
}

func TestDeleteZone_AbortsWhenProviderRefuses(t *testing.T) {
	zones := newFakeZoneRepo()
	zones.byID["z1"] = &infradomain.Zone{ID: "z1", SiteID: "site-1", Name: "rack-a"}
	realizer := &fakeRealizer{opErr: errors.New("zone still has machines")}
	svc := newTestService(zones, newFakePoolRepo(), &fakeSiteReader{exists: true}, &fakeServerLocator{}, realizer)

	if err := svc.DeleteZone(context.Background(), "z1"); err == nil {
		t.Fatalf("DeleteZone should fail when the provider refuses")
	}
	if _, ok := zones.byID["z1"]; !ok {
		t.Errorf("the swallow record must remain when provider delete is refused")
	}
}

func TestAssignServer_NothingToAssign(t *testing.T) {
	svc := newTestService(newFakeZoneRepo(), newFakePoolRepo(), &fakeSiteReader{}, &fakeServerLocator{}, &fakeRealizer{})
	_, err := svc.AssignServer(context.Background(), AssignServerInput{ServerID: "s1"})
	if !errors.Is(err, infradomain.ErrNothingToAssign) {
		t.Fatalf("error: got %v, want ErrNothingToAssign", err)
	}
}

func TestAssignServer_RejectsCrossSite(t *testing.T) {
	zones := newFakeZoneRepo()
	zones.byID["z1"] = &infradomain.Zone{ID: "z1", SiteID: "other-site", Name: "rack-a"}
	servers := &fakeServerLocator{target: &infradomain.ServerPlacementTarget{
		ServerID: "s1", SiteID: "site-1", IntegrationID: "int-1", ProviderMachineID: "m1",
	}}
	realizer := &fakeRealizer{}
	svc := newTestService(zones, newFakePoolRepo(), &fakeSiteReader{}, servers, realizer)

	zoneID := "z1"
	_, err := svc.AssignServer(context.Background(), AssignServerInput{ServerID: "s1", ZoneID: &zoneID})
	if !errors.Is(err, infradomain.ErrGroupingSiteMismatch) {
		t.Fatalf("error: got %v, want ErrGroupingSiteMismatch", err)
	}
	if realizer.assignedZone != "" {
		t.Errorf("no provider assignment should happen on a cross-site reference")
	}
}

func TestAssignServer_AssignsZoneAndPool(t *testing.T) {
	zones := newFakeZoneRepo()
	zones.byID["z1"] = &infradomain.Zone{ID: "z1", SiteID: "site-1", Name: "rack-a"}
	pools := newFakePoolRepo()
	pools.byID["p1"] = &infradomain.Pool{ID: "p1", SiteID: "site-1", Name: "research"}
	servers := &fakeServerLocator{target: &infradomain.ServerPlacementTarget{
		ServerID: "s1", SiteID: "site-1", IntegrationID: "int-1", ProviderMachineID: "m1",
		ObservedZone: "default", ObservedPool: "default",
	}}
	realizer := &fakeRealizer{}
	svc := newTestService(zones, pools, &fakeSiteReader{}, servers, realizer)

	zoneID, poolID := "z1", "p1"
	result, err := svc.AssignServer(context.Background(), AssignServerInput{ServerID: "s1", ZoneID: &zoneID, PoolID: &poolID})
	if err != nil {
		t.Fatalf("AssignServer: %v", err)
	}
	if result.Zone != "rack-a" || result.Pool != "research" {
		t.Errorf("result: got zone=%q pool=%q, want rack-a/research", result.Zone, result.Pool)
	}
	if realizer.assignedZone != "rack-a" || realizer.assignedPool != "research" {
		t.Errorf("realizer: got zone=%q pool=%q, want rack-a/research", realizer.assignedZone, realizer.assignedPool)
	}
	if realizer.assignedMachine != "m1" || realizer.assignedIntegration != "int-1" {
		t.Errorf("realizer addressed integration=%q machine=%q, want int-1/m1",
			realizer.assignedIntegration, realizer.assignedMachine)
	}
}

func TestAssignServer_ServerNotFound(t *testing.T) {
	servers := &fakeServerLocator{err: infradomain.ErrServerNotFound}
	svc := newTestService(newFakeZoneRepo(), newFakePoolRepo(), &fakeSiteReader{}, servers, &fakeRealizer{})
	zoneID := "z1"
	_, err := svc.AssignServer(context.Background(), AssignServerInput{ServerID: "missing", ZoneID: &zoneID})
	if !errors.Is(err, infradomain.ErrServerNotFound) {
		t.Fatalf("error: got %v, want ErrServerNotFound", err)
	}
}

// --- fakes ---

type fakeZoneRepo struct {
	byID map[string]*infradomain.Zone
}

func newFakeZoneRepo() *fakeZoneRepo { return &fakeZoneRepo{byID: map[string]*infradomain.Zone{}} }

func (r *fakeZoneRepo) Create(_ context.Context, zone *infradomain.Zone) error {
	for _, existing := range r.byID {
		if existing.SiteID == zone.SiteID && existing.Name == zone.Name {
			return infradomain.ErrZoneNameTaken
		}
	}
	r.byID[zone.ID] = zone
	return nil
}

func (r *fakeZoneRepo) FindByID(_ context.Context, id string) (*infradomain.Zone, error) {
	zone, ok := r.byID[id]
	if !ok {
		return nil, infradomain.ErrZoneNotFound
	}
	return zone, nil
}

func (r *fakeZoneRepo) List(_ context.Context, siteID string) ([]*infradomain.Zone, error) {
	var out []*infradomain.Zone
	for _, zone := range r.byID {
		if siteID == "" || zone.SiteID == siteID {
			out = append(out, zone)
		}
	}
	return out, nil
}

func (r *fakeZoneRepo) Update(_ context.Context, zone *infradomain.Zone) error {
	if _, ok := r.byID[zone.ID]; !ok {
		return infradomain.ErrZoneNotFound
	}
	r.byID[zone.ID] = zone
	return nil
}

func (r *fakeZoneRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.byID[id]; !ok {
		return infradomain.ErrZoneNotFound
	}
	delete(r.byID, id)
	return nil
}

type fakePoolRepo struct {
	byID map[string]*infradomain.Pool
}

func newFakePoolRepo() *fakePoolRepo { return &fakePoolRepo{byID: map[string]*infradomain.Pool{}} }

func (r *fakePoolRepo) Create(_ context.Context, pool *infradomain.Pool) error {
	for _, existing := range r.byID {
		if existing.SiteID == pool.SiteID && existing.Name == pool.Name {
			return infradomain.ErrPoolNameTaken
		}
	}
	r.byID[pool.ID] = pool
	return nil
}

func (r *fakePoolRepo) FindByID(_ context.Context, id string) (*infradomain.Pool, error) {
	pool, ok := r.byID[id]
	if !ok {
		return nil, infradomain.ErrPoolNotFound
	}
	return pool, nil
}

func (r *fakePoolRepo) List(_ context.Context, siteID string) ([]*infradomain.Pool, error) {
	var out []*infradomain.Pool
	for _, pool := range r.byID {
		if siteID == "" || pool.SiteID == siteID {
			out = append(out, pool)
		}
	}
	return out, nil
}

func (r *fakePoolRepo) Update(_ context.Context, pool *infradomain.Pool) error {
	if _, ok := r.byID[pool.ID]; !ok {
		return infradomain.ErrPoolNotFound
	}
	r.byID[pool.ID] = pool
	return nil
}

func (r *fakePoolRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.byID[id]; !ok {
		return infradomain.ErrPoolNotFound
	}
	delete(r.byID, id)
	return nil
}

type fakeSiteReader struct {
	exists bool
	err    error
}

func (r *fakeSiteReader) Exists(_ context.Context, _ string) (bool, error) {
	return r.exists, r.err
}

type fakeServerLocator struct {
	target *infradomain.ServerPlacementTarget
	err    error
}

func (r *fakeServerLocator) Locate(_ context.Context, _ string) (*infradomain.ServerPlacementTarget, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.target, nil
}

// fakeRealizer records what it was asked to realize and returns configurable outcomes. realized
// and opErr drive the catalog operations; assign*Err drive placement.
type fakeRealizer struct {
	realized bool
	opErr    error

	ensuredZone string
	ensuredPool string

	assignZoneErr error
	assignPoolErr error

	assignedZone        string
	assignedPool        string
	assignedMachine     string
	assignedIntegration string
}

func (r *fakeRealizer) EnsureZone(_ context.Context, _, name, _ string) (bool, error) {
	r.ensuredZone = name
	return r.realized, r.opErr
}

func (r *fakeRealizer) RenameZone(_ context.Context, _, _, newName, _ string) (bool, error) {
	r.ensuredZone = newName
	return r.realized, r.opErr
}

func (r *fakeRealizer) DeleteZone(_ context.Context, _, _ string) (bool, error) {
	return r.realized, r.opErr
}

func (r *fakeRealizer) EnsurePool(_ context.Context, _, name, _ string) (bool, error) {
	r.ensuredPool = name
	return r.realized, r.opErr
}

func (r *fakeRealizer) RenamePool(_ context.Context, _, _, newName, _ string) (bool, error) {
	r.ensuredPool = newName
	return r.realized, r.opErr
}

func (r *fakeRealizer) DeletePool(_ context.Context, _, _ string) (bool, error) {
	return r.realized, r.opErr
}

func (r *fakeRealizer) AssignServerZone(_ context.Context, integrationID, machineID, zoneName string) error {
	r.assignedIntegration = integrationID
	r.assignedMachine = machineID
	r.assignedZone = zoneName
	return r.assignZoneErr
}

func (r *fakeRealizer) AssignServerPool(_ context.Context, integrationID, machineID, poolName string) error {
	r.assignedIntegration = integrationID
	r.assignedMachine = machineID
	r.assignedPool = poolName
	return r.assignPoolErr
}
