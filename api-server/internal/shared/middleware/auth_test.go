package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/identity"
	"github.com/maple52046/swallow/internal/shared/jwt"
)

type stubKeys struct {
	principal *identity.Principal
	err       error
	seen      string
}

func (s *stubKeys) VerifyAPIKey(_ context.Context, secret string) (*identity.Principal, error) {
	s.seen = secret
	return s.principal, s.err
}

func call(t *testing.T, app *fiber.App, path, bearer string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp.StatusCode
}

func newApp(authn *Authenticator) *fiber.App {
	app := fiber.New()
	ok := func(c *fiber.Ctx) error { return c.SendString(string(GetPrincipal(c).Method)) }
	app.Get("/admin", authn.Require(), AdminOnly(), ok)
	app.Get("/session-only", authn.Require(), SessionOnly(), ok)
	app.Get("/optional", authn.Optional(), func(c *fiber.Ctx) error {
		if GetPrincipal(c) == nil {
			return c.SendStatus(fiber.StatusNoContent)
		}
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func TestAuthenticator_RoutesBearerValues(t *testing.T) {
	tokens := jwt.NewService("secret", time.Minute)
	access, _, err := tokens.Issue(jwt.AccessTokenInput{UserID: "u1", Username: "alice", Role: "admin", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	keys := &stubKeys{principal: &identity.Principal{UserID: "u1", Role: "admin", Method: identity.MethodAPIKey}}
	app := newApp(NewAuthenticator(tokens, keys))

	tests := []struct {
		name   string
		path   string
		bearer string
		want   int
	}{
		{name: "access token", path: "/admin", bearer: access, want: http.StatusOK},
		{name: "api key", path: "/admin", bearer: "swk_valid", want: http.StatusOK},
		{name: "garbage", path: "/admin", bearer: "not-a-token", want: http.StatusUnauthorized},
		{name: "no credential", path: "/admin", want: http.StatusUnauthorized},
		{name: "session-only accepts an access token", path: "/session-only", bearer: access, want: http.StatusOK},
		{name: "session-only refuses an api key", path: "/session-only", bearer: "swk_valid", want: http.StatusForbidden},
		{name: "optional without credential", path: "/optional", want: http.StatusNoContent},
		{name: "optional with a bad credential", path: "/optional", bearer: "not-a-token", want: http.StatusNoContent},
		{name: "optional with an access token", path: "/optional", bearer: access, want: http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := call(t, app, tc.path, tc.bearer); got != tc.want {
				t.Errorf("GET %s with %q = %d, want %d", tc.path, tc.bearer, got, tc.want)
			}
		})
	}
	if keys.seen != "swk_valid" {
		t.Errorf("verifier saw %q, want the full API Key secret", keys.seen)
	}
}

func TestAuthenticator_APIKeyFailureModes(t *testing.T) {
	tokens := jwt.NewService("secret", time.Minute)

	if got := call(t, newApp(NewAuthenticator(tokens, nil)), "/admin", "swk_any"); got != http.StatusUnauthorized {
		t.Errorf("API key without a verifier = %d, want 401", got)
	}
	if got := call(t, newApp(NewAuthenticator(tokens, &stubKeys{})), "/admin", "swk_unknown"); got != http.StatusUnauthorized {
		t.Errorf("rejected API key = %d, want 401", got)
	}
	// A verifier outage must not be reported as a bad credential.
	if got := call(t, newApp(NewAuthenticator(tokens, &stubKeys{err: errors.New("mongo down")})), "/admin", "swk_any"); got != http.StatusInternalServerError {
		t.Errorf("verifier failure = %d, want 500", got)
	}
	user := &stubKeys{principal: &identity.Principal{UserID: "u2", Role: "user", Method: identity.MethodAPIKey}}
	if got := call(t, newApp(NewAuthenticator(tokens, user)), "/admin", "swk_user"); got != http.StatusForbidden {
		t.Errorf("non-admin API key on an admin route = %d, want 403", got)
	}
}

func TestBearerTokenFromQuery_NeverPromotesAPIKeys(t *testing.T) {
	app := fiber.New()
	app.Get("/stream", BearerTokenFromQuery("access_token"), func(c *fiber.Ctx) error {
		return c.SendString(c.Get(fiber.HeaderAuthorization))
	})
	for query, want := range map[string]string{"jwt.value": "Bearer jwt.value", "swk_secret": ""} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/stream?access_token="+query, nil), -1)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		buf := make([]byte, 64)
		n, _ := resp.Body.Read(buf)
		if got := string(buf[:n]); got != want {
			t.Errorf("query %q promoted to %q, want %q", query, got, want)
		}
	}
}
