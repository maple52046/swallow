package command

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
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

// --cancel-active-workflows is decision 061's cancelActiveWorkflows: it reaches the body of a
// Platform delete, a release, and a recover, next to the other flags.
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
	for _, run := range []func() error{
		func() error { return runCommand(t, platformsDeleteCmd(), "plat-1", "--cancel-active-workflows") },
		func() error {
			return runCommand(t, platformsDeleteCmd(), "plat-1", "--release-servers", "--erase", "--cancel-active-workflows")
		},
		func() error {
			return runCommand(t, provisioningReleaseCmd(), "--server", "srv-1", "--cancel-active-workflows")
		},
		func() error {
			return runCommand(t, provisioningRecoverCmd(), "--server", "srv-1", "--cancel-active-workflows")
		},
		func() error { return runCommand(t, platformsDeleteCmd(), "plat-1") },
	} {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	want := []map[string]any{
		{"releaseServers": false, "cancelActiveWorkflows": true},
		{"releaseServers": true, "releaseOptions": map[string]any{"erase": true}, "cancelActiveWorkflows": true},
		{"serverIds": []any{"srv-1"}, "cancelActiveWorkflows": true},
		{"serverIds": []any{"srv-1"}, "cancelActiveWorkflows": true},
		{"releaseServers": false},
	}
	if !reflect.DeepEqual(bodies, want) {
		t.Errorf("bodies = %v, want %v", bodies, want)
	}
}
