package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Slurm HA path depends on the deploy-slurm playbook provisioning a shared StateSaveLocation
// itself: a managed NFS export on a selected state server, mounted on every controller with a
// fail-closed mount guard before slurmctld starts. These assertions pin the trusted-var
// expressions and role wiring so a refactor cannot silently drop the shared-state mechanism and
// reintroduce the "every slurmctld DOWN, heartbeat file missing" HA failure.
func TestSlurmPlaybookProvisionsSharedControllerState(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "automation", "playbooks")
	cases := []struct {
		path     string
		required []string
	}{
		{
			path: "deploy-slurm.yml",
			required: []string{
				"swallow_slurm_state_server_id",
				"swallow_slurm_high_availability",
				"slurm_state_mount_options: rw,_netdev,hard,timeo=600,retrans=2,vers=3",
				"slurm_controller_state_mode",
				"slurm_state_source",
				"hosts: slurm_state_server",
				"role: slurm_state_server",
				"role: slurm_controller_state",
			},
		},
		{
			path: filepath.Join("roles", "slurm_controller_state", "tasks", "main.yml"),
			required: []string{
				"slurm_controller_state_mode == 'shared'",
				"slurm_controller_state_mode == 'local'",
				"create: true",
				"RequiresMountsFor={{ slurm_state_save_location }}",
				"ConditionPathIsMountPoint={{ slurm_state_save_location }}",
				"argv: [mountpoint, -q, \"{{ slurm_state_save_location }}\"]",
			},
		},
		{
			path: filepath.Join("roles", "slurm_state_server", "tasks", "main.yml"),
			required: []string{
				"nfs-kernel-server",
				"groups['slurm_controller']",
				"root_squash",
				"findmnt, --noheadings, --output, FSTYPE",
				"slurm_state_export_filesystem.stdout | trim == 'overlay'",
				"slurm_state_ephemeral_tmpfs_size",
			},
		},
		{
			// slurm_config must not recreate/chown the state dir: slurm_controller_state owns it
			// (a root_squash NFS mount in HA), so this documented invariant is pinned here.
			path:     filepath.Join("roles", "slurm_config", "tasks", "main.yml"),
			required: []string{"StateSaveLocation is intentionally absent"},
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

	// The shared state must be exported and mounted before slurmctld starts, so the play order
	// is load-bearing: state server, then controller state, then the controller-config play.
	deploy, err := os.ReadFile(filepath.Join(root, "deploy-slurm.yml"))
	if err != nil {
		t.Fatalf("read deploy-slurm.yml: %v", err)
	}
	content := string(deploy)
	stateServerPlay := strings.Index(content, "hosts: slurm_state_server")
	controllerStatePlay := strings.Index(content, "role: slurm_controller_state")
	// The primary controller must be configured and started before the backups so it becomes
	// active and writes the shared heartbeat first; a backup started first hangs the deploy.
	primaryConfigPlay := strings.Index(content, "Configure the primary controller and start slurmctld first")
	backupConfigPlay := strings.Index(content, "Configure and start the backup controllers")
	if stateServerPlay == -1 || controllerStatePlay == -1 || primaryConfigPlay == -1 || backupConfigPlay == -1 {
		t.Fatal("deploy-slurm.yml must define the state-server, controller-state, primary-config, and backup-config plays")
	}
	if !(stateServerPlay < controllerStatePlay && controllerStatePlay < primaryConfigPlay && primaryConfigPlay < backupConfigPlay) {
		t.Errorf("play order must be state server -> controller state -> primary config -> backup config, got %d, %d, %d, %d",
			stateServerPlay, controllerStatePlay, primaryConfigPlay, backupConfigPlay)
	}
	// The backup play must exclude the primary so it is not reconfigured/restarted out of order.
	if !strings.Contains(content, "hosts: slurm_controller:!slurm_primary") {
		t.Error("the backup controller play must target slurm_controller:!slurm_primary")
	}
}

// The login role and workload shared storage (self-hosted from the login node, or external)
// depend on the playbook wiring the login/workload facts, groups, and plays. These assertions
// pin that wiring so a refactor cannot silently drop login submission or workload mounts.
func TestSlurmPlaybookWiresLoginAndWorkloadStorage(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "automation", "playbooks")
	cases := []struct {
		path     string
		required []string
	}{
		{
			path: "deploy-slurm.yml",
			required: []string{
				"swallow_slurm_login_ids",
				"swallow_slurm_workload_enabled",
				"swallow_slurm_workload_source",
				"if (swallow_slurm_workload_mode | default('')) == 'self-hosted'",
				"hosts: slurm_login",
				"role: slurm_login",
				"hosts: slurm_workload_server",
				"role: slurm_workload_storage_server",
				"hosts: slurm_workload_client",
				"role: slurm_workload_storage_client",
			},
		},
		{
			path:     filepath.Join("roles", "slurm_login", "tasks", "main.yml"),
			required: []string{"SACKD_OPTIONS", "slurm_login_config_mode == 'configless'"},
		},
		{
			path: filepath.Join("roles", "slurm_workload_storage_server", "tasks", "main.yml"),
			required: []string{
				"nfs-kernel-server",
				"groups['slurm_workload_client']",
				"root_squash",
				"findmnt, --noheadings, --output, FSTYPE",
				"slurm_workload_export_filesystem.stdout | trim == 'overlay'",
				"slurm_workload_ephemeral_tmpfs_size",
			},
		},
		{
			path: filepath.Join("roles", "slurm_workload_storage_client", "tasks", "main.yml"),
			required: []string{
				"nfs-common",
				"create: true",
				"argv: [mountpoint, -q, \"{{ slurm_workload_mount_path }}\"]",
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

	// The workload client play must run after the compute play (the workload filesystem is for
	// jobs, and mounting on all nodes is a post-cluster add-on), and the login play after the
	// controllers are up so configless discovery succeeds.
	deploy, err := os.ReadFile(filepath.Join(root, "deploy-slurm.yml"))
	if err != nil {
		t.Fatalf("read deploy-slurm.yml: %v", err)
	}
	content := string(deploy)
	computePlay := strings.Index(content, "Configure compute nodes and start slurmd")
	loginPlay := strings.Index(content, "hosts: slurm_login")
	workloadClientPlay := strings.Index(content, "hosts: slurm_workload_client")
	if computePlay == -1 || loginPlay == -1 || workloadClientPlay == -1 {
		t.Fatal("deploy-slurm.yml must define the compute, login, and workload-client plays")
	}
	if !(computePlay < loginPlay && loginPlay < workloadClientPlay) {
		t.Errorf("play order must be compute -> login -> workload client, got %d, %d, %d",
			computePlay, loginPlay, workloadClientPlay)
	}
	// Dynamic group names can collide with provider-owned inventory groups. Both workload roles
	// must independently honor the frozen enabled flag, so choosing no shared storage cannot
	// install NFS packages, create /shared, or write fstab even if such a group already exists.
	if !strings.Contains(content, "when: (swallow_slurm_workload_enabled | default(false) | bool) and (slurm_is_workload_server | bool)") {
		t.Error("the workload server role must be guarded by the enabled intent and selected server")
	}
	if !strings.Contains(content, "when: swallow_slurm_workload_enabled | default(false) | bool\n") {
		t.Error("the workload client role must be guarded by the enabled intent")
	}
}

// Login is a capability, not an exclusive node type. A controller+login node must retain the
// authoritative slurm.conf rendered by slurm_config; only pure login clients converge to
// configless discovery. Otherwise the later login play removes the primary controller's config
// and systemd skips slurmrestd because its packaged ConditionPathExists is no longer satisfied.
func TestSlurmLoginPreservesControllerConfiguration(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "automation", "playbooks", "roles", "slurm_login", "tasks", "main.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read slurm login tasks: %v", err)
	}
	tasks := string(raw)

	guardedTasks := []string{
		"Remove any local slurm.conf on a configless login host",
		"Configure sackd controller discovery",
		"Enable and start sackd on configless login hosts",
		"Configure direct configless discovery when sackd is not used",
	}
	for _, name := range guardedTasks {
		start := strings.Index(tasks, "- name: "+name)
		if start == -1 {
			t.Errorf("slurm login role missing task %q", name)
			continue
		}
		end := strings.Index(tasks[start+1:], "\n- name: ")
		section := tasks[start:]
		if end >= 0 {
			section = tasks[start : start+1+end]
		}
		if !strings.Contains(section, "not (slurm_is_controller | bool)") {
			t.Errorf("task %q must exclude controller+login nodes", name)
		}
	}

	stop := strings.Index(tasks, "- name: Stop sackd when it is not the selected login configuration manager")
	if stop == -1 || !strings.Contains(tasks[stop:], "(slurm_is_controller | bool) or") {
		t.Error("controller+login nodes must stop stale sackd and use their local configuration")
	}
}

func TestSlurmPackageInstallsRetryAfterFreshBootLockRaces(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "automation", "playbooks", "roles")
	cases := map[string]string{
		filepath.Join("slurm_munge", "tasks", "main.yml"):                   "until: slurm_munge_package is succeeded",
		filepath.Join("slurm_controller_state", "tasks", "main.yml"):        "until: slurm_controller_state_nfs_package is succeeded",
		filepath.Join("slurm_state_server", "tasks", "main.yml"):            "until: slurm_state_server_nfs_package is succeeded",
		filepath.Join("slurm_workload_storage_client", "tasks", "main.yml"): "until: slurm_workload_client_nfs_package is succeeded",
		filepath.Join("slurm_workload_storage_server", "tasks", "main.yml"): "until: slurm_workload_server_nfs_package is succeeded",
	}

	for path, retryCondition := range cases {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatalf("read playbook artifact: %v", err)
			}
			content := string(raw)
			if !strings.Contains(content, retryCondition) {
				t.Errorf("%s missing %q", path, retryCondition)
			}
			if !strings.Contains(content, "retries: 120") || !strings.Contains(content, "delay: 5") {
				t.Errorf("%s must bound apt retry to ten minutes", path)
			}
		})
	}
}
