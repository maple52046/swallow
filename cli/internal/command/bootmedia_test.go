package command

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/spf13/cobra"
)

// TestBootMediaCommandsFollowTheContract checks each Boot Media command's method, path, query,
// and body against server-detail-actions.md: enable sends {"enabled": true, "isoId"}, disable
// only {"enabled": false}, get asks for the live read only with --live, and the probe posts with
// no body.
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
	run(serversBootMediaCmd(), "enable", "srv-1", "--iso", "iso-1")
	run(serversBootMediaCmd(), "disable", "srv-1")
	run(serversRedfishProbeCmd(), "srv-1")

	want := []seen{
		{method: "GET", path: "/api/v1/servers/srv-1/boot-media", query: "live=true"},
		{method: "PUT", path: "/api/v1/servers/srv-1/boot-media", body: map[string]any{"enabled": true, "isoId": "iso-1"}},
		{method: "PUT", path: "/api/v1/servers/srv-1/boot-media", body: map[string]any{"enabled": false}},
		{method: "POST", path: "/api/v1/servers/srv-1/redfish/probe"},
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %+v, want %d requests", requests, len(want))
	}
	for i := range want {
		got := requests[i]
		if got.method != want[i].method || got.path != want[i].path || got.query != want[i].query ||
			got.body["enabled"] != want[i].body["enabled"] || got.body["isoId"] != want[i].body["isoId"] || len(got.body) != len(want[i].body) {
			t.Errorf("request %d = %+v, want %+v", i, got, want[i])
		}
	}
}

// Enabling without --iso is refused before any request: the contract requires an isoId.
func TestBootMediaEnableRequiresISO(t *testing.T) {
	useTestServer(t, func(http.ResponseWriter, *http.Request) {
		t.Error("enable without --iso reached the API")
	})
	cmd := serversBootMediaCmd()
	cmd.SetArgs([]string{"enable", "srv-1"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil {
		t.Fatal("enable without --iso succeeded")
	}
}

// TestBootISOCommandsFollowTheContract checks each Boot ISO command against boot-isos.md: list
// narrows by Site and integration, create posts exactly name, integrationId, and rackAddress, and
// get and delete address one ISO.
func TestBootISOCommandsFollowTheContract(t *testing.T) {
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
		if r.Method == "DELETE" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"iso-1"}`))
	})

	run := func(args ...string) {
		t.Helper()
		cmd := provisioningBootISOsCmd()
		cmd.SetArgs(args)
		cmd.SetContext(context.Background())
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	run("list", "--site-id", "site-a", "--integration", "maas-a")
	run("create", "--name", "tainan-rack", "--integration", "maas-a", "--rack", " 10.0.0.2:5248 ")
	run("get", "iso-1")
	run("delete", "iso-1")

	want := []seen{
		{method: "GET", path: "/api/v1/provisioning/boot-isos", query: "integrationId=maas-a&siteId=site-a"},
		{method: "POST", path: "/api/v1/provisioning/boot-isos", body: map[string]any{"name": "tainan-rack", "integrationId": "maas-a", "rackAddress": "10.0.0.2:5248"}},
		{method: "GET", path: "/api/v1/provisioning/boot-isos/iso-1"},
		{method: "DELETE", path: "/api/v1/provisioning/boot-isos/iso-1"},
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %+v, want %d requests", requests, len(want))
	}
	for i := range want {
		got := requests[i]
		same := got.method == want[i].method && got.path == want[i].path && got.query == want[i].query && len(got.body) == len(want[i].body)
		for key, value := range want[i].body {
			same = same && got.body[key] == value
		}
		if !same {
			t.Errorf("request %d = %+v, want %+v", i, got, want[i])
		}
	}
}
