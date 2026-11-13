package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// ServerWorkflowHolds reports whether a running Workflow holds a Server's resource lease, which
// holds the Server's convergence (glossary: Hold): the Workflow is changing the machine, and the
// controller must not change it underneath.
type ServerWorkflowHolds interface {
	HeldByWorkflow(ctx context.Context, serverID string) (bool, error)
}

// ServerGroupCatalog resolves a Server's Zone placement against the Zone catalog, on the Server's
// own provisioner. It is a narrow read port over the infrastructure feature. A Pool placement needs
// no realization (decision 082), so the port has nothing for Pools.
type ServerGroupCatalog interface {
	// ZoneOn returns the name the Zone with this id has on the provisioner integrationID.
	// realized is false while the catalog controller has not created it there yet; found is false
	// when no such Zone exists.
	ZoneOn(ctx context.Context, zoneID, integrationID string) (name string, realized, found bool, err error)
	// ZoneNamed returns the id of the Site's Zone known as name, by its catalog name or by its
	// name on the provisioner integrationID; found is false when the catalog has none.
	ZoneNamed(ctx context.Context, siteID, integrationID, name string) (id string, found bool, err error)
}

// serverField is one provisioner-realized part of a Server's spec (decision 071). The controller
// asks each field how the observed machine departs from the spec, realizes the spec through the
// provider, and re-observes the field afterwards.
type serverField interface {
	// name is the field name used in drift messages and events.
	name() string
	// drift describes how the observed state departs from the spec, or "" when it matches. An
	// error means the field could not be judged now (for example its catalog could not be read).
	drift(ctx context.Context, server *serverdomain.Server) (string, error)
	// capable reports whether the provider can realize the field at all.
	capable(provider provisioningdomain.OSProvisioningProvider) bool
	// realize changes the provider to match the spec.
	realize(ctx context.Context, provider provisioningdomain.OSProvisioningProvider, server *serverdomain.Server) error
	// observe re-reads the field from the provider and records it on server and in the store.
	observe(ctx context.Context, provider provisioningdomain.OSProvisioningProvider, server *serverdomain.Server) error
}

// ServerController converges the provisioner-realized fields of every Server to its spec
// (glossary: Controller; desired-state-model.md §6, §7). It is the only writer of a Server's
// conditions and observedGeneration.
//
// For each Server it:
//   - adopts the first observation as the spec when the Server has none (§6.5), writing nothing to
//     the provisioner;
//   - reports held while the Server is locked, in Rescue Mode, or leased by a Workflow, and makes no
//     provider change then;
//   - reports provider_missing while the provisioner no longer reports the machine;
//   - converges a spec change on any trigger, and corrects drift (observed departed from a spec that
//     was realized) only on the periodic resync, as decision 071 requires;
//   - retries transient provider errors through the runtime's backoff, and stops on a rejection with
//     requires_attention until the spec changes.
//
// Status is written only when a condition or the observed generation changes, so routine
// observation does not cause writes, and therefore no change-feed loop.
type ServerController struct {
	servers   serverdomain.ServerRepository
	store     serverdomain.ServerConvergenceStore
	providers provisioningdomain.ProviderFactory
	holds     ServerWorkflowHolds
	catalog   ServerGroupCatalog
	events    convergence.EventRecorder
	fields    []serverField
	defaults  *groupDefaults
	now       func() time.Time
	log       *slog.Logger
}

var _ convergence.Controller = (*ServerController)(nil)

// NewServerController wires the Server repository and its desired-state store, the provider
// factory, the Workflow lease reader, the Zone catalog, and the convergence timeline.
func NewServerController(
	servers serverdomain.ServerRepository,
	desired serverdomain.ServerConvergenceStore,
	providers provisioningdomain.ProviderFactory,
	holds ServerWorkflowHolds,
	catalog ServerGroupCatalog,
	events convergence.EventRecorder,
) *ServerController {
	defaults := &groupDefaults{providers: providers, names: map[string]string{}}
	return &ServerController{
		servers: servers, store: desired, providers: providers, holds: holds, catalog: catalog, events: events,
		fields: []serverField{
			serverTags{store: desired},
			serverPlacement{store: desired, catalog: catalog, defaults: defaults},
		},
		defaults: defaults,
		now:      func() time.Time { return time.Now().UTC() },
		log:      slog.Default().With("controller", "server"),
	}
}

