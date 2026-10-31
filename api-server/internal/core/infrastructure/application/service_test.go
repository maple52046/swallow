package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

type serviceFixture struct {
	service    *GroupingService
	zones      *fakeZoneRepo
	pools      *fakePoolRepo
	placements *fakePlacements
	provider   *fakeGroupingProvider
	references fakeReferences
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	f := &serviceFixture{
		zones: newFakeZoneRepo(), pools: newFakePoolRepo(),
		placements: &fakePlacements{current: &infradomain.ServerPlacement{ServerID: "srv-1", SiteID: "site-a", Generation: 1}},
		provider:   newFakeGroupingProvider(),
		references: fakeReferences{zones: map[string]int{}, pools: map[string]int{}},
	}
	factory := &fakeFactory{providers: map[string]provisioningdomain.OSProvisioningProvider{"maas-a": f.provider}}
	f.service = NewGroupingService(f.zones, f.pools, &fakeSiteReader{exists: true}, f.placements, f.references, factory)
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
		t.Errorf("result = %+v, want generation 2, zone z1, pool cleared to the default", result)
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
