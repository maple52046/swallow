package ansible

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func readRoleDefaults(t *testing.T, role string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(roleDir(t, role), "defaults", "main.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var defaults map[string]any
	if err := yaml.Unmarshal(content, &defaults); err != nil {
		t.Fatalf("%s defaults: %v", role, err)
	}
	return defaults
}

// omnistat-query on a compute node reads the Site Prometheus through the Platform Grafana's
// read-only datasource proxy, which reaches it through swallow's gateway and already serves the
// same data to anonymous viewers; the proxy UID must be the one the grafana role provisions.
func TestOmnistatQueryReadsPrometheusThroughGrafana(t *testing.T) {
	template, err := os.ReadFile(filepath.Join(roleDir(t, "omnistat"), "templates", "omnistat.conf.j2"))
	if err != nil {
		t.Fatal(err)
	}
	var url string
	for _, line := range strings.Split(string(template), "\n") {
		if strings.HasPrefix(line, "prometheus_url = ") {
			url = strings.TrimPrefix(line, "prometheus_url = ")
		}
	}
	want := "http://{{ omnistat_observability_address }}:{{ omnistat_grafana_port }}/api/datasources/proxy/uid/{{ omnistat_grafana_datasource_uid }}"
	if url != want {
		t.Errorf("omnistat-query prometheus_url = %q, want Grafana's datasource proxy %q", url, want)
	}

	omnistat, provisioning := readRoleDefaults(t, "omnistat"), readRoleDefaults(t, "grafana_provisioning")
	if omnistat["omnistat_grafana_datasource_uid"] != provisioning["grafana_provisioning_datasource_uid"] {
		t.Errorf("omnistat datasource UID %v, grafana provisions %v; they must match",
			omnistat["omnistat_grafana_datasource_uid"], provisioning["grafana_provisioning_datasource_uid"])
	}
}

// The Site Prometheus scrapes the exporter, so it admits the node itself and the installation's
// scrape addresses, and the playbook proves each exporter on its own node instead of waiting on a
// Prometheus; it still reports the Grafana URL omnistat-query reads.
func TestOmnistatAdmitsTheSitePrometheus(t *testing.T) {
	template, err := os.ReadFile(filepath.Join(roleDir(t, "omnistat"), "templates", "omnistat.conf.j2"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "allowed_ips = {{ (['127.0.0.1'] + omnistat_scrape_addresses) | unique | join(', ') }}"; !strings.Contains(string(template), want) {
		t.Errorf("omnistat.conf.j2 does not render allowed_ips as %q", want)
	}
	if got := readRoleDefaults(t, "omnistat")["omnistat_scrape_addresses"]; got != "{{ swallow_omnistat_scrape_addresses | default([]) }}" {
		t.Errorf("omnistat_scrape_addresses = %v, want the trusted scrape addresses", got)
	}
	tasks, err := os.ReadFile(filepath.Join(roleDir(t, "omnistat"), "tasks", "main.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tasks), `url: "http://127.0.0.1:{{ omnistat_port }}/metrics"`) {
		t.Error("the omnistat role does not check its own exporter on the loopback address")
	}
	playbook := slurmPlaybookFile(t, "configure-omnistat.yml")
	for _, gone := range []string{"/api/v1/targets", "prometheus/endpoint", "swallow_omnistat_prometheus_port"} {
		if strings.Contains(playbook, gone) {
			t.Errorf("configure-omnistat.yml still contains %q", gone)
		}
	}
	if !strings.Contains(playbook, "{'grafanaUrl': omnistat_grafana_url}") {
		t.Error("configure-omnistat.yml no longer reports the Grafana URL")
	}
}

// Decision 064: every patch swallow applies to Omnistat exists, explains itself in a header, and is
// listed in the development documentation with how to tell whether a newer version still needs
// it, so whoever bumps the version can decide what to carry over.
func TestOmnistatPatchesAreDocumented(t *testing.T) {
	sources, _ := readRoleDefaults(t, "omnistat")["omnistat_sources"].(map[string]any)
	if len(sources) == 0 {
		t.Fatal("omnistat_sources is empty")
	}
	guide, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "docs", "development", "platform-deployment.md"))
	if err != nil {
		t.Fatal(err)
	}
	for version, raw := range sources {
		source, _ := raw.(map[string]any)
		patches, _ := source["patches"].([]any)
		if len(patches) == 0 {
			t.Errorf("Omnistat %s lists no patches; drop the key instead of leaving it empty, or list them", version)
		}
		for _, item := range patches {
			name := item.(string)
			if !strings.HasPrefix(name, "omnistat-"+version+"-") || !strings.HasSuffix(name, ".patch") {
				t.Errorf("patch %q is not named omnistat-%s-<topic>.patch", name, version)
			}
			content, err := os.ReadFile(filepath.Join(roleDir(t, "omnistat"), "files", name))
			if err != nil {
				t.Errorf("patch %s: %v", name, err)
				continue
			}
			if !strings.HasPrefix(string(content), "# ") || !strings.Contains(string(content), "Re-check upstream") {
				t.Errorf("patch %s has no header explaining it and when to re-check upstream", name)
			}
			if !strings.Contains(string(guide), "`"+name+"`") {
				t.Errorf("patch %s is not listed in docs/development/platform-deployment.md", name)
			}
		}
	}
}

// A patch added later must reach nodes that already carry the earlier ones, so each patch is
// checked (reverse dry run) and applied on its own rather than as one bundle.
func TestOmnistatAppliesEachPatchOnItsOwn(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(roleDir(t, "omnistat"), "tasks", "install.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var tasks []map[string]any
	if err := yaml.Unmarshal(content, &tasks); err != nil {
		t.Fatal(err)
	}
	byName := map[string]map[string]any{}
	for _, task := range tasks {
		byName[task["name"].(string)] = task
	}
	check, apply := byName["Check which patches the source already carries"], byName["Apply the patches the source does not carry yet"]
	if check == nil || apply == nil {
		t.Fatalf("patch tasks missing: check %v, apply %v", check != nil, apply != nil)
	}
	if loop, _ := check["loop"].(string); !strings.Contains(loop, ".patches") || !strings.Contains(strings.Join(argvOf(check), " "), "--reverse") {
		t.Errorf("check task = %v, want a reverse dry run per listed patch", check)
	}
	if loop, _ := apply["loop"].(string); !strings.Contains(loop, "omnistat_patch_applied.results") || apply["when"] != "item.rc != 0" {
		t.Errorf("apply task = %v, want each patch applied only when its own check failed", apply)
	}
}

// The exporter's configuration turns the counters on, with the profile Omnistat's collector reads,
// only while the node collects them; off, it has no rocprofiler sections at all. The rendered file is
// read with Python's configparser, as Omnistat reads it.
func TestOmnistatConfigurationCollectsTheCountersOnlyWhenOn(t *testing.T) {
	t.Parallel()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	run := newRoleRun(t, "omnistat", false)
	template, err := filepath.Abs(filepath.Join(roleDir(t, "omnistat"), "templates", "omnistat.conf.j2"))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	writeScratch(t, run.dir, "play.yml", "- hosts: all\n  gather_facts: false\n  tasks:\n"+
		"    - ansible.builtin.template:\n        src: "+template+"\n        dest: \"{{ rendered }}\"\n")
	read := `import configparser, json, sys
config = configparser.ConfigParser()
config.read(sys.argv[1])
profile = config.get("omnistat.collectors.rocprofiler", "profile", fallback="")
print(json.dumps({"enabled": config.get("omnistat.collectors", "enable_rocprofiler"), "profile": profile,
    "mode": config.get("omnistat.collectors.rocprofiler." + profile, "sampling_mode", fallback=""),
    "counters": json.loads(config.get("omnistat.collectors.rocprofiler." + profile, "counters", fallback="[]"))}))`
	for _, tc := range []struct {
		enabled bool
		want    string
	}{
		{enabled: true, want: `{"enabled": "True", "profile": "valubusy", "mode": "constant", "counters": ["VALUBusy"]}`},
		{enabled: false, want: `{"enabled": "False", "profile": "", "mode": "", "counters": []}`},
	} {
		rendered := filepath.Join(out, fmt.Sprintf("omnistat-%v.conf", tc.enabled))
		vars := map[string]any{
			"rendered": rendered, "omnistat_rocprofiler_enabled": tc.enabled, "omnistat_counter_profile": "valubusy",
			"omnistat_counters": []string{"VALUBusy"}, "omnistat_port": 8001, "omnistat_gpu": true, "omnistat_rocm_path": "/opt/rocm",
			"omnistat_scrape_addresses": []string{"10.0.0.2"}, "omnistat_job_file": "/tmp/omni_rmsjobinfo",
			"omnistat_observability_address": "10.0.0.1", "omnistat_grafana_port": 3000,
			"omnistat_grafana_datasource_uid": "prometheus", "omnistat_system_name": "slurm",
		}
		if output, _, failed := run.run(vars); failed != 0 {
			t.Fatalf("rendering failed:\n%s", output)
		}
		got, err := exec.Command(python, "-c", read, rendered).Output()
		if err != nil {
			t.Fatalf("read the rendered configuration: %v", err)
		}
		if strings.TrimSpace(string(got)) != tc.want {
			t.Errorf("with the counters on = %v the exporter reads %s, want %s", tc.enabled, got, tc.want)
		}
	}
}

// The counters reach the exporter and the Platform: CAP_PERFMON comes only with them, an exporter
// that does not serve them in time is restarted without them (exporter_failed) instead of failing
// the run, the run's result reports each node, and uninstalling removes the drop-in.
func TestOmnistatHardwareCountersReachTheExporterAndTheResult(t *testing.T) {
	read := func(rel string) string {
		raw, err := os.ReadFile(filepath.Join(roleDir(t, "omnistat"), rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	dropin := read(filepath.Join("templates", "perfmon.conf.j2"))
	for _, want := range []string{"[Service]\n", "AmbientCapabilities=CAP_PERFMON\n", "CapabilityBoundingSet=CAP_PERFMON\n"} {
		if !strings.Contains(dropin, want) {
			t.Errorf("perfmon.conf.j2 lacks %q", want)
		}
	}
	service := read(filepath.Join("tasks", "service.yml"))
	for _, want := range []string{"when: omnistat_rocprofiler_enabled | bool", "state: absent", "when: not omnistat_rocprofiler_enabled | bool"} {
		if !strings.Contains(service, want) {
			t.Errorf("service.yml lacks %q: the drop-in must exist exactly while the node collects the counters", want)
		}
	}
	var tasks []map[string]any
	readYAML(t, filepath.Join(roleDir(t, "omnistat"), "tasks", "main.yml"), &tasks)
	includes, fellBack := 0, false
	for _, task := range tasks {
		if task["ansible.builtin.include_tasks"] == "service.yml" {
			includes++
		}
		if block, ok := task["block"].([]any); ok && task["when"] == "omnistat_counters_fell_back | bool" {
			fellBack = strings.Contains(fmt.Sprint(block), "exporter_failed") && strings.Contains(fmt.Sprint(block), "service.yml")
		}
		if name, _ := task["name"].(string); name == "Wait for the exporter to serve the hardware counters" && task["ignore_errors"] != true {
			t.Errorf("waiting for the counters must not fail the run: %v", task)
		}
	}
	if includes != 1 || !fellBack {
		t.Errorf("main.yml configures the exporter %d times directly and falls back %v, want once and a fallback that records exporter_failed and configures it again", includes, fellBack)
	}
	if playbook := slurmPlaybookFile(t, "configure-omnistat.yml"); !strings.Contains(playbook, "{'hardwareCounters': {'nodes': omnistat_hardware_counter_nodes}}") ||
		!strings.Contains(playbook, "omnistat_hardware_counters_result") {
		t.Error("configure-omnistat.yml does not report each node's hardware counters in the run's result")
	}
	if uninstall := slurmPlaybookFile(t, "uninstall-slurm.yml"); !strings.Contains(uninstall, "/etc/systemd/system/omnistat.service.d/10-swallow-perfmon.conf") ||
		!strings.Contains(fmt.Sprint(readRoleDefaults(t, "omnistat")["omnistat_perfmon_dropin"]), "omnistat.service.d/10-swallow-perfmon.conf") {
		t.Error("uninstall-slurm.yml does not remove the exporter's CAP_PERFMON drop-in")
	}
}

func argvOf(task map[string]any) []string {
	command, _ := task["ansible.builtin.command"].(map[string]any)
	raw, _ := command["argv"].([]any)
	argv := make([]string, len(raw))
	for i, item := range raw {
		argv[i], _ = item.(string)
	}
	return argv
}
