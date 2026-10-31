package tests

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// fakeServerHolds reports a Workflow lease on the listed Servers.
type fakeServerHolds struct {
	held map[string]bool
}

func (h *fakeServerHolds) HeldByWorkflow(_ context.Context, serverID string) (bool, error) {
	return h.held[serverID], nil
}

// fakeConvergenceEvents keeps the convergence timeline in memory.
type fakeConvergenceEvents struct {
	mu     sync.Mutex
	events []convergence.Event
}

func (r *fakeConvergenceEvents) Record(_ context.Context, event convergence.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

func (r *fakeConvergenceEvents) List(_ context.Context, kind, id string, _ int) ([]convergence.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []convergence.Event
	for _, event := range r.events {
		if event.ResourceKind == kind && event.ResourceID == id {
			out = append(out, event)
		}
	}
	return out, nil
}

func (r *fakeConvergenceEvents) types() []convergence.EventType {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]convergence.EventType, len(r.events))
	for i, event := range r.events {
		out[i] = event.Type
	}
	return out
}

// countingConvergenceStore counts status writes, so a test can pin that a steady Server causes
// none (and therefore no change-feed loop).
type countingConvergenceStore struct {
	*fakeServerRepo
	statusWrites int
}

func (s *countingConvergenceStore) SetConvergence(ctx context.Context, id string, status serverdomain.ServerConvergence) error {
	s.statusWrites++
	return s.fakeServerRepo.SetConvergence(ctx, id, status)
}

// fakeGroupCatalog answers the Server controller's catalog lookups from fixed Zones and Pools:
// each maps a catalog id to its name on the provisioner ("" while not realized there).
type fakeGroupCatalog struct {
	zones map[string]string
	pools map[string]string
}

func (c *fakeGroupCatalog) ZoneOn(_ context.Context, id, _ string) (string, bool, bool, error) {
	name, found := c.zones[id]
	return name, name != "", found, nil
}

func (c *fakeGroupCatalog) PoolOn(_ context.Context, id, _ string) (string, bool, bool, error) {
	name, found := c.pools[id]
	return name, name != "", found, nil
}

func (c *fakeGroupCatalog) ZoneNamed(_ context.Context, _, _, name string) (string, bool, error) {
	for id, zone := range c.zones {
		if zone == name {
			return id, true, nil
		}
	}
	return "", false, nil
}

func (c *fakeGroupCatalog) PoolNamed(_ context.Context, _, _, name string) (string, bool, error) {
	for id, pool := range c.pools {
		if pool == name {
			return id, true, nil
		}
	}
	return "", false, nil
}

// groupingFakeProvider is fakeProvider with zones and pools: assigning one moves the machine.
type groupingFakeProvider struct {
	*fakeProvider
}

func (p *groupingFakeProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	capabilities := p.fakeProvider.Capabilities()
	capabilities.Grouping = true
	return capabilities
}

func (p *groupingFakeProvider) ListZones(context.Context) ([]provisioningdomain.ProviderGroup, error) {
	return nil, nil
}

func (p *groupingFakeProvider) ListPools(context.Context) ([]provisioningdomain.ProviderGroup, error) {
	return nil, nil
}

func (p *groupingFakeProvider) DefaultGroupNames() (string, string) { return "default", "default" }

func (p *groupingFakeProvider) EnsureZone(context.Context, string, string) error         { return nil }
func (p *groupingFakeProvider) RenameZone(context.Context, string, string, string) error { return nil }
func (p *groupingFakeProvider) DeleteZone(context.Context, string) error                 { return nil }
func (p *groupingFakeProvider) EnsurePool(context.Context, string, string) error         { return nil }
func (p *groupingFakeProvider) RenamePool(context.Context, string, string, string) error { return nil }
func (p *groupingFakeProvider) DeletePool(context.Context, string) error                 { return nil }

