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
		noPlatformPools{}, noPoolAccess{}, &storetest.Transactor{})
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

type noPlatformPools struct{}

func (noPlatformPools) PoolOf(context.Context, string) (string, bool, error) { return "", false, nil }

type noPoolAccess struct{}

func (noPoolAccess) DeletePool(context.Context, string) error { return nil }
