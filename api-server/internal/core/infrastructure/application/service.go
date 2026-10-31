// Package application coordinates swallow's Infrastructure grouping use cases: managing Zones
// and Pools, converging them onto the Site's provisioners, and placing a Server in them.
//
// Zones and Pools are migrated resources (decision 071). The API writes their spec — name and
// description — and the catalog controller makes every grouping-capable provisioner of the Site
// match it, so a provisioner added later receives the existing catalog. A Server's placement is
// part of the Server's spec; this package validates and writes it, and the Server controller
// realizes it. Deleting is the one synchronous provider write: the group is removed from each
// provisioner before the record, so a provisioner that refuses keeps both. The package depends
// only on domain ports, so it runs without MongoDB, HTTP, or a live provider.
package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/maple52046/swallow/internal/core/convergence"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// GroupingService manages Zones and Pools and Server placement.
type GroupingService struct {
	zones      infradomain.ZoneRepository
	pools      infradomain.PoolRepository
	sites      infradomain.SiteReader
	placements infradomain.ServerPlacements
	references infradomain.PlacementReferences
	providers  provisioningdomain.ProviderFactory
	now        func() time.Time
	newID      func() string
}

// NewGroupingService wires the grouping use cases. The provider factory is used only to delete a
// group from the provisioners that hold it. now and newID are injected so tests get a
// deterministic clock and id; production uses UTC wall-clock time and a random UUID.
func NewGroupingService(
	zones infradomain.ZoneRepository,
	pools infradomain.PoolRepository,
	sites infradomain.SiteReader,
	placements infradomain.ServerPlacements,
	references infradomain.PlacementReferences,
	providers provisioningdomain.ProviderFactory,
) *GroupingService {
	return &GroupingService{
		zones:      zones,
		pools:      pools,
		sites:      sites,
		placements: placements,
		references: references,
		providers:  providers,
		now:        func() time.Time { return time.Now().UTC() },
		newID:      uuid.NewString,
	}
}

// GroupItem is the API representation of a Zone or Pool (infrastructure.md). The two share a shape
// because they are structurally identical resources; their distinct meaning is carried by which
// endpoint returns them.
type GroupItem struct {
	ID         string          `json:"id"`
	SiteID     string          `json:"siteId"`
	Generation int64           `json:"generation"`
	Spec       GroupSpecItem   `json:"spec"`
	Status     GroupStatusItem `json:"status"`
}

// GroupSpecItem is the published spec of a Zone or Pool.
type GroupSpecItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GroupStatusItem is the published status: the aggregate conditions over every provisioner of the
// Site and one realization per provisioner.
type GroupStatusItem struct {
	ObservedGeneration int64                       `json:"observedGeneration"`
	Conditions         []convergence.ConditionItem `json:"conditions"`
	Provisioners       []GroupRealizationItem      `json:"provisioners"`
	CreatedAt          string                      `json:"createdAt"`
	UpdatedAt          string                      `json:"updatedAt"`
}

// GroupRealizationItem is a Zone's or Pool's realization on one provisioner Integration.
// ProviderName is "" until the group exists there; ObservedAt is null before the first observation.
type GroupRealizationItem struct {
	IntegrationID      string                      `json:"integrationId"`
	ProviderName       string                      `json:"providerName"`
	ObservedGeneration int64                       `json:"observedGeneration"`
	ObservedAt         *string                     `json:"observedAt"`
	Conditions         []convergence.ConditionItem `json:"conditions"`
}

// CreateGroupInput is the create payload for a Zone or Pool.
type CreateGroupInput struct {
	SiteID      string
	Name        string
	Description string
}

// UpdateGroupInput is the patch payload for a Zone or Pool. A nil field is left unchanged.
type UpdateGroupInput struct {
	Name        *string
	Description *string
}

// --- Zones ---

