package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	apikeyapp "github.com/maple52046/swallow/internal/apikey/application"
	apikeydelivery "github.com/maple52046/swallow/internal/apikey/delivery"
	apikeydomain "github.com/maple52046/swallow/internal/apikey/domain"
	authapp "github.com/maple52046/swallow/internal/auth/application"
	authdelivery "github.com/maple52046/swallow/internal/auth/delivery"
	authdomain "github.com/maple52046/swallow/internal/auth/domain"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

type fakeAPIKeyRepo struct {
	mu   sync.Mutex
	keys map[string]*apikeydomain.APIKey
}

func (r *fakeAPIKeyRepo) List(_ context.Context, userID string) ([]*apikeydomain.APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*apikeydomain.APIKey
	for _, k := range r.keys {
		if k.UserID == userID {
			c := *k
			out = append(out, &c)
		}
	}
	return out, nil
}

func (r *fakeAPIKeyRepo) Count(ctx context.Context, userID string) (int, error) {
	keys, err := r.List(ctx, userID)
	return len(keys), err
}

func (r *fakeAPIKeyRepo) Create(_ context.Context, key *apikeydomain.APIKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range r.keys {
		if k.UserID == key.UserID && strings.EqualFold(k.Name, key.Name) {
			return apikeydomain.ErrDuplicateName
		}
	}
	c := *key
	r.keys[key.ID] = &c
	return nil
}

func (r *fakeAPIKeyRepo) FindBySecretHash(_ context.Context, hash string) (*apikeydomain.APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range r.keys {
		if k.SecretHash == hash {
			c := *k
			return &c, nil
		}
	}
	return nil, apikeydomain.ErrKeyNotFound
}

func (r *fakeAPIKeyRepo) Delete(_ context.Context, userID, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if k, ok := r.keys[id]; ok && k.UserID == userID {
		delete(r.keys, id)
		return nil
	}
	return apikeydomain.ErrKeyNotFound
}

func (r *fakeAPIKeyRepo) TouchLastUsed(_ context.Context, id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if k, ok := r.keys[id]; ok {
		k.LastUsedAt = &at
	}
	return nil
}

type fakeOwners struct{ users *fakeUserRepo }

func (o fakeOwners) FindOwner(ctx context.Context, id string) (*apikeydomain.Owner, error) {
	u, err := o.users.FindByID(ctx, id)
	if err != nil {
		return nil, apikeydomain.ErrOwnerNotFound
	}
	return &apikeydomain.Owner{ID: u.ID, Username: u.Username, Role: string(u.Role)}, nil
}

// setupAPIKeyApp mounts login, me, the api-keys routes, an admin-only probe route, and a
// query-token route exactly as the API wires them, with the authenticator accepting API Keys.
func setupAPIKeyApp(t *testing.T) (*fiber.App, *fakeUserRepo) {
	t.Helper()
	users := newFakeUserRepo()
	hash, err := authdomain.HashPassword("correct-pass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	users.users["user-1"] = &authdomain.User{ID: "user-1", Username: "alice", PasswordHash: hash, Role: authdomain.RoleAdmin}

	jwtSvc := jwt.NewService("test-secret", 15*time.Minute)
	keys := apikeyapp.NewService(&fakeAPIKeyRepo{keys: map[string]*apikeydomain.APIKey{}}, fakeOwners{users: users})
	authn := middleware.NewAuthenticator(jwtSvc, keys)
	auth := authdelivery.NewAuthHandler(
		authapp.NewSessionService(users, newFakeSessionRepo(), jwtSvc, authapp.SessionConfig{RefreshTokenTTL: time.Hour, MaxAge: 24 * time.Hour}),
		authapp.NewMeUseCase(users),
	)
	handler := apikeydelivery.NewHandler(keys)

	app := fiber.New()
	app.Post("/api/v1/auth/login", auth.Login)
	app.Get("/api/v1/auth/me", authn.Require(), auth.Me)
	admin := []fiber.Handler{authn.Require(), middleware.AdminOnly()}
	group := app.Group("/api/v1/api-keys", admin...)
	group.Get("/", handler.List)
	group.Post("/", middleware.SessionOnly(), handler.Create)
	group.Delete("/:keyId", handler.Delete)
	app.Get("/api/v1/probe", append(admin, func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })...)
	app.Get("/api/v1/stream", middleware.BearerTokenFromQuery("access_token"), authn.Require(),
		func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	return app, users
}

func sessionToken(t *testing.T, app *fiber.App) string {
	t.Helper()
	resp := doRequest(t, app, "POST", "/api/v1/auth/login", map[string]string{"username": "alice", "password": "correct-pass"}, nil)
	token, _ := parseBody(t, resp)["accessToken"].(string)
	if token == "" {
		t.Fatal("login returned no access token")
	}
	return token
}

func createKey(t *testing.T, app *fiber.App, bearer, name string) (id, secret string) {
	t.Helper()
	resp := doRequest(t, app, "POST", "/api/v1/api-keys", map[string]any{"name": name},
		map[string]string{"Authorization": "Bearer " + bearer})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create api key status = %d, want 201", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("create Cache-Control = %q, want no-store", got)
	}
	body := parseBody(t, resp)
	key, _ := body["key"].(map[string]any)
	secret, _ = body["secret"].(string)
	id, _ = key["id"].(string)
	return id, secret
}