// Name implements convergence.Controller.
func (c *ServerController) Name() string { return "server" }

// ownershipCollection holds Workflow resource ownership, keyed `server:<id>` for a Server; a
// Server owned by an unfinished Workflow is held (decision 072).
const ownershipCollection = "resource_leases"

// Collections implements convergence.Controller. Ownership is watched so a Server converges as
// soon as the Workflow that held it ends, instead of at the next resync.
func (c *ServerController) Collections() []string { return []string{"servers", ownershipCollection} }

// KeysForChange implements convergence.Controller: a changed Server is its own key, a deleted one
// needs nothing, and a change of a Server's ownership (taken or released) reconciles that Server.
func (c *ServerController) KeysForChange(_ context.Context, change store.Change) ([]string, error) {
	if change.Collection == ownershipCollection {
		if id, ok := strings.CutPrefix(change.Key, "server:"); ok {
			return []string{id}, nil
		}
		return nil, nil
	}
	if change.Operation == store.ChangeDelete {
		return nil, nil
	}
	return []string{change.Key}, nil
}

// Keys implements convergence.Controller with every Server, including absent ones (they report
// provider_missing).
func (c *ServerController) Keys(ctx context.Context) ([]string, error) {
	result, err := c.servers.List(ctx, serverdomain.ListFilter{IncludeAbsent: true})
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(result.Servers))
	for i, server := range result.Servers {
		keys[i] = server.ID
	}
	return keys, nil
}

// Reconcile implements convergence.Controller.
func (c *ServerController) Reconcile(ctx context.Context, request convergence.Request) error {
	server, err := c.servers.FindByID(ctx, request.Key)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if server.Generation == 0 {
		if server, err = c.adopt(ctx, server); err != nil || server == nil {
			return err
		}
	}

	s := &serverStatus{conditions: server.Convergence.Conditions, observedGeneration: server.Convergence.ObservedGeneration, now: c.now()}
	generation := server.Generation
	held, holdReason, holdMessage, err := c.holdOf(ctx, server)
	if err != nil {
		return err
	}
	if held {
		s.set(convergence.ConditionHeld, convergence.ConditionTrue, holdReason, holdMessage, generation)
	} else {
		s.set(convergence.ConditionHeld, convergence.ConditionFalse, convergence.ReasonNotHeld, "", generation)
	}

	if server.Absent {
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderMissing,
			"The provisioner no longer reports this machine.", generation)
		return c.write(ctx, server, s)
	}
	if attention, ok := convergence.FindCondition(s.conditions, convergence.ConditionRequiresAttention); ok &&
		attention.Status == convergence.ConditionTrue && attention.ObservedGeneration == generation {
		// Stopped after a rejection; only a spec change (a new generation) tries again.
		return c.write(ctx, server, s)
	}

	pending := s.observedGeneration < generation
	drifts, err := c.drifts(ctx, server)
	if err != nil {
		return err
	}
	if len(drifts) == 0 {
		c.markRealized(ctx, server, s, pending, false)
		return c.write(ctx, server, s)
	}

	message := driftMessage(drifts)
	if !pending {
		if !convergence.IsTrue(s.conditions, convergence.ConditionDrifted) {
			c.record(ctx, server, convergence.EventDriftDetected, driftedFields(drifts), message)
		}
		s.set(convergence.ConditionDrifted, convergence.ConditionTrue, convergence.ReasonDriftDetected, message, generation)
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonConverging,
			"Drift is corrected at the next resync.", generation)
		if request.Trigger != convergence.TriggerResync {
			return c.write(ctx, server, s)
		}
	}
	if held {
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonConverging,
			"Convergence waits for the hold to end.", generation)
		return c.write(ctx, server, s)
	}

	if err := c.realize(ctx, server, drifts); err != nil {
		var missing capabilityMissingError
		var rejected *provisioningdomain.ProviderError
		switch {
		case errors.As(err, &missing):
			s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonCapabilityMissing, err.Error(), generation)
			return c.write(ctx, server, s)
		case errors.As(err, &rejected) && rejected.Kind == provisioningdomain.ProviderErrorRejected:
			s.set(convergence.ConditionRequiresAttention, convergence.ConditionTrue, convergence.ReasonProviderRejected, rejected.Detail, generation)
			s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonFailed, rejected.Detail, generation)
			c.record(ctx, server, convergence.EventConvergenceFailed, driftedFields(drifts), rejected.Detail)
			return c.write(ctx, server, s)
		default:
			s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderUnreachable,
				"The provisioner could not be reached; retrying.", generation)
			if werr := c.write(ctx, server, s); werr != nil {
				return werr
			}
			return err
		}
	}
	remaining, err := c.drifts(ctx, server)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		// The provider accepted the change but reports something else; observe again later.
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonConverging, driftMessage(remaining), generation)
		if werr := c.write(ctx, server, s); werr != nil {
			return werr
		}
		return fmt.Errorf("server %s still differs from its spec after converging", server.ID)
	}
	c.markRealized(ctx, server, s, pending, !pending)
	return c.write(ctx, server, s)
}