// CreateZone validates the request, ensures the Site exists, and stores the Zone at generation 1.
// The catalog controller creates it on the Site's provisioners afterwards.
func (s *GroupingService) CreateZone(ctx context.Context, input CreateGroupInput) (*GroupItem, error) {
	name, siteID, err := s.validateCreate(ctx, input)
	if err != nil {
		return nil, err
	}
	now := s.now()
	zone := &infradomain.Zone{
		ID: s.newID(), SiteID: siteID, Generation: 1,
		Name: name, Description: input.Description, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.zones.Create(ctx, zone); err != nil {
		return nil, err
	}
	item := zoneItem(zone)
	return &item, nil
}

// ListZones returns Zones, optionally restricted to one Site.
func (s *GroupingService) ListZones(ctx context.Context, siteID string) ([]GroupItem, error) {
	zones, err := s.zones.List(ctx, strings.TrimSpace(siteID))
	if err != nil {
		return nil, err
	}
	items := make([]GroupItem, 0, len(zones))
	for _, zone := range zones {
		items = append(items, zoneItem(zone))
	}
	return items, nil
}

// GetZone returns one Zone or ErrZoneNotFound.
func (s *GroupingService) GetZone(ctx context.Context, id string) (*GroupItem, error) {
	zone, err := s.zones.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := zoneItem(zone)
	return &item, nil
}

// UpdateZone applies the patch to the spec and increments the generation; a patch that changes
// nothing returns the Zone as stored. The provisioners are renamed by the catalog controller.
func (s *GroupingService) UpdateZone(ctx context.Context, id string, input UpdateGroupInput) (*GroupItem, error) {
	zone, err := s.zones.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	name, description, changed, err := planUpdate(zone.Name, zone.Description, input)
	if err != nil {
		return nil, err
	}
	if changed {
		zone.Name, zone.Description, zone.UpdatedAt = name, description, s.now()
		if err := s.zones.UpdateSpec(ctx, zone); err != nil {
			return nil, err
		}
	}
	item := zoneItem(zone)
	return &item, nil
}

// DeleteZone refuses a Zone that is still a Server's placement (ErrGroupInUse), removes it from
// every provisioner that holds it, then deletes the record. A provisioner that refuses — for example
// a zone still holding machines — aborts the delete with that refusal and keeps the record.
func (s *GroupingService) DeleteZone(ctx context.Context, id string) error {
	zone, err := s.zones.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.refuseInUse(s.references.CountZone(ctx, id)); err != nil {
		return err
	}
	err = s.deleteFromProvisioners(ctx, zone.Status, func(gc provisioningdomain.GroupingController, name string) error {
		if defaultZone, _ := gc.DefaultGroupNames(); name == defaultZone {
			return nil
		}
		return gc.DeleteZone(ctx, name)
	})
	if err != nil {
		return err
	}
	return s.zones.Delete(ctx, id)
}

// --- Pools ---

// CreatePool mirrors CreateZone for a resource pool.
func (s *GroupingService) CreatePool(ctx context.Context, input CreateGroupInput) (*GroupItem, error) {
	name, siteID, err := s.validateCreate(ctx, input)
	if err != nil {
		return nil, err
	}
	now := s.now()
	pool := &infradomain.Pool{
		ID: s.newID(), SiteID: siteID, Generation: 1,
		Name: name, Description: input.Description, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.pools.Create(ctx, pool); err != nil {
		return nil, err
	}
	item := poolItem(pool)
	return &item, nil
}

// ListPools returns Pools, optionally restricted to one Site.
func (s *GroupingService) ListPools(ctx context.Context, siteID string) ([]GroupItem, error) {
	pools, err := s.pools.List(ctx, strings.TrimSpace(siteID))
	if err != nil {
		return nil, err
	}
	items := make([]GroupItem, 0, len(pools))
	for _, pool := range pools {
		items = append(items, poolItem(pool))
	}
	return items, nil
}

// GetPool returns one Pool or ErrPoolNotFound.
func (s *GroupingService) GetPool(ctx context.Context, id string) (*GroupItem, error) {
	pool, err := s.pools.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := poolItem(pool)
	return &item, nil
}

// UpdatePool mirrors UpdateZone for a resource pool.
func (s *GroupingService) UpdatePool(ctx context.Context, id string, input UpdateGroupInput) (*GroupItem, error) {
	pool, err := s.pools.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	name, description, changed, err := planUpdate(pool.Name, pool.Description, input)
	if err != nil {
		return nil, err
	}
	if changed {
		pool.Name, pool.Description, pool.UpdatedAt = name, description, s.now()
		if err := s.pools.UpdateSpec(ctx, pool); err != nil {
			return nil, err
		}
	}
	item := poolItem(pool)
	return &item, nil
}

// DeletePool mirrors DeleteZone for a resource pool.
func (s *GroupingService) DeletePool(ctx context.Context, id string) error {
	pool, err := s.pools.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.refuseInUse(s.references.CountPool(ctx, id)); err != nil {
		return err
	}
	err = s.deleteFromProvisioners(ctx, pool.Status, func(gc provisioningdomain.GroupingController, name string) error {
		if _, defaultPool := gc.DefaultGroupNames(); name == defaultPool {
			return nil
		}
		return gc.DeletePool(ctx, name)
	})
	if err != nil {
		return err
	}
	return s.pools.Delete(ctx, id)
}

// --- Placement ---

// PlacementChange is one half of a placement request: Set false leaves the field unchanged; Set
// true with an empty ID returns the Server to the provisioner's default group.
type PlacementChange struct {
	Set bool
	ID  string
}

// AssignServerInput carries a Server placement; at least one of Zone and Pool must be Set.
type AssignServerInput struct {
	ServerID string
	Zone     PlacementChange
	Pool     PlacementChange
}

// PlacementResult is the Server's placement spec after the request: the new generation and the
// Zone and Pool ids, null for the provisioner's default group.
type PlacementResult struct {
	ServerID   string  `json:"serverId"`
	Generation int64   `json:"generation"`
	ZoneID     *string `json:"zoneId"`
	PoolID     *string `json:"poolId"`
}

// AssignServer writes a Server's placement spec; the Server controller moves the machine
// afterwards. A referenced Zone or Pool must belong to the Server's Site
// (ErrGroupingSiteMismatch). A Server that is not adopted yet is ErrServerNotAdopted, and a
// concurrent spec change ErrPlacementConflict.
func (s *GroupingService) AssignServer(ctx context.Context, input AssignServerInput) (*PlacementResult, error) {
	if !input.Zone.Set && !input.Pool.Set {
		return nil, infradomain.ErrNothingToAssign
	}
	current, err := s.placements.Read(ctx, input.ServerID)
	if err != nil {
		return nil, err
	}
	if current.Generation == 0 {
		return nil, infradomain.ErrServerNotAdopted
	}
	zoneID, poolID := current.ZoneID, current.PoolID
	if input.Zone.Set {
		if input.Zone.ID != "" {
			zone, err := s.zones.FindByID(ctx, input.Zone.ID)
			if err != nil {
				return nil, err
			}
			if zone.SiteID != current.SiteID {
				return nil, infradomain.ErrGroupingSiteMismatch
			}
		}
		zoneID = input.Zone.ID
	}
	if input.Pool.Set {
		if input.Pool.ID != "" {
			pool, err := s.pools.FindByID(ctx, input.Pool.ID)
			if err != nil {
				return nil, err
			}
			if pool.SiteID != current.SiteID {
				return nil, infradomain.ErrGroupingSiteMismatch
			}
		}
		poolID = input.Pool.ID
	}
	generation, err := s.placements.Write(ctx, input.ServerID, current.Generation, zoneID, poolID)
	if err != nil {
		return nil, err
	}
	return &PlacementResult{ServerID: input.ServerID, Generation: generation, ZoneID: optionalID(zoneID), PoolID: optionalID(poolID)}, nil
}

func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

func (s *GroupingService) refuseInUse(count int, err error) error {
	if err != nil {
		return err
	}
	if count > 0 {
		return infradomain.ErrGroupInUse
	}
	return nil
}

// deleteFromProvisioners runs remove against every provisioner on which the group exists. A
// provisioner that can no longer group, whose Integration was deleted, or that the group never
// reached, has nothing to remove.
func (s *GroupingService) deleteFromProvisioners(
	ctx context.Context,
	status infradomain.GroupStatus,
	remove func(provisioningdomain.GroupingController, string) error,
) error {
	for _, realization := range status.Provisioners {
		if realization.ProviderName == "" {
			continue
		}
		provider, err := s.providers.For(ctx, realization.TargetID)
		if errors.Is(err, sitedomain.ErrIntegrationNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		gc, ok := provider.(provisioningdomain.GroupingController)
		if !ok || !provider.Capabilities().Grouping {
			continue
		}
		if err := remove(gc, realization.ProviderName); err != nil {
			return err
		}
	}
	return nil
}

// validateCreate normalizes and checks a create request shared by Zones and Pools: a valid name
// and an existing Site. It returns the cleaned name and Site id.
func (s *GroupingService) validateCreate(ctx context.Context, input CreateGroupInput) (name, siteID string, err error) {
	name, err = infradomain.ValidateName(input.Name)
	if err != nil {
		return "", "", err
	}
	siteID = strings.TrimSpace(input.SiteID)
	if siteID == "" {
		return "", "", infradomain.ErrInvalidGroup
	}
	exists, err := s.sites.Exists(ctx, siteID)
	if err != nil {
		return "", "", err
	}
	if !exists {
		return "", "", infradomain.ErrSiteNotFound
	}
	return name, siteID, nil
}

// planUpdate resolves a patch against current values, returning the new name and description and
// whether anything changed. A present name is validated; an absent field keeps its current value.
func planUpdate(currentName, currentDescription string, input UpdateGroupInput) (name, description string, changed bool, err error) {
	name = currentName
	description = currentDescription
	if input.Name != nil {
		validated, verr := infradomain.ValidateName(*input.Name)
		if verr != nil {
			return "", "", false, verr
		}
		name = validated
	}
	if input.Description != nil {
		description = *input.Description
	}
	return name, description, name != currentName || description != currentDescription, nil
}

func zoneItem(zone *infradomain.Zone) GroupItem {
	return groupItem(zone.ID, zone.SiteID, zone.Generation, zone.Name, zone.Description, zone.Status, zone.CreatedAt, zone.UpdatedAt)
}

func poolItem(pool *infradomain.Pool) GroupItem {
	return groupItem(pool.ID, pool.SiteID, pool.Generation, pool.Name, pool.Description, pool.Status, pool.CreatedAt, pool.UpdatedAt)
}

func groupItem(id, siteID string, generation int64, name, description string, status infradomain.GroupStatus, createdAt, updatedAt time.Time) GroupItem {
	provisioners := make([]GroupRealizationItem, 0, len(status.Provisioners))
	for _, r := range status.Provisioners {
		var observedAt *string
		if r.ObservedAt != nil {
			at := wire.Time(*r.ObservedAt)
			observedAt = &at
		}
		provisioners = append(provisioners, GroupRealizationItem{
			IntegrationID: r.TargetID, ProviderName: r.ProviderName, ObservedGeneration: r.ObservedGeneration,
			ObservedAt: observedAt, Conditions: convergence.ConditionItems(r.Conditions),
		})
	}
	return GroupItem{
		ID: id, SiteID: siteID, Generation: generation,
		Spec: GroupSpecItem{Name: name, Description: description},
		Status: GroupStatusItem{
			ObservedGeneration: status.ObservedGeneration,
			Conditions:         convergence.ConditionItems(status.Conditions),
			Provisioners:       provisioners,
			CreatedAt:          wire.Time(createdAt),
			UpdatedAt:          wire.Time(updatedAt),
		},
	}
}
