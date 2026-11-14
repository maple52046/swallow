package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// scalingRequest is one request the test server saw.
type scalingRequest struct {
	method, path, query string
	body                map[string]any
}

// scalingServer answers GET /platforms/plat-1 with platform and records every request; writes answer
// with response.
func scalingServer(t *testing.T, platform, response string) *[]scalingRequest {
	t.Helper()
	var requests []scalingRequest
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		request := scalingRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&request.body)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(platform))
			return
		}
		_, _ = w.Write([]byte(response))
	})
	return &requests
}

// runScaling runs one command constructor with args and returns its stderr.
func runScaling(t *testing.T, build func() *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd := build()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	return stderr.String(), err
}

// legacyPlatform is a Slurm Platform from before node groups: a controller and two compute nodes.
const legacyPlatform = `{"generation":7,"spec":{"members":[{"serverId":"c1","roles":["controller"]},` +
	`{"serverId":"g1","roles":["compute"]},{"serverId":"g2","roles":["compute"]}],` +
	`"settings":{"slurm":{"clusterName":"lab"}},"machinePreparation":{"mode":"provision_os","generatedTemplates":[{"id":"t1"}]}}}`

// writtenSpec is the spec of the last write.
func writtenSpec(t *testing.T, requests []scalingRequest) map[string]any {
	t.Helper()
	last := requests[len(requests)-1]
	if last.method != http.MethodPut || last.path != "/api/v1/platforms/plat-1/spec" {
		t.Fatalf("last request = %s %s, want the spec write", last.method, last.path)
	}
	if last.body["generation"] != float64(7) {
		t.Errorf("generation = %v, want the one read", last.body["generation"])
	}
	spec, _ := last.body["spec"].(map[string]any)
	return spec
}

// Removing nodes drops them from the members and their group, records their removal choices, keeps
// the machine preparation as read, and approves the plan with --approve. A legacy spec gets the groups
// and the partition the API derives.
func TestNodesRemoveWritesTheRemovalChoices(t *testing.T) {
	requests := scalingServer(t, legacyPlatform, `{"platform":{},"plan":{"id":"p1","state":"approved"},"warnings":[]}`)
	stderr, err := runScaling(t, platformsNodesCmd, "remove", "plat-1", "--server", "g2", "--drain-timeout", "30m", "--force", "--release", "--erase", "--approve")
	if err != nil {
		t.Fatal(err)
	}
	if last := (*requests)[len(*requests)-1]; last.query != "approve=true" {
		t.Errorf("query = %q, want approve=true", last.query)
	}
	spec := writtenSpec(t, *requests)
	if want := []any{
		map[string]any{"serverId": "c1", "roles": []any{"controller"}},
		map[string]any{"serverId": "g1", "roles": []any{"compute"}},
	}; !reflect.DeepEqual(spec["members"], want) {
		t.Errorf("members = %v", spec["members"])
	}
	if want := []any{map[string]any{
		"serverId": "g2", "drainTimeoutSeconds": float64(1800), "force": true, "releaseServer": true,
		"erase": true, "secureErase": false, "quickErase": false, "unbindStaticIps": false,
	}}; !reflect.DeepEqual(spec["memberRemovals"], want) {
		t.Errorf("memberRemovals = %v", spec["memberRemovals"])
	}
	slurm := spec["settings"].(map[string]any)["slurm"].(map[string]any)
	groups := slurm["nodeGroups"].([]any)
	if compute := groups[1].(map[string]any); compute["name"] != "compute" || !reflect.DeepEqual(compute["members"], []any{"g1"}) {
		t.Errorf("groups = %v", groups)
	}
	if partitions := slurm["partitions"].([]any); len(partitions) != 1 || partitions[0].(map[string]any)["name"] != "main" {
		t.Errorf("partitions = %v", partitions)
	}
	if preparation := spec["machinePreparation"].(map[string]any); preparation["generatedTemplates"] == nil {
		t.Errorf("machinePreparation = %v, want it written back as read", preparation)
	}
	if !strings.Contains(stderr, "Change Plan p1 was approved") {
		t.Errorf("stderr = %q", stderr)
	}
}