// adopt takes the first observation as the spec (desired-state-model.md §6.5): the observed manual
// tags, plus any a schema migration carried into the spec, and the observed zone as a catalog id
// ("" for the provisioner's default). A zone the catalog does not hold yet makes adoption wait for
// the catalog controller to adopt it, so the spec never moves a machine out of a zone swallow has
// not seen. The adopted Server is in no Pool: the provisioner's resource pool is not a Pool
// (decision 082), and only an admin grants access to a Server. When the API wrote a spec first,
// adoption does nothing and the Server is re-read.
func (c *ServerController) adopt(ctx context.Context, server *serverdomain.Server) (*serverdomain.Server, error) {
	zoneID, err := c.adoptedZone(ctx, server)
	if err != nil {
		return nil, err
	}
	tags := server.Observed.ManualTags()
	carried := false
	for _, tag := range server.Spec.Tags {
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
			carried = true
		}
	}
	spec := serverdomain.ServerSpec{Tags: tags, ZoneID: zoneID}
	adopted, err := c.store.AdoptSpec(ctx, server.ID, spec)
	if err != nil {
		return nil, err
	}
	if !adopted {
		server, err = c.servers.FindByID(ctx, server.ID)
		if errors.Is(err, serverdomain.ErrServerNotFound) {
			return nil, nil
		}
		return server, err
	}
	server.Generation, server.Spec = 1, spec
	// The adopted spec is the observed state, so it is realized by definition — unless a migration
	// carried tags the machine does not have, which then converge like any spec change.
	if !carried {
		server.Convergence.ObservedGeneration = 1
	}
	c.record(ctx, server, convergence.EventAdopted, nil, "The observed state became the spec.")
	return server, nil
}

// adoptedZone resolves the observed zone name onto a catalog id.
func (c *ServerController) adoptedZone(ctx context.Context, server *serverdomain.Server) (string, error) {
	zone := server.Observed.ProviderZone
	if zone == "" {
		return "", nil
	}
	defaultZone, capable, err := c.defaults.of(ctx, server.Source.IntegrationID)
	if err != nil || !capable || zone == defaultZone {
		return "", err
	}
	id, found, err := c.catalog.ZoneNamed(ctx, server.Source.SiteID, server.Source.IntegrationID, zone)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("server %s: zone %q is not in the catalog yet; adoption waits for it", server.ID, zone)
	}
	return id, nil
}

func (c *ServerController) markRealized(ctx context.Context, server *serverdomain.Server, s *serverStatus, pending, corrected bool) {
	generation := server.Generation
	wasDrifted := convergence.IsTrue(s.conditions, convergence.ConditionDrifted)
	s.set(convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged, "", generation)
	s.set(convergence.ConditionDrifted, convergence.ConditionFalse, convergence.ReasonInSync, "", generation)
	s.set(convergence.ConditionRequiresAttention, convergence.ConditionFalse, convergence.ReasonAttentionNotNeeded, "", generation)
	s.observedGeneration = generation
	switch {
	case corrected:
		c.record(ctx, server, convergence.EventDriftCorrected, nil, "Drift was corrected to the spec.")
	case pending:
		c.record(ctx, server, convergence.EventConverged, nil, "The spec was realized.")
	case wasDrifted:
		c.record(ctx, server, convergence.EventDriftResolved, nil, "The observed state matches the spec again.")
	}
}