func (p *groupingFakeProvider) SetMachineZone(_ context.Context, machineID, zone string) (*provisioningdomain.Machine, error) {
	p.actions = append(p.actions, "setZone "+zone)
	p.machines[machineID].Zone = zone
	return p.machines[machineID], nil
}

func (p *groupingFakeProvider) SetMachinePool(_ context.Context, machineID, pool string) (*provisioningdomain.Machine, error) {
	p.actions = append(p.actions, "setPool "+pool)
	p.machines[machineID].ResourcePool = pool
	return p.machines[machineID], nil
}

type serverControllerFixture struct {
	servers    *fakeServerRepo
	store      *countingConvergenceStore
	provider   *fakeProvider
	factory    *fakeProviderFactory
	catalog    *fakeGroupCatalog
	machine    *provisioningdomain.Machine
	holds      *fakeServerHolds
	events     *fakeConvergenceEvents
	controller *provisioningapp.ServerController
}

// setupServerController seeds srv-1, observed with the given tags, on a provisioner that reports
// its machine with the same tags. The Server has no spec yet (generation 0).
func setupServerController(t *testing.T, observedTags ...string) *serverControllerFixture {
	t.Helper()
	servers := newFakeServerRepo()
	servers.servers["srv-1"] = &serverdomain.Server{
		ID:     "srv-1",
		Origin: serverdomain.OriginDiscovered,
		Source: serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: "machine-srv-1"},
		Observed: serverdomain.Observed{
			Hostname: "gpu-node-01",
			Tags:     slices.Clone(observedTags),
		},
		Provisioning: &serverdomain.ProvisioningStatus{State: "deployed", IntegrationID: testIntegrationID},
	}
	machine := testMachine("machine-srv-1", "gpu-node-01")
	machine.Tags = slices.Clone(observedTags)
	provider := newFakeProvider().withMachine(machine)
	factory := newFakeProviderFactory()
	factory.providers[testIntegrationID] = provider
	store := &countingConvergenceStore{fakeServerRepo: servers}
	holds := &fakeServerHolds{held: map[string]bool{}}
	events := &fakeConvergenceEvents{}
	catalog := &fakeGroupCatalog{zones: map[string]string{}, pools: map[string]string{}}
	return &serverControllerFixture{
		servers: servers, store: store, provider: provider, factory: factory, catalog: catalog,
		machine: machine, holds: holds, events: events,
		controller: provisioningapp.NewServerController(servers, store, factory, holds, catalog, events),
	}
}

// withGrouping makes the provisioner hold zones and pools, with the machine observed in the given
// ones. It must run before the first reconcile, which caches the provisioner's default groups.
func (f *serverControllerFixture) withGrouping(zone, pool string) *serverControllerFixture {
	f.factory.providers[testIntegrationID] = &groupingFakeProvider{fakeProvider: f.provider}
	f.machine.Zone, f.machine.ResourcePool = zone, pool
	f.server().Observed.ProviderZone, f.server().Observed.ProviderResourcePool = zone, pool
	return f
}

func (f *serverControllerFixture) reconcile(t *testing.T, trigger convergence.Trigger) error {
	t.Helper()
	return f.controller.Reconcile(context.Background(), convergence.Request{Key: "srv-1", Trigger: trigger})
}

func (f *serverControllerFixture) mustReconcile(t *testing.T, trigger convergence.Trigger) {
	t.Helper()
	if err := f.reconcile(t, trigger); err != nil {
		t.Fatalf("reconcile (%s): %v", trigger, err)
	}
}

func (f *serverControllerFixture) server() *serverdomain.Server {
	return f.servers.servers["srv-1"]
}

// setSpec writes a spec change the way the tag edit does, on top of the adopted spec.
func (f *serverControllerFixture) setSpec(t *testing.T, tags ...string) {
	t.Helper()
	spec := f.server().Spec
	spec.Tags = tags
	if _, err := f.servers.UpdateSpec(context.Background(), "srv-1", f.server().Generation, spec); err != nil {
		t.Fatalf("update spec: %v", err)
	}
}

