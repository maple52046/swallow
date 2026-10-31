package application

import (
	"context"
	"errors"
	"sort"
	"sync"

	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

type fakeZoneRepo struct {
	mu        sync.Mutex
	byID      map[string]*infradomain.Zone
	setStatus int
}

func newFakeZoneRepo() *fakeZoneRepo { return &fakeZoneRepo{byID: map[string]*infradomain.Zone{}} }

func (r *fakeZoneRepo) Create(_ context.Context, zone *infradomain.Zone) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.byID {
		if existing.SiteID == zone.SiteID && existing.Name == zone.Name {
			return infradomain.ErrZoneNameTaken
		}
	}
	copied := *zone
	r.byID[zone.ID] = &copied
	return nil
}

func (r *fakeZoneRepo) FindByID(_ context.Context, id string) (*infradomain.Zone, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	zone, ok := r.byID[id]
	if !ok {
		return nil, infradomain.ErrZoneNotFound
	}
	copied := *zone
	return &copied, nil
}

func (r *fakeZoneRepo) FindByName(_ context.Context, siteID, name string) (*infradomain.Zone, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, zone := range r.byID {
		if zone.SiteID == siteID && zone.Name == name {
			copied := *zone
			return &copied, nil
		}
	}
	return nil, infradomain.ErrZoneNotFound
}

func (r *fakeZoneRepo) List(_ context.Context, siteID string) ([]*infradomain.Zone, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*infradomain.Zone
	for _, zone := range r.byID {
		if siteID == "" || zone.SiteID == siteID {
			copied := *zone
			out = append(out, &copied)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *fakeZoneRepo) UpdateSpec(_ context.Context, zone *infradomain.Zone) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.byID[zone.ID]
	if !ok {
		return infradomain.ErrZoneNotFound
	}
	stored.Name, stored.Description, stored.UpdatedAt = zone.Name, zone.Description, zone.UpdatedAt
	stored.Generation++
	zone.Generation = stored.Generation
	return nil
}

func (r *fakeZoneRepo) SetStatus(_ context.Context, id string, status infradomain.GroupStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.byID[id]
	if !ok {
		return infradomain.ErrZoneNotFound
	}
	stored.Status = status
	r.setStatus++
	return nil
}

func (r *fakeZoneRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id]; !ok {
		return infradomain.ErrZoneNotFound
	}
	delete(r.byID, id)
	return nil
}

func (r *fakeZoneRepo) get(id string) *infradomain.Zone {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id]
}

type fakePoolRepo struct {
	mu   sync.Mutex
	byID map[string]*infradomain.Pool
}

func newFakePoolRepo() *fakePoolRepo { return &fakePoolRepo{byID: map[string]*infradomain.Pool{}} }

func (r *fakePoolRepo) Create(_ context.Context, pool *infradomain.Pool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.byID {
		if existing.SiteID == pool.SiteID && existing.Name == pool.Name {
			return infradomain.ErrPoolNameTaken
		}
	}
	copied := *pool
	r.byID[pool.ID] = &copied
	return nil
}

func (r *fakePoolRepo) FindByID(_ context.Context, id string) (*infradomain.Pool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pool, ok := r.byID[id]
	if !ok {
		return nil, infradomain.ErrPoolNotFound
	}
	copied := *pool
	return &copied, nil
}

func (r *fakePoolRepo) FindByName(_ context.Context, siteID, name string) (*infradomain.Pool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, pool := range r.byID {
		if pool.SiteID == siteID && pool.Name == name {
			copied := *pool
			return &copied, nil
		}
	}
	return nil, infradomain.ErrPoolNotFound
}

