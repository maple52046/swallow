package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/maple52046/swallow/internal/core/convergence"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// CatalogController converges the Zone and Pool catalog onto every grouping-capable provisioner
// of each Site (glossary: Controller; decision 071). It is the only writer of their status.
//
// Keys are `zone:<id>` and `pool:<id>` for one group, and `adopt:<siteId>` for taking groups that
// exist only in a provisioner into the catalog. For each group and provisioner it:
//   - creates the group when it is missing there and renames it or rewrites its description when
//     the spec changed, at once on a spec change or a provisioner seen for the first time;
//   - reports drift (the group was removed, renamed, or redescribed in the provisioner) and
//     corrects it only on the periodic resync;
//   - reports a provisioner that cannot group as capability_missing, keeping the spec, and stops
//     on a rejection with requires_attention until the spec changes.
//
// Adoption runs on resync and when an Integration changes. A provisioner group that is not the
// provisioner's default and that no catalog group already names becomes a catalog group whose
// spec is the provisioner's name and description. A catalog group that has no generation yet (one
// migrated from before desired state) is adopted at its first observation: the provisioner's
// description of the group with its name becomes the spec. Adopting writes nothing to the
// provisioner.
// Groups are never deleted from a provisioner here: deleting is the API's synchronous action.
type CatalogController struct {
	zones     infradomain.ZoneRepository
	pools     infradomain.PoolRepository
	targets   infradomain.GroupingTargets
	providers provisioningdomain.ProviderFactory
	events    convergence.EventRecorder
	now       func() time.Time
	newID     func() string
	log       *slog.Logger
}

var _ convergence.Controller = (*CatalogController)(nil)

// NewCatalogController wires the catalog repositories, the Site provisioner lookup, the provider
// factory, and the convergence timeline.
func NewCatalogController(
	zones infradomain.ZoneRepository,
	pools infradomain.PoolRepository,
	targets infradomain.GroupingTargets,
	providers provisioningdomain.ProviderFactory,
	events convergence.EventRecorder,
) *CatalogController {
	return &CatalogController{
		zones: zones, pools: pools, targets: targets, providers: providers, events: events,
		now:   func() time.Time { return time.Now().UTC() },
		newID: uuid.NewString,
		log:   slog.Default().With("controller", "zone-pool-catalog"),
	}
}

// Name implements convergence.Controller.
func (c *CatalogController) Name() string { return "zone-pool-catalog" }

// Collections implements convergence.Controller. Integrations are watched so a provisioner that
// is added, enabled, or re-credentialed receives the catalog without waiting for the resync.
func (c *CatalogController) Collections() []string {
	return []string{"zones", "pools", "integrations"}
}

// KeysForChange implements convergence.Controller.
func (c *CatalogController) KeysForChange(ctx context.Context, change store.Change) ([]string, error) {
	switch change.Collection {
	case "zones", "pools":
		if change.Operation == store.ChangeDelete {
			return nil, nil
		}
		return []string{strings.TrimSuffix(change.Collection, "s") + ":" + change.Key}, nil
	default:
		return c.Keys(ctx)
	}
}

// Keys implements convergence.Controller: an adoption key per Site with a provisioner, then every
// Zone and Pool.
func (c *CatalogController) Keys(ctx context.Context) ([]string, error) {
	sites, err := c.targets.Sites(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(sites))
	for _, site := range sites {
		keys = append(keys, "adopt:"+site)
	}
	for _, kind := range c.kinds() {
		groups, err := kind.list(ctx, "")
		if err != nil {
			return nil, err
		}
		for _, group := range groups {
			keys = append(keys, kind.name+":"+group.ID)
		}
	}
	return keys, nil
}

// Reconcile implements convergence.Controller.
func (c *CatalogController) Reconcile(ctx context.Context, request convergence.Request) error {
	kindName, id, ok := strings.Cut(request.Key, ":")
	if !ok {
		return fmt.Errorf("catalog controller: malformed key %q", request.Key)
	}
	if kindName == "adopt" {
		return c.adopt(ctx, id)
	}
	for _, kind := range c.kinds() {
		if kind.name == kindName {
			return c.reconcileGroup(ctx, kind, id, request.Trigger)
		}
	}
	return fmt.Errorf("catalog controller: unknown key %q", request.Key)
}