// setPlacement writes a placement change the way the placement endpoint does.
func (f *serverControllerFixture) setPlacement(t *testing.T, zoneID, poolID string) {
	t.Helper()
	spec := f.server().Spec
	spec.ZoneID, spec.PoolID = zoneID, poolID
	if _, err := f.servers.UpdateSpec(context.Background(), "srv-1", f.server().Generation, spec); err != nil {
		t.Fatalf("update spec: %v", err)
	}
}

func (f *serverControllerFixture) condition(t *testing.T, conditionType convergence.ConditionType) convergence.Condition {
	t.Helper()
	condition, ok := convergence.FindCondition(f.server().Convergence.Conditions, conditionType)
	if !ok {
		t.Fatalf("no %s condition in %+v", conditionType, f.server().Convergence.Conditions)
	}
	return condition
}

func (f *serverControllerFixture) assertCondition(t *testing.T, conditionType convergence.ConditionType, status convergence.ConditionStatus, reason string) {
	t.Helper()
	condition := f.condition(t, conditionType)
	if condition.Status != status || condition.Reason != reason {
		t.Fatalf("%s = %s/%s (%q), want %s/%s", conditionType, condition.Status, condition.Reason, condition.Message, status, reason)
	}
}

// The first observation becomes the spec without touching the provisioner: adoption must never
// rewrite a machine an operator already configured (desired-state-model.md §6.5).
func TestServerController_AdoptsFirstObservationWithoutProviderWrites(t *testing.T) {
	f := setupServerController(t, "gpu", "amd-gpu")
	f.server().Observed.AutomaticTags = []string{"amd-gpu"}

	f.mustReconcile(t, convergence.TriggerResync)

	server := f.server()
	if server.Generation != 1 || !slices.Equal(server.Spec.Tags, []string{"gpu"}) {
		t.Fatalf("adopted spec = generation %d tags %v, want generation 1 tags [gpu] (automatic tags excluded)", server.Generation, server.Spec.Tags)
	}
	if server.Convergence.ObservedGeneration != 1 {
		t.Errorf("observedGeneration = %d, want 1", server.Convergence.ObservedGeneration)
	}
	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged)
	if len(f.provider.actions) != 0 || len(f.provider.ensuredTags) != 0 {
		t.Errorf("adoption drove the provisioner: actions %v, ensured %v", f.provider.actions, f.provider.ensuredTags)
	}
	if got := f.events.types(); !slices.Equal(got, []convergence.EventType{convergence.EventAdopted}) {
		t.Errorf("events = %v, want [adopted]", got)
	}
}

// A spec change converges on the change trigger, adding missing manual tags and removing extra
// ones, and leaves automatic tags alone.
func TestServerController_ConvergesSpecChangeImmediately(t *testing.T) {
	f := setupServerController(t, "gpu", "amd-gpu")
	f.server().Observed.AutomaticTags = []string{"amd-gpu"}
	f.mustReconcile(t, convergence.TriggerResync)

	f.setSpec(t, "ml")
	f.mustReconcile(t, convergence.TriggerChange)

	if !slices.Equal(f.provider.actions, []string{"addTag ml", "removeTag gpu"}) {
		t.Fatalf("provider actions = %v, want [addTag ml removeTag gpu]", f.provider.actions)
	}
	if got := f.server().Observed.Tags; !slices.Contains(got, "ml") || slices.Contains(got, "gpu") || !slices.Contains(got, "amd-gpu") {
		t.Errorf("observed tags after converging = %v, want ml and amd-gpu without gpu", got)
	}
	if f.server().Convergence.ObservedGeneration != 2 {
		t.Errorf("observedGeneration = %d, want 2", f.server().Convergence.ObservedGeneration)
	}
	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged)
	if got := f.events.types(); !slices.Equal(got, []convergence.EventType{convergence.EventAdopted, convergence.EventConverged}) {
		t.Errorf("events = %v, want [adopted converged]", got)
	}
}