// holdOf reports whether the Server's convergence is held, and why.
func (c *ServerController) holdOf(ctx context.Context, server *serverdomain.Server) (bool, string, string, error) {
	if p := server.Provisioning; p != nil {
		if p.Locked {
			return true, convergence.ReasonServerLocked, "The Server is locked in its provisioner.", nil
		}
		if p.State == string(provisioningdomain.MachineStatusRescue) {
			return true, convergence.ReasonRescueMode, "The Server is in Rescue Mode.", nil
		}
	}
	held, err := c.holds.HeldByWorkflow(ctx, server.ID)
	if err != nil {
		return false, "", "", err
	}
	if held {
		return true, convergence.ReasonWorkflowLease, "A running Workflow holds this Server.", nil
	}
	return false, "", "", nil
}

// fieldDrift is one field's departure from the spec.
type fieldDrift struct {
	field   serverField
	message string
}

func (c *ServerController) drifts(ctx context.Context, server *serverdomain.Server) ([]fieldDrift, error) {
	var out []fieldDrift
	for _, field := range c.fields {
		message, err := field.drift(ctx, server)
		if err != nil {
			return nil, err
		}
		if message != "" {
			out = append(out, fieldDrift{field: field, message: message})
		}
	}
	return out, nil
}

func driftMessage(drifts []fieldDrift) string {
	parts := make([]string, len(drifts))
	for i, d := range drifts {
		parts[i] = d.message
	}
	return strings.Join(parts, " ")
}

func driftedFields(drifts []fieldDrift) []string {
	out := make([]string, len(drifts))
	for i, d := range drifts {
		out[i] = d.field.name()
	}
	return out
}

// capabilityMissingError means the provider cannot realize a field; the spec is kept.
type capabilityMissingError struct{ field string }

func (e capabilityMissingError) Error() string {
	return fmt.Sprintf("The provisioner cannot set %s.", e.field)
}

// realize changes every drifted field on the provider and re-observes it.
func (c *ServerController) realize(ctx context.Context, server *serverdomain.Server, drifts []fieldDrift) error {
	provider, err := c.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return err
	}
	for _, d := range drifts {
		field := d.field
		if !field.capable(provider) {
			return capabilityMissingError{field: field.name()}
		}
		if err := field.realize(ctx, provider, server); err != nil {
			return err
		}
		if err := field.observe(ctx, provider, server); err != nil {
			return err
		}
	}
	return nil
}

func (c *ServerController) write(ctx context.Context, server *serverdomain.Server, s *serverStatus) error {
	if !s.changed && s.observedGeneration == server.Convergence.ObservedGeneration {
		return nil
	}
	return c.store.SetConvergence(ctx, server.ID, serverdomain.ServerConvergence{
		ObservedGeneration: s.observedGeneration, Conditions: s.conditions,
	})
}

func (c *ServerController) record(ctx context.Context, server *serverdomain.Server, eventType convergence.EventType, fields []string, message string) {
	if c.events == nil {
		return
	}
	err := c.events.Record(ctx, convergence.Event{
		ResourceKind: "server", ResourceID: server.ID, Type: eventType, Fields: fields,
		Message: message, Generation: server.Generation, At: c.now(),
	})
	if err != nil {
		c.log.Warn("convergence event not recorded", "server", server.ID, "type", eventType, "error", err)
	}
}

// serverStatus accumulates condition changes during one Reconcile.
type serverStatus struct {
	conditions         []convergence.Condition
	observedGeneration int64
	changed            bool
	now                time.Time
}

func (s *serverStatus) set(t convergence.ConditionType, status convergence.ConditionStatus, reason, message string, generation int64) {
	var changed bool
	s.conditions, changed = convergence.SetCondition(s.conditions, convergence.Condition{
		Type: t, Status: status, Reason: reason, Message: message, ObservedGeneration: generation,
	}, s.now)
	s.changed = s.changed || changed
}

