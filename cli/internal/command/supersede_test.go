package command

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runCommand(t *testing.T, cmd *cobra.Command, args ...string) error {
	t.Helper()
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	return cmd.Execute()
}

// --cancel-active-workflows is decision 061's cancelActiveWorkflows: it reaches the body of an
// uninstall, a release, and a recover, whether that body comes from --file or from flags.
func TestCancelActiveWorkflowsFlagReachesTheBody(t *testing.T) {
	var bodies []map[string]any
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"operationId":"op-1"}`))
	})
	file := filepath.Join(t.TempDir(), "uninstall.yaml")
	if err := os.WriteFile(file, []byte("releaseServers: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, run := range []func() error{
		func() error { return runCommand(t, platformsUninstallCmd(), "plat-1", "--cancel-active-workflows") },
		func() error {
			return runCommand(t, platformsUninstallCmd(), "plat-1", "-f", file, "--cancel-active-workflows")
		},
		func() error {
			return runCommand(t, provisioningReleaseCmd(), "--server", "srv-1", "--cancel-active-workflows")
		},
		func() error {
			return runCommand(t, provisioningRecoverCmd(), "--server", "srv-1", "--cancel-active-workflows")
		},
		func() error { return runCommand(t, platformsUninstallCmd(), "plat-1") },
	} {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	want := []map[string]any{
		{"cancelActiveWorkflows": true},
		{"releaseServers": true, "cancelActiveWorkflows": true},
		{"serverIds": []any{"srv-1"}, "cancelActiveWorkflows": true},
		{"serverIds": []any{"srv-1"}, "cancelActiveWorkflows": true},
		nil,
	}
	if !reflect.DeepEqual(bodies, want) {
		t.Errorf("bodies = %v, want %v", bodies, want)
	}
}

// An uninstall refused because a Workflow holds the Servers names that Workflow and the flag that
// gets past it; any other conflict is reported unchanged.
func TestUninstallConflictHintsTheFlag(t *testing.T) {
	message := "a target is already in an unfinished operation: Server srv-1 already has active durable work"
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/workflows/" {
			if r.URL.Query().Get("platformId") != "plat-1" || r.URL.Query().Get("active") != "true" {
				t.Errorf("workflow query = %s, want the platform's active Workflows", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"wf-1","intent":"Deploy Slurm Platform lab","status":"requires_attention"}]}`))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "conflict", "message": message}})
	})
	err := runCommand(t, platformsUninstallCmd(), "plat-1")
	if err == nil || !strings.Contains(err.Error(), "--cancel-active-workflows") ||
		!strings.Contains(err.Error(), "wf-1 (Deploy Slurm Platform lab, requires_attention)") {
		t.Errorf("err = %v, want the holding Workflow and the flag named", err)
	}

	message = "platform cannot be uninstalled in its current state: platform lifecycle is uninstalling"
	if err := runCommand(t, platformsUninstallCmd(), "plat-1"); err == nil || strings.Contains(err.Error(), "--cancel-active-workflows") {
		t.Errorf("err = %v, want the lifecycle conflict without the flag hint", err)
	}
}