// A node whose removal has not finished is out of the spec but still applied: removing it again
// rewrites only its removal, so a drain that timed out can be retried with --force; a Server that is
// neither is refused before anything is written.
func TestNodesRemoveRewritesAnUnfinishedRemoval(t *testing.T) {
	leaving := `{"generation":7,"spec":{"members":[{"serverId":"c1","roles":["controller"]},{"serverId":"g1","roles":["compute"]}],` +
		`"settings":{"slurm":{"nodeGroups":[{"name":"compute","role":"compute","members":["g1"]}],"partitions":[{"name":"main","default":true,"groups":["compute"]}]}},` +
		`"memberRemovals":[{"serverId":"g2","drainTimeoutSeconds":120,"force":false}]},` +
		`"status":{"applied":{"members":[{"serverId":"c1"},{"serverId":"g1"},{"serverId":"g2"}]}}}`
	requests := scalingServer(t, leaving, `{"platform":{},"plan":{"id":"p2","state":"pending_approval"},"warnings":[]}`)
	if _, err := runScaling(t, platformsNodesCmd, "remove", "plat-1", "--server", "g2", "--drain-timeout", "2m", "--force"); err != nil {
		t.Fatal(err)
	}
	spec := writtenSpec(t, *requests)
	if members := spec["members"].([]any); len(members) != 2 {
		t.Errorf("members = %v, want them unchanged", members)
	}
	if want := []any{map[string]any{
		"serverId": "g2", "drainTimeoutSeconds": float64(120), "force": true, "releaseServer": false,
		"erase": false, "secureErase": false, "quickErase": false, "unbindStaticIps": false,
	}}; !reflect.DeepEqual(spec["memberRemovals"], want) {
		t.Errorf("memberRemovals = %v, want g2's removal forced", spec["memberRemovals"])
	}
	before := len(*requests)
	if _, err := runScaling(t, platformsNodesCmd, "remove", "plat-1", "--server", "g9"); err == nil || len(*requests) != before+1 {
		t.Errorf("a Server that is not a member: err = %v, requests %d, want refused after the read", err, len(*requests)-before)
	}
}

// Adding nodes puts them in the group and the members with role compute; a control-plane group and a
// Server already a member are refused before anything is written. The API's warnings go to stderr.
func TestNodesAddJoinsAComputeGroupAndPrintsWarnings(t *testing.T) {
	requests := scalingServer(t, legacyPlatform, `{"platform":{},"plan":null,"warnings":["server g3 in node group compute has 7 of the 8 GPUs the group expects"]}`)
	stderr, err := runScaling(t, platformsNodesCmd, "add", "plat-1", "--group", "compute", "--server", "g3")
	if err != nil {
		t.Fatal(err)
	}
	spec := writtenSpec(t, *requests)
	members := spec["members"].([]any)
	if last := members[len(members)-1]; !reflect.DeepEqual(last, map[string]any{"serverId": "g3", "roles": []any{"compute"}}) {
		t.Errorf("added member = %v", last)
	}
	if !strings.Contains(stderr, "Warning: server g3 in node group compute has 7 of the 8 GPUs") {
		t.Errorf("stderr = %q", stderr)
	}
	for _, args := range [][]string{
		{"add", "plat-1", "--group", "controller", "--server", "g3"},
		{"add", "plat-1", "--group", "compute", "--server", "g1"},
		{"add", "plat-1", "--group", "missing", "--server", "g3"},
	} {
		before := len(*requests)
		if _, err := runScaling(t, platformsNodesCmd, args...); err == nil {
			t.Errorf("%v: no error", args)
		}
		if len(*requests) != before+1 {
			t.Errorf("%v wrote the spec; want only the read", args)
		}
	}
}

// Groups and partitions are added and changed in the slurm setting; making a partition the default
// clears the other default, and removing the default partition is refused.
func TestGroupAndPartitionCommandsEditTheSlurmSetting(t *testing.T) {
	requests := scalingServer(t, legacyPlatform, `{"platform":{},"plan":null,"warnings":[]}`)
	if _, err := runScaling(t, platformsGroupsCmd, "add", "plat-1", "--name", "mi300x", "--template", "t9", "--expected-gpus", "8", "--partition", "main"); err != nil {
		t.Fatal(err)
	}
	slurm := writtenSpec(t, *requests)["settings"].(map[string]any)["slurm"].(map[string]any)
	groups := slurm["nodeGroups"].([]any)
	if added := groups[len(groups)-1]; !reflect.DeepEqual(added, map[string]any{"name": "mi300x", "role": "compute", "templateId": "t9", "expectedGpusPerNode": float64(8), "members": []any{}}) {
		t.Errorf("added group = %v", added)
	}
	if main := slurm["partitions"].([]any)[0].(map[string]any); !reflect.DeepEqual(main["groups"], []any{"compute", "mi300x"}) {
		t.Errorf("main = %v", main)
	}

	if _, err := runScaling(t, platformsPartitionsCmd, "add", "plat-1", "--name", "big", "--group", "compute", "--default", "--max-time", "2-00:00:00", "--def-mem-per-cpu", "4096"); err != nil {
		t.Fatal(err)
	}
	partitions := writtenSpec(t, *requests)["settings"].(map[string]any)["slurm"].(map[string]any)["partitions"].([]any)
	if want := []any{
		map[string]any{"name": "main", "default": false, "groups": []any{"compute"}, "maxTime": "INFINITE"},
		map[string]any{"name": "big", "default": true, "groups": []any{"compute"}, "maxTime": "2-00:00:00", "defMemPerCpu": float64(4096)},
	}; !reflect.DeepEqual(partitions, want) {
		t.Errorf("partitions = %v", partitions)
	}

	before := len(*requests)
	if _, err := runScaling(t, platformsPartitionsCmd, "remove", "plat-1", "main"); err == nil || len(*requests) != before+1 {
		t.Errorf("removing the default partition: err %v, requests %d; want it refused before writing", err, len(*requests)-before)
	}
	if _, err := runScaling(t, platformsPartitionsCmd, "add", "plat-1", "--name", "nogroups"); err == nil {
		t.Error("a partition without --group was accepted")
	}
}

