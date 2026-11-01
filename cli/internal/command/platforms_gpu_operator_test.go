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
// "Manage The GPU Operator": releases reads the catalog, and set and uninstall read the Platform and
// write its spec back with settings.gpuOperator replaced, at the generation they read.
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
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms/plat-1" {
			_, _ = w.Write([]byte(`{"generation":3,"spec":{"members":[{"serverId":"s1","roles":["control-plane","workloads"]}],` +
				`"settings":{"k0sVersion":"v1.36.3+k0s.2"},"machinePreparation":null}}`))
			return
		}
		_, _ = w.Write([]byte(`{"platform":{},"plan":null}`))
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

	members := []any{map[string]any{"serverId": "s1", "roles": []any{"control-plane", "workloads"}}}
	specWith := func(operator map[string]any) map[string]any {
		return map[string]any{"generation": float64(3), "spec": map[string]any{
			"members": members, "settings": map[string]any{"k0sVersion": "v1.36.3+k0s.2", "gpuOperator": operator},
		}}
	}
	want := []seen{
		{method: "GET", path: "/api/v1/platforms/gpu-operator/releases"},
		{method: "GET", path: "/api/v1/platforms/plat-1"},
		{method: "PUT", path: "/api/v1/platforms/plat-1/spec", body: specWith(map[string]any{
			"enabled": true, "allocationMode": "dra", "draDriverVersion": "v1.0.1",
		})},
		{method: "GET", path: "/api/v1/platforms/plat-1"},
		{method: "PUT", path: "/api/v1/platforms/plat-1/spec", body: specWith(map[string]any{"enabled": false})},
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
