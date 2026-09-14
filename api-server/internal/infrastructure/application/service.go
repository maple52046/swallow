// Package application coordinates swallow's Infrastructure grouping use cases: managing Zones
// and Pools and assigning a Server to them.
//
// Zones and Pools are plain Site-scoped CRUD with one twist — each write is also propagated to
// the Site's provisioner when it is grouping-capable — so, like the site feature, one service
// with methods carries the flow rather than a use-case type per operation. The service owns the
// realize-then-persist ordering and the cross-Site placement rule; it depends only on domain
// ports, so it runs without MongoDB, HTTP, or a live provider.
package application

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	infradomain "github.com/maple52046/swallow/internal/infrastructure/domain"
)

// GroupingService manages Zones and Pools and Server placement.
type GroupingService struct {
	zones    infradomain.ZoneRepository
	pools    infradomain.PoolRepository
	sites    infradomain.SiteReader
	servers  infradomain.ServerLocator
	realizer infradomain.GroupingRealizer
	now      func() time.Time
	newID    func() string
}

// NewGroupingService wires the grouping use cases. now and newID are injected so tests get a
// deterministic clock and id; production uses UTC wall-clock time and a random UUID.
func NewGroupingService(
	zones infradomain.ZoneRepository,
	pools infradomain.PoolRepository,
	sites infradomain.SiteReader,
	servers infradomain.ServerLocator,
	realizer infradomain.GroupingRealizer,
) *GroupingService {
	return &GroupingService{
		zones:    zones,
		pools:    pools,
		sites:    sites,
		servers:  servers,
		realizer: realizer,
		now:      func() time.Time { return time.Now().UTC() },
		newID:    uuid.NewString,
	}
}

