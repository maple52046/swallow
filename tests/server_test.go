package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	serverapp "github.com/AFDEAPAC/swallow/internal/server/application"
	serverdelivery "github.com/AFDEAPAC/swallow/internal/server/delivery"
	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
	"github.com/AFDEAPAC/swallow/internal/shared/jwt"
	"github.com/AFDEAPAC/swallow/internal/shared/middleware"
	"github.com/AFDEAPAC/swallow/internal/shared/pagination"
)

// fakeServerRepo is an in-memory ServerRepository for testing.
type fakeServerRepo struct {
	servers map[string]*serverdomain.Server
}

func newFakeServerRepo() *fakeServerRepo {
	return &fakeServerRepo{servers: make(map[string]*serverdomain.Server)}
}

func (r *fakeServerRepo) Create(_ context.Context, server *serverdomain.Server) error {
	for _, s := range r.servers {
		if s.Hostname == server.Hostname {
			return serverdomain.ErrHostnameTaken
		}
		if s.IP == server.IP {
			return serverdomain.ErrIPTaken
		}
	}
	r.servers[server.ID] = server
	return nil
}

func (r *fakeServerRepo) List(_ context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	var all []*serverdomain.Server
	for _, s := range r.servers {
		if filter.Status != "" && s.Status != filter.Status {
			continue
		}
		all = append(all, s)
	}
	total := len(all)
	start := filter.Offset
	if start > total {
		start = total
	}
	end := start + filter.Limit
	if end > total {
		end = total
	}
	return serverdomain.ListResult{Servers: all[start:end], Total: total}, nil
}

func (r *fakeServerRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.servers[id]; !ok {
		return serverdomain.ErrServerNotFound
	}
	delete(r.servers, id)
	return nil
}

