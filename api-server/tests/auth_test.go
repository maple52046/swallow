package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	authapp "github.com/maple52046/swallow/internal/auth/application"
	authdelivery "github.com/maple52046/swallow/internal/auth/delivery"
	authdomain "github.com/maple52046/swallow/internal/auth/domain"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

// fakeUserRepo is an in-memory UserRepository for testing.
type fakeUserRepo struct {
	users map[string]*authdomain.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{users: make(map[string]*authdomain.User)}
}

func (r *fakeUserRepo) FindByUsername(_ context.Context, username string) (*authdomain.User, error) {
	for _, u := range r.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, authdomain.ErrUserNotFound
}

func (r *fakeUserRepo) FindByID(_ context.Context, id string) (*authdomain.User, error) {
	u, ok := r.users[id]
	if !ok {
		return nil, authdomain.ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) Create(_ context.Context, user *authdomain.User) error {
	r.users[user.ID] = user
	return nil
}

func (r *fakeUserRepo) ExistsByUsername(_ context.Context, username string) (bool, error) {
	for _, u := range r.users {
		if u.Username == username {
			return true, nil
		}
	}
	return false, nil
}

// fakeSessionRepo is an in-memory SessionRepository for testing. Rotate keeps the production
// compare-and-swap semantics so concurrent-refresh behavior is exercised faithfully.
type fakeSessionRepo struct {
	mu       sync.Mutex
	sessions map[string]*authdomain.Session
}

func newFakeSessionRepo() *fakeSessionRepo {
	return &fakeSessionRepo{sessions: make(map[string]*authdomain.Session)}
}

func (r *fakeSessionRepo) Create(_ context.Context, s *authdomain.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *s
	r.sessions[s.ID] = &copied
	return nil
}

func (r *fakeSessionRepo) find(match func(*authdomain.Session) bool) (*authdomain.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if match(s) {
			copied := *s
			return &copied, nil
		}
	}
	return nil, authdomain.ErrSessionNotFound
}

func (r *fakeSessionRepo) FindByTokenHash(_ context.Context, hash string) (*authdomain.Session, error) {
	return r.find(func(s *authdomain.Session) bool { return s.TokenHash == hash })
}

func (r *fakeSessionRepo) FindByPreviousTokenHash(_ context.Context, hash string) (*authdomain.Session, error) {
	return r.find(func(s *authdomain.Session) bool { return s.PreviousTokenHash != "" && s.PreviousTokenHash == hash })
}

func (r *fakeSessionRepo) FindByID(_ context.Context, id string) (*authdomain.Session, error) {
	return r.find(func(s *authdomain.Session) bool { return s.ID == id })
}

func (r *fakeSessionRepo) Rotate(_ context.Context, id, fromHash, toHash string, rotatedAt, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok || s.TokenHash != fromHash || s.RevokedAt != nil {
		return authdomain.ErrSessionNotFound
	}
	s.PreviousTokenHash, s.TokenHash = fromHash, toHash
	s.RotatedAt, s.LastUsedAt, s.ExpiresAt = rotatedAt, rotatedAt, expiresAt
	return nil
}

func (r *fakeSessionRepo) Touch(_ context.Context, id string, usedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[id]; ok {
		s.LastUsedAt = usedAt
	}
	return nil
}

func (r *fakeSessionRepo) Revoke(_ context.Context, id string, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[id]; ok && s.RevokedAt == nil {
		at := revokedAt
		s.RevokedAt = &at
	}
	return nil
}

func setupAuthApp(t *testing.T) (*fiber.App, *fakeUserRepo, *jwt.Service) {
	t.Helper()
	app, users, _, jwtSvc := setupAuthAppWithSessions(t, authapp.SessionConfig{})
	return app, users, jwtSvc
}

// setupAuthAppWithSessions mounts the auth routes the way the API does. A zero cfg uses one-week
// idle and 30-day maximum lifetimes; RotationGrace may be set to exercise the reuse rules.
func setupAuthAppWithSessions(t *testing.T, cfg authapp.SessionConfig) (*fiber.App, *fakeUserRepo, *fakeSessionRepo, *jwt.Service) {
	t.Helper()

	repo := newFakeUserRepo()
	sessions := newFakeSessionRepo()
	if cfg.RefreshTokenTTL == 0 {
		cfg.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 30 * 24 * time.Hour
	}
	jwtSvc := jwt.NewService("test-secret", time.Hour)

	hash, err := authdomain.HashPassword("correct-pass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repo.users["user-1"] = &authdomain.User{
		ID:           "user-1",
		Username:     "alice",
		PasswordHash: hash,
		Role:         authdomain.RoleAdmin,
		CreatedAt:    time.Now(),
	}

	handler := authdelivery.NewAuthHandler(
		authapp.NewSessionService(repo, sessions, jwtSvc, cfg),
		authapp.NewMeUseCase(repo),
	)
	authn := middleware.NewAuthenticator(jwtSvc, nil)

	app := fiber.New()
	app.Post("/api/v1/auth/login", handler.Login)
	app.Post("/api/v1/auth/refresh", handler.Refresh)
	app.Post("/api/v1/auth/logout", authn.Optional(), handler.Logout)
	app.Get("/api/v1/auth/me", authn.Require(), handler.Me)

	return app, repo, sessions, jwtSvc
}

func doRequest(t *testing.T, app *fiber.App, method, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func parseBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return m
}

func TestLogin_Success(t *testing.T) {
	app, _, _ := setupAuthApp(t)

	resp := doRequest(t, app, "POST", "/api/v1/auth/login", map[string]string{
		"username": "alice",
		"password": "correct-pass",
	}, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["accessToken"] == nil || body["accessToken"] == "" {
		t.Fatal("expected accessToken in response")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	app, _, _ := setupAuthApp(t)

	resp := doRequest(t, app, "POST", "/api/v1/auth/login", map[string]string{
		"username": "alice",
		"password": "wrong-pass",
	}, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "unauthorized" {
		t.Fatalf("expected code=unauthorized, got %v", errObj["code"])
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	app, _, _ := setupAuthApp(t)

	resp := doRequest(t, app, "POST", "/api/v1/auth/login", map[string]string{
		"username": "nobody",
		"password": "any",
	}, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestLogin_MissingFields(t *testing.T) {
	app, _, _ := setupAuthApp(t)

	resp := doRequest(t, app, "POST", "/api/v1/auth/login", map[string]string{
		"username": "alice",
	}, nil)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMe_Success(t *testing.T) {
	app, _, jwtSvc := setupAuthApp(t)

	token, err := jwtSvc.Sign("user-1", "alice", "admin")
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	resp := doRequest(t, app, "GET", "/api/v1/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["username"] != "alice" {
		t.Fatalf("expected username=alice, got %v", body["username"])
	}
	if body["role"] != "admin" {
		t.Fatalf("expected role=admin, got %v", body["role"])
	}
}

func TestMe_NoToken(t *testing.T) {
	app, _, _ := setupAuthApp(t)

	resp := doRequest(t, app, "GET", "/api/v1/auth/me", nil, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMe_InvalidToken(t *testing.T) {
	app, _, _ := setupAuthApp(t)

	resp := doRequest(t, app, "GET", "/api/v1/auth/me", nil, map[string]string{
		"Authorization": "Bearer invalid.token.here",
	})

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
