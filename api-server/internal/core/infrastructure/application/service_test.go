package application

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
)

type serviceFixture struct {
	service    *GroupingService
	zones      *fakeZoneRepo
	pools      *fakePoolRepo
	placements *fakePlacements
	provider   *fakeGroupingProvider
	references fakeReferences
	// platformServers maps a Platform id onto its Servers; cleaned records the Pools whose
	// memberships were deleted, and tx counts the transactions the service started.
	platformServers fakePlatformServers
	cleaned         *fakePoolAccess
	tx              *storetest.Transactor
}

type fakePlatformServers map[string][]string

func (p fakePlatformServers) ServersOf(_ context.Context, platformID string) ([]string, bool, error) {
	ids, ok := p[platformID]
	return slices.Clone(ids), ok, nil
}

type fakePoolAccess struct{ deleted []string }

func (a *fakePoolAccess) DeletePool(_ context.Context, poolID string) error {
	a.deleted = append(a.deleted, poolID)
	return nil
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	f := &serviceFixture{
		zones: newFakeZoneRepo(), pools: newFakePoolRepo(),
		placements:      &fakePlacements{current: &infradomain.ServerPlacement{ServerID: "srv-1", SiteID: "site-a", Generation: 1}},
		provider:        newFakeGroupingProvider(),
		references:      fakeReferences{zones: map[string]int{}, pools: map[string]int{}},
		platformServers: fakePlatformServers{}, cleaned: &fakePoolAccess{}, tx: &storetest.Transactor{},
	}
	factory := &fakeFactory{providers: map[string]provisioningdomain.OSProvisioningProvider{"maas-a": f.provider}}
	f.service = NewGroupingService(f.zones, f.pools, &fakeSiteReader{exists: true}, f.placements, f.references, factory,
		f.platformServers, f.cleaned, f.tx)
	f.service.now = func() time.Time { return time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC) }
	ids := 0
	f.service.newID = func() string { ids++; return "id-" + string(rune('0'+ids)) }
	return f
}

// Creating a Zone only stores its spec: the catalog controller creates it on the provisioners, so
// the API never waits for, or fails on, a provisioner.
func TestCreateZone_StoresTheSpecWithoutTheProvisioner(t *testing.T) {
	f := newServiceFixture(t)

	item, err := f.service.CreateZone(context.Background(), CreateGroupInput{SiteID: "site-a", Name: " rack-a ", Description: "top"})
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if item.Generation != 1 || item.Spec.Name != "rack-a" || item.Spec.Description != "top" {
		t.Errorf("created = %+v, want generation 1 with the trimmed name", item)
	}
	if len(item.Status.Conditions) != 0 || len(item.Status.Provisioners) != 0 {
		t.Errorf("a new Zone has no judgement yet, got %+v", item.Status)
	}
	if len(f.provider.calls) != 0 {
		t.Errorf("create drove the provisioner: %v", f.provider.calls)
	}
}

func TestCreateZone_SiteMustExistAndNameMustBeValid(t *testing.T) {
	f := newServiceFixture(t)
	f.service.sites = &fakeSiteReader{exists: false}
	if _, err := f.service.CreateZone(context.Background(), CreateGroupInput{SiteID: "nope", Name: "rack-a"}); !errors.Is(err, infradomain.ErrSiteNotFound) {
		t.Errorf("unknown Site: err = %v, want ErrSiteNotFound", err)
	}
	f.service.sites = &fakeSiteReader{exists: true}
	if _, err := f.service.CreateZone(context.Background(), CreateGroupInput{SiteID: "site-a", Name: "   "}); !errors.Is(err, infradomain.ErrInvalidGroup) {
		t.Errorf("blank name: err = %v, want ErrInvalidGroup", err)
	}
}

