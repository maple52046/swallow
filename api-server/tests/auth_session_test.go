package tests

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	authapp "github.com/maple52046/swallow/internal/auth/application"
)

// refreshCookie returns the swallow_refresh Set-Cookie of resp, or nil.
func refreshCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == "swallow_refresh" {
			return c
		}
	}
	return nil
}

func TestLogin_CookieDeliverySetsHttpOnlyRefreshCookie(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{})

	resp := doRequest(t, app, "POST", "/api/v1/auth/login",
		map[string]string{"username": "alice", "password": "correct-pass"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["accessToken"] == "" || body["accessTokenExpiresAt"] == nil {
		t.Errorf("login body = %v, want accessToken and accessTokenExpiresAt", body)
	}
	if _, ok := body["refreshToken"]; ok {
		t.Errorf("cookie delivery leaked refreshToken in the body: %v", body)
	}
	cookie := refreshCookie(resp)
	if cookie == nil || cookie.Value == "" {
		t.Fatalf("login did not set the swallow_refresh cookie: %v", resp.Header.Values("Set-Cookie"))
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api/v1/auth" {
		t.Errorf("cookie = %+v, want HttpOnly, SameSite=Strict, Path=/api/v1/auth", cookie)
	}
	if cookie.Secure {
		t.Error("cookie is Secure on a plain-HTTP request; the dev stack could not use it")
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestLogin_CookieIsSecureBehindTLSProxy(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{})

	resp := doRequest(t, app, "POST", "/api/v1/auth/login",
		map[string]string{"username": "alice", "password": "correct-pass"},
		map[string]string{"X-Forwarded-Proto": "https"})
	if cookie := refreshCookie(resp); cookie == nil || !cookie.Secure {
		t.Fatalf("cookie = %+v, want Secure behind an HTTPS proxy", cookie)
	}
}

func TestLogin_RejectsUnknownDelivery(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{})

	resp := doRequest(t, app, "POST", "/api/v1/auth/login", map[string]string{
		"username": "alice", "password": "correct-pass", "refreshTokenDelivery": "header",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRefresh_BodyDeliveryRotatesToken(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{})
	first := bodyLogin(t, app)

	resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", map[string]string{"refreshToken": first}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200", resp.StatusCode)
	}
	body := parseBody(t, resp)
	second, _ := body["refreshToken"].(string)
	if second == "" || second == first {
		t.Fatalf("refresh returned refreshToken %q, want a new token", second)
	}
	if refreshCookie(resp) != nil {
		t.Error("body delivery must not set a cookie")
	}

	resp = doRequest(t, app, "POST", "/api/v1/auth/refresh", map[string]string{"refreshToken": second}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh with the rotated token status = %d, want 200", resp.StatusCode)
	}
}

func TestRefresh_CookieDeliveryRotatesCookie(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{})
	login := doRequest(t, app, "POST", "/api/v1/auth/login",
		map[string]string{"username": "alice", "password": "correct-pass"}, nil)
	cookie := refreshCookie(login)

	resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", nil,
		map[string]string{"Cookie": "swallow_refresh=" + cookie.Value})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200", resp.StatusCode)
	}
	rotated := refreshCookie(resp)
	if rotated == nil || rotated.Value == "" || rotated.Value == cookie.Value {
		t.Fatalf("refresh cookie = %+v, want a new value", rotated)
	}
	if _, ok := parseBody(t, resp)["refreshToken"]; ok {
		t.Error("cookie delivery leaked refreshToken in the body")
	}
}

func TestRefresh_ReuseWithinGraceIssuesAccessTokenOnly(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{RotationGrace: time.Hour})
	first := bodyLogin(t, app)
	if resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", map[string]string{"refreshToken": first}, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("first refresh status = %d, want 200", resp.StatusCode)
	}

	// A second tab or CLI process still holding the old token refreshes moments later.
	resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", map[string]string{"refreshToken": first}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("grace refresh status = %d, want 200", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["accessToken"] == "" {
		t.Error("grace refresh returned no access token")
	}
	if _, ok := body["refreshToken"]; ok {
		t.Errorf("grace refresh rotated again: %v", body)
	}
}