func (r *fakeServerRepo) ExistsByHostname(_ context.Context, hostname string) (bool, error) {
	for _, s := range r.servers {
		if s.Hostname == hostname {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeServerRepo) ExistsByIP(_ context.Context, ip string) (bool, error) {
	for _, s := range r.servers {
		if s.IP == ip {
			return true, nil
		}
	}
	return false, nil
}

func setupServerApp(t *testing.T) (*fiber.App, *fakeServerRepo, *jwt.Service) {
	t.Helper()

	repo := newFakeServerRepo()
	jwtSvc := jwt.NewService("test-secret", time.Hour)

	createUC := serverapp.NewCreateServerUseCase(repo)
	listUC := serverapp.NewListServersUseCase(repo)
	deleteUC := serverapp.NewDeleteServerUseCase(repo)
	handler := serverdelivery.NewServerHandler(createUC, listUC, deleteUC)

	app := fiber.New()
	servers := app.Group("/api/v1/servers", middleware.Auth(jwtSvc), middleware.AdminOnly())
	servers.Post("/", handler.Create)
	servers.Get("/", handler.List)
	servers.Delete("/:id", handler.Delete)

	return app, repo, jwtSvc
}

func adminToken(t *testing.T, jwtSvc *jwt.Service) string {
	t.Helper()
	token, err := jwtSvc.Sign("admin-1", "admin", "admin")
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

func userToken(t *testing.T, jwtSvc *jwt.Service) string {
	t.Helper()
	token, err := jwtSvc.Sign("user-1", "alice", "user")
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

func TestCreateServer_Success(t *testing.T) {
	app, _, jwtSvc := setupServerApp(t)

	resp := doRequest(t, app, "POST", "/api/v1/servers/", map[string]string{
		"hostname": "node-01",
		"ip":       "10.0.0.1",
	}, map[string]string{"Authorization": "Bearer " + adminToken(t, jwtSvc)})

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["id"] == nil || body["id"] == "" {
		t.Fatal("expected id in response")
	}
}

func TestCreateServer_DuplicateHostname(t *testing.T) {
	app, repo, jwtSvc := setupServerApp(t)

	repo.servers["existing"] = &serverdomain.Server{
		ID: "existing", Hostname: "node-01", IP: "10.0.0.2",
		Status: serverdomain.StatusUnknown, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	resp := doRequest(t, app, "POST", "/api/v1/servers/", map[string]string{
		"hostname": "node-01",
		"ip":       "10.0.0.99",
	}, map[string]string{"Authorization": "Bearer " + adminToken(t, jwtSvc)})

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "conflict" {
		t.Fatalf("expected code=conflict, got %v", errObj["code"])
	}
}

func TestCreateServer_DuplicateIP(t *testing.T) {
	app, repo, jwtSvc := setupServerApp(t)

	repo.servers["existing"] = &serverdomain.Server{
		ID: "existing", Hostname: "node-existing", IP: "10.0.0.1",
		Status: serverdomain.StatusUnknown, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	resp := doRequest(t, app, "POST", "/api/v1/servers/", map[string]string{
		"hostname": "node-new",
		"ip":       "10.0.0.1",
	}, map[string]string{"Authorization": "Bearer " + adminToken(t, jwtSvc)})

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestCreateServer_MissingFields(t *testing.T) {
	app, _, jwtSvc := setupServerApp(t)

	resp := doRequest(t, app, "POST", "/api/v1/servers/", map[string]string{
		"hostname": "node-01",
	}, map[string]string{"Authorization": "Bearer " + adminToken(t, jwtSvc)})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestListServers_Success(t *testing.T) {
	app, repo, jwtSvc := setupServerApp(t)

	now := time.Now()
	repo.servers["s1"] = &serverdomain.Server{ID: "s1", Hostname: "node-01", IP: "10.0.0.1", Status: serverdomain.StatusUnknown, CreatedAt: now, UpdatedAt: now}
	repo.servers["s2"] = &serverdomain.Server{ID: "s2", Hostname: "node-02", IP: "10.0.0.2", Status: serverdomain.StatusLive, CreatedAt: now, UpdatedAt: now}

	resp := doRequest(t, app, "GET", "/api/v1/servers/", nil, map[string]string{
		"Authorization": "Bearer " + adminToken(t, jwtSvc),
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["total"].(float64) != 2 {
		t.Fatalf("expected total=2, got %v", body["total"])
	}
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestListServers_Pagination(t *testing.T) {
	app, repo, jwtSvc := setupServerApp(t)

	now := time.Now()
	for i := 0; i < 5; i++ {
		id := "s" + string(rune('0'+i))
		repo.servers[id] = &serverdomain.Server{
			ID: id, Hostname: "node-0" + string(rune('0'+i)),
			IP: "10.0.0." + string(rune('1'+i)),
			Status: serverdomain.StatusUnknown, CreatedAt: now, UpdatedAt: now,
		}
	}

	_ = pagination.Page{}
	resp := doRequest(t, app, "GET", "/api/v1/servers/?page=1&pageSize=2", nil, map[string]string{
		"Authorization": "Bearer " + adminToken(t, jwtSvc),
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items (pageSize=2), got %d", len(items))
	}
	if body["total"].(float64) != 5 {
		t.Fatalf("expected total=5, got %v", body["total"])
	}
}

func TestDeleteServer_Success(t *testing.T) {
	app, repo, jwtSvc := setupServerApp(t)

	now := time.Now()
	repo.servers["srv-del"] = &serverdomain.Server{
		ID: "srv-del", Hostname: "to-delete", IP: "10.0.9.9",
		Status: serverdomain.StatusUnknown, CreatedAt: now, UpdatedAt: now,
	}

	resp := doRequest(t, app, "DELETE", "/api/v1/servers/srv-del", nil, map[string]string{
		"Authorization": "Bearer " + adminToken(t, jwtSvc),
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["success"] != true {
		t.Fatalf("expected success=true, got %v", body["success"])
	}
	if _, exists := repo.servers["srv-del"]; exists {
		t.Fatal("server should have been deleted")
	}
}

func TestDeleteServer_NotFound(t *testing.T) {
	app, _, jwtSvc := setupServerApp(t)

	resp := doRequest(t, app, "DELETE", "/api/v1/servers/nonexistent", nil, map[string]string{
		"Authorization": "Bearer " + adminToken(t, jwtSvc),
	})

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServerRoutes_NonAdminForbidden(t *testing.T) {
	app, _, jwtSvc := setupServerApp(t)

	resp := doRequest(t, app, "GET", "/api/v1/servers/", nil, map[string]string{
		"Authorization": "Bearer " + userToken(t, jwtSvc),
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestServerRoutes_NoToken(t *testing.T) {
	app, _, _ := setupServerApp(t)

	resp := doRequest(t, app, "GET", "/api/v1/servers/", nil, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
