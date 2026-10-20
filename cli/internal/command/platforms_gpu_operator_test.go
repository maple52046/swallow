package command

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestGPUOperatorCommandsFollowTheContract checks each GPU Operator command against platforms.md
// "Manage The GPU Operator": releases reads the catalog, set passes the settings file through as
// the PUT body unchanged, and uninstall sends exactly {"enabled": false}.
func TestGPUOperatorCommandsFollowTheContract(t *testing.T) {
	type seen struct {
		method, path string
		body         map[string]any
	}
	var requests []seen
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		request := seen{method: r.Method, path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&request.body)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"platformId":"plat-1","workflowId":"wf-1"}`))
	})

	settings := filepath.Join(t.TempDir(), "gpu-operator.yaml")
	if err := os.WriteFile(settings, []byte("enabled: true\nallocationMode: dra\ndraDriverVersion: v1.0.1\n"), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := platformsGPUOperatorCmd()
		cmd.SetArgs(args)
		cmd.SetContext(context.Background())
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	run("releases")
	run("set", "plat-1", "-f", settings)
	run("uninstall", "plat-1")

	want := []seen{
		{method: "GET", path: "/api/v1/platforms/gpu-operator/releases"},
		{method: "PUT", path: "/api/v1/platforms/plat-1/gpu-operator", body: map[string]any{
			"enabled": true, "allocationMode": "dra", "draDriverVersion": "v1.0.1",
		}},
		{method: "PUT", path: "/api/v1/platforms/plat-1/gpu-operator", body: map[string]any{"enabled": false}},
	}
	if !reflect.DeepEqual(requests, want) {
		t.Errorf("requests = %+v, want %+v", requests, want)
	}
}

// Setting without a body file is refused before any request: the contract requires enabled.
func TestGPUOperatorSetRequiresABody(t *testing.T) {
	useTestServer(t, func(http.ResponseWriter, *http.Request) {
		t.Error("set without -f reached the API")
	})
	cmd := platformsGPUOperatorCmd()
	cmd.SetArgs([]string{"set", "plat-1"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil {
		t.Fatal("set without -f succeeded")
	}
}
