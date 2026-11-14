package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/infrastructure/application"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	"github.com/maple52046/swallow/internal/shared/apicontract"
)

// The Pool routes answer in the contract's installation-wide shape (no Site, generation, or
// status), refuse a duplicate name across the installation, and accept a placement in a Pool
// whatever the Server's Site; none of them reaches a provisioner.
func TestPoolRoutes(t *testing.T) {
	pools := &memoryPools{byID: map[string]*infradomain.Pool{}}
	placements := &memoryPlacements{current: infradomain.ServerPlacement{ServerID: "srv-1", SiteID: "site-b", Generation: 1}}
	service := application.NewGroupingService(nil, pools, nil, placements, noReferences{}, noProviders{},
		platformServers{}, noPoolAccess{}, &storetest.Transactor{})
	handler := NewInfrastructureHandler(service)
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	app.Post("/api/v1/infrastructure/pools", handler.CreatePool)
	app.Get("/api/v1/infrastructure/pools", handler.ListPools)
	app.Get("/api/v1/infrastructure/pools/:id", handler.GetPool)
	app.Patch("/api/v1/infrastructure/pools/:id", handler.UpdatePool)
	app.Delete("/api/v1/infrastructure/pools/:id", handler.DeletePool)
	app.Put("/api/v1/servers/:id/placement", handler.AssignServerPlacement)

	resp := do(t, app, "POST", "/api/v1/infrastructure/pools", `{"name":"research","description":"GPU team"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create Pool: status %d", resp.StatusCode)
	}
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created Pool: %v", err)
	}
	id, _ := created["id"].(string)
	if created["name"] != "research" || id == "" {
		t.Fatalf("created = %v, want research with an id", created)
	}
	if resp := do(t, app, "POST", "/api/v1/infrastructure/pools", `{"name":"research"}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate Pool name: status %d, want 409", resp.StatusCode)
	}
	if resp := do(t, app, "POST", "/api/v1/infrastructure/pools", `{"description":"no name"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Pool without a name: status %d, want 400", resp.StatusCode)
	}
	if resp := do(t, app, "GET", "/api/v1/infrastructure/pools", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("list Pools: status %d", resp.StatusCode)
	}
	if resp := do(t, app, "PATCH", "/api/v1/infrastructure/pools/"+id, `{"name":"ml"}`); resp.StatusCode != http.StatusOK {
		t.Errorf("rename Pool: status %d", resp.StatusCode)
	}
	if resp := do(t, app, "PUT", "/api/v1/servers/srv-1/placement", `{"poolId":"`+id+`"}`); resp.StatusCode != http.StatusOK {
		t.Errorf("place a Server of another Site in the Pool: status %d, want 200", resp.StatusCode)
	}
	if resp := do(t, app, "GET", "/api/v1/infrastructure/pools/missing", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown Pool: status %d, want 404", resp.StatusCode)
	}
	if resp := do(t, app, "DELETE", "/api/v1/infrastructure/pools/"+id, ""); resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete Pool: status %d, want 204", resp.StatusCode)
	}
}

// A Platform's Server refuses a Pool of its own (409) and moves with its Platform, which answers
// with the Platform's Pool and every Server's placement; poolId must be given, as an id or null.
func TestPlatformPoolRoute(t *testing.T) {
	pools := &memoryPools{byID: map[string]*infradomain.Pool{"p-1": {ID: "p-1", Name: "research"}}}
	placements := &memoryPlacements{current: infradomain.ServerPlacement{ServerID: "srv-1", SiteID: "site-a", Generation: 1, ZoneID: "z-1", PlatformID: "plat-1"}}
	service := application.NewGroupingService(nil, pools, nil, placements, noReferences{}, noProviders{},
		platformServers{"plat-1": {"srv-1"}}, noPoolAccess{}, &storetest.Transactor{})
	handler := NewInfrastructureHandler(service)
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	app.Put("/api/v1/servers/:id/placement", handler.AssignServerPlacement)
	app.Put("/api/v1/platforms/:id/pool", handler.MovePlatformToPool)

	if resp := do(t, app, "PUT", "/api/v1/servers/srv-1/placement", `{"poolId":"p-1"}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("placing a claimed Server in a Pool: status %d, want 409", resp.StatusCode)
	}
	for _, body := range []string{`{}`, `{"poolId":5}`, `not json`} {
		if resp := do(t, app, "PUT", "/api/v1/platforms/plat-1/pool", body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("moving with body %s: status %d, want 400", body, resp.StatusCode)
		}
	}
	if resp := do(t, app, "PUT", "/api/v1/platforms/missing/pool", `{"poolId":"p-1"}`); resp.StatusCode != http.StatusNotFound {
		t.Errorf("moving an unknown Platform: status %d, want 404", resp.StatusCode)
	}
	if resp := do(t, app, "PUT", "/api/v1/platforms/plat-1/pool", `{"poolId":"p-missing"}`); resp.StatusCode != http.StatusNotFound {
		t.Errorf("moving to an unknown Pool: status %d, want 404", resp.StatusCode)
	}

	resp := do(t, app, "PUT", "/api/v1/platforms/plat-1/pool", `{"poolId":"p-1"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("moving the Platform: status %d, want 200", resp.StatusCode)
	}
	var moved application.PlatformPoolResult
	if err := json.NewDecoder(resp.Body).Decode(&moved); err != nil {
		t.Fatalf("decode the move: %v", err)
	}
	if len(moved.Servers) != 1 || moved.Servers[0].PoolID == nil || *moved.Servers[0].PoolID != "p-1" ||
		moved.Servers[0].ZoneID == nil || *moved.Servers[0].ZoneID != "z-1" {
		t.Errorf("moved = %+v, want srv-1 in p-1, still in z-1", moved)
	}
	if resp := do(t, app, "PUT", "/api/v1/platforms/plat-1/pool", `{"poolId":null}`); resp.StatusCode != http.StatusOK {
		t.Errorf("moving the Platform out of every Pool: status %d, want 200", resp.StatusCode)
	}
}

func do(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// memoryPools is an in-memory PoolRepository enforcing installation-wide name uniqueness.
type memoryPools struct{ byID map[string]*infradomain.Pool }

func (r *memoryPools) Create(_ context.Context, pool *infradomain.Pool) error {
	for _, existing := range r.byID {
		if existing.Name == pool.Name {
			return infradomain.ErrPoolNameTaken
		}
	}
	copied := *pool
	r.byID[pool.ID] = &copied
	return nil
}

func (r *memoryPools) FindByID(_ context.Context, id string) (*infradomain.Pool, error) {
	pool, ok := r.byID[id]
	if !ok {
		return nil, infradomain.ErrPoolNotFound
	}
	copied := *pool
	return &copied, nil
}

func (r *memoryPools) List(context.Context) ([]*infradomain.Pool, error) {
	out := make([]*infradomain.Pool, 0, len(r.byID))
	for _, pool := range r.byID {
		copied := *pool
		out = append(out, &copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *memoryPools) Update(_ context.Context, pool *infradomain.Pool) error {
	if _, ok := r.byID[pool.ID]; !ok {
		return infradomain.ErrPoolNotFound
	}
	copied := *pool
	r.byID[pool.ID] = &copied
	return nil
}

func (r *memoryPools) Delete(_ context.Context, id string) error {
	if _, ok := r.byID[id]; !ok {
		return infradomain.ErrPoolNotFound
	}
	delete(r.byID, id)
	return nil
}

// memoryPlacements holds one adopted Server's placement.
type memoryPlacements struct{ current infradomain.ServerPlacement }

func (p *memoryPlacements) Read(_ context.Context, serverID string) (*infradomain.ServerPlacement, error) {
	if serverID != p.current.ServerID {
		return nil, infradomain.ErrServerNotFound
	}
	copied := p.current
	return &copied, nil
}

func (p *memoryPlacements) Write(_ context.Context, _ string, expected int64, zoneID, poolID string) (int64, error) {
	if expected != p.current.Generation {
		return 0, infradomain.ErrPlacementConflict
	}
	p.current.Generation++
	p.current.ZoneID, p.current.PoolID = zoneID, poolID
	return p.current.Generation, nil
}

type noReferences struct{}

func (noReferences) CountZone(context.Context, string) (int, error) { return 0, nil }
func (noReferences) CountPool(context.Context, string) (int, error) { return 0, nil }

// noProviders fails every lookup, so a test fails if a Pool route reaches a provisioner.
type noProviders struct{}

func (noProviders) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return nil, provisioningdomain.ErrIntegrationNotProvisioner
}

// platformServers maps a Platform id onto its Servers; an absent id is an unknown Platform.
type platformServers map[string][]string

func (p platformServers) ServersOf(_ context.Context, platformID string) ([]string, bool, error) {
	ids, ok := p[platformID]
	return ids, ok, nil
}

type noPoolAccess struct{}

func (noPoolAccess) DeletePool(context.Context, string) error { return nil }