// reconcileGroup converges one group on every provisioner of its Site and writes the status when
// it changed. A transient provider error is returned after the status is written, so the runtime
// retries with backoff.
func (c *CatalogController) reconcileGroup(ctx context.Context, kind groupKind, id string, trigger convergence.Trigger) error {
	group, err := kind.find(ctx, id)
	if err != nil || group == nil {
		return err
	}
	targets, err := c.targets.ForSite(ctx, group.SiteID)
	if err != nil {
		return err
	}
	if group.Generation == 0 {
		if err := c.adoptGroup(ctx, kind, group, targets); err != nil {
			return err
		}
	}
	now := c.now()
	next := infradomain.GroupStatus{ObservedGeneration: group.Status.ObservedGeneration}
	var transient error
	for _, target := range targets {
		realization, err := c.reconcileTarget(ctx, kind, group, group.Status.Realization(target), trigger, now)
		if err != nil {
			transient = err
		}
		next.Provisioners = append(next.Provisioners, realization)
	}
	next.Conditions = c.aggregate(group, next.Provisioners, now)
	if convergence.IsTrue(next.Conditions, convergence.ConditionRealized) {
		next.ObservedGeneration = group.Generation
	}
	if !reflect.DeepEqual(next, group.Status) {
		if err := kind.setStatus(ctx, group.ID, next); err != nil && !isGroupNotFound(err) {
			return err
		}
	}
	return transient
}

// reconcileTarget converges one group on one provisioner and returns its new realization.
func (c *CatalogController) reconcileTarget(
	ctx context.Context,
	kind groupKind,
	group *catalogGroup,
	r infradomain.GroupRealization,
	trigger convergence.Trigger,
	now time.Time,
) (infradomain.GroupRealization, error) {
	before := r
	s := &targetConditions{conditions: r.Conditions, now: now}
	generation := group.Generation
	observed := func() infradomain.GroupRealization {
		r.Conditions = s.conditions
		if s.changed || trigger == convergence.TriggerResync || r.ObservedAt == nil {
			r.ObservedAt = &now
		} else {
			r.ObservedAt = before.ObservedAt
		}
		return r
	}

	provider, err := c.providers.For(ctx, r.TargetID)
	if err != nil {
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderUnreachable,
			"The provisioner could not be reached; retrying.", generation)
		return observed(), err
	}
	gc, ok := provider.(provisioningdomain.GroupingController)
	if !ok || !provider.Capabilities().Grouping {
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonCapabilityMissing,
			fmt.Sprintf("The provisioner cannot hold %ss; swallow keeps this one.", kind.name), generation)
		return observed(), nil
	}
	if attention, found := convergence.FindCondition(s.conditions, convergence.ConditionRequiresAttention); found &&
		attention.Status == convergence.ConditionTrue && attention.ObservedGeneration == generation {
		return observed(), nil
	}
	groups, err := kind.providerGroups(ctx, gc)
	if err != nil {
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderUnreachable,
			"The provisioner could not be reached; retrying.", generation)
		return observed(), err
	}

	pending := r.ObservedGeneration < generation
	drift := groupDrift(groups, r.ProviderName, group.Name, group.Description)
	if drift == "" {
		r.ProviderName = group.Name
		c.markRealized(ctx, kind, group, &r, s, pending, false)
		return observed(), nil
	}
	if !pending {
		if !convergence.IsTrue(s.conditions, convergence.ConditionDrifted) {
			c.record(ctx, kind, group, convergence.EventDriftDetected, drift)
		}
		s.set(convergence.ConditionDrifted, convergence.ConditionTrue, convergence.ReasonDriftDetected, drift, generation)
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonConverging,
			"Drift is corrected at the next resync.", generation)
		if trigger != convergence.TriggerResync {
			return observed(), nil
		}
	}

	if err := realizeGroup(ctx, kind, gc, groups, r.ProviderName, group.Name, group.Description); err != nil {
		var rejected *provisioningdomain.ProviderError
		if errors.As(err, &rejected) && rejected.Kind == provisioningdomain.ProviderErrorRejected {
			s.set(convergence.ConditionRequiresAttention, convergence.ConditionTrue, convergence.ReasonProviderRejected, rejected.Detail, generation)
			s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonFailed, rejected.Detail, generation)
			c.record(ctx, kind, group, convergence.EventConvergenceFailed, rejected.Detail)
			return observed(), nil
		}
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderUnreachable,
			"The provisioner could not be reached; retrying.", generation)
		return observed(), err
	}
	r.ProviderName = group.Name
	c.markRealized(ctx, kind, group, &r, s, pending, !pending)
	return observed(), nil
}

