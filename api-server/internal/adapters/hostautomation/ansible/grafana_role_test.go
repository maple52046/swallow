package ansible

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

// Every dashboard bundle the software domain accepts ships as a directory of dashboards the grafana
// role provisions into its own folder, and every shipped directory is a bundle the domain accepts.
func TestGrafanaShipsEveryDashboardBundle(t *testing.T) {
	root := filepath.Join(roleDir(t, "grafana"), "files", "dashboards")
	folders, _ := readRoleDefaults(t, "grafana")["grafana_dashboard_folders"].(map[string]any)
	for _, bundle := range []string{
		softwaredomain.DashboardBundleOmnistat, softwaredomain.DashboardBundleDeviceMetricsExporter, softwaredomain.DashboardBundleNodeExporter,
	} {
		matches, err := filepath.Glob(filepath.Join(root, bundle, "*.json"))
		if err != nil || len(matches) == 0 {
			t.Errorf("bundle %s ships no dashboard (%v)", bundle, err)
		}
		if folders[bundle] == nil {
			t.Errorf("bundle %s has no folder in grafana_dashboard_folders", bundle)
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
	path := filepath.Join(roleDir(t, "grafana"), "files", "dashboards", softwaredomain.DashboardBundleNodeExporter, "servers.json")
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
	uid := readRoleDefaults(t, "grafana")["grafana_datasource_uid"]
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

// A run on one Server reports where its Grafana serves, after every dashboard is served; swallow
// records it as the Platform Grafana Monitoring Link. The token never reaches Grafana's own files.
func TestGrafanaReportsItsURLAndKeepsTheTokenOutOfItsFiles(t *testing.T) {
	tasks, err := os.ReadFile(filepath.Join(roleDir(t, "grafana"), "tasks", "main.yml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(tasks)
	report := strings.Index(content, "name: Report the Grafana URL to Swallow")
	wait := strings.Index(content, "name: Wait for Grafana to serve every provisioned dashboard")
	if report < 0 || wait < 0 || report < wait {
		t.Error("the Grafana URL must be reported after every provisioned dashboard is served")
	}
	for _, want := range []string{"{'grafanaUrl': grafana_url}", "ansible_play_hosts_all | length == 1", "no_log: true"} {
		if !strings.Contains(content, want) {
			t.Errorf("grafana tasks do not contain %q", want)
		}
	}
	datasource, err := os.ReadFile(filepath.Join(roleDir(t, "grafana"), "templates", "datasource.yaml.j2"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(datasource), "grafana_datasource_token }}") {
		t.Error("the datasource file must name the token's environment variable, not hold the token")
	}
}
