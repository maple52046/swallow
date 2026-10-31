package ansible

import (
	"os"
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

// Prometheus listens on the observability Server's loopback only, so omnistat-query on a compute
// node reads it through Grafana's read-only datasource proxy, which already serves the same data
// to anonymous viewers; the proxy UID must be the one the grafana role provisions.
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

	omnistat, grafana := readRoleDefaults(t, "omnistat"), readRoleDefaults(t, "grafana")
	if omnistat["omnistat_grafana_datasource_uid"] != grafana["grafana_datasource_uid"] {
		t.Errorf("omnistat datasource UID %v, grafana provisions %v; they must match",
			omnistat["omnistat_grafana_datasource_uid"], grafana["grafana_datasource_uid"])
	}
}

// Decision 064: every patch Swallow applies to Omnistat exists, explains itself in a header, and is
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
	content, err := os.ReadFile(filepath.Join(roleDir(t, "omnistat"), "tasks", "main.yml"))
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

func argvOf(task map[string]any) []string {
	command, _ := task["ansible.builtin.command"].(map[string]any)
	raw, _ := command["argv"].([]any)
	argv := make([]string, len(raw))
	for i, item := range raw {
		argv[i], _ = item.(string)
	}
	return argv
}
