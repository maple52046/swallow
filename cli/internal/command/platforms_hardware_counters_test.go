package command

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// TestHardwareCountersCommandsFollowTheContract checks enable and disable against platforms.md
// "Omnistat": each reads the Platform and writes its spec back with only
// settings.omnistat.hardwareCounters changed, at the generation it read, and a Platform whose spec
// has no enabled Omnistat is refused before anything is written.
func TestHardwareCountersCommandsFollowTheContract(t *testing.T) {
	type seen struct {
		method, path string
		body         map[string]any
	}
	var requests []seen
	platform := `{"generation":4,"spec":{"members":[{"serverId":"s1","roles":["controller"]},{"serverId":"s2","roles":["compute"]}],` +
		`"settings":{"slurm":{"clusterName":"lab"},"omnistat":{"enabled":true,"installMethod":"native","hardwareCounters":false}},` +
		`"machinePreparation":{"mode":"existing_os"}}}`
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		request := seen{method: r.Method, path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&request.body)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms/plat-1":
			_, _ = w.Write([]byte(platform))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms/plat-dme":
			_, _ = w.Write([]byte(`{"generation":2,"spec":{"members":[],"settings":{"deviceMetricsExporter":{"enabled":true}}}}`))
		default:
			_, _ = w.Write([]byte(`{"platform":{},"plan":null}`))
		}
	})
	run := func(args ...string) error {
		t.Helper()
		cmd := platformsHardwareCountersCmd()
		cmd.SetArgs(args)
		cmd.SetContext(context.Background())
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		return cmd.Execute()
	}
	for _, args := range [][]string{{"enable", "plat-1"}, {"disable", "plat-1"}} {
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}

	members := []any{
		map[string]any{"serverId": "s1", "roles": []any{"controller"}},
		map[string]any{"serverId": "s2", "roles": []any{"compute"}},
	}
	specWith := func(hardwareCounters bool) map[string]any {
		return map[string]any{"generation": float64(4), "spec": map[string]any{
			"members": members,
			"settings": map[string]any{
				"slurm":    map[string]any{"clusterName": "lab"},
				"omnistat": map[string]any{"enabled": true, "installMethod": "native", "hardwareCounters": hardwareCounters},
			},
			"machinePreparation": map[string]any{"mode": "existing_os"},
		}}
	}
	want := []seen{
		{method: http.MethodGet, path: "/api/v1/platforms/plat-1"},
		{method: http.MethodPut, path: "/api/v1/platforms/plat-1/spec", body: specWith(true)},
		{method: http.MethodGet, path: "/api/v1/platforms/plat-1"},
		{method: http.MethodPut, path: "/api/v1/platforms/plat-1/spec", body: specWith(false)},
	}
	if !reflect.DeepEqual(requests, want) {
		t.Errorf("requests = %+v, want %+v", requests, want)
	}

	requests = nil
	err := run("enable", "plat-dme")
	if err == nil || !strings.Contains(err.Error(), "no Omnistat") {
		t.Errorf("enable on a Platform without Omnistat: err = %v, want the refusal", err)
	}
	if len(requests) != 1 || requests[0].method != http.MethodGet {
		t.Errorf("requests = %+v, want only the read", requests)
	}
}