// serverTags is the manual-tag field: the spec is the complete set of manual tags, so a missing
// tag is added and an extra manual tag removed; automatic tags are never touched (decision 071),
// whatever the provisioner does with them.
type serverTags struct {
	store serverdomain.ServerConvergenceStore
}

func (serverTags) name() string { return "tags" }

// wanted is the manual tags the spec asks for: the spec without the tags the Server observes as
// automatic. A spec adopted before the provisioner adapter recognized a tag as automatic can still
// name it; converging it would fight the provisioner, so it is skipped until a spec write drops it
// (server-tags.md). The classification is the reconcile's, read from the provisioner's catalog.
func (serverTags) wanted(server *serverdomain.Server) []string {
	return slices.DeleteFunc(slices.Clone(server.Spec.Tags), func(tag string) bool {
		return slices.Contains(server.Observed.AutomaticTags, tag)
	})
}

func (f serverTags) drift(_ context.Context, server *serverdomain.Server) (string, error) {
	missing, extra := tagDifference(f.wanted(server), server.Observed.ManualTags())
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing "+strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		parts = append(parts, "unexpected "+strings.Join(extra, ", "))
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "Manual tags differ from the spec (" + strings.Join(parts, "; ") + ").", nil
}

func (serverTags) capable(provider provisioningdomain.OSProvisioningProvider) bool {
	_, ok := provider.(provisioningdomain.MachineTagController)
	return ok && provider.Capabilities().Tagging
}

func (f serverTags) realize(ctx context.Context, provider provisioningdomain.OSProvisioningProvider, server *serverdomain.Server) error {
	controller := provider.(provisioningdomain.MachineTagController)
	missing, extra := tagDifference(f.wanted(server), server.Observed.ManualTags())
	machine := []string{server.Source.ProviderMachineID}
	for _, name := range missing {
		if err := controller.EnsureTag(ctx, name); err != nil {
			return err
		}
		if err := controller.AddTag(ctx, name, machine); err != nil {
			return err
		}
	}
	for _, name := range extra {
		if err := controller.RemoveTag(ctx, name, machine); err != nil {
			return err
		}
	}
	return nil
}

// observe re-reads the machine's tags. Tags the controller just added are manual by construction;
// the automatic classification of the rest is kept from the last reconcile pass.
func (f serverTags) observe(ctx context.Context, provider provisioningdomain.OSProvisioningProvider, server *serverdomain.Server) error {
	machine, err := provider.GetMachine(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return err
	}
	automatic := intersectTags(machine.Tags, server.Observed.AutomaticTags)
	server.Observed.Tags = slices.Clone(machine.Tags)
	server.Observed.AutomaticTags = automatic
	return f.store.SetObservedTags(ctx, server.ID, server.Observed.Tags, automatic)
}

// tagDifference returns the tags of want missing from have, and those of have not in want.
func tagDifference(want, have []string) (missing, extra []string) {
	for _, tag := range want {
		if !slices.Contains(have, tag) {
			missing = append(missing, tag)
		}
	}
	for _, tag := range have {
		if !slices.Contains(want, tag) {
			extra = append(extra, tag)
		}
	}
	return missing, extra
}

// serverPlacement is the Zone field: the spec names a catalog Zone by id ("" for the provisioner's
// default zone), and the machine must be in the zone's name on its own provisioner. A Pool
// placement is part of the same spec but needs no realization (decision 082), so it is never judged
// here; the machine's resource pool is only observed.
type serverPlacement struct {
	store    serverdomain.ServerConvergenceStore
	catalog  ServerGroupCatalog
	defaults *groupDefaults
}

// zoneTarget is the provider zone name a Server must be in, or why it cannot be judged.
type zoneTarget struct {
	name    string
	missing bool // the referenced catalog Zone no longer exists
	pending bool // the catalog Zone is not on the provisioner yet
	capable bool
}

func (serverPlacement) name() string { return "placement" }