func (r *fakePoolRepo) List(_ context.Context, siteID string) ([]*infradomain.Pool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*infradomain.Pool
	for _, pool := range r.byID {
		if siteID == "" || pool.SiteID == siteID {
			copied := *pool
			out = append(out, &copied)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *fakePoolRepo) UpdateSpec(_ context.Context, pool *infradomain.Pool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.byID[pool.ID]
	if !ok {
		return infradomain.ErrPoolNotFound
	}
	stored.Name, stored.Description, stored.UpdatedAt = pool.Name, pool.Description, pool.UpdatedAt
	stored.Generation++
	pool.Generation = stored.Generation
	return nil
}

func (r *fakePoolRepo) SetStatus(_ context.Context, id string, status infradomain.GroupStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.byID[id]
	if !ok {
		return infradomain.ErrPoolNotFound
	}
	stored.Status = status
	return nil
}

func (r *fakePoolRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id]; !ok {
		return infradomain.ErrPoolNotFound
	}
	delete(r.byID, id)
	return nil
}

type fakeSiteReader struct{ exists bool }

func (r *fakeSiteReader) Exists(context.Context, string) (bool, error) { return r.exists, nil }

type fakePlacements struct {
	current *infradomain.ServerPlacement
	writes  []infradomain.ServerPlacement
}

func (p *fakePlacements) Read(_ context.Context, serverID string) (*infradomain.ServerPlacement, error) {
	if p.current == nil || p.current.ServerID != serverID {
		return nil, infradomain.ErrServerNotFound
	}
	copied := *p.current
	return &copied, nil
}

func (p *fakePlacements) Write(_ context.Context, serverID string, expected int64, zoneID, poolID string) (int64, error) {
	if p.current.Generation != expected {
		return 0, infradomain.ErrPlacementConflict
	}
	p.current.Generation++
	p.current.ZoneID, p.current.PoolID = zoneID, poolID
	p.writes = append(p.writes, *p.current)
	return p.current.Generation, nil
}

type fakeReferences struct{ zones, pools map[string]int }

func (r fakeReferences) CountZone(_ context.Context, id string) (int, error) { return r.zones[id], nil }
func (r fakeReferences) CountPool(_ context.Context, id string) (int, error) { return r.pools[id], nil }

type fakeTargets struct{ bySite map[string][]string }

func (t fakeTargets) ForSite(_ context.Context, siteID string) ([]string, error) {
	return t.bySite[siteID], nil
}

func (t fakeTargets) Sites(context.Context) ([]string, error) {
	var sites []string
	for site := range t.bySite {
		sites = append(sites, site)
	}
	sort.Strings(sites)
	return sites, nil
}

type fakeFactory struct {
	providers map[string]provisioningdomain.OSProvisioningProvider
	err       error
}

func (f *fakeFactory) For(_ context.Context, integrationID string) (provisioningdomain.OSProvisioningProvider, error) {
	if f.err != nil {
		return nil, f.err
	}
	provider, ok := f.providers[integrationID]
	if !ok {
		return nil, errors.New("no such integration")
	}
	return provider, nil
}

// fakeGroupingProvider is a provisioner with zones and pools; grouping false models one without
// the capability. calls records every write as "op name".
type fakeGroupingProvider struct {
	grouping bool
	zones    []provisioningdomain.ProviderGroup
	pools    []provisioningdomain.ProviderGroup
	writeErr error
	listErr  error
	calls    []string
}

func newFakeGroupingProvider() *fakeGroupingProvider {
	return &fakeGroupingProvider{
		grouping: true,
		zones:    []provisioningdomain.ProviderGroup{{Name: "default", Default: true}},
		pools:    []provisioningdomain.ProviderGroup{{Name: "default", Default: true}},
	}
}

func (p *fakeGroupingProvider) Name() string { return "fake" }
func (p *fakeGroupingProvider) Probe(context.Context) (provisioningdomain.ProviderInfo, error) {
	return provisioningdomain.ProviderInfo{}, nil
}
func (p *fakeGroupingProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{Grouping: p.grouping}
}
func (p *fakeGroupingProvider) ListMachines(context.Context, provisioningdomain.MachineFilter) ([]*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *fakeGroupingProvider) GetMachine(context.Context, string) (*provisioningdomain.Machine, error) {
	return nil, provisioningdomain.ErrMachineNotFound
}
func (p *fakeGroupingProvider) ListOSImages(context.Context) ([]*provisioningdomain.OSImage, error) {
	return nil, nil
}
func (p *fakeGroupingProvider) Deploy(context.Context, provisioningdomain.DeployRequest) (*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *fakeGroupingProvider) Release(context.Context, string) (*provisioningdomain.Machine, error) {
	return nil, nil
}

func (p *fakeGroupingProvider) ListZones(context.Context) ([]provisioningdomain.ProviderGroup, error) {
	return append([]provisioningdomain.ProviderGroup(nil), p.zones...), p.listErr
}
func (p *fakeGroupingProvider) ListPools(context.Context) ([]provisioningdomain.ProviderGroup, error) {
	return append([]provisioningdomain.ProviderGroup(nil), p.pools...), p.listErr
}
func (p *fakeGroupingProvider) DefaultGroupNames() (string, string) { return "default", "default" }

func (p *fakeGroupingProvider) EnsureZone(_ context.Context, name, description string) error {
	return p.write("ensureZone "+name, func() { p.zones = ensureGroup(p.zones, name, description) })
}
func (p *fakeGroupingProvider) RenameZone(_ context.Context, from, to, description string) error {
	return p.write("renameZone "+from+"->"+to, func() { p.zones = renameGroup(p.zones, from, to, description) })
}
func (p *fakeGroupingProvider) DeleteZone(_ context.Context, name string) error {
	return p.write("deleteZone "+name, func() { p.zones = removeGroup(p.zones, name) })
}
func (p *fakeGroupingProvider) EnsurePool(_ context.Context, name, description string) error {
	return p.write("ensurePool "+name, func() { p.pools = ensureGroup(p.pools, name, description) })
}
func (p *fakeGroupingProvider) RenamePool(_ context.Context, from, to, description string) error {
	return p.write("renamePool "+from+"->"+to, func() { p.pools = renameGroup(p.pools, from, to, description) })
}
func (p *fakeGroupingProvider) DeletePool(_ context.Context, name string) error {
	return p.write("deletePool "+name, func() { p.pools = removeGroup(p.pools, name) })
}
func (p *fakeGroupingProvider) SetMachineZone(context.Context, string, string) (*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *fakeGroupingProvider) SetMachinePool(context.Context, string, string) (*provisioningdomain.Machine, error) {
	return nil, nil
}

func (p *fakeGroupingProvider) write(call string, apply func()) error {
	if p.writeErr != nil {
		return p.writeErr
	}
	p.calls = append(p.calls, call)
	apply()
	return nil
}

func ensureGroup(groups []provisioningdomain.ProviderGroup, name, description string) []provisioningdomain.ProviderGroup {
	for _, g := range groups {
		if g.Name == name {
			return groups
		}
	}
	return append(groups, provisioningdomain.ProviderGroup{Name: name, Description: description})
}

func renameGroup(groups []provisioningdomain.ProviderGroup, from, to, description string) []provisioningdomain.ProviderGroup {
	for i := range groups {
		if groups[i].Name == from {
			groups[i].Name, groups[i].Description = to, description
			return groups
		}
	}
	return ensureGroup(groups, to, description)
}

func removeGroup(groups []provisioningdomain.ProviderGroup, name string) []provisioningdomain.ProviderGroup {
	out := groups[:0]
	for _, g := range groups {
		if g.Name != name {
			out = append(out, g)
		}
	}
	return out
}