func (c *CatalogController) markRealized(
	ctx context.Context, kind groupKind, group *catalogGroup, r *infradomain.GroupRealization,
	s *targetConditions, pending, corrected bool,
) {
	generation := group.Generation
	wasDrifted := convergence.IsTrue(s.conditions, convergence.ConditionDrifted)
	s.set(convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged, "", generation)
	s.set(convergence.ConditionDrifted, convergence.ConditionFalse, convergence.ReasonInSync, "", generation)
	s.set(convergence.ConditionRequiresAttention, convergence.ConditionFalse, convergence.ReasonAttentionNotNeeded, "", generation)
	r.ObservedGeneration = generation
	switch {
	case corrected:
		c.record(ctx, kind, group, convergence.EventDriftCorrected, "Drift was corrected to the spec.")
	case pending:
		c.record(ctx, kind, group, convergence.EventConverged, "The spec was realized.")
	case wasDrifted:
		c.record(ctx, kind, group, convergence.EventDriftResolved, "The provisioner matches the spec again.")
	}
}

// aggregate summarizes the per-provisioner realizations: realized only when every provisioner
// realized the current generation (a Site without provisioners has nothing to realize), drifted
// or requires_attention when any provisioner is. A false realized carries the first failing
// provisioner's reason and message.
func (c *CatalogController) aggregate(group *catalogGroup, realizations []infradomain.GroupRealization, now time.Time) []convergence.Condition {
	s := &targetConditions{conditions: group.Status.Conditions, now: now}
	generation := group.Generation
	realized, realizedReason, realizedMessage := true, convergence.ReasonConverged, ""
	if len(realizations) == 0 {
		realizedMessage = "The Site has no provisioner to hold it."
	}
	drifted, attention := false, false
	var driftMessage, attentionMessage string
	for _, r := range realizations {
		cond, ok := convergence.FindCondition(r.Conditions, convergence.ConditionRealized)
		if realized && (!ok || cond.Status != convergence.ConditionTrue || r.ObservedGeneration < generation) {
			realized = false
			realizedReason, realizedMessage = convergence.ReasonConverging, ""
			if ok && cond.Status == convergence.ConditionFalse {
				realizedReason, realizedMessage = cond.Reason, cond.Message
			}
		}
		if d, ok := convergence.FindCondition(r.Conditions, convergence.ConditionDrifted); ok && d.Status == convergence.ConditionTrue && !drifted {
			drifted, driftMessage = true, d.Message
		}
		if a, ok := convergence.FindCondition(r.Conditions, convergence.ConditionRequiresAttention); ok && a.Status == convergence.ConditionTrue && !attention {
			attention, attentionMessage = true, a.Message
		}
	}
	if realized {
		s.set(convergence.ConditionRealized, convergence.ConditionTrue, realizedReason, realizedMessage, generation)
	} else {
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, realizedReason, realizedMessage, generation)
	}
	if drifted {
		s.set(convergence.ConditionDrifted, convergence.ConditionTrue, convergence.ReasonDriftDetected, driftMessage, generation)
	} else {
		s.set(convergence.ConditionDrifted, convergence.ConditionFalse, convergence.ReasonInSync, "", generation)
	}
	if attention {
		s.set(convergence.ConditionRequiresAttention, convergence.ConditionTrue, convergence.ReasonProviderRejected, attentionMessage, generation)
	} else {
		s.set(convergence.ConditionRequiresAttention, convergence.ConditionFalse, convergence.ReasonAttentionNotNeeded, "", generation)
	}
	return s.conditions
}