// Drift made outside swallow (a tag added in the MAAS UI) is reported at once but corrected only
// at the periodic resync (decision 071: correction without real-time monitoring).
func TestServerController_CorrectsDriftOnlyAtResync(t *testing.T) {
	f := setupServerController(t, "gpu")
	f.mustReconcile(t, convergence.TriggerResync)

	f.machine.Tags = append(f.machine.Tags, "hand-made")
	f.server().Observed.Tags = slices.Clone(f.machine.Tags)
	f.mustReconcile(t, convergence.TriggerChange)

	f.assertCondition(t, convergence.ConditionDrifted, convergence.ConditionTrue, convergence.ReasonDriftDetected)
	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonConverging)
	if len(f.provider.actions) != 0 {
		t.Fatalf("drift was corrected on a change trigger: %v", f.provider.actions)
	}

	f.mustReconcile(t, convergence.TriggerResync)

	if !slices.Equal(f.provider.actions, []string{"removeTag hand-made"}) {
		t.Fatalf("provider actions at resync = %v, want [removeTag hand-made]", f.provider.actions)
	}
	f.assertCondition(t, convergence.ConditionDrifted, convergence.ConditionFalse, convergence.ReasonInSync)
	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged)
	want := []convergence.EventType{convergence.EventAdopted, convergence.EventDriftDetected, convergence.EventDriftCorrected}
	if got := f.events.types(); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

// While the Server is held, the controller reports why and makes no provider change; the change
// converges once the hold ends.
func TestServerController_HoldDefersConvergence(t *testing.T) {
	cases := []struct {
		name   string
		hold   func(*serverControllerFixture)
		reason string
	}{
		{"server lock", func(f *serverControllerFixture) { f.server().Provisioning.Locked = true }, convergence.ReasonServerLocked},
		{"rescue mode", func(f *serverControllerFixture) {
			f.server().Provisioning.State = string(provisioningdomain.MachineStatusRescue)
		}, convergence.ReasonRescueMode},
		{"workflow lease", func(f *serverControllerFixture) { f.holds.held["srv-1"] = true }, convergence.ReasonWorkflowLease},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupServerController(t, "gpu")
			f.mustReconcile(t, convergence.TriggerResync)
			tc.hold(f)
			f.setSpec(t, "gpu", "ml")

			f.mustReconcile(t, convergence.TriggerResync)

			f.assertCondition(t, convergence.ConditionHeld, convergence.ConditionTrue, tc.reason)
			f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonConverging)
			if len(f.provider.actions) != 0 {
				t.Fatalf("a held Server was changed: %v", f.provider.actions)
			}

			f.server().Provisioning.Locked = false
			f.server().Provisioning.State = "deployed"
			f.holds.held["srv-1"] = false
			f.mustReconcile(t, convergence.TriggerChange)

			f.assertCondition(t, convergence.ConditionHeld, convergence.ConditionFalse, convergence.ReasonNotHeld)
			if !slices.Equal(f.provider.actions, []string{"addTag ml"}) {
				t.Errorf("after the hold ended, actions = %v, want [addTag ml]", f.provider.actions)
			}
		})
	}
}

// A Server whose machine the provisioner no longer reports is not realized, and nothing is sent to
// the provisioner on its behalf.
func TestServerController_ReportsProviderMissing(t *testing.T) {
	f := setupServerController(t, "gpu")
	f.mustReconcile(t, convergence.TriggerResync)
	f.server().Absent = true
	f.setSpec(t, "gpu", "ml")

	f.mustReconcile(t, convergence.TriggerResync)

	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderMissing)
	if len(f.provider.actions) != 0 {
		t.Errorf("an absent Server was driven: %v", f.provider.actions)
	}
}