func (f serverPlacement) target(ctx context.Context, server *serverdomain.Server) (zoneTarget, error) {
	defaultZone, capable, err := f.defaults.of(ctx, server.Source.IntegrationID)
	if err != nil {
		return zoneTarget{}, err
	}
	target := zoneTarget{name: defaultZone, capable: capable}
	if server.Spec.ZoneID == "" {
		return target, nil
	}
	name, realized, found, err := f.catalog.ZoneOn(ctx, server.Spec.ZoneID, server.Source.IntegrationID)
	if err != nil {
		return zoneTarget{}, err
	}
	target.name, target.pending, target.missing = name, found && !realized, !found
	return target, nil
}

// drift reports a machine outside its spec zone. A provisioner without grouping reports no zone,
// so only a spec that names a Zone can differ there.
func (f serverPlacement) drift(ctx context.Context, server *serverdomain.Server) (string, error) {
	if server.Spec.ZoneID == "" && server.Observed.ProviderZone == "" {
		return "", nil
	}
	t, err := f.target(ctx, server)
	if err != nil {
		return "", err
	}
	switch {
	case t.missing:
		return "Placement differs from the spec (its zone no longer exists in the catalog).", nil
	case t.pending:
		return "Placement differs from the spec (its zone is not on the provisioner yet).", nil
	case !t.capable && t.name == "", server.Observed.ProviderZone == t.name:
		return "", nil
	}
	return fmt.Sprintf("Placement differs from the spec (zone is %q, the spec wants %q).", server.Observed.ProviderZone, t.name), nil
}

func (serverPlacement) capable(provider provisioningdomain.OSProvisioningProvider) bool {
	_, ok := provider.(provisioningdomain.GroupingController)
	return ok && provider.Capabilities().Grouping
}

// realize moves the machine into the spec's zone. A Zone the catalog no longer holds is a
// rejection (the spec must change); one the catalog controller has not created on the provisioner
// yet is transient.
func (f serverPlacement) realize(ctx context.Context, provider provisioningdomain.OSProvisioningProvider, server *serverdomain.Server) error {
	t, err := f.target(ctx, server)
	if err != nil {
		return err
	}
	switch {
	case t.missing:
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: "The zone in this Server's placement no longer exists; choose another.",
		}
	case t.pending:
		return fmt.Errorf("server %s: its zone is not on the provisioner yet", server.ID)
	case server.Observed.ProviderZone == t.name:
		return nil
	}
	_, err = provider.(provisioningdomain.GroupingController).SetMachineZone(ctx, server.Source.ProviderMachineID, t.name)
	return err
}

// observe re-reads the machine's zone and resource pool; both are recorded, though only the zone is
// judged against the spec.
func (f serverPlacement) observe(ctx context.Context, provider provisioningdomain.OSProvisioningProvider, server *serverdomain.Server) error {
	machine, err := provider.GetMachine(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return err
	}
	server.Observed.ProviderZone, server.Observed.ProviderResourcePool = machine.Zone, machine.ResourcePool
	return f.store.SetObservedPlacement(ctx, server.ID, machine.Zone, machine.ResourcePool)
}

// groupDefaults caches each provisioner's default zone name, which never changes for a
// provisioner, so judging a placement does not rebuild a provider for every Server.
type groupDefaults struct {
	providers provisioningdomain.ProviderFactory
	mu        sync.Mutex
	names     map[string]string
	incapable map[string]bool
}

// of returns the provisioner's default zone name and whether it can group at all.
func (d *groupDefaults) of(ctx context.Context, integrationID string) (zone string, capable bool, err error) {
	d.mu.Lock()
	if name, ok := d.names[integrationID]; ok {
		d.mu.Unlock()
		return name, true, nil
	}
	if d.incapable[integrationID] {
		d.mu.Unlock()
		return "", false, nil
	}
	d.mu.Unlock()

	provider, err := d.providers.For(ctx, integrationID)
	if err != nil {
		return "", false, err
	}
	controller, ok := provider.(provisioningdomain.GroupingController)
	d.mu.Lock()
	defer d.mu.Unlock()
	if !ok || !provider.Capabilities().Grouping {
		if d.incapable == nil {
			d.incapable = map[string]bool{}
		}
		d.incapable[integrationID] = true
		return "", false, nil
	}
	zone = controller.DefaultZoneName()
	d.names[integrationID] = zone
	return zone, true, nil
}
