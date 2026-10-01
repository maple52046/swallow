package command

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maple52046/swallow/cli/internal/config"
	"github.com/maple52046/swallow/cli/internal/output"
)

// useTestServer points the process-global runtime at an httptest server for one test.
func useTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	previousRT, previousTimeout := rt, gf.timeout
	rt = runtime{cfg: &config.Config{Endpoint: server.URL, Token: "test-token"}, format: output.FormatJSON}
	gf.timeout = 5 * time.Second
	t.Cleanup(func() { rt, gf.timeout = previousRT, previousTimeout })
}

// TestGenerateWritesPrivateKeyToNewFileOnly guards the one-time private key: it is written to the
// named file with 0600, and an existing file is never overwritten — the command fails before
// asking the API for a key pair whose private half would otherwise be lost.
func TestGenerateWritesPrivateKeyToNewFileOnly(t *testing.T) {
	var calls atomic.Int32
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/ssh-keys/generate" {
			t.Errorf("request = %s %s, want POST /api/v1/ssh-keys/generate", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":{"id":"k1","name":"jumpbox","purpose":"access"},"privateKey":"PRIVATE-KEY\n"}`))
	})
	path := filepath.Join(t.TempDir(), "id_ed25519_jumpbox")

	run := func() error {
		cmd := sshKeysGenerateCmd()
		cmd.SetArgs([]string{"--name", "jumpbox", "--private-key-out", path})
		cmd.SetContext(context.Background())
		return cmd.Execute()
	}
	if err := run(); err != nil {
		t.Fatalf("generate: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat private key file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("private key file mode = %o, want 600", got)
	}
	if data, _ := os.ReadFile(path); string(data) != "PRIVATE-KEY\n" {
		t.Errorf("private key file = %q, want the API's private key", data)
	}

	if err := run(); err == nil {
		t.Fatalf("generate into an existing file succeeded, want an error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("API calls = %d, want 1: an unusable output path must fail before generating", got)
	}
}