// A rename is a spec change (a new generation); a patch that changes nothing keeps the generation.
func TestUpdateZone_IncrementsTheGenerationOnlyOnChange(t *testing.T) {
	f := newServiceFixture(t)
	created, _ := f.service.CreateZone(context.Background(), CreateGroupInput{SiteID: "site-a", Name: "rack-a"})

	same := "rack-a"
	unchanged, err := f.service.UpdateZone(context.Background(), created.ID, UpdateGroupInput{Name: &same})
	if err != nil || unchanged.Generation != 1 {
		t.Fatalf("no-op update = %+v, %v; want generation 1", unchanged, err)
	}
	renamed := "rack-b"
	updated, err := f.service.UpdateZone(context.Background(), created.ID, UpdateGroupInput{Name: &renamed})
	if err != nil || updated.Generation != 2 || updated.Spec.Name != "rack-b" {
		t.Fatalf("rename = %+v, %v; want generation 2 named rack-b", updated, err)
	}
	if len(f.provider.calls) != 0 {
		t.Errorf("update drove the provisioner: %v", f.provider.calls)
	}
}

// Deleting removes the group from each provisioner that holds it before the record, keeps the
// provisioner's default group, and refuses while a Server is placed in it.
func TestDeleteZone_RemovesFromProvisionersThenTheRecord(t *testing.T) {
	f := newServiceFixture(t)
	f.provider.zones = append(f.provider.zones, provisioningdomain.ProviderGroup{Name: "rack-a"})
	f.zones.byID["z1"] = &infradomain.Zone{ID: "z1", SiteID: "site-a", Generation: 1, Name: "rack-a",
		Status: realizedOn("maas-a", "rack-a", 1)}
	f.zones.byID["z-default"] = &infradomain.Zone{ID: "z-default", SiteID: "site-a", Generation: 1, Name: "default",
		Status: realizedOn("maas-a", "default", 1)}

	f.references.zones["z1"] = 1
	if err := f.service.DeleteZone(context.Background(), "z1"); !errors.Is(err, infradomain.ErrGroupInUse) {
		t.Fatalf("delete of a placed Zone: err = %v, want ErrGroupInUse", err)
	}
	delete(f.references.zones, "z1")

	if err := f.service.DeleteZone(context.Background(), "z1"); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
	if f.zones.get("z1") != nil || len(f.provider.calls) != 1 || f.provider.calls[0] != "deleteZone rack-a" {
		t.Errorf("after delete: record %v, provider calls %v", f.zones.get("z1"), f.provider.calls)
	}
	if err := f.service.DeleteZone(context.Background(), "z-default"); err != nil {
		t.Fatalf("DeleteZone(default): %v", err)
	}
	if len(f.provider.calls) != 1 {
		t.Errorf("the provisioner's default zone must stay, calls %v", f.provider.calls)
	}
}

func TestDeleteZone_AbortsWhenTheProvisionerRefuses(t *testing.T) {
	f := newServiceFixture(t)
	f.zones.byID["z1"] = &infradomain.Zone{ID: "z1", SiteID: "site-a", Generation: 1, Name: "rack-a",
		Status: realizedOn("maas-a", "rack-a", 1)}
	f.provider.writeErr = &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorRejected, Detail: "zone has machines"}

	if err := f.service.DeleteZone(context.Background(), "z1"); err == nil {
		t.Fatal("a provisioner refusal must abort the delete")
	}
	if f.zones.get("z1") == nil {
		t.Error("the record must stay while the provisioner still holds the zone")
	}
}

func TestAssignServer_WritesThePlacementSpec(t *testing.T) {
	f := newServiceFixture(t)
	f.zones.byID["z1"] = &infradomain.Zone{ID: "z1", SiteID: "site-a", Name: "rack-a"}
	f.placements.current.PoolID = "p-old"

	result, err := f.service.AssignServer(context.Background(), AssignServerInput{
		ServerID: "srv-1", Zone: PlacementChange{Set: true, ID: "z1"}, Pool: PlacementChange{Set: true},
	})
	if err != nil {
		t.Fatalf("AssignServer: %v", err)
	}
	if result.Generation != 2 || result.ZoneID == nil || *result.ZoneID != "z1" || result.PoolID != nil {
		t.Errorf("result = %+v, want generation 2, zone z1, out of every Pool", result)
	}
	if len(f.provider.calls) != 0 {
		t.Errorf("placement drove the provisioner: %v", f.provider.calls)
	}
}

