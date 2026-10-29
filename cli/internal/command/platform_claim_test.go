package command

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A release or recover refused because a Platform claims a target names that Platform and the
// uninstall that releases its Servers, whether the targets came from --server or --file; the
// --cancel-active-workflows hint, which cannot get past a claim, is not offered.
func TestPlatformClaimConflictHintsTheUninstall(t *testing.T) {
	const message = "Server gpu-node-02 belongs to kubernetes Platform production-k0s; uninstall the Platform " +
		"(with releaseServers to also release its Servers) instead of releasing the Server directly"
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/srv-1":
			_, _ = w.Write([]byte(`{"id":"srv-1","platformClaim":null}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/srv-2":
			_, _ = w.Write([]byte(`{"id":"srv-2","platformClaim":{"platformId":"platform-a","claimedAt":"2026-10-29T00:00:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms/platform-a":
			_, _ = w.Write([]byte(`{"id":"platform-a","name":"production-k0s"}`))
		default:
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "conflict", "message": message}})
		}
	})
	file := filepath.Join(t.TempDir(), "release.yaml")
	if err := os.WriteFile(file, []byte("serverIds: [srv-1, srv-2]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"release flags":   {"release", "--server", "srv-1", "--server", "srv-2"},
		"release file":    {"release", "-f", file},
		"recover flags":   {"recover", "--server", "srv-1", "--server", "srv-2", "--cancel-active-workflows"},
		"recover by file": {"recover", "-f", file},
	} {
		cmd := provisioningReleaseCmd()
		if args[0] == "recover" {
			cmd = provisioningRecoverCmd()
		}
		err := runCommand(t, cmd, args[1:]...)
		if err == nil {
			t.Fatalf("%s: err = nil, want the claim conflict", name)
		}
		got := err.Error()
		for _, want := range []string{message, "srv-2 belongs to Platform production-k0s (platform-a)", "swallow platforms uninstall platform-a", `"releaseServers": true`} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: err = %q, want it to contain %q", name, got, want)
			}
		}
		if strings.Contains(got, "rerun with --cancel-active-workflows") {
			t.Errorf("%s: err = %q, want no --cancel-active-workflows hint for a claim", name, got)
		}
	}
}

// A Server delete refused because a Platform claims the Server names that Platform and both ways
// to release the claim (uninstall or record deletion).
func TestPlatformClaimConflictHintsBeforeServerDelete(t *testing.T) {
	const message = "Server gpu-node-02 belongs to kubernetes Platform production-k0s; uninstall the Platform " +
		"(with releaseServers to also release its Servers) or delete its record before deleting the Server"
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/srv-2":
			_, _ = w.Write([]byte(`{"id":"srv-2","platformClaim":{"platformId":"platform-a"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms/platform-a":
			_, _ = w.Write([]byte(`{"id":"platform-a","name":"production-k0s"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/servers/srv-2":
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "conflict", "message": message}})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	err := runCommand(t, serversDeleteCmd(), "srv-2")
	if err == nil {
		t.Fatal("err = nil, want the claim conflict")
	}
	for _, want := range []string{message, "srv-2 belongs to Platform production-k0s (platform-a)",
		"swallow platforms uninstall platform-a", "swallow platforms delete platform-a"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// A conflict whose targets carry no claim, or whose Platform cannot be read, keeps the API's
// message; an unreadable Platform is named by its id.
func TestPlatformClaimHintFallsBack(t *testing.T) {
	const message = "server state changed; refresh and retry"
	platformReadable := true
	claimed := false
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/srv-1":
			if claimed {
				_, _ = w.Write([]byte(`{"id":"srv-1","platformClaim":{"platformId":"platform-a"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"srv-1","platformClaim":null}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms/platform-a" && platformReadable:
			_, _ = w.Write([]byte(`{"id":"platform-a","name":"production-k0s"}`))
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "not_found", "message": "not found"}})
		default:
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "conflict", "message": message}})
		}
	})

	err := runCommand(t, provisioningReleaseCmd(), "--server", "srv-1")
	if err == nil || !strings.HasSuffix(err.Error(), ": "+message) {
		t.Errorf("unclaimed: err = %v, want the API message unchanged", err)
	}

	claimed, platformReadable = true, false
	err = runCommand(t, provisioningReleaseCmd(), "--server", "srv-1")
	if err == nil || !strings.Contains(err.Error(), "srv-1 belongs to Platform platform-a;") {
		t.Errorf("unreadable Platform: err = %v, want the Platform named by its id", err)
	}
}
