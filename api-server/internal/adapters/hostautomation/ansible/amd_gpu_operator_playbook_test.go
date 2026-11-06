package ansible

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

// TestAMDGPUOperatorPlaybookHonoursTheTrustedContract pins the parts of the GPU Operator playbook
// that the Go launcher and decision 062 depend on: every trusted variable is consumed, the managed
// nodes come from the operator's Node Feature Discovery and never from a node list, the play
// order converges the amdgpu loader and DRA links between the cluster and apply plays, and the
// safety rules (no KMM, exact-link removal, workload refusal, a post-boot driver load that k0s is
// ordered after) stay in place. The metrics exporter runs on the host network, where the Site
// Prometheus scrapes it, and every managed node's exporter must answer.
func TestAMDGPUOperatorPlaybookHonoursTheTrustedContract(t *testing.T) {
	role := filepath.Join("roles", "amd_gpu_operator")
	cases := []struct {
		path      string
		required  []string
		forbidden []string
	}{
		{
			path: "configure-amd-gpu-operator.yml",
			required: []string{
				"swallow_amd_gpu_operator_state == 'present'",
				"hostvars[swallow_amd_gpu_operator_controller_id].amd_gpu_operator_desired_nodes",
				"swallow_amd_gpu_operator_controller_id in ansible_play_hosts_all",
				"amd_gpu_operator_node_name: \"{{ ansible_hostname }}\"",
				"tasks_from: host_preflight",
				"tasks_from: cluster",
				"tasks_from: host_driver",
				"tasks_from: host_paths",
				"tasks_from: apply",
			},
			forbidden: []string{"swallow_amd_gpu_operator_node_ids"},
		},
		{
			path: filepath.Join(role, "tasks", "cluster.yml"),
			required: []string{
				"[gpu_operator_unmanaged]",
				"[dra_unavailable]",
				"[gpu_workloads_present]",
				"[amd_gpu_not_detected]",
				"amd_gpu_operator_nfd_gpu_labels",
				"amd_gpu_operator_nfd_evaluated_annotation",
				"-l swallow-managed=true",
				"--labels swallow-managed=true",
				"swallow_amd_gpu_operator_version",
				"swallow_amd_gpu_operator_allocation_mode",
				// helm is the shared pinned binary, never one this role downloads itself.
				"name: helm",
				"amd_gpu_operator_helm: \"{{ helm_binary }}\"",
				"Turn the previous allocation mode off first",
			},
			forbidden: []string{"get.helm.sh"},
		},
		{
			path: filepath.Join("roles", "helm", "tasks", "main.yml"),
			required: []string{
				"url: \"{{ swallow_local_repository.files }}{{ helm_archives[helm_arch].path }}\"",
				"checksum: \"sha256:{{ helm_archives[helm_arch].sha256 }}\"",
				"helm_binary: \"{{ helm_home }}/{{ helm_version }}/helm\"",
			},
		},
		{
			path: filepath.Join(role, "tasks", "host_preflight.yml"),
			required: []string{
				"[amdgpu_driver_missing]", "/sbin/modinfo, -F, filename, amdgpu",
				"[metrics_exporter_port_conflict]", "amd_gpu_operator_metrics_conflicting_unit",
			},
		},
		{
			path: filepath.Join(role, "tasks", "host_metrics.yml"),
			required: []string{
				"http://{{ ansible_host }}:{{ amd_gpu_operator_metrics_exporter_port }}/metrics",
				"[gpu_operator_not_ready]",
			},
		},
		{
			path: filepath.Join(role, "tasks", "host_driver.yml"),
			required: []string{
				"name: amdgpu_loader",
				"'present' if amd_gpu_operator_managed | bool else 'absent'",
				"amdgpu_loader_unit: \"{{ amd_gpu_operator_driver_unit }}\"",
				"amdgpu_loader_dependents: \"{{ amd_gpu_operator_k0s_units }}\"",
				// k0s also runs non-GPU work, so a driver failure only orders it.
				"amdgpu_loader_strict: false",
			},
		},
		{
			path: filepath.Join("roles", "amdgpu_loader", "tasks", "main.yml"),
			required: []string{
				"After={{ amdgpu_loader_unit }}",
				"'Requires' if amdgpu_loader_strict | bool else 'Wants'",
				"enabled: false",
				"amdgpu_loader_options_file",
			},
		},
		{
			path: filepath.Join("roles", "amdgpu_loader", "tasks", "apply.yml"),
			required: []string{
				"[amdgpu_option_unknown]",
				"[amdgpu_driver_missing]",
				"[amdgpu_option_not_applied]",
				"/dev/kfd",
				"reload.yml",
			},
			// Loading again (unload or reboot) lives only in reload.yml, behind its guards.
			forbidden: []string{"ansible.builtin.reboot", "modprobe -r", "rmmod"},
		},
		{
			path: filepath.Join("roles", "amdgpu_loader", "tasks", "reload.yml"),
			required: []string{
				"--property=RequiredBy",
				"amdgpu_loader_unload_timeout",
				"[amdgpu_reload_blocked]",
				"memoryBackedRoot",
				"ansible.builtin.reboot",
			},
			forbidden: []string{"rmmod"},
		},
		{
			path: filepath.Join("roles", "amdgpu_loader", "templates", "amdgpu-load.service.j2"),
			required: []string{
				"After=network-online.target {{ amdgpu_loader_ssh_unit }}",
				"ExecStart=/sbin/modprobe amdgpu",
				"TimeoutStartSec={{ amdgpu_loader_timeout }}",
			},
		},
		{
			path: filepath.Join(role, "tasks", "host_paths.yml"),
			required: []string{
				"swallow_amd_gpu_operator_allocation_mode == 'dra'",
				"item.stat.lnk_source == item.item.target",
				"amd_gpu_operator_dra_common_cdi",
			},
		},
		{
			path: filepath.Join(role, "tasks", "apply.yml"),
			required: []string{
				"[gpu_operator_not_ready]",
				"observedGeneration",
				"'^amd\\.com/'",
				"'gpu.amd.com'",
				"swallow_amd_gpu_operator_node_labeller",
			},
		},
		{
			path: filepath.Join(role, "templates", "deviceconfig.yaml.j2"),
			required: []string{
				"swallow_amd_gpu_operator_images",
				"swallow_amd_gpu_operator_resource_naming",
				"swallow_amd_gpu_operator_metrics_exporter",
				"hostNetwork: true",
				"port: {{ amd_gpu_operator_metrics_exporter_port }}",
				"name: {{ amd_gpu_operator_metrics_config }}",
				"enable: false",
			},
		},
		{
			path: filepath.Join(role, "templates", "metrics-config.yaml.j2"),
			required: []string{
				"'MetricsFieldPrefix': amd_gpu_operator_metrics_prefix",
				"'ProfilerMetrics': {'all': false}",
				"config.json: |",
			},
		},
		{
			path:     filepath.Join(role, "templates", "values.yaml.j2"),
			required: []string{"kmm:\n  enabled: false", "remediation:\n  enabled: false", "install: false"},
			// NFD keeps the chart's defaults: it has to see every node to find the GPU nodes.
			forbidden: []string{"nodeSelector"},
		},
		{
			path:     filepath.Join(role, "defaults", "main.yml"),
			required: []string{"feature.node.kubernetes.io/amd-gpu", "feature.node.kubernetes.io/amd-vgpu"},
		},
		{
			path: "uninstall-kubernetes.yml",
			required: []string{
				"/var/lib/kubelet/plugins_registry", "item.stat.lnk_source == item.item.target",
				"/etc/systemd/system/swallow-amdgpu-load.service",
				"/etc/modprobe.d/swallow-amdgpu.conf",
				"/var/run/cdi/k8s.gpu.amd.com-gpu_common.yaml",
			},
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
}

// The operator's Device Metrics Exporter serves the same metric names, on the same port, as the
// standalone and Slurm exporter, so one dashboard bundle and one scrape target shape serve all three
// (decision 062). Its port default is the one the software domain scrapes at, and the launcher
// passes the platform domain's, which equals it.
func TestGPUOperatorMetricsExporterMatchesTheStandaloneExporter(t *testing.T) {
	operator := readRoleDefaults(t, "amd_gpu_operator")
	standalone, err := os.ReadFile(filepath.Join(roleDir(t, "device_metrics_exporter"), "templates", "config.json.j2"))
	if err != nil {
		t.Fatal(err)
	}
	prefix, _ := operator["amd_gpu_operator_metrics_prefix"].(string)
	if prefix == "" || !strings.Contains(string(standalone), "'MetricsFieldPrefix': '"+prefix+"'") {
		t.Errorf("operator metric prefix %q is not the standalone exporter's", prefix)
	}
	port := fmt.Sprint(softwaredomain.DeviceMetricsExporterPort)
	if got := fmt.Sprint(operator["amd_gpu_operator_metrics_exporter_port"]); !strings.Contains(got, "default("+port+")") {
		t.Errorf("amd_gpu_operator_metrics_exporter_port = %s, want the default %s", got, port)
	}
	if platformdomain.DeviceMetricsExporterPort != softwaredomain.DeviceMetricsExporterPort {
		t.Errorf("platform domain port %d differs from the software domain's %d",
			platformdomain.DeviceMetricsExporterPort, softwaredomain.DeviceMetricsExporterPort)
	}
}