// adopt takes the provisioner groups of a Site that no catalog group names into the catalog. A
// provisioner that cannot be read is skipped; the next resync tries again.
func (c *CatalogController) adopt(ctx context.Context, siteID string) error {
	targets, err := c.targets.ForSite(ctx, siteID)
	if err != nil {
		return err
	}
	for _, target := range targets {
		provider, err := c.providers.For(ctx, target)
		if err != nil {
			c.log.Warn("catalog adoption skipped a provisioner", "integration", target, "error", err)
			continue
		}
		gc, ok := provider.(provisioningdomain.GroupingController)
		if !ok || !provider.Capabilities().Grouping {
			continue
		}
		for _, kind := range c.kinds() {
			if err := c.adoptKind(ctx, kind, gc, siteID, target); err != nil {
				c.log.Warn("catalog adoption failed", "kind", kind.name, "integration", target, "error", err)
			}
		}
	}
	return nil
}

func (c *CatalogController) adoptKind(ctx context.Context, kind groupKind, gc provisioningdomain.GroupingController, siteID, target string) error {
	groups, err := kind.providerGroups(ctx, gc)
	if err != nil {
		return err
	}
	catalog, err := kind.list(ctx, siteID)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, group := range catalog {
		known[group.Name] = true
		if name := group.Status.Realization(target).ProviderName; name != "" {
			known[name] = true
		}
	}
	now := c.now()
	for _, providerGroup := range groups {
		if providerGroup.Default || known[providerGroup.Name] {
			continue
		}
		if _, err := infradomain.ValidateName(providerGroup.Name); err != nil {
			continue
		}
		group := &catalogGroup{
			ID: c.newID(), SiteID: siteID, Generation: 1,
			Name: providerGroup.Name, Description: providerGroup.Description,
			CreatedAt: now, UpdatedAt: now,
		}
		realization := infradomain.GroupRealization{
			TargetStatus: convergence.TargetStatus{TargetID: target, ObservedGeneration: 1, ObservedAt: &now},
			ProviderName: providerGroup.Name,
		}
		s := &targetConditions{now: now}
		s.set(convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged, "", 1)
		s.set(convergence.ConditionDrifted, convergence.ConditionFalse, convergence.ReasonInSync, "", 1)
		s.set(convergence.ConditionRequiresAttention, convergence.ConditionFalse, convergence.ReasonAttentionNotNeeded, "", 1)
		realization.Conditions = s.conditions
		group.Status = infradomain.GroupStatus{ObservedGeneration: 1, Provisioners: []infradomain.GroupRealization{realization}}
		group.Status.Conditions = c.aggregate(group, group.Status.Provisioners, now)
		if err := kind.create(ctx, group); err != nil {
			if isGroupNameTaken(err) {
				continue
			}
			return err
		}
		known[group.Name] = true
		c.record(ctx, kind, group, convergence.EventAdopted, "The provisioner's group became part of the catalog.")
	}
	return nil
}

// adoptGroup makes a group without a generation desired state: the first grouping-capable
// provisioner that holds a group of its name supplies the description, which only the store
// receives, and that provisioner counts as having realized generation 1. When no provisioner holds
// it, the group's own description becomes the spec and is realized normally. A provisioner that
// cannot be read leaves the group unadopted; the runtime retries.
func (c *CatalogController) adoptGroup(ctx context.Context, kind groupKind, group *catalogGroup, targets []string) error {
	description, source := group.Description, ""
	for _, target := range targets {
		provider, err := c.providers.For(ctx, target)
		if err != nil {
			return err
		}
		gc, ok := provider.(provisioningdomain.GroupingController)
		if !ok || !provider.Capabilities().Grouping {
			continue
		}
		groups, err := kind.providerGroups(ctx, gc)
		if err != nil {
			return err
		}
		if observed := findGroup(groups, group.Name); observed != nil {
			description, source = observed.Description, target
			break
		}
	}
	if err := kind.adoptSpec(ctx, group.ID, description, c.now()); err != nil {
		return err
	}
	group.Generation, group.Description = 1, description
	message := "The catalog group became the spec."
	if source != "" {
		group.Status.Provisioners = adoptedRealization(group.Status.Provisioners, source, group.Name)
		message = "The provisioner's group became the spec."
	}
	c.record(ctx, kind, group, convergence.EventAdopted, message)
	return nil
}