// A provisioner without tag support keeps the spec and reports the missing capability instead of
// failing the controller.
func TestServerController_ReportsCapabilityMissing(t *testing.T) {
	f := setupServerController(t, "gpu")
	f.mustReconcile(t, convergence.TriggerResync)
	f.provider.capabilities.Tagging = false
	f.setSpec(t, "gpu", "ml")

	f.mustReconcile(t, convergence.TriggerChange)

	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonCapabilityMissing)
	if !slices.Equal(f.server().Spec.Tags, []string{"gpu", "ml"}) {
		t.Errorf("spec = %v, want it kept", f.server().Spec.Tags)
	}
}

// A provisioner rejection stops convergence with requires_attention until the spec changes; a
// resync does not retry the rejected change.
func TestServerController_RejectionRequiresAttentionUntilSpecChanges(t *testing.T) {
	f := setupServerController(t, "gpu")
	f.mustReconcile(t, convergence.TriggerResync)
	f.provider.autoTags["amd-gpu"] = true
	f.setSpec(t, "gpu", "amd-gpu")

	f.mustReconcile(t, convergence.TriggerChange)

	f.assertCondition(t, convergence.ConditionRequiresAttention, convergence.ConditionTrue, convergence.ReasonProviderRejected)
	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonFailed)
	if got := f.events.types(); got[len(got)-1] != convergence.EventConvergenceFailed {
		t.Errorf("events = %v, want convergence_failed last", got)
	}
	ensured := len(f.provider.ensuredTags)

	f.mustReconcile(t, convergence.TriggerResync)
	if len(f.provider.ensuredTags) != ensured {
		t.Fatal("a resync retried a change the provisioner rejected")
	}

	f.setSpec(t, "gpu")
	f.mustReconcile(t, convergence.TriggerChange)

	f.assertCondition(t, convergence.ConditionRequiresAttention, convergence.ConditionFalse, convergence.ReasonAttentionNotNeeded)
	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged)
}

// An unreachable provisioner is transient: the controller reports it and returns the error so the
// runtime retries with backoff.
func TestServerController_TransientErrorIsRetried(t *testing.T) {
	f := setupServerController(t, "gpu")
	f.mustReconcile(t, convergence.TriggerResync)
	f.provider.tagErr = errors.New("connection refused")
	f.setSpec(t, "gpu", "ml")

	if err := f.reconcile(t, convergence.TriggerChange); err == nil {
		t.Fatal("a transient provider error must be returned for retry")
	}
	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderUnreachable)
	if f.server().Convergence.ObservedGeneration != 1 {
		t.Errorf("observedGeneration = %d, want 1 until the spec is realized", f.server().Convergence.ObservedGeneration)
	}

	f.provider.tagErr = nil
	f.mustReconcile(t, convergence.TriggerChange)
	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged)
}

// A Server already matching its spec causes no status write, so routine observation does not feed
// the change stream back into the controller.
func TestServerController_SteadyStateWritesNothing(t *testing.T) {
	f := setupServerController(t, "gpu")
	f.mustReconcile(t, convergence.TriggerResync)
	writes := f.store.statusWrites

	f.mustReconcile(t, convergence.TriggerChange)
	f.mustReconcile(t, convergence.TriggerResync)

	if f.store.statusWrites != writes {
		t.Errorf("status writes = %d, want %d (no change)", f.store.statusWrites, writes)
	}
}

// Adoption maps the observed zone and pool onto catalog ids (the provisioner's default onto "")
// without moving the machine.
func TestServerController_AdoptsThePlacementAsCatalogIDs(t *testing.T) {
	f := setupServerController(t, "gpu").withGrouping("rack-a", "default")
	f.catalog.zones["z1"] = "rack-a"

	f.mustReconcile(t, convergence.TriggerResync)

	if spec := f.server().Spec; spec.ZoneID != "z1" || spec.PoolID != "" {
		t.Fatalf("adopted placement = %+v, want zone z1 and the default pool", spec)
	}
	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged)
	if len(f.provider.actions) != 0 {
		t.Errorf("adoption moved the machine: %v", f.provider.actions)
	}
}