// Rescan posts to the node's rescan route.
func TestNodesRescanPostsTheRoute(t *testing.T) {
	requests := scalingServer(t, legacyPlatform, `{"operationId":"op-1"}`)
	if _, err := runScaling(t, platformsNodesCmd, "rescan", "plat-1", "g1"); err != nil {
		t.Fatal(err)
	}
	if last := (*requests)[len(*requests)-1]; last.method != http.MethodPost || last.path != "/api/v1/platforms/plat-1/slurm/nodes/g1/rescan" {
		t.Errorf("request = %s %s", last.method, last.path)
	}
}

// The minimum hardware commands replace the floor as a whole with a PATCH of minimumHardware only.
func TestTemplateMinimumHardwareCommands(t *testing.T) {
	requests := scalingServer(t, `{}`, `{"id":"t1"}`)
	if _, err := runScaling(t, provisioningTemplateMinimumHardwareCmd, "set", "t1", "--cpu-cores", "32", "--gpu-model", "MI300X", "--gpu-count", "8"); err != nil {
		t.Fatal(err)
	}
	if _, err := runScaling(t, provisioningTemplateMinimumHardwareCmd, "clear", "t1"); err != nil {
		t.Fatal(err)
	}
	none := map[string]any{"cpuCores": nil, "memoryMiB": nil, "storageGB": nil, "gpuModel": nil, "gpuCount": nil}
	set := map[string]any{"cpuCores": float64(32), "memoryMiB": nil, "storageGB": nil, "gpuModel": "MI300X", "gpuCount": float64(8)}
	want := []scalingRequest{
		{method: http.MethodPatch, path: "/api/v1/provisioning/templates/t1", body: map[string]any{"minimumHardware": set}},
		{method: http.MethodPatch, path: "/api/v1/provisioning/templates/t1", body: map[string]any{"minimumHardware": none}},
	}
	if !reflect.DeepEqual(*requests, want) {
		t.Errorf("requests = %+v, want %+v", *requests, want)
	}
	if _, err := runScaling(t, provisioningTemplateMinimumHardwareCmd, "set", "t1", "--cpu-cores", "0"); err == nil {
		t.Error("--cpu-cores 0 was accepted")
	}
}

// A new node's --interface and --ip become its machine preparation network assignment; an address
// without an interface, an override for a Server not being added, and a platform whose members keep
// their OS are refused before anything is written.
func TestNodesAddWritesANetworkOverride(t *testing.T) {
	requests := scalingServer(t, legacyPlatform, `{"platform":{},"plan":null,"warnings":[]}`)
	if _, err := runScaling(t, platformsNodesCmd, "add", "plat-1", "--group", "compute", "--server", "g3", "--interface", "g3=nic-7", "--ip", "g3=10.0.0.53"); err != nil {
		t.Fatal(err)
	}
	preparation := writtenSpec(t, *requests)["machinePreparation"].(map[string]any)
	if want := []any{map[string]any{"serverId": "g3", "interfaceId": "nic-7", "ipAddress": "10.0.0.53"}}; !reflect.DeepEqual(preparation["network"].(map[string]any)["assignments"], want) {
		t.Errorf("assignments = %v", preparation["network"])
	}
	for _, args := range [][]string{
		{"add", "plat-1", "--group", "compute", "--server", "g3", "--ip", "g3=10.0.0.53"},
		{"add", "plat-1", "--group", "compute", "--server", "g3", "--interface", "g4=nic-1"},
		{"add", "plat-1", "--group", "compute", "--server", "g3", "--interface", "g3=nic-1", "--ip", "g3=10.0.0.999"},
	} {
		before := len(*requests)
		if _, err := runScaling(t, platformsNodesCmd, args...); err == nil || len(*requests) != before {
			t.Errorf("%v: err %v, %d requests; want it refused before reading", args, err, len(*requests)-before)
		}
	}
}

// A platform whose members keep their OS has no network to override.
func TestNodesAddRefusesANetworkOverrideWithoutProvisioning(t *testing.T) {
	existing := strings.Replace(legacyPlatform, `"mode":"provision_os"`, `"mode":"existing_os"`, 1)
	requests := scalingServer(t, existing, `{"platform":{},"plan":null,"warnings":[]}`)
	if _, err := runScaling(t, platformsNodesCmd, "add", "plat-1", "--group", "compute", "--server", "g3", "--interface", "g3=nic-7"); err == nil {
		t.Error("an override on an existing_os platform was accepted")
	}
	if len(*requests) != 1 {
		t.Errorf("requests = %d, want only the read", len(*requests))
	}
}