func TestAssignServer_Refusals(t *testing.T) {
	f := newServiceFixture(t)
	f.zones.byID["z-other"] = &infradomain.Zone{ID: "z-other", SiteID: "site-b", Name: "rack-x"}
	ctx := context.Background()

	if _, err := f.service.AssignServer(ctx, AssignServerInput{ServerID: "srv-1"}); !errors.Is(err, infradomain.ErrNothingToAssign) {
		t.Errorf("empty request: err = %v", err)
	}
	if _, err := f.service.AssignServer(ctx, AssignServerInput{ServerID: "srv-1", Zone: PlacementChange{Set: true, ID: "z-other"}}); !errors.Is(err, infradomain.ErrGroupingSiteMismatch) {
		t.Errorf("cross-Site zone: err = %v", err)
	}
	if _, err := f.service.AssignServer(ctx, AssignServerInput{ServerID: "missing", Zone: PlacementChange{Set: true}}); !errors.Is(err, infradomain.ErrServerNotFound) {
		t.Errorf("unknown Server: err = %v", err)
	}
	f.placements.current.Generation = 0
	if _, err := f.service.AssignServer(ctx, AssignServerInput{ServerID: "srv-1", Zone: PlacementChange{Set: true}}); !errors.Is(err, infradomain.ErrServerNotAdopted) {
		t.Errorf("not adopted: err = %v", err)
	}
}

// realizedOn is a status realized at generation on one provisioner as providerName.
func realizedOn(integrationID, providerName string, generation int64) infradomain.GroupStatus {
	at := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	conditions := []convergence.Condition{
		{Type: convergence.ConditionRealized, Status: convergence.ConditionTrue, Reason: convergence.ReasonConverged, LastTransitionTime: at, ObservedGeneration: generation},
		{Type: convergence.ConditionDrifted, Status: convergence.ConditionFalse, Reason: convergence.ReasonInSync, LastTransitionTime: at, ObservedGeneration: generation},
		{Type: convergence.ConditionRequiresAttention, Status: convergence.ConditionFalse, Reason: convergence.ReasonAttentionNotNeeded, LastTransitionTime: at, ObservedGeneration: generation},
	}
	return infradomain.GroupStatus{
		ObservedGeneration: generation,
		Conditions:         conditions,
		Provisioners: []infradomain.GroupRealization{{
			TargetStatus: convergence.TargetStatus{TargetID: integrationID, ObservedGeneration: generation, ObservedAt: &at, Conditions: conditions},
			ProviderName: providerName,
		}},
	}
}

// A Server a Platform claims keeps its Pool — no other Pool, and not out of every Pool — while its
// Zone may change, because its Pool changes only with the whole Platform. A claim whose Platform is
// gone holds nothing.
func TestAssignServer_KeepsAClaimedServerInItsPlatformsPool(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	f.pools.byID["p-a"] = &infradomain.Pool{ID: "p-a", Name: "a"}
	f.pools.byID["p-b"] = &infradomain.Pool{ID: "p-b", Name: "b"}
	f.placements.current.PoolID = "p-a"
	f.placements.current.PlatformID = "plat-1"
	f.platformServers["plat-1"] = []string{"srv-1"}

	for name, pool := range map[string]PlacementChange{
		"another Pool":      {Set: true, ID: "p-b"},
		"out of every Pool": {Set: true},
	} {
		if _, err := f.service.AssignServer(ctx, AssignServerInput{ServerID: "srv-1", Pool: pool}); !errors.Is(err, infradomain.ErrClaimedServerPool) {
			t.Errorf("moving a claimed Server %s: err = %v, want ErrClaimedServerPool", name, err)
		}
	}
	if len(f.placements.writes) != 0 {
		t.Errorf("a refused placement wrote %v", f.placements.writes)
	}
	if _, err := f.service.AssignServer(ctx, AssignServerInput{ServerID: "srv-1", Zone: PlacementChange{Set: true}, Pool: PlacementChange{Set: true, ID: "p-a"}}); err != nil {
		t.Errorf("changing only the Zone of a claimed Server: %v", err)
	}
	delete(f.platformServers, "plat-1")
	if _, err := f.service.AssignServer(ctx, AssignServerInput{ServerID: "srv-1", Pool: PlacementChange{Set: true, ID: "p-b"}}); err != nil {
		t.Errorf("moving a Server whose claiming Platform no longer exists: %v", err)
	}
}