// adoptedRealization records that target already holds generation 1 of the group as name.
func adoptedRealization(realizations []infradomain.GroupRealization, target, name string) []infradomain.GroupRealization {
	for i := range realizations {
		if realizations[i].TargetID == target {
			realizations[i].ObservedGeneration, realizations[i].ProviderName = 1, name
			return realizations
		}
	}
	return append(realizations, infradomain.GroupRealization{
		TargetStatus: convergence.TargetStatus{TargetID: target, ObservedGeneration: 1},
		ProviderName: name,
	})
}

func (c *CatalogController) record(ctx context.Context, kind groupKind, group *catalogGroup, eventType convergence.EventType, message string) {
	if c.events == nil {
		return
	}
	err := c.events.Record(ctx, convergence.Event{
		ResourceKind: kind.name, ResourceID: group.ID, Type: eventType,
		Message: message, Generation: group.Generation, At: c.now(),
	})
	if err != nil {
		c.log.Warn("convergence event not recorded", kind.name, group.ID, "type", eventType, "error", err)
	}
}

// groupDrift describes how the provisioner departs from the spec for a group realized there as
// providerName ("" before it was), or returns "" when it matches.
func groupDrift(groups []provisioningdomain.ProviderGroup, providerName, name, description string) string {
	current := findGroup(groups, providerName)
	if current == nil && providerName != name {
		current = findGroup(groups, name)
	}
	switch {
	case current == nil:
		return fmt.Sprintf("%q is missing from the provisioner.", name)
	case current.Name != name:
		return fmt.Sprintf("The provisioner calls it %q.", current.Name)
	case current.Description != description:
		return "The provisioner has a different description."
	}
	return ""
}

// realizeGroup makes the provisioner hold the group as name with description: it renames the
// group it was realized as, rewrites the description of a group that already has the name, or
// creates it.
func realizeGroup(
	ctx context.Context, kind groupKind, gc provisioningdomain.GroupingController,
	groups []provisioningdomain.ProviderGroup, providerName, name, description string,
) error {
	if existing := findGroup(groups, name); existing != nil {
		if existing.Description == description {
			return nil
		}
		return kind.rename(ctx, gc, name, name, description)
	}
	if providerName != "" && findGroup(groups, providerName) != nil {
		return kind.rename(ctx, gc, providerName, name, description)
	}
	return kind.ensure(ctx, gc, name, description)
}

func findGroup(groups []provisioningdomain.ProviderGroup, name string) *provisioningdomain.ProviderGroup {
	if name == "" {
		return nil
	}
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i]
		}
	}
	return nil
}

// targetConditions accumulates condition changes during one Reconcile.
type targetConditions struct {
	conditions []convergence.Condition
	changed    bool
	now        time.Time
}

func (s *targetConditions) set(t convergence.ConditionType, status convergence.ConditionStatus, reason, message string, generation int64) {
	var changed bool
	s.conditions, changed = convergence.SetCondition(s.conditions, convergence.Condition{
		Type: t, Status: status, Reason: reason, Message: message, ObservedGeneration: generation,
	}, s.now)
	s.changed = s.changed || changed
}

