package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// sessionServer fakes auth/refresh and one protected endpoint. accept is the
// access token the protected endpoint currently accepts; rotate controls whether
// refresh returns a new refresh token.
type sessionServer struct {
	mu        sync.Mutex
	accept    string
	rotate    bool
	refreshOK bool
	calls     []string
	gotBodies []string
}

func (s *sessionServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls = append(s.calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/auth/refresh" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.gotBodies = append(s.gotBodies, body["refreshToken"])
			if r.Header.Get("Authorization") != "" {
				t.Error("refresh must not send an Authorization header")
			}
			if !s.refreshOK {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"Session expired."}}`))
				return
			}
			s.accept = "access-2"
			resp := map[string]any{"accessToken": "access-2", "accessTokenExpiresAt": time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339)}
			if s.rotate {
				resp["refreshToken"] = "refresh-2"
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+s.accept {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"Missing or invalid token."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func newSessionClient(t *testing.T, url string, expiresAt time.Time, saved *[]SessionTokens) *Client {
	t.Helper()
	c, err := New(Options{
		Endpoint: url, Token: "access-1", TokenExpiresAt: expiresAt, RefreshToken: "refresh-1",
		OnSessionRefreshed: func(tokens SessionTokens) error {
			*saved = append(*saved, tokens)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestDo_RefreshesAndRetriesOnceAfter401(t *testing.T) {
	s := &sessionServer{accept: "access-other", rotate: true, refreshOK: true}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	var saved []SessionTokens
	c := newSessionClient(t, srv.URL, time.Now().Add(10*time.Minute), &saved)

	if err := c.JSON(context.Background(), Request{Method: "POST", Path: "servers/x/refresh", Body: map[string]string{"a": "b"}, Auth: AuthBearer}, nil); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	want := []string{"POST /api/v1/servers/x/refresh", "POST /api/v1/auth/refresh", "POST /api/v1/servers/x/refresh"}
	if strings.Join(s.calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", s.calls, want)
	}
	if len(saved) != 1 || saved[0].AccessToken != "access-2" || saved[0].RefreshToken != "refresh-2" {
		t.Errorf("persisted = %+v, want one renewal with the rotated refresh token", saved)
	}
}

func TestDo_RefreshesBeforeExpiry(t *testing.T) {
	s := &sessionServer{accept: "access-2", rotate: true, refreshOK: true}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	var saved []SessionTokens
	c := newSessionClient(t, srv.URL, time.Now().Add(30*time.Second), &saved)

	if err := c.JSON(context.Background(), Request{Method: "GET", Path: "servers/", Auth: AuthBearer}, nil); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if len(s.calls) != 2 || s.calls[0] != "POST /api/v1/auth/refresh" {
		t.Errorf("calls = %v, want a refresh before the request", s.calls)
	}
}

func TestDo_GraceRefreshKeepsCurrentRefreshToken(t *testing.T) {
	s := &sessionServer{accept: "access-other", rotate: false, refreshOK: true}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	var saved []SessionTokens
	c := newSessionClient(t, srv.URL, time.Time{}, &saved)

	if err := c.JSON(context.Background(), Request{Method: "GET", Path: "servers/", Auth: AuthBearer}, nil); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if len(saved) != 1 || saved[0].RefreshToken != "" {
		t.Errorf("persisted = %+v, want a renewal without a refresh token", saved)
	}
	if c.refreshToken != "refresh-1" {
		t.Errorf("client refresh token = %q, want the existing refresh-1 kept", c.refreshToken)
	}
}

func TestDo_EndedSessionAsksToLogInAgain(t *testing.T) {
	s := &sessionServer{accept: "access-other", refreshOK: false}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	var saved []SessionTokens
	c := newSessionClient(t, srv.URL, time.Time{}, &saved)

	err := c.JSON(context.Background(), Request{Method: "GET", Path: "servers/", Auth: AuthBearer}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized || !strings.Contains(err.Error(), "swallow login") {
		t.Fatalf("error = %v, want a 401 telling the operator to run swallow login", err)
	}
	if len(saved) != 0 {
		t.Errorf("persisted %+v after a failed refresh, want nothing", saved)
	}
}

func TestDo_DoesNotReplayRawBodyAfter401(t *testing.T) {
	s := &sessionServer{accept: "access-other", refreshOK: true}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	var saved []SessionTokens
	c := newSessionClient(t, srv.URL, time.Time{}, &saved)

	err := c.Discard(context.Background(), Request{Method: "POST", Path: "provisioning/images", RawBody: strings.NewReader("stream"), Auth: AuthBearer})
	if err == nil || len(s.calls) != 1 {
		t.Fatalf("err = %v, calls = %v; want one attempt and an error, since a stream cannot be resent", err, s.calls)
	}
}

func TestAPIKeyIsSentAndNeverRefreshed(t *testing.T) {
	var gotAuth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.URL.Path+" "+r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"Missing or invalid token."}}`))
	}))
	defer srv.Close()
	c, err := New(Options{Endpoint: srv.URL, APIKey: "swk_key", Token: "ignored", RefreshToken: "ignored"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = c.JSON(context.Background(), Request{Method: "GET", Path: "servers/", Auth: AuthBearer}, nil)
	if len(gotAuth) != 1 || gotAuth[0] != "/api/v1/servers/ Bearer swk_key" {
		t.Errorf("requests = %v, want one request carrying the API key", gotAuth)
	}
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("error = %v, want a hint about the API key", err)
	}
}