// Moving a Platform writes the Pool of each of its Servers once, in one transaction, keeping their
// Zones; the result lists every Server in id order, and moving it again writes nothing.
func TestMovePlatformToPool_MovesEveryServerTogether(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	f.pools.byID["p-b"] = &infradomain.Pool{ID: "p-b", Name: "b"}
	f.placements.current = &infradomain.ServerPlacement{ServerID: "srv-1", Generation: 3, ZoneID: "z-1", PoolID: "p-a", PlatformID: "plat-1"}
	f.placements.others = map[string]*infradomain.ServerPlacement{
		"srv-2": {ServerID: "srv-2", Generation: 1, PlatformID: "plat-1"},
		"srv-3": {ServerID: "srv-3", Generation: 5, PoolID: "p-b", PlatformID: "plat-1"},
	}
	// The claims and the spec's members overlap and come in no order.
	f.platformServers["plat-1"] = []string{"srv-2", "srv-1", "srv-3", "srv-1"}

	result, err := f.service.MovePlatformToPool(ctx, MovePlatformInput{PlatformID: "plat-1", PoolID: "p-b"})
	if err != nil {
		t.Fatalf("MovePlatformToPool: %v", err)
	}
	type row struct {
		id         string
		generation int64
		zone, pool string
	}
	var got []row
	for _, server := range result.Servers {
		got = append(got, row{server.ServerID, server.Generation, deref(server.ZoneID), deref(server.PoolID)})
	}
	want := []row{{"srv-1", 4, "z-1", "p-b"}, {"srv-2", 2, "", "p-b"}, {"srv-3", 5, "", "p-b"}}
	if !slices.Equal(got, want) {
		t.Errorf("servers = %+v, want %+v", got, want)
	}
	if result.PlatformID != "plat-1" || deref(result.PoolID) != "p-b" {
		t.Errorf("result = %+v, want Platform plat-1 in p-b", result)
	}
	if len(f.placements.writes) != 2 || slices.Contains(f.placements.inTx, false) || f.tx.Calls() != 1 {
		t.Errorf("writes = %d (in transaction: %v) in %d transactions, want 2 writes in one transaction",
			len(f.placements.writes), f.placements.inTx, f.tx.Calls())
	}

	if _, err := f.service.MovePlatformToPool(ctx, MovePlatformInput{PlatformID: "plat-1", PoolID: "p-b"}); err != nil {
		t.Fatalf("repeating the move: %v", err)
	}
	if len(f.placements.writes) != 2 {
		t.Errorf("repeating the move wrote %d more placements, want none", len(f.placements.writes)-2)
	}

	out, err := f.service.MovePlatformToPool(ctx, MovePlatformInput{PlatformID: "plat-1"})
	if err != nil {
		t.Fatalf("moving the Platform out of every Pool: %v", err)
	}
	if out.PoolID != nil || out.Servers[0].PoolID != nil {
		t.Errorf("out of every Pool: result = %+v, want null Pools", out)
	}
}

func TestMovePlatformToPool_Refusals(t *testing.T) {
	tests := []struct {
		name    string
		servers map[string][]string
		poolID  string
		adopted bool
		wantErr error
	}{
		{name: "unknown Platform", servers: map[string][]string{}, adopted: true, wantErr: infradomain.ErrPlatformNotFound},
		{name: "Platform without Servers", servers: map[string][]string{"plat-1": nil}, adopted: true, wantErr: infradomain.ErrPlatformHasNoServers},
		{name: "only Servers with no record", servers: map[string][]string{"plat-1": {"gone"}}, adopted: true, wantErr: infradomain.ErrPlatformHasNoServers},
		{name: "unknown Pool", servers: map[string][]string{"plat-1": {"srv-1"}}, poolID: "p-missing", adopted: true, wantErr: infradomain.ErrPoolNotFound},
		{name: "Server not adopted yet", servers: map[string][]string{"plat-1": {"srv-1"}}, wantErr: infradomain.ErrServerNotAdopted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newServiceFixture(t)
			for id, servers := range tc.servers {
				f.platformServers[id] = servers
			}
			if !tc.adopted {
				f.placements.current.Generation = 0
			}
			_, err := f.service.MovePlatformToPool(context.Background(), MovePlatformInput{PlatformID: "plat-1", PoolID: tc.poolID})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("MovePlatformToPool error = %v, want %v", err, tc.wantErr)
			}
			if len(f.placements.writes) != 0 {
				t.Errorf("a refused move wrote %v", f.placements.writes)
			}
		})
	}
}

