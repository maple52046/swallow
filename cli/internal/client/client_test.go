package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient builds a client pointed at a test server with a fixed token so
// tests can assert on the Authorization header the client attaches.
func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	c, err := New(Options{Endpoint: url, Token: "session-token", MachineToken: "machine-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestJSONSuccessAndBasePathAndAuth(t *testing.T) {
	var gotPath, gotAuth, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"srv-1","hostname":"gpu-1"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	var out map[string]any
	err := c.JSON(context.Background(), Request{
		Method: "GET",
		Path:   "servers/srv-1",
		Query:  map[string][]string{"siteId": {"site-1"}},
		Auth:   AuthBearer,
	}, &out)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if gotPath != "/api/v1/servers/srv-1" {
		t.Errorf("path = %q, want /api/v1/servers/srv-1", gotPath)
	}
	if gotAuth != "Bearer session-token" {
		t.Errorf("auth = %q, want Bearer session-token", gotAuth)
	}
	if gotQuery != "siteId=site-1" {
		t.Errorf("query = %q, want siteId=site-1", gotQuery)
	}
	if out["hostname"] != "gpu-1" {
		t.Errorf("decoded hostname = %v, want gpu-1", out["hostname"])
	}
}

func TestJSONErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"locked","requestId":"req-9"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.JSON(context.Background(), Request{Method: "POST", Path: "servers/x/lock", Auth: AuthBearer}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v", err)
	}
	if apiErr.Status != http.StatusConflict || apiErr.Code != "conflict" || apiErr.RequestID != "req-9" {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
}

func TestMachineAuthPrefersMachineToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	var out any
	if err := c.JSON(context.Background(), Request{Method: "GET", Path: "discovery/prometheus", Auth: AuthMachine}, &out); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if gotAuth != "Bearer machine-token" {
		t.Errorf("auth = %q, want Bearer machine-token", gotAuth)
	}
}

func TestNoAuthOmitsHeader(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{"accessToken":"t"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	var out any
	if err := c.JSON(context.Background(), Request{Method: "POST", Path: "auth/login", Auth: AuthNone, Body: map[string]string{"username": "a"}}, &out); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if hadAuth {
		t.Error("AuthNone should not send an Authorization header")
	}
}

func TestNewRejectsEmptyAndRelativeEndpoint(t *testing.T) {
	if _, err := New(Options{Endpoint: ""}); err == nil {
		t.Error("expected error for empty endpoint")
	}
	if _, err := New(Options{Endpoint: "not-a-url"}); err == nil {
		t.Error("expected error for endpoint without scheme/host")
	}
}
