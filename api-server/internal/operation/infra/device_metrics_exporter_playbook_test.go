package infra

import (
	"path/filepath"
	"strings"
	"testing"
)

// The Device Metrics Exporter is the Managed Software kind device-metrics-exporter (decision 068):
// one playbook installs it on a single Server and, with Slurm attribution, onto a Slurm Platform's
// GPU nodes. The former Slurm-only name stays registered for the same file so a Workflow accepted
// before the rename can still be retried.
func TestShippedManifestResolvesDeviceMetricsExporterPlaybooks(t *testing.T) {
	automation := filepath.Join("..", "..", "..", "automation")
	catalog, err := LoadManifestCatalog(filepath.Join(automation, "manifest.json"), filepath.Join(automation, "playbooks"))
	if err != nil {
		t.Fatalf("load shipped manifest: %v", err)
	}
	install, err := catalog.Resolve("deploy-device-metrics-exporter")
	if err != nil {
		t.Fatal(err)
	}
	if legacy, err := catalog.Resolve("configure-device-metrics-exporter"); err != nil || legacy != install {
		t.Errorf("configure-device-metrics-exporter = %q, %v; want the same playbook as %q", legacy, err, install)
	}
	if _, err := catalog.Resolve("uninstall-device-metrics-exporter"); err != nil {
		t.Error(err)
	}
}

// A standalone install has no compute ids, no observability Server, and no Slurm attribution, so
// every target runs the exporter, gets Swallow's amdgpu loader, and skips the Slurm scrape check;
// a run with compute ids but no attribution (accepted before decision 068) stays a Slurm run.
func TestDeviceMetricsExporterPlaybookServesStandaloneAndSlurm(t *testing.T) {
	for _, tc := range []struct {
		path     string
		required []string
	}{
		{
			path: "deploy-device-metrics-exporter.yml",
			required: []string{
				"swallow_device_metrics_exporter_compute_ids | default(ansible_play_hosts_all)",
				"swallow_device_metrics_exporter_observability_id | default('')",
				"tasks_from: preflight",
				"name: amdgpu_loader",
				"when: dme_job_attribution | trim == 'none'",
				"default('slurm' if swallow_device_metrics_exporter_compute_ids is defined else 'none', true)",
			},
		},
		{
			path: filepath.Join("roles", "device_metrics_exporter", "defaults", "main.yml"),
			required: []string{
				"default('slurm' if swallow_device_metrics_exporter_compute_ids is defined else 'none', true)",
				"device_metrics_exporter_slurm: \"{{ device_metrics_exporter_job_attribution == 'slurm' }}\"",
			},
		},
		{
			path:     filepath.Join("roles", "device_metrics_exporter", "tasks", "main.yml"),
			required: []string{"-enable-slurm-scl={{ device_metrics_exporter_slurm | string | lower }}", "include_tasks: preflight.yml"},
		},
		{
			path: filepath.Join("roles", "device_metrics_exporter", "templates", "config.json.j2"),
			// The order-preserving filter keeps the rendered config, and so the exporter, unchanged
			// across reruns.
			required: []string{"reject('in', device_metrics_exporter_job_labels) | list", "rejectattr('key', 'equalto', 'CustomLabels')"},
		},
	} {
		content := slurmPlaybookFile(t, tc.path)
		for _, want := range tc.required {
			if !strings.Contains(content, want) {
				t.Errorf("%s does not contain %q", tc.path, want)
			}
		}
	}
}

// Swallow's amdgpu loader is shared with the Slurm GPU role and the GPU Operator. A software
// uninstall removes it only when no unit still orders itself after it, and the Slurm uninstall
// removes the exporter through the same shared tasks.
func TestDeviceMetricsExporterUninstallKeepsASharedLoader(t *testing.T) {
	uninstall := slurmPlaybookFile(t, "uninstall-device-metrics-exporter.yml")
	for _, want := range []string{"tasks_from: uninstall", "patterns: 10-swallow-after-amdgpu.conf", "amdgpu_loader_state: absent", "when: dme_loader_dependents.matched == 0"} {
		if !strings.Contains(uninstall, want) {
			t.Errorf("uninstall-device-metrics-exporter.yml does not contain %q", want)
		}
	}
	shared := slurmPlaybookFile(t, "roles", "device_metrics_exporter", "tasks", "uninstall.yml")
	if strings.Contains(shared, "amdgpu_loader") && !strings.Contains(shared, "amdgpu loader is not touched here") {
		t.Error("the shared exporter removal must leave the amdgpu loader to its caller")
	}
	if slurm := slurmPlaybookFile(t, "uninstall-slurm.yml"); !strings.Contains(slurm, "name: device_metrics_exporter\n        tasks_from: uninstall") {
		t.Error("uninstall-slurm.yml does not remove the exporter through the shared tasks")
	}
}