// deref is the value of an optional id, "" for none.
func deref(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

// Deleting a Pool also deletes its memberships and Sudo Policy, refuses while a Server is placed in
// it, and never calls a provisioner.
func TestDeletePool_RemovesItsAccess(t *testing.T) {
	f := newServiceFixture(t)
	f.pools.byID["p-a"] = &infradomain.Pool{ID: "p-a", Name: "a"}

	f.references.pools["p-a"] = 1
	if err := f.service.DeletePool(context.Background(), "p-a"); !errors.Is(err, infradomain.ErrGroupInUse) {
		t.Fatalf("delete of a placed Pool: err = %v, want ErrGroupInUse", err)
	}
	delete(f.references.pools, "p-a")

	if err := f.service.DeletePool(context.Background(), "p-a"); err != nil {
		t.Fatalf("DeletePool: %v", err)
	}
	if len(f.cleaned.deleted) != 1 || f.cleaned.deleted[0] != "p-a" {
		t.Errorf("access cleaned for %v, want p-a", f.cleaned.deleted)
	}
	if _, err := f.pools.FindByID(context.Background(), "p-a"); !errors.Is(err, infradomain.ErrPoolNotFound) {
		t.Errorf("the Pool record must be gone: err = %v", err)
	}
	if len(f.provider.calls) != 0 {
		t.Errorf("deleting a Pool drove the provisioner: %v", f.provider.calls)
	}
}

// A Pool belongs to the installation: its name is unique across it, it names no Site, and nothing
// reaches a provisioner.
func TestCreatePool_IsInstallationWide(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()

	created, err := f.service.CreatePool(ctx, CreatePoolInput{Name: " research ", Description: "GPU team"})
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	if created.Name != "research" || created.Description != "GPU team" || created.CreatedAt == "" {
		t.Errorf("created = %+v, want the trimmed name with its description", created)
	}
	if _, err := f.service.CreatePool(ctx, CreatePoolInput{Name: "research"}); !errors.Is(err, infradomain.ErrPoolNameTaken) {
		t.Errorf("a second Pool named research: err = %v, want ErrPoolNameTaken", err)
	}
	if _, err := f.service.CreatePool(ctx, CreatePoolInput{Name: "  "}); !errors.Is(err, infradomain.ErrInvalidGroup) {
		t.Errorf("blank name: err = %v, want ErrInvalidGroup", err)
	}
	renamed := "research-2"
	updated, err := f.service.UpdatePool(ctx, created.ID, UpdateGroupInput{Name: &renamed})
	if err != nil || updated.ID != created.ID || updated.Name != "research-2" {
		t.Errorf("rename = %+v, %v; want the same Pool named research-2", updated, err)
	}
	if len(f.provider.calls) != 0 {
		t.Errorf("Pool writes drove the provisioner: %v", f.provider.calls)
	}
}

// A Pool may hold Servers of any Site; only a Zone must share the Server's Site.
func TestAssignServer_PoolOfAnySite(t *testing.T) {
	f := newServiceFixture(t)
	f.pools.byID["p-a"] = &infradomain.Pool{ID: "p-a", Name: "a"}

	result, err := f.service.AssignServer(context.Background(), AssignServerInput{
		ServerID: "srv-1", Pool: PlacementChange{Set: true, ID: "p-a"},
	})
	if err != nil {
		t.Fatalf("AssignServer: %v", err)
	}
	if result.PoolID == nil || *result.PoolID != "p-a" {
		t.Errorf("result = %+v, want pool p-a", result)
	}
	if _, err := f.service.AssignServer(context.Background(), AssignServerInput{
		ServerID: "srv-1", Pool: PlacementChange{Set: true, ID: "missing"},
	}); !errors.Is(err, infradomain.ErrPoolNotFound) {
		t.Errorf("unknown Pool: err = %v, want ErrPoolNotFound", err)
	}
}