// catalogGroup is a Zone or Pool as the controller handles it, so both kinds share one algorithm.
type catalogGroup struct {
	ID          string
	SiteID      string
	Generation  int64
	Name        string
	Description string
	Status      infradomain.GroupStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// groupKind adapts the Zone or Pool repository and provider calls to catalogGroup.
type groupKind struct {
	name           string
	find           func(ctx context.Context, id string) (*catalogGroup, error)
	list           func(ctx context.Context, siteID string) ([]*catalogGroup, error)
	create         func(ctx context.Context, group *catalogGroup) error
	setStatus      func(ctx context.Context, id string, status infradomain.GroupStatus) error
	adoptSpec      func(ctx context.Context, id, description string, at time.Time) error
	providerGroups func(ctx context.Context, gc provisioningdomain.GroupingController) ([]provisioningdomain.ProviderGroup, error)
	ensure         func(ctx context.Context, gc provisioningdomain.GroupingController, name, description string) error
	rename         func(ctx context.Context, gc provisioningdomain.GroupingController, from, to, description string) error
}

func (c *CatalogController) kinds() []groupKind {
	return []groupKind{
		{
			name: "zone",
			find: func(ctx context.Context, id string) (*catalogGroup, error) {
				zone, err := c.zones.FindByID(ctx, id)
				if errors.Is(err, infradomain.ErrZoneNotFound) {
					return nil, nil
				}
				if err != nil {
					return nil, err
				}
				return fromZone(zone), nil
			},
			list: func(ctx context.Context, siteID string) ([]*catalogGroup, error) {
				zones, err := c.zones.List(ctx, siteID)
				out := make([]*catalogGroup, len(zones))
				for i, zone := range zones {
					out[i] = fromZone(zone)
				}
				return out, err
			},
			create: func(ctx context.Context, g *catalogGroup) error {
				return c.zones.Create(ctx, &infradomain.Zone{
					ID: g.ID, SiteID: g.SiteID, Generation: g.Generation, Name: g.Name, Description: g.Description,
					Status: g.Status, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
				})
			},
			setStatus: c.zones.SetStatus,
			adoptSpec: c.zones.AdoptSpec,
			providerGroups: func(ctx context.Context, gc provisioningdomain.GroupingController) ([]provisioningdomain.ProviderGroup, error) {
				return gc.ListZones(ctx)
			},
			ensure: func(ctx context.Context, gc provisioningdomain.GroupingController, name, description string) error {
				return gc.EnsureZone(ctx, name, description)
			},
			rename: func(ctx context.Context, gc provisioningdomain.GroupingController, from, to, description string) error {
				return gc.RenameZone(ctx, from, to, description)
			},
		},
		{
			name: "pool",
			find: func(ctx context.Context, id string) (*catalogGroup, error) {
				pool, err := c.pools.FindByID(ctx, id)
				if errors.Is(err, infradomain.ErrPoolNotFound) {
					return nil, nil
				}
				if err != nil {
					return nil, err
				}
				return fromPool(pool), nil
			},
			list: func(ctx context.Context, siteID string) ([]*catalogGroup, error) {
				pools, err := c.pools.List(ctx, siteID)
				out := make([]*catalogGroup, len(pools))
				for i, pool := range pools {
					out[i] = fromPool(pool)
				}
				return out, err
			},
			create: func(ctx context.Context, g *catalogGroup) error {
				return c.pools.Create(ctx, &infradomain.Pool{
					ID: g.ID, SiteID: g.SiteID, Generation: g.Generation, Name: g.Name, Description: g.Description,
					Status: g.Status, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
				})
			},
			setStatus: c.pools.SetStatus,
			adoptSpec: c.pools.AdoptSpec,
			providerGroups: func(ctx context.Context, gc provisioningdomain.GroupingController) ([]provisioningdomain.ProviderGroup, error) {
				return gc.ListPools(ctx)
			},
			ensure: func(ctx context.Context, gc provisioningdomain.GroupingController, name, description string) error {
				return gc.EnsurePool(ctx, name, description)
			},
			rename: func(ctx context.Context, gc provisioningdomain.GroupingController, from, to, description string) error {
				return gc.RenamePool(ctx, from, to, description)
			},
		},
	}
}

func fromZone(zone *infradomain.Zone) *catalogGroup {
	return &catalogGroup{
		ID: zone.ID, SiteID: zone.SiteID, Generation: zone.Generation, Name: zone.Name, Description: zone.Description,
		Status: zone.Status, CreatedAt: zone.CreatedAt, UpdatedAt: zone.UpdatedAt,
	}
}

func fromPool(pool *infradomain.Pool) *catalogGroup {
	return &catalogGroup{
		ID: pool.ID, SiteID: pool.SiteID, Generation: pool.Generation, Name: pool.Name, Description: pool.Description,
		Status: pool.Status, CreatedAt: pool.CreatedAt, UpdatedAt: pool.UpdatedAt,
	}
}

func isGroupNotFound(err error) bool {
	return errors.Is(err, infradomain.ErrZoneNotFound) || errors.Is(err, infradomain.ErrPoolNotFound)
}

func isGroupNameTaken(err error) bool {
	return errors.Is(err, infradomain.ErrZoneNameTaken) || errors.Is(err, infradomain.ErrPoolNameTaken)
}
