package command

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/spf13/cobra"
)

// TestBootMediaCommandsFollowTheContract checks each Boot Media command's method, path, query,
// and body against server-detail-actions.md: enable/disable send only {"enabled": bool}, get
// asks for the live read only with --live, and the probe posts with no body.
func TestBootMediaCommandsFollowTheContract(t *testing.T) {
	type seen struct {
		method, path, query string
		body                map[string]any
	}
	var requests []seen
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		request := seen{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&request.body)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"serverId":"srv-1"}`))
	})

	run := func(cmd *cobra.Command, args ...string) {
		t.Helper()
		cmd.SetArgs(args)
		cmd.SetContext(context.Background())
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	run(serversBootMediaCmd(), "get", "srv-1", "--live")
	run(serversBootMediaCmd(), "enable", "srv-1")
	run(serversBootMediaCmd(), "disable", "srv-1")
	run(serversRedfishProbeCmd(), "srv-1")

	want := []seen{
		{method: "GET", path: "/api/v1/servers/srv-1/boot-media", query: "live=true"},
		{method: "PUT", path: "/api/v1/servers/srv-1/boot-media", body: map[string]any{"enabled": true}},
		{method: "PUT", path: "/api/v1/servers/srv-1/boot-media", body: map[string]any{"enabled": false}},
		{method: "POST", path: "/api/v1/servers/srv-1/redfish/probe"},
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %+v, want %d requests", requests, len(want))
	}
	for i := range want {
		got := requests[i]
		if got.method != want[i].method || got.path != want[i].path || got.query != want[i].query ||
			got.body["enabled"] != want[i].body["enabled"] || len(got.body) != len(want[i].body) {
			t.Errorf("request %d = %+v, want %+v", i, got, want[i])
		}
	}
}
