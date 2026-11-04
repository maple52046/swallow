package ansible

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

// provisioningRole is the shared role that holds the datasource and the dashboard bundles every
// swallow-installed Grafana provisions, so the Grafana Managed Software and the Kubernetes Platform
// Grafana read one copy.
const provisioningRole = "grafana_provisioning"

// Every dashboard bundle the software domain accepts ships as a directory of dashboards the shared
// provisioning role delivers into its own folder, and every shipped directory is a bundle the
// domain accepts.
func TestGrafanaShipsEveryDashboardBundle(t *testing.T) {
	root := filepath.Join(roleDir(t, provisioningRole), "files", "dashboards")
	folders, _ := readRoleDefaults(t, provisioningRole)["grafana_provisioning_folders"].(map[string]any)
	for _, bundle := range []string{
		softwaredomain.DashboardBundleOmnistat, softwaredomain.DashboardBundleDeviceMetricsExporter, softwaredomain.DashboardBundleNodeExporter,
	} {
		matches, err := filepath.Glob(filepath.Join(root, bundle, "*.json"))
		if err != nil || len(matches) == 0 {
			t.Errorf("bundle %s ships no dashboard (%v)", bundle, err)
		}
		if folders[bundle] == nil {
			t.Errorf("bundle %s has no folder in grafana_provisioning_folders", bundle)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if err := softwaredomain.ValidateDashboardBundle(entry.Name()); err != nil {
				t.Errorf("shipped directory %s: %v", entry.Name(), err)
			}
		}
	}
}

