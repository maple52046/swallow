package ansible

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

// The Kubernetes Platform Grafana (monitoring.md §6.2) reads only its trusted variables and the
// sealed token, keeps the token and the admin password in Secrets the chart only names, provisions
// from the shared role, and reports its URL only once every dashboard is served at that address.
func TestKubernetesPlatformGrafanaPlaybookHonoursTheContract(t *testing.T) {
	role := filepath.Join("roles", "k0s_platform_grafana")
	cases := []struct {
		path      string
		required  []string
		forbidden []string
	}{
		{
			path: "configure-kubernetes-grafana.yml",
			required: []string{
				"swallow_k0s_grafana_controller_id in ansible_play_hosts_all",
				"inventory_hostname in swallow_k0s_grafana_node_ids",
				"tasks_from: firewall",
				"hosts: k0s_platform_grafana_controller",
			},
		},
		{
			path: filepath.Join(role, "defaults", "main.yml"),
			required: []string{
				"swallow_k0s_grafana_node_ids", "swallow_k0s_grafana_datasource_url",
				"swallow_k0s_grafana_dashboards", "swallow_grafana_datasource_token",
				"k0s_platform_grafana_node_port: 30300",
			},
		},
		{
			path: filepath.Join(role, "tasks", "main.yml"),
			required: []string{
				"name: helm",
				"name: grafana_provisioning",
				"tasks_from: kubernetes",
				"[platform_grafana_node_port_in_use]",
				"[platform_grafana_chart_mismatch]",
				"lookup('ansible.builtin.password', '/dev/null'",
				"--server-side",
				"upgrade --install",
				"--labels swallow-managed=true",
				"--wait --timeout",
				"(lookup('ansible.builtin.template', 'values.yaml.j2') | from_yaml)",
				"/api/search?type=dash-db",
				"{'grafanaUrl': k0s_platform_grafana_url | trim}",
			},
		},
		{
			path: filepath.Join(role, "templates", "values.yaml.j2"),
			required: []string{
				"existingSecret: {{ k0s_platform_grafana_admin_secret }}",
				"envFromSecrets:",
				"type: NodePort",
				"nodePort: {{ k0s_platform_grafana_node_port | int }}",
				"persistence:\n  enabled: false",
				"testFramework:\n  enabled: false",
				"sha: {{ k0s_platform_grafana_image.sha256 }}",
				"disable_login_form: true",
				"org_role: Viewer",
				"grafana_provisioning_datasources",
				"dashboardsConfigMaps:",
			},
			// The values reach helm's release history, so neither secret may be in them.
			forbidden: []string{"datasource_token", "ansible.builtin.password", "adminPassword"},
		},
		{
			path:     filepath.Join(role, "tasks", "firewall.yml"),
			required: []string{"--query-port=", "--add-port=", "['--permanent']"},
			// A reload flushes the rules kube-proxy and kube-router program.
			forbidden: []string{"--reload"},
		},
		{
			path:     "uninstall-kubernetes.yml",
			required: []string{"/usr/local/lib/swallow/platform-grafana", "/usr/local/lib/swallow/helm"},
		},
	}
	for _, tc := range cases {
		content, err := os.ReadFile(shippedFile(t, tc.path))
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}
		for _, required := range tc.required {
			if !strings.Contains(string(content), required) {
				t.Errorf("%s does not contain %q", tc.path, required)
			}
		}
		for _, forbidden := range tc.forbidden {
			if strings.Contains(string(content), forbidden) {
				t.Errorf("%s contains %q", tc.path, forbidden)
			}
		}
	}

	// Every task that touches the token or the generated password hides its arguments and output.
	tasks, err := os.ReadFile(shippedFile(t, filepath.Join(role, "tasks", "main.yml")))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Create the admin Secret with a generated password", "Apply the datasource token Secret"} {
		start := strings.Index(string(tasks), "- name: "+name)
		if start < 0 {
			t.Errorf("tasks/main.yml has no task %q", name)
			continue
		}
		task := string(tasks[start:])
		if next := strings.Index(task[1:], "\n- name: "); next >= 0 {
			task = task[:next+1]
		}
		if !strings.Contains(task, "no_log: true") {
			t.Errorf("task %q does not set no_log", name)
		}
	}
}

// The bundled chart is the pinned grafana-community release, its archive is the checksum the role
// verifies, and the Grafana image is the release the Grafana Managed Software installs, pinned by
// digest.
func TestKubernetesPlatformGrafanaShipsThePinnedChart(t *testing.T) {
	defaults := readRoleDefaults(t, "k0s_platform_grafana")
	version, _ := defaults["k0s_platform_grafana_chart_version"].(string)
	archive := filepath.Join(roleDir(t, "k0s_platform_grafana"), "files", "grafana-"+version+".tgz")
	content, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("the pinned chart %s is not bundled: %v", version, err)
	}
	sum := sha256.Sum256(content)
	if got := hex.EncodeToString(sum[:]); got != defaults["k0s_platform_grafana_chart_sha256"] {
		t.Errorf("%s SHA-256 = %s, want k0s_platform_grafana_chart_sha256 %v", archive, got, defaults["k0s_platform_grafana_chart_sha256"])
	}
	chart := readChartMetadata(t, archive)
	if chart.Name != "grafana" || chart.Version != version {
		t.Errorf("Chart.yaml = %+v, want grafana %s", chart, version)
	}
	image, _ := defaults["k0s_platform_grafana_image"].(map[string]any)
	if image["tag"] != softwaredomain.DefaultGrafanaVersion {
		t.Errorf("image tag %v, want the Grafana Managed Software release %s", image["tag"], softwaredomain.DefaultGrafanaVersion)
	}
	if major := strings.Split(softwaredomain.DefaultGrafanaVersion, ".")[0]; !strings.HasPrefix(chart.AppVersion, major+".") {
		t.Errorf("chart appVersion %s is not Grafana %s", chart.AppVersion, major)
	}
	if digest, _ := image["sha256"].(string); len(digest) != 64 {
		t.Errorf("image digest %q is not a SHA-256", digest)
	}
}

// chartMetadata is the part of a chart's Chart.yaml the pin is checked against.
type chartMetadata struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	AppVersion string `yaml:"appVersion"`
}

// readChartMetadata reads <chart>/Chart.yaml from a packaged chart archive.
func readChartMetadata(t *testing.T, archive string) chartMetadata {
	t.Helper()
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	unzipped, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	entries := tar.NewReader(unzipped)
	for {
		header, err := entries.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if parts := strings.Split(header.Name, "/"); len(parts) != 2 || parts[1] != "Chart.yaml" {
			continue
		}
		raw, err := io.ReadAll(entries)
		if err != nil {
			t.Fatal(err)
		}
		var chart chartMetadata
		if err := yaml.Unmarshal(raw, &chart); err != nil {
			t.Fatalf("%s Chart.yaml: %v", archive, err)
		}
		return chart
	}
	t.Fatalf("%s has no Chart.yaml", archive)
	return chartMetadata{}
}