// A machine in a group the catalog does not hold yet waits for the catalog controller to adopt it,
// rather than adopting a spec that would move the machine to the default group.
func TestServerController_AdoptionWaitsForTheCatalog(t *testing.T) {
	f := setupServerController(t).withGrouping("rack-new", "default")

	if err := f.reconcile(t, convergence.TriggerResync); err == nil {
		t.Fatal("adoption must wait (and be retried) while the zone is not in the catalog")
	}
	if f.server().Generation != 0 {
		t.Fatalf("generation = %d, want 0 until adopted", f.server().Generation)
	}

	f.catalog.zones["z-new"] = "rack-new"
	f.mustReconcile(t, convergence.TriggerResync)
	if f.server().Spec.ZoneID != "z-new" {
		t.Errorf("zone = %q, want z-new once the catalog holds it", f.server().Spec.ZoneID)
	}
}

// A placement change moves the machine at once; clearing it returns the machine to the default.
func TestServerController_ConvergesAndClearsThePlacement(t *testing.T) {
	f := setupServerController(t).withGrouping("default", "default")
	f.catalog.zones["z1"] = "rack-a"
	f.catalog.pools["p1"] = "gpu"
	f.mustReconcile(t, convergence.TriggerResync)

	f.setPlacement(t, "z1", "p1")
	f.mustReconcile(t, convergence.TriggerChange)

	if !slices.Equal(f.provider.actions, []string{"setZone rack-a", "setPool gpu"}) {
		t.Fatalf("actions = %v, want the machine moved to rack-a and gpu", f.provider.actions)
	}
	if observed := f.server().Observed; observed.ProviderZone != "rack-a" || observed.ProviderResourcePool != "gpu" {
		t.Errorf("observed placement = %q/%q, want rack-a/gpu", observed.ProviderZone, observed.ProviderResourcePool)
	}
	f.assertCondition(t, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged)

	f.setPlacement(t, "", "p1")
	f.mustReconcile(t, convergence.TriggerChange)

	if got := f.provider.actions[len(f.provider.actions)-1]; got != "setZone default" {
		t.Fatalf("last action = %q, want the machine back in the default zone", got)
	}
}

// A Zone the catalog controller has not created on the provisioner yet is transient; a Zone that
// no longer exists stops convergence until the spec changes.
func TestServerController_PlacementWaitsForOrRejectsTheGroup(t *testing.T) {
	f := setupServerController(t).withGrouping("default", "default")
	f.mustReconcile(t, convergence.TriggerResync)

	f.catalog.zones["z-pending"] = ""
	f.setPlacement(t, "z-pending", "")
	if err := f.reconcile(t, convergence.TriggerChange); err == nil {
		t.Fatal("a zone not on the provisioner yet must be retried")
	}
	if len(f.provider.actions) != 0 {
		t.Fatalf("the machine moved before its zone existed: %v", f.provider.actions)
	}

	f.setPlacement(t, "z-gone", "")
	f.mustReconcile(t, convergence.TriggerChange)
	f.assertCondition(t, convergence.ConditionRequiresAttention, convergence.ConditionTrue, convergence.ReasonProviderRejected)
}

// Tags a schema migration carried into the spec of a Server not adopted yet are kept by adoption
// and converge at once.
func TestServerController_AdoptionKeepsMigratedTags(t *testing.T) {
	f := setupServerController(t, "gpu")
	f.server().Spec.Tags = []string{"legacy"}

	f.mustReconcile(t, convergence.TriggerChange)

	if !equalStringSet(f.server().Spec.Tags, []string{"gpu", "legacy"}) {
		t.Fatalf("adopted tags = %v, want gpu and legacy", f.server().Spec.Tags)
	}
	if !slices.Equal(f.provider.actions, []string{"addTag legacy"}) {
		t.Errorf("actions = %v, want legacy added at once", f.provider.actions)
	}
}
