package pulp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

func TestClientTranslatesPulpFailures(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantKind   localrepodomain.ProviderErrorKind
		wantDetail string
	}{
		{"a refused password is the Integration's credential", http.StatusUnauthorized, `{"detail":"Invalid username/password."}`,
			localrepodomain.ProviderErrorAuth, "Pulp rejected the admin password the repository Integration is configured with."},
		{"a server error is transient", http.StatusInternalServerError, "<html>oops</html>",
			localrepodomain.ProviderErrorUnavailable, "Pulp reported an internal error (HTTP 500)."},
		{"a validation error keeps Pulp's reasons", http.StatusBadRequest, `{"url":["Enter a valid URL."],"non_field_errors":["Bad."]}`,
			localrepodomain.ProviderErrorRejected, "Pulp refused the request: Bad.; url: Enter a valid URL."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c, err := newClient(server.URL, fakePassword, server.Client())
			if err != nil {
				t.Fatalf("newClient() error = %v", err)
			}

			err = c.get(context.Background(), api(debRemotes), url.Values{"name": {"ubuntu"}}, nil)
			var providerErr *localrepodomain.ProviderError
			if !errors.As(err, &providerErr) {
				t.Fatalf("get() error = %v, want a ProviderError", err)
			}
			if providerErr.Kind != tc.wantKind || providerErr.Detail != tc.wantDetail {
				t.Errorf("get() error = %q: %q, want %q: %q", providerErr.Kind, providerErr.Detail, tc.wantKind, tc.wantDetail)
			}
			if providerErr.Operation != "GET /remotes/deb/apt/" || providerErr.Endpoint != server.URL {
				t.Errorf("get() evidence = %q at %q, want the call and the endpoint", providerErr.Operation, providerErr.Endpoint)
			}
			if !isStatus(err, tc.status) {
				t.Errorf("get() error %v does not keep HTTP %d as its cause", err, tc.status)
			}
			if strings.Contains(err.Error(), fakePassword) {
				t.Errorf("get() error %q shows the admin password", err)
			}
		})
	}
}

func TestClientReportsUnreachablePulpAsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	c, err := newClient(server.URL, fakePassword, server.Client())
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	err = c.get(context.Background(), api("/tasks/"), nil, nil)
	var providerErr *localrepodomain.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != localrepodomain.ProviderErrorUnavailable || providerErr.Err == nil {
		t.Errorf("get() against a closed server error = %v, want an unavailable ProviderError with its cause", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.get(ctx, api("/tasks/"), nil, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("get() with a canceled context error = %v, want context.Canceled", err)
	}
}

func TestNewClientAcceptsEndpointForms(t *testing.T) {
	for raw, want := range map[string]string{
		"http://pulp-web:8080":               "http://pulp-web:8080",
		"http://pulp-web:8080/":              "http://pulp-web:8080",
		"http://pulp-web:8080/pulp/api/v3/":  "http://pulp-web:8080",
		"https://proxy.example/pulp-prefix/": "https://proxy.example/pulp-prefix",
	} {
		c, err := newClient(raw, fakePassword, http.DefaultClient)
		if err != nil || c.base != want {
			t.Errorf("newClient(%q) base = %v, %v; want %q", raw, c, err, want)
		}
	}
	for _, raw := range []string{"", "pulp-web:8080", "ftp://pulp", "http://admin:pw@pulp-web:8080"} {
		if _, err := newClient(raw, fakePassword, http.DefaultClient); err == nil {
			t.Errorf("newClient(%q) error = nil, want a refused endpoint", raw)
		}
	}
}

func TestCorrelationIDIsAStableUUID(t *testing.T) {
	a, b := correlationID("file", "k0s/x", "https://example.com/x"), correlationID("file", "k0s/x", "https://example.com/x")
	if a != b || a == correlationID("file", "k0s/y", "https://example.com/x") {
		t.Errorf("correlationID() = %q, %q; want equal for one change and different for another", a, b)
	}
	if len(a) != 32 || a[12] != '4' || !strings.ContainsRune("89ab", rune(a[16])) {
		t.Errorf("correlationID() = %q, want a version 4 UUID in 32 hex digits", a)
	}
}