// The node-exporter bundle is swallow's own: every panel reads the provisioned datasource by its
// uid, and the Servers are chosen by server_id, the label every Site Prometheus target carries.
func TestNodeExporterDashboardReadsTheProvisionedDatasource(t *testing.T) {
	path := filepath.Join(roleDir(t, provisioningRole), "files", "dashboards", softwaredomain.DashboardBundleNodeExporter, "servers.json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dashboard struct {
		UID        string `json:"uid"`
		Templating struct {
			List []struct {
				Name       string `json:"name"`
				Definition string `json:"definition"`
			} `json:"list"`
		} `json:"templating"`
		Panels []struct {
			Title      string `json:"title"`
			Datasource struct {
				UID string `json:"uid"`
			} `json:"datasource"`
			Targets []struct {
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(content, &dashboard); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	uid := readRoleDefaults(t, provisioningRole)["grafana_provisioning_datasource_uid"]
	if len(dashboard.Templating.List) != 1 || dashboard.Templating.List[0].Name != "server_id" ||
		dashboard.Templating.List[0].Definition != "label_values(node_uname_info, server_id)" {
		t.Errorf("variables = %+v, want server_id from node_uname_info", dashboard.Templating.List)
	}
	if len(dashboard.Panels) == 0 {
		t.Fatal("the dashboard has no panels")
	}
	for _, panel := range dashboard.Panels {
		if panel.Datasource.UID != uid {
			t.Errorf("panel %q reads datasource %q, want %v", panel.Title, panel.Datasource.UID, uid)
		}
		for _, target := range panel.Targets {
			if !strings.Contains(target.Expr, `server_id=~"$server_id"`) {
				t.Errorf("panel %q query %q is not limited to the chosen Servers", panel.Title, target.Expr)
			}
		}
	}
}

// Every shipped bundle names the dashboard a Grafana provisioned with it opens on, by the uid one of
// the bundle's own dashboards carries, so the /d/<uid> link swallow records stays valid; and the
// bundles rank in the order a Grafana with several of them picks from: the GPU or job monitoring a
// Platform has before the Servers every Platform Grafana carries.
func TestGrafanaBundlesNameTheirLandingDashboard(t *testing.T) {
	defaults := readRoleDefaults(t, provisioningRole)
	landing, _ := defaults["grafana_provisioning_landing_dashboards"].(map[string]any)
	root := filepath.Join(roleDir(t, provisioningRole), "files", "dashboards")
	wantOrder := []string{
		softwaredomain.DashboardBundleDeviceMetricsExporter, softwaredomain.DashboardBundleOmnistat, softwaredomain.DashboardBundleNodeExporter,
	}
	for _, bundle := range wantOrder {
		uid, _ := landing[bundle].(string)
		if uids := bundleDashboardUIDs(t, filepath.Join(root, bundle)); uid == "" || !slices.Contains(uids, uid) {
			t.Errorf("bundle %s lands on uid %q, want one of its dashboards' uids %v", bundle, uid, uids)
		}
	}
	order, _ := defaults["grafana_provisioning_landing_order"].([]any)
	if got, want := fmt.Sprint(order), fmt.Sprint(wantOrder); got != want {
		t.Errorf("grafana_provisioning_landing_order = %s, want %s", got, want)
	}
}

// The link swallow records opens the landing dashboard of the bundles a Grafana provisions, whatever
// order they are requested in, and the Grafana root when there is no bundle.
func TestGrafanaLandingPathFollowsTheBundleOrder(t *testing.T) {
	python := requireJinja(t)
	defaults := readRoleDefaults(t, provisioningRole)
	template, _ := defaults["grafana_provisioning_landing_path"].(string)
	const (
		dme      = softwaredomain.DashboardBundleDeviceMetricsExporter
		omnistat = softwaredomain.DashboardBundleOmnistat
		servers  = softwaredomain.DashboardBundleNodeExporter
	)
	tests := []struct {
		name    string
		bundles []string
		want    string
	}{
		{name: "no bundle keeps the root", bundles: []string{}, want: ""},
		{name: "Servers alone", bundles: []string{servers}, want: "/d/swallow-node-exporter"},
		{name: "Omnistat over the Servers", bundles: []string{servers, omnistat}, want: "/d/edqnz7pce7togc-rms"},
		{name: "Device Metrics Exporter over the Servers", bundles: []string{servers, dme}, want: "/d/fe0ayyeeoxvy8f"},
		{name: "Device Metrics Exporter over Omnistat", bundles: []string{omnistat, servers, dme}, want: "/d/fe0ayyeeoxvy8f"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderJinja(t, python, template, map[string]any{
				"grafana_provisioning_bundles":            tc.bundles,
				"grafana_provisioning_landing_dashboards": defaults["grafana_provisioning_landing_dashboards"],
				"grafana_provisioning_landing_order":      defaults["grafana_provisioning_landing_order"],
			})
			if got != tc.want {
				t.Errorf("grafana_provisioning_landing_path for bundles %v = %q, want %q", tc.bundles, got, tc.want)
			}
		})
	}
}

// A run on one Server reports its Grafana's landing dashboard, after every dashboard is served at
// the root its waits check; swallow records it as the Platform Grafana Monitoring Link. The token
// never reaches Grafana's own files, and the Grafana role takes its provisioning from the shared
// role rather than a copy of its own.
func TestGrafanaReportsItsURLAndKeepsTheTokenOutOfItsFiles(t *testing.T) {
	path := filepath.Join(roleDir(t, "grafana"), "tasks", "main.yml")
	tasks, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(tasks)
	report := strings.Index(content, "name: Report the Grafana URL to Swallow")
	wait := strings.Index(content, "name: Wait for Grafana to serve every provisioned dashboard")
	if report < 0 || wait < 0 || report < wait {
		t.Error("the Grafana URL must be reported after every provisioned dashboard is served")
	}
	checkReportsTheLandingDashboard(t, path, "grafana_url", "grafana_dashboards")
	for _, want := range []string{
		"grafana_port }}/api/health", "grafana_port }}/api/search?type=dash-db",
		"ansible_play_hosts_all | length == 1", "no_log: true",
		"name: " + provisioningRole, "tasks_from: files", "{{ grafana_provisioning_token_variable }}=Bearer",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("grafana tasks do not contain %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(roleDir(t, "grafana"), "files")); !os.IsNotExist(err) {
		t.Errorf("the grafana role ships files of its own (%v); the dashboard bundles belong to %s", err, provisioningRole)
	}
	datasource, err := os.ReadFile(filepath.Join(roleDir(t, provisioningRole), "templates", "datasources.yaml.j2"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(datasource), "datasource_token }}") || !strings.Contains(string(datasource), "$__env{") {
		t.Error("the datasource file must name the token's environment variable, not hold the token")
	}
}

// checkReportsTheLandingDashboard pins how a role that installs a Grafana reports the Platform
// Grafana link. Its report task appends the shared grafana_provisioning_landing_path to root, the
// expression of the Grafana root its waits use, and sets the shared role's bundle input to bundles,
// the role's own bundle list, because the include's vars end with the include and the path would
// otherwise be chosen from no bundle. No other task reads the path, so every wait stays on the root.
func checkReportsTheLandingDashboard(t *testing.T, path, root, bundles string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(content), "grafana_provisioning_landing_path"); got != 1 {
		t.Errorf("%s names grafana_provisioning_landing_path %d times, want once, in the report", path, got)
	}
	task := namedTask(t, path, "Report the Grafana URL to Swallow")
	args, _ := task["ansible.builtin.copy"].(map[string]any)
	if want := "{{ {'grafanaUrl': " + root + " ~ grafana_provisioning_landing_path} | to_json }}"; args["content"] != want {
		t.Errorf("%s reports %v, want %s", path, args["content"], want)
	}
	vars, _ := task["vars"].(map[string]any)
	if want := "{{ " + bundles + " }}"; vars["grafana_provisioning_bundles"] != want {
		t.Errorf("%s reports with grafana_provisioning_bundles %v, want %s", path, vars["grafana_provisioning_bundles"], want)
	}
}

// namedTask returns the task called name in a role's task file, descending into blocks, so a test
// checks that task's own arguments and vars rather than text anywhere in the file.
func namedTask(t *testing.T, path, name string) map[string]any {
	t.Helper()
	var tasks []any
	readYAML(t, path, &tasks)
	if task := findNamedTask(tasks, name); task != nil {
		return task
	}
	t.Fatalf("%s has no task %q", path, name)
	return nil
}

// findNamedTask is namedTask's search; it returns nil when no task in the list or its blocks has
// that name.
func findNamedTask(tasks []any, name string) map[string]any {
	for _, item := range tasks {
		task, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if task["name"] == name {
			return task
		}
		for _, key := range []string{"block", "rescue", "always"} {
			if nested, ok := task[key].([]any); ok {
				if found := findNamedTask(nested, name); found != nil {
					return found
				}
			}
		}
	}
	return nil
}

// bundleDashboardUIDs returns the uids of the dashboards a shipped bundle directory holds.
func bundleDashboardUIDs(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var uids []string
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var dashboard struct {
			UID string `json:"uid"`
		}
		if err := json.Unmarshal(content, &dashboard); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		uids = append(uids, dashboard.UID)
	}
	return uids
}

// requireJinja returns the python3 that renderJinja runs, skipping the test where python3 or its
// jinja2 module, the engine Ansible templates with, is missing.
func requireJinja(t *testing.T) string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	if err := exec.Command(python, "-c", "import jinja2").Run(); err != nil {
		t.Skip("python3 has no jinja2 module")
	}
	return python
}

// renderJinja renders a variable's template with vars through jinja2 with strict undefined
// variables, as Ansible does. It stands in for Ansible only for templates built from Jinja's own
// filters and tests; a template that uses an Ansible filter fails here.
func renderJinja(t *testing.T, python, template string, vars map[string]any) string {
	t.Helper()
	input, err := json.Marshal(map[string]any{"template": template, "vars": vars})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-c", `import json, sys, jinja2
data = json.load(sys.stdin)
env = jinja2.Environment(undefined=jinja2.StrictUndefined)
sys.stdout.write(env.from_string(data["template"]).render(**data["vars"]))`)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("render %q: %v: %s", template, err, stderr.String())
	}
	return string(out)
}
