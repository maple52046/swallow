package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	authapp "github.com/AFDEAPAC/swallow/internal/auth/application"
	authdelivery "github.com/AFDEAPAC/swallow/internal/auth/delivery"
	authdomain "github.com/AFDEAPAC/swallow/internal/auth/domain"
	"github.com/AFDEAPAC/swallow/internal/shared/jwt"
	"github.com/AFDEAPAC/swallow/internal/shared/middleware"
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

func setupAuthApp(t *testing.T) (*fiber.App, *fakeUserRepo, *jwt.Service) {
	t.Helper()

	repo := newFakeUserRepo()
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

	loginUC := authapp.NewLoginUseCase(repo, jwtSvc)
	meUC := authapp.NewMeUseCase(repo)
	handler := authdelivery.NewAuthHandler(loginUC, meUC)

	app := fiber.New()
	app.Post("/api/v1/auth/login", handler.Login)
	app.Get("/api/v1/auth/me", middleware.Auth(jwtSvc), handler.Me)

	return app, repo, jwtSvc
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
