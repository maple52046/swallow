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
)

type catalogFixture struct {
	controller *CatalogController
	zones      *fakeZoneRepo
	pools      *fakePoolRepo
	provider   *fakeGroupingProvider
	factory    *fakeFactory
}

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	f := &catalogFixture{zones: newFakeZoneRepo(), pools: newFakePoolRepo(), provider: newFakeGroupingProvider()}
	f.factory = &fakeFactory{providers: map[string]provisioningdomain.OSProvisioningProvider{"maas-a": f.provider}}
	f.controller = NewCatalogController(f.zones, f.pools, fakeTargets{bySite: map[string][]string{"site-a": {"maas-a"}}}, f.factory, nil)
	f.controller.now = func() time.Time { return time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC) }
	ids := 0
	f.controller.newID = func() string { ids++; return "adopted-" + string(rune('0'+ids)) }
	return f
}

func (f *catalogFixture) addZone(name, description string) *infradomain.Zone {
	zone := &infradomain.Zone{ID: "z-" + name, SiteID: "site-a", Generation: 1, Name: name, Description: description}
	f.zones.byID[zone.ID] = zone
	return zone
}

func (f *catalogFixture) reconcile(t *testing.T, key string, trigger convergence.Trigger) error {
	t.Helper()
	return f.controller.Reconcile(context.Background(), convergence.Request{Key: key, Trigger: trigger})
}

func (f *catalogFixture) mustReconcile(t *testing.T, key string, trigger convergence.Trigger) {
	t.Helper()
	if err := f.reconcile(t, key, trigger); err != nil {
		t.Fatalf("reconcile %s (%s): %v", key, trigger, err)
	}
}

func assertGroupCondition(t *testing.T, conditions []convergence.Condition, conditionType convergence.ConditionType, status convergence.ConditionStatus, reason string) {
	t.Helper()
	c, ok := convergence.FindCondition(conditions, conditionType)
	if !ok || c.Status != status || c.Reason != reason {
		t.Fatalf("%s = %+v, want %s/%s", conditionType, c, status, reason)
	}
}

// A new Zone is created on the provisioner at once, and its status says so for this generation.
func TestCatalog_CreatesANewZoneOnTheProvisioner(t *testing.T) {
	f := newCatalogFixture(t)
	f.addZone("rack-a", "top of rack")

	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerChange)

	if !slices.Equal(f.provider.calls, []string{"ensureZone rack-a"}) {
		t.Fatalf("provider calls = %v, want [ensureZone rack-a]", f.provider.calls)
	}
	zone := f.zones.get("z-rack-a")
	if zone.Status.ObservedGeneration != 1 || len(zone.Status.Provisioners) != 1 || zone.Status.Provisioners[0].ProviderName != "rack-a" {
		t.Fatalf("status = %+v, want realized as rack-a at generation 1", zone.Status)
	}
	assertGroupCondition(t, zone.Status.Conditions, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged)
}

// A rename renames the group the Zone was realized as, so its machines stay in it.
func TestCatalog_RenamesTheRealizedGroup(t *testing.T) {
	f := newCatalogFixture(t)
	f.addZone("rack-a", "")
	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerChange)

	zone := f.zones.get("z-rack-a")
	zone.Name, zone.Generation = "rack-b", 2
	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerChange)

	if !slices.Equal(f.provider.calls, []string{"ensureZone rack-a", "renameZone rack-a->rack-b"}) {
		t.Fatalf("provider calls = %v", f.provider.calls)
	}
	if got := f.zones.get("z-rack-a").Status; got.ObservedGeneration != 2 || got.Provisioners[0].ProviderName != "rack-b" {
		t.Errorf("status = %+v, want realized as rack-b at generation 2", got)
	}
}

// A change made in the provisioner is drift: reported on a change trigger, corrected at resync.
func TestCatalog_CorrectsDriftOnlyAtResync(t *testing.T) {
	f := newCatalogFixture(t)
	f.addZone("rack-a", "top")
	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerChange)

	f.provider.zones = removeGroup(f.provider.zones, "rack-a")
	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerChange)

	zone := f.zones.get("z-rack-a")
	assertGroupCondition(t, zone.Status.Conditions, convergence.ConditionDrifted, convergence.ConditionTrue, convergence.ReasonDriftDetected)
	if len(f.provider.calls) != 1 {
		t.Fatalf("drift was corrected on a change trigger: %v", f.provider.calls)
	}

	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerResync)

	if !slices.Equal(f.provider.calls, []string{"ensureZone rack-a", "ensureZone rack-a"}) {
		t.Fatalf("provider calls after resync = %v", f.provider.calls)
	}
	zone = f.zones.get("z-rack-a")
	assertGroupCondition(t, zone.Status.Conditions, convergence.ConditionDrifted, convergence.ConditionFalse, convergence.ReasonInSync)
	assertGroupCondition(t, zone.Status.Conditions, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged)
}

