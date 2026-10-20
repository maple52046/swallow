package infra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAMDGPUOperatorPlaybookHonoursTheTrustedContract pins the parts of the GPU Operator playbook
// that the Go launcher and decision 062 depend on: every trusted variable is consumed, the managed
// nodes come from the operator's Node Feature Discovery and never from a node list, the play
// order converges the amdgpu loader and DRA links between the cluster and apply plays, and the
// safety rules (no KMM, no host networking for the metrics exporter, exact-link removal, workload
// refusal, a post-boot driver load that k0s is ordered after) stay in place.
func TestAMDGPUOperatorPlaybookHonoursTheTrustedContract(t *testing.T) {
	root := filepath.Join("..", "..", "..", "automation", "playbooks")
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
				"amd_gpu_operator_helm_checksums[amd_gpu_operator_arch]",
				"Turn the previous allocation mode off first",
			},
		},
		{
			path:     filepath.Join(role, "tasks", "host_preflight.yml"),
			required: []string{"[amdgpu_driver_missing]", "/sbin/modinfo, -F, filename, amdgpu"},
		},
		{
			path: filepath.Join(role, "tasks", "host_driver.yml"),
			required: []string{
				"[amdgpu_driver_missing]",
				"/dev/kfd",
				"amd_gpu_operator_driver_unit",
				"After={{ amd_gpu_operator_driver_unit }}",
				"enabled: false",
			},
		},
		{
			path: filepath.Join(role, "templates", "amdgpu-load.service.j2"),
			required: []string{
				"After=network-online.target {{ amd_gpu_operator_ssh_unit }}",
				"ExecStart=/sbin/modprobe amdgpu",
				"TimeoutStartSec={{ amd_gpu_operator_driver_load_timeout }}",
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
				"hostNetwork: false",
				"enable: false",
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
				"/var/run/cdi/k8s.gpu.amd.com-gpu_common.yaml",
			},
		},
	}
	for _, tc := range cases {
		content, err := os.ReadFile(filepath.Join(root, tc.path))
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