func TestAPIKey_LifecycleAndAuthentication(t *testing.T) {
	app, _ := setupAPIKeyApp(t)
	access := sessionToken(t, app)
	id, secret := createKey(t, app, access, "ci-runner")
	if !strings.HasPrefix(secret, "swk_") {
		t.Fatalf("secret = %q, want swk_ prefix", secret)
	}

	resp := doRequest(t, app, "GET", "/api/v1/probe", nil, map[string]string{"Authorization": "Bearer " + secret})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin route with the API key status = %d, want 204", resp.StatusCode)
	}
	resp = doRequest(t, app, "GET", "/api/v1/auth/me", nil, map[string]string{"Authorization": "Bearer " + secret})
	if me := parseBody(t, resp); me["authMethod"] != "api_key" || me["username"] != "alice" {
		t.Errorf("me with the API key = %v, want alice via api_key", me)
	}

	resp = doRequest(t, app, "GET", "/api/v1/api-keys", nil, map[string]string{"Authorization": "Bearer " + access})
	var items []map[string]any
	decodeJSON(t, resp, &items)
	if len(items) != 1 || items[0]["name"] != "ci-runner" || items[0]["lastUsedAt"] == nil {
		t.Errorf("list = %v, want one key with lastUsedAt recorded", items)
	}
	if _, leaked := items[0]["secret"]; leaked {
		t.Error("list leaked the secret")
	}

	if resp := doRequest(t, app, "DELETE", "/api/v1/api-keys/"+id, nil, map[string]string{"Authorization": "Bearer " + access}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}
	if resp := doRequest(t, app, "GET", "/api/v1/probe", nil, map[string]string{"Authorization": "Bearer " + secret}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("deleted key status = %d, want 401", resp.StatusCode)
	}
}

func TestAPIKey_CannotCreateAPIKeys(t *testing.T) {
	app, _ := setupAPIKeyApp(t)
	_, secret := createKey(t, app, sessionToken(t, app), "ci-runner")

	resp := doRequest(t, app, "POST", "/api/v1/api-keys", map[string]any{"name": "minted"},
		map[string]string{"Authorization": "Bearer " + secret})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("create with an API key status = %d, want 403", resp.StatusCode)
	}
	// Listing and deleting with a key are allowed.
	if resp := doRequest(t, app, "GET", "/api/v1/api-keys", nil, map[string]string{"Authorization": "Bearer " + secret}); resp.StatusCode != http.StatusOK {
		t.Errorf("list with an API key status = %d, want 200", resp.StatusCode)
	}
}

func TestAPIKey_RejectedInQueryString(t *testing.T) {
	app, _ := setupAPIKeyApp(t)
	access := sessionToken(t, app)
	_, secret := createKey(t, app, access, "ci-runner")

	if resp := doRequest(t, app, "GET", "/api/v1/stream?access_token="+secret, nil, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("API key in query status = %d, want 401", resp.StatusCode)
	}
	if resp := doRequest(t, app, "GET", "/api/v1/stream?access_token="+access, nil, nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("access token in query status = %d, want 204", resp.StatusCode)
	}
}

func TestAPIKey_StopsWorkingWhenOwnerIsDeleted(t *testing.T) {
	app, users := setupAPIKeyApp(t)
	_, secret := createKey(t, app, sessionToken(t, app), "ci-runner")
	delete(users.users, "user-1")

	if resp := doRequest(t, app, "GET", "/api/v1/probe", nil, map[string]string{"Authorization": "Bearer " + secret}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("orphaned key status = %d, want 401", resp.StatusCode)
	}
}

func TestAPIKey_CreateValidationAndConflicts(t *testing.T) {
	app, _ := setupAPIKeyApp(t)
	access := sessionToken(t, app)
	auth := map[string]string{"Authorization": "Bearer " + access}
	createKey(t, app, access, "laptop")

	tests := []struct {
		name string
		body map[string]any
		want int
	}{
		{name: "missing name", body: map[string]any{}, want: http.StatusBadRequest},
		{name: "past expiry", body: map[string]any{"name": "old", "expiresAt": "2001-01-01T00:00:00Z"}, want: http.StatusBadRequest},
		{name: "malformed expiry", body: map[string]any{"name": "bad", "expiresAt": "next week"}, want: http.StatusBadRequest},
		{name: "duplicate name differs only in case", body: map[string]any{"name": "Laptop"}, want: http.StatusConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if resp := doRequest(t, app, "POST", "/api/v1/api-keys", tc.body, auth); resp.StatusCode != tc.want {
				t.Errorf("create %v status = %d, want %d", tc.body, resp.StatusCode, tc.want)
			}
		})
	}
	if resp := doRequest(t, app, "DELETE", "/api/v1/api-keys/unknown", nil, auth); resp.StatusCode != http.StatusNotFound {
		t.Errorf("delete unknown status = %d, want 404", resp.StatusCode)
	}
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}
