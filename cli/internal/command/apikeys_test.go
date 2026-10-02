package command

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maple52046/swallow/cli/internal/client"
)

// TestAPIKeysCreateWritesSecretToNewFile guards the one-time secret: it goes to the
// named file with 0600, the requested lifetime becomes an absolute expiresAt, and an
// existing file is never overwritten.
func TestAPIKeysCreateWritesSecretToNewFile(t *testing.T) {
	var body map[string]any
	calls := 0
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/api-keys" {
			t.Errorf("request = %s %s, want POST /api/v1/api-keys", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":{"id":"k1","name":"ci"},"secret":"swk_SECRET"}`))
	})
	path := filepath.Join(t.TempDir(), "ci.key")
	run := func() error {
		cmd := apiKeysCreateCmd()
		cmd.SetArgs([]string{"--name", "ci", "--expires-in", "90d", "--secret-out", path})
		cmd.SetContext(context.Background())
		return cmd.Execute()
	}

	if err := run(); err != nil {
		t.Fatalf("create: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat secret file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("secret file mode = %o, want 600", got)
	}
	if data, _ := os.ReadFile(path); strings.TrimSpace(string(data)) != "swk_SECRET" {
		t.Errorf("secret file = %q, want the secret", data)
	}
	expiresAt, err := time.Parse(time.RFC3339, body["expiresAt"].(string))
	if err != nil || time.Until(expiresAt) < 89*24*time.Hour || time.Until(expiresAt) > 90*24*time.Hour {
		t.Errorf("expiresAt = %v (%v), want about 90 days from now", body["expiresAt"], err)
	}

	if err := run(); err == nil {
		t.Error("create overwrote an existing secret file")
	}
	if calls != 1 {
		t.Errorf("API calls = %d, want 1: the second run must fail before creating a key", calls)
	}
}

func TestParseExpiresIn(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "90d", want: 90 * 24 * time.Hour},
		{in: "12h", want: 12 * time.Hour},
		{in: "30m", want: 30 * time.Minute},
		{in: "0d", wantErr: true},
		{in: "-1h", wantErr: true},
		{in: "soon", wantErr: true},
	}
	for _, tc := range tests {
		got, err := parseExpiresIn(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("parseExpiresIn(%q) = %v, %v; want %v, error %v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

// TestPersistSessionRereadsTheProfile confirms a renewal is written through
// config.Update: the renewed access token is saved and a newer refresh token on disk
// is kept when the renewal did not rotate it.
func TestPersistSessionRereadsTheProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("endpoint: https://s.example\ntoken: old\nrefreshToken: on-disk\n"), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	useTestServer(t, func(http.ResponseWriter, *http.Request) {})
	rt.path = path

	if err := persistSession(clientSessionTokens("renewed", "")); err != nil {
		t.Fatalf("persistSession: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "token: renewed") || !strings.Contains(string(data), "refreshToken: on-disk") {
		t.Errorf("profile after renewal:\n%s\nwant the renewed token and the on-disk refresh token", data)
	}
}

func clientSessionTokens(access, refresh string) client.SessionTokens {
	return client.SessionTokens{AccessToken: access, AccessTokenExpiresAt: time.Now().Add(15 * time.Minute), RefreshToken: refresh}
}