// GroupItem is the API representation of a Zone or Pool. The two share a shape because they are
// structurally identical records; their distinct meaning is carried by which endpoint returns them.
type GroupItem struct {
	ID               string `json:"id"`
	SiteID           string `json:"siteId"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	ProviderRealized bool   `json:"providerRealized"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
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

// CreateZone validates the request, ensures the Site exists, realizes the Zone in the Site's
// provisioner when capable, then persists it. Realization runs before persistence so a provider
// refusal leaves no swallow record; because provider EnsureZone is idempotent, a later duplicate
// name (ErrZoneNameTaken) at persistence costs only a harmless repeat ensure.
func (s *GroupingService) CreateZone(ctx context.Context, input CreateGroupInput) (*GroupItem, error) {
	name, siteID, err := s.validateCreate(ctx, input)
	if err != nil {
		return nil, err
	}

	realized, err := s.realizer.EnsureZone(ctx, siteID, name, input.Description)
	if err != nil {
		return nil, err
	}

	now := s.now()
	zone := &infradomain.Zone{
		ID:               s.newID(),
		SiteID:           siteID,
		Name:             name,
		Description:      input.Description,
		ProviderRealized: realized,
		CreatedAt:        now,
		UpdatedAt:        now,
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

// UpdateZone applies the patch, propagates a name/description change to the provider when
// capable, and persists the result. A change is realized before persistence for the same reason
// as CreateZone.
func (s *GroupingService) UpdateZone(ctx context.Context, id string, input UpdateGroupInput) (*GroupItem, error) {
	zone, err := s.zones.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	newName, newDescription, changed, err := planUpdate(zone.Name, zone.Description, input)
	if err != nil {
		return nil, err
	}
	if !changed {
		item := zoneItem(zone)
		return &item, nil
	}

	realized, err := s.realizer.RenameZone(ctx, zone.SiteID, zone.Name, newName, newDescription)
	if err != nil {
		return nil, err
	}

	zone.Name = newName
	zone.Description = newDescription
	zone.ProviderRealized = realized
	zone.UpdatedAt = s.now()
	if err := s.zones.Update(ctx, zone); err != nil {
		return nil, err
	}
	item := zoneItem(zone)
	return &item, nil
}

// DeleteZone removes the Zone from the provider first (when capable) then from swallow. A
// provider that refuses — for example a zone still holding machines — aborts the delete with that
// refusal, so the swallow record is not removed while the provider still has the group.
func (s *GroupingService) DeleteZone(ctx context.Context, id string) error {
	zone, err := s.zones.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.realizer.DeleteZone(ctx, zone.SiteID, zone.Name); err != nil {
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

	realized, err := s.realizer.EnsurePool(ctx, siteID, name, input.Description)
	if err != nil {
		return nil, err
	}

	now := s.now()
	pool := &infradomain.Pool{
		ID:               s.newID(),
		SiteID:           siteID,
		Name:             name,
		Description:      input.Description,
		ProviderRealized: realized,
		CreatedAt:        now,
		UpdatedAt:        now,
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

	newName, newDescription, changed, err := planUpdate(pool.Name, pool.Description, input)
	if err != nil {
		return nil, err
	}
	if !changed {
		item := poolItem(pool)
		return &item, nil
	}

	realized, err := s.realizer.RenamePool(ctx, pool.SiteID, pool.Name, newName, newDescription)
	if err != nil {
		return nil, err
	}

	pool.Name = newName
	pool.Description = newDescription
	pool.ProviderRealized = realized
	pool.UpdatedAt = s.now()
	if err := s.pools.Update(ctx, pool); err != nil {
		return nil, err
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
	if _, err := s.realizer.DeletePool(ctx, pool.SiteID, pool.Name); err != nil {
		return err
	}
	return s.pools.Delete(ctx, id)
}

// --- Placement ---

// AssignServerInput carries a Server placement. A nil id leaves that grouping unchanged; at
// least one of the two must be set.
type AssignServerInput struct {
	ServerID string
	ZoneID   *string
	PoolID   *string
}

// PlacementResult reports the effective provider group names after a placement. A field that the
// request left unchanged carries the Server's currently observed value.
type PlacementResult struct {
	ServerID string `json:"serverId"`
	Zone     string `json:"zone"`
	Pool     string `json:"pool"`
}

// AssignServer moves a Server into a Zone and/or Pool by driving its provisioner.
//
// The referenced Zone/Pool must belong to the Server's Site; a cross-Site reference is refused
// (ErrGroupingSiteMismatch). Because a Server always has a provisioner, a provisioner that cannot
// group surfaces as ErrProviderGroupingUnsupported from the realizer rather than a silent no-op.
func (s *GroupingService) AssignServer(ctx context.Context, input AssignServerInput) (*PlacementResult, error) {
	if input.ZoneID == nil && input.PoolID == nil {
		return nil, infradomain.ErrNothingToAssign
	}

	target, err := s.servers.Locate(ctx, input.ServerID)
	if err != nil {
		return nil, err
	}

	result := &PlacementResult{
		ServerID: target.ServerID,
		Zone:     target.ObservedZone,
		Pool:     target.ObservedPool,
	}

	if input.ZoneID != nil {
		zone, err := s.zones.FindByID(ctx, *input.ZoneID)
		if err != nil {
			return nil, err
		}
		if zone.SiteID != target.SiteID {
			return nil, infradomain.ErrGroupingSiteMismatch
		}
		if err := s.realizer.AssignServerZone(ctx, target.IntegrationID, target.ProviderMachineID, zone.Name); err != nil {
			return nil, err
		}
		result.Zone = zone.Name
	}

	if input.PoolID != nil {
		pool, err := s.pools.FindByID(ctx, *input.PoolID)
		if err != nil {
			return nil, err
		}
		if pool.SiteID != target.SiteID {
			return nil, infradomain.ErrGroupingSiteMismatch
		}
		if err := s.realizer.AssignServerPool(ctx, target.IntegrationID, target.ProviderMachineID, pool.Name); err != nil {
			return nil, err
		}
		result.Pool = pool.Name
	}

	return result, nil
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
	return GroupItem{
		ID:               zone.ID,
		SiteID:           zone.SiteID,
		Name:             zone.Name,
		Description:      zone.Description,
		ProviderRealized: zone.ProviderRealized,
		CreatedAt:        zone.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        zone.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func poolItem(pool *infradomain.Pool) GroupItem {
	return GroupItem{
		ID:               pool.ID,
		SiteID:           pool.SiteID,
		Name:             pool.Name,
		Description:      pool.Description,
		ProviderRealized: pool.ProviderRealized,
		CreatedAt:        pool.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        pool.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
