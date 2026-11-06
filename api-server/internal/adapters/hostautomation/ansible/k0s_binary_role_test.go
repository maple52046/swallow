package ansible

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// k0s_binary installs the pinned k0s from the Local Repository's files and treats a binary as
// installed only when it reports the pinned version, so a truncated download left by an earlier run
// is replaced instead of failing every later step (DF-96). A rerun at the pinned version changes
// nothing, and a version the Local Repository does not hold fails before any download.
func TestK0sBinaryInstallsTheVersionItVerifies(t *testing.T) {
	t.Parallel()
	const version = "v1.0.0+k0s.0"
	binary := "#!/bin/sh\necho " + version + "\n"
	sum := sha256.Sum256([]byte(binary))
	path := "k0s/" + version + "/k0s-" + version + "-amd64"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/"+path {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(binary))
	}))
	t.Cleanup(server.Close)

	run := newRoleRun(t, "k0s_binary", true)
	installed := filepath.Join(t.TempDir(), "k0s")
	release := map[string]any{"path": path, "sha256": hex.EncodeToString(sum[:])}
	vars := func(pinned string) map[string]any {
		return map[string]any{
			"swallow_k0s_version":      pinned,
			"swallow_local_repository": map[string]any{"files": server.URL + "/files/"},
			"k0s_binary_path":          installed,
			"k0s_binary_releases":      map[string]any{version: map[string]any{"amd64": release, "arm64": release}},
		}
	}

	if out, changed, failed := run.run(vars(version)); failed != 0 || changed == 0 {
		t.Fatalf("first run changed %d and failed %d tasks, want k0s installed:\n%s", changed, failed, out)
	}
	if info, err := os.Stat(installed); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed k0s = %v (stat error %v), want an executable binary", info, err)
	}
	if out, changed, failed := run.run(vars(version)); failed != 0 || changed != 0 {
		t.Errorf("rerun at the pinned version changed %d and failed %d tasks, want nothing:\n%s", changed, failed, out)
	}

	if err := os.WriteFile(installed, []byte(binary[:5]), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, changed, failed := run.run(vars(version)); failed != 0 || changed == 0 {
		t.Errorf("run over a truncated binary changed %d and failed %d tasks, want it replaced:\n%s", changed, failed, out)
	}
	if got := readScratch(t, filepath.Dir(installed), "k0s"); got != binary {
		t.Errorf("k0s after replacing a truncated binary = %q, want the verified release", got)
	}

	if out, _, failed := run.run(vars("v9.9.9+k0s.0")); failed == 0 || !strings.Contains(out, "[k0s_version_unavailable]") {
		t.Errorf("a version the Local Repository does not hold = %d failed tasks, want [k0s_version_unavailable]:\n%s", failed, out)
	}
}