func TestRefresh_ReuseAfterGraceRevokesSession(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{RotationGrace: time.Nanosecond})
	first := bodyLogin(t, app)
	resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", map[string]string{"refreshToken": first}, nil)
	second, _ := parseBody(t, resp)["refreshToken"].(string)
	time.Sleep(time.Millisecond)

	if resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", map[string]string{"refreshToken": first}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused token status = %d, want 401", resp.StatusCode)
	}
	// Reuse is treated as theft: the legitimate newer token stops working too.
	if resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", map[string]string{"refreshToken": second}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token after reuse status = %d, want 401 (session revoked)", resp.StatusCode)
	}
}

func TestRefresh_UnknownTokenIs401AndClearsCookie(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{})

	resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", nil,
		map[string]string{"Cookie": "swallow_refresh=not-a-token"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	cleared := refreshCookie(resp)
	if cleared == nil || cleared.Value != "" {
		t.Errorf("refresh did not clear the cookie: %v", resp.Header.Values("Set-Cookie"))
	}

	if resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", nil, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("refresh with no token status = %d, want 401", resp.StatusCode)
	}
}

func TestLogout_RevokesSessionAndIsIdempotent(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{})
	token := bodyLogin(t, app)

	for i := 0; i < 2; i++ {
		resp := doRequest(t, app, "POST", "/api/v1/auth/logout", map[string]string{"refreshToken": token}, nil)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("logout #%d status = %d, want 204", i+1, resp.StatusCode)
		}
	}
	if resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", map[string]string{"refreshToken": token}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want 401", resp.StatusCode)
	}
}

func TestLogout_ByAccessTokenRevokesItsSession(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{})
	resp := doRequest(t, app, "POST", "/api/v1/auth/login", map[string]string{
		"username": "alice", "password": "correct-pass", "refreshTokenDelivery": "body",
	}, nil)
	body := parseBody(t, resp)
	access, _ := body["accessToken"].(string)
	refresh, _ := body["refreshToken"].(string)

	if resp := doRequest(t, app, "POST", "/api/v1/auth/logout", nil,
		map[string]string{"Authorization": "Bearer " + access}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", resp.StatusCode)
	}
	if resp := doRequest(t, app, "POST", "/api/v1/auth/refresh", map[string]string{"refreshToken": refresh}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh after access-token logout status = %d, want 401", resp.StatusCode)
	}
	// Logout with nothing at all still answers 204.
	if resp := doRequest(t, app, "POST", "/api/v1/auth/logout", nil, nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("anonymous logout status = %d, want 204", resp.StatusCode)
	}
}

func TestMe_ReportsSessionAuthMethod(t *testing.T) {
	app, _, _, _ := setupAuthAppWithSessions(t, authapp.SessionConfig{})
	resp := doRequest(t, app, "POST", "/api/v1/auth/login",
		map[string]string{"username": "alice", "password": "correct-pass"}, nil)
	access, _ := parseBody(t, resp)["accessToken"].(string)

	resp = doRequest(t, app, "GET", "/api/v1/auth/me", nil, map[string]string{"Authorization": "Bearer " + access})
	if got := parseBody(t, resp)["authMethod"]; got != "session" {
		t.Errorf("authMethod = %v, want session", got)
	}
}

// bodyLogin logs alice in with body delivery and returns the refresh token.
func bodyLogin(t *testing.T, app *fiber.App) string {
	t.Helper()
	resp := doRequest(t, app, "POST", "/api/v1/auth/login", map[string]string{
		"username": "alice", "password": "correct-pass", "refreshTokenDelivery": "body",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("body login status = %d, want 200", resp.StatusCode)
	}
	token, _ := parseBody(t, resp)["refreshToken"].(string)
	if token == "" || strings.Contains(token, " ") {
		t.Fatalf("body login refreshToken = %q, want an opaque token", token)
	}
	return token
}