// A provisioner without grouping keeps the spec and reports capability_missing.
func TestCatalog_ReportsAProvisionerThatCannotGroup(t *testing.T) {
	f := newCatalogFixture(t)
	f.provider.grouping = false
	f.addZone("rack-a", "")

	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerChange)

	zone := f.zones.get("z-rack-a")
	assertGroupCondition(t, zone.Status.Provisioners[0].Conditions, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonCapabilityMissing)
	assertGroupCondition(t, zone.Status.Conditions, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonCapabilityMissing)
	if zone.Status.ObservedGeneration != 0 {
		t.Errorf("observedGeneration = %d, want 0 while unrealized", zone.Status.ObservedGeneration)
	}
}

// A rejection stops convergence with requires_attention; a resync does not retry it.
func TestCatalog_RejectionRequiresAttention(t *testing.T) {
	f := newCatalogFixture(t)
	f.addZone("rack-a", "")
	f.provider.writeErr = &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorRejected, Detail: "name not allowed"}

	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerChange)
	assertGroupCondition(t, f.zones.get("z-rack-a").Status.Conditions, convergence.ConditionRequiresAttention, convergence.ConditionTrue, convergence.ReasonProviderRejected)

	f.provider.writeErr = nil
	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerResync)
	if len(f.provider.calls) != 0 {
		t.Fatalf("a resync retried a rejected change: %v", f.provider.calls)
	}
}

// An unreachable provisioner is transient: reported, and returned for the runtime to retry.
func TestCatalog_UnreachableProvisionerIsRetried(t *testing.T) {
	f := newCatalogFixture(t)
	f.addZone("rack-a", "")
	f.provider.listErr = errors.New("connection refused")

	if err := f.reconcile(t, "zone:z-rack-a", convergence.TriggerChange); err == nil {
		t.Fatal("an unreachable provisioner must be returned for retry")
	}
	assertGroupCondition(t, f.zones.get("z-rack-a").Status.Conditions, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderUnreachable)
}

// Groups that exist only in the provisioner join the catalog without a provisioner write; the
// provisioner's default and groups the catalog already names are left out.
func TestCatalog_AdoptsProvisionerOnlyGroups(t *testing.T) {
	f := newCatalogFixture(t)
	f.addZone("rack-a", "")
	f.provider.zones = append(f.provider.zones,
		provisioningdomain.ProviderGroup{Name: "rack-a"},
		provisioningdomain.ProviderGroup{Name: "rack-z", Description: "made in MAAS"})
	f.provider.pools = append(f.provider.pools, provisioningdomain.ProviderGroup{Name: "gpu"})

	f.mustReconcile(t, "adopt:site-a", convergence.TriggerResync)

	zones, _ := f.zones.List(context.Background(), "site-a")
	var names []string
	for _, zone := range zones {
		names = append(names, zone.Name)
	}
	if !slices.Equal(names, []string{"rack-a", "rack-z"}) {
		t.Fatalf("zones after adoption = %v, want [rack-a rack-z]", names)
	}
	adopted, err := f.zones.FindByName(context.Background(), "site-a", "rack-z")
	if err != nil || adopted.Description != "made in MAAS" || adopted.Status.ObservedGeneration != 1 {
		t.Fatalf("adopted zone = %+v, %v", adopted, err)
	}
	assertGroupCondition(t, adopted.Status.Conditions, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged)
	if pools, _ := f.pools.List(context.Background(), "site-a"); len(pools) != 1 || pools[0].Name != "gpu" {
		t.Fatalf("pools after adoption = %+v, want [gpu]", pools)
	}
	if len(f.provider.calls) != 0 {
		t.Errorf("adoption drove the provisioner: %v", f.provider.calls)
	}
}

// A realized Zone causes no status write on a change trigger, so the change feed does not loop.
func TestCatalog_SteadyStateWritesNothing(t *testing.T) {
	f := newCatalogFixture(t)
	f.addZone("rack-a", "")
	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerChange)
	writes := f.zones.setStatus

	f.mustReconcile(t, "zone:z-rack-a", convergence.TriggerChange)

	if f.zones.setStatus != writes {
		t.Errorf("status writes = %d, want %d", f.zones.setStatus, writes)
	}
}

func TestCatalog_KeysCoverAdoptionAndEveryGroup(t *testing.T) {
	f := newCatalogFixture(t)
	f.addZone("rack-a", "")
	f.pools.byID["p1"] = &infradomain.Pool{ID: "p1", SiteID: "site-a", Generation: 1, Name: "gpu"}

	keys, err := f.controller.Keys(context.Background())
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if !slices.Equal(keys, []string{"adopt:site-a", "zone:z-rack-a", "pool:p1"}) {
		t.Errorf("keys = %v", keys)
	}
}
