package infra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The software deployment feature (docs/development/software-deployment.md, decision 038) adds
// standalone Managed Software playbooks. LoadManifestCatalog stats each manifest path at startup,
// so a registered-but-missing file would fail the API on boot; this pins that the shipped
// manifest resolves every software playbook.
func TestShippedManifestResolvesSoftwarePlaybooks(t *testing.T) {
	automation := filepath.Join("..", "..", "..", "automation")
	catalog, err := LoadManifestCatalog(
		filepath.Join(automation, "manifest.json"),
		filepath.Join(automation, "playbooks"),
	)
	if err != nil {
		t.Fatalf("load shipped manifest: %v", err)
	}
	for _, name := range []string{
		"deploy-docker-ce", "uninstall-docker-ce",
		"deploy-podman", "uninstall-podman",
		"deploy-nfs", "uninstall-nfs",
	} {
		if _, err := catalog.Resolve(name); err != nil {
			t.Errorf("shipped manifest must register %q: %v", name, err)
		}
	}
}

// The NFS playbook must classify hosts by the trusted swallow_software_roles map and wire both
// the server and client roles, because server and client are variants of one software driven by
// per-serverId roles. These assertions keep a refactor from dropping the role classification and
// silently making every host a server (or a client).
func TestNFSPlaybookClassifiesRolesFromTrustedVars(t *testing.T) {
	root := filepath.Join("..", "..", "..", "automation", "playbooks")
	cases := []struct {
		path     string
		required []string
	}{
		{
			path: "deploy-nfs.yml",
			required: []string{
				"swallow_software_roles[inventory_hostname]",
				"nfs_is_server",
				"nfs_is_client",
				"key: \"nfs_server_{{ nfs_is_server | bool }}\"",
				"key: \"nfs_client_{{ nfs_is_client | bool }}\"",
				"hosts: nfs_server_True",
				"hosts: nfs_client_True",
				"role: nfs_server",
				"role: nfs_client",
			},
		},
		{
			path: filepath.Join("roles", "nfs_server", "tasks", "main.yml"),
			required: []string{
				"nfs-kernel-server",
				"nfs_server_state == 'present'",
				"nfs_server_state == 'absent'",
				"nfs_server_export_options",
			},
		},
		{
			path:     filepath.Join("roles", "nfs_server", "defaults", "main.yml"),
			required: []string{"root_squash"},
		},
		{
			path: filepath.Join("roles", "nfs_client", "tasks", "main.yml"),
			required: []string{
				"nfs-common",
				"nfs_client_state == 'present'",
				"nfs_client_state == 'absent'",
				"argv: [mountpoint, -q, \"{{ nfs_client_mount_path }}\"]",
			},
		},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(filepath.Join(root, tc.path))
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}
		content := string(raw)
		for _, required := range tc.required {
			if !strings.Contains(content, required) {
				t.Errorf("%s missing %q", tc.path, required)
			}
		}
	}
}

// The Docker Host Explorer dials the Engine API listener the docker_ce role converges from the
// trusted swallow_docker_enable_api / swallow_docker_api_port vars (decision 043). These assertions
// keep the playbooks wired to those vars, the listener tied to state=present, the stock fd://
// socket preserved, and uninstall closing what install opened.
func TestDockerCEPlaybooksConvergeEngineAPIListener(t *testing.T) {
	root := filepath.Join("..", "..", "..", "automation", "playbooks")
	cases := []struct {
		path     string
		required []string
	}{
		{
			path:     "deploy-docker-ce.yml",
			required: []string{"swallow_docker_enable_api", "swallow_docker_api_port", "role: docker_ce"},
		},
		{
			path:     "uninstall-docker-ce.yml",
			required: []string{"docker_ce_state: absent", "swallow_docker_api_port"},
		},
		{
			path:     filepath.Join("roles", "docker_ce", "tasks", "main.yml"),
			required: []string{"include_tasks: api.yml"},
		},
		{
			path: filepath.Join("roles", "docker_ce", "tasks", "api.yml"),
			required: []string{
				"docker_ce_state == 'present' and (docker_ce_api_enabled | bool)",
				"-H fd:// -H tcp://0.0.0.0:{{ docker_ce_api_port }}",
				"state: absent",
				"state: restarted",
				"firewall-cmd",
				"ansible.builtin.wait_for",
			},
		},
		{
			path:     filepath.Join("roles", "docker_ce", "defaults", "main.yml"),
			required: []string{"docker_ce_api_enabled: false", "docker_ce_api_port: 2375"},
		},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(filepath.Join(root, tc.path))
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}
		content := string(raw)
		for _, required := range tc.required {
			if !strings.Contains(content, required) {
				t.Errorf("%s missing %q", tc.path, required)
			}
		}
	}
}

// The container-runtime roles must carry symmetric present/absent branches so uninstall is a real
// removal path, not a no-op. Docker CE and Podman are mutually exclusive software kinds; each
// owns one install implementation shared by any future platform composition.
func TestContainerRuntimeRolesHaveInstallAndRemoveBranches(t *testing.T) {
	root := filepath.Join("..", "..", "..", "automation", "playbooks", "roles")
	cases := []struct {
		path     string
		required []string
	}{
		{
			path: filepath.Join("docker_ce", "tasks", "main.yml"),
			required: []string{
				"docker_ce_state == 'present'",
				"docker_ce_state == 'absent'",
				"name: docker",
				"download.docker.com",
			},
		},
		{
			path: filepath.Join("podman", "tasks", "main.yml"),
			required: []string{
				"podman_state == 'present'",
				"podman_state == 'absent'",
			},
		},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(filepath.Join(root, tc.path))
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}
		content := string(raw)
		for _, required := range tc.required {
			if !strings.Contains(content, required) {
				t.Errorf("%s missing %q", tc.path, required)
			}
		}
	}
}
