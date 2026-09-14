package infra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestK0sPlaybookSupportsFlexibleTopologiesAndLegacyRetries(t *testing.T) {
	root := filepath.Join("..", "..", "..", "automation", "playbooks")
	cases := []struct {
		path     string
		required []string
	}{
		{
			path: "deploy-kubernetes.yml",
			required: []string{
				"swallow_k0s_workload_controller_ids | default([])",
				"k0s_runs_workloads",
				"role: k0s_host_preflight",
			},
		},
		{
			path: filepath.Join("roles", "k0s_host_preflight", "tasks", "main.yml"),
			required: []string{
				"/sys/fs/cgroup/cgroup.controllers",
				"xt_REDIRECT",
				"iptable_nat",
				"/usr/local/bin/k0s",
				"sysinfo",
				"k0s_sysinfo.stdout_lines",
				"swallow_k0s_ephemeral_root | default(false) | bool",
				"ephemeral-snapshotter.toml",
				"snapshotter = \"native\"",
				"use_local_image_pull = true",
			},
		},
		{
			path: filepath.Join("roles", "k0s_config", "templates", "k0s.yaml.j2"),
			required: []string{
				"swallow_k0s_api_address | default(hostvars[swallow_k0s_initial_controller_id].ansible_host, true)",
				"swallow_k0s_high_availability | default(true)",
				"{% if k0s_ha %}",
			},
		},
		{
			path:     filepath.Join("roles", "k0s_controller_bootstrap", "tasks", "main.yml"),
			required: []string{"--enable-worker --no-taints", "when: k0s_runs_workloads | bool"},
		},
		{
			path:     filepath.Join("roles", "k0s_cluster_credential", "tasks", "main.yml"),
			required: []string{"swallow_k0s_api_address | default(hostvars[swallow_k0s_initial_controller_id].ansible_host, true)"},
		},
		{
			path:     filepath.Join("roles", "k0s_verify", "tasks", "main.yml"),
			required: []string{"swallow_k0s_workload_ids | default(swallow_k0s_worker_ids)"},
		},
		{
			path: filepath.Join("roles", "k0s_worker_join", "tasks", "main.yml"),
			required: []string{
				"Ensure /etc/k0s exists",
				"path: /etc/k0s",
				"state: directory",
				"dest: /etc/k0s/join-token",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, tc.path))
			if err != nil {
				t.Fatalf("read playbook artifact: %v", err)
			}
			content := string(raw)
			for _, required := range tc.required {
				if !strings.Contains(content, required) {
					t.Errorf("%s missing %q", tc.path, required)
				}
			}
		})
	}

	bootstrap, err := os.ReadFile(filepath.Join(root, "roles", "k0s_controller_bootstrap", "tasks", "main.yml"))
	if err != nil {
		t.Fatalf("read bootstrap role: %v", err)
	}
	if strings.Contains(string(bootstrap), "--single") {
		t.Error("standalone deployment must remain scale-out capable and not use --single")
	}

	workerJoin, err := os.ReadFile(filepath.Join(root, "roles", "k0s_worker_join", "tasks", "main.yml"))
	if err != nil {
		t.Fatalf("read worker join role: %v", err)
	}
	workerJoinContent := string(workerJoin)
	directoryTask := strings.Index(workerJoinContent, "Ensure /etc/k0s exists")
	tokenTask := strings.Index(workerJoinContent, "Write the join token")
	if directoryTask == -1 || tokenTask == -1 || directoryTask > tokenTask {
		t.Error("worker join role must create /etc/k0s before writing its join token")
	}
}
