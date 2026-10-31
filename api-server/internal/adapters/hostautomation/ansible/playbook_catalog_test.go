package ansible

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// writeManifest writes a schemaVersion 1 manifest registering entries (name to path) and an empty
// playbook behind every path under root, and returns the manifest's path.
func writeManifest(t *testing.T, root string, entries map[string]string) string {
	t.Helper()
	var listed []string
	for name, path := range entries {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		listed = append(listed, fmt.Sprintf(`{"name": %q, "path": %q}`, name, path))
	}
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	body := `{"schemaVersion": 1, "playbooks": [` + strings.Join(listed, ",") + `]}`
	if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestManifestCatalogAllowsOnlyRegisteredFiles(t *testing.T) {
	root := t.TempDir()
	manifest := writeManifest(t, root, map[string]string{"ping": "shared/playbooks/ping.yml"})

	catalog, err := LoadManifestCatalog(manifest, root)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if path, err := catalog.Resolve("ping"); err != nil || path != "shared/playbooks/ping.yml" {
		t.Fatalf("resolve registered playbook: path=%q err=%v", path, err)
	}
	if _, err := catalog.Resolve("../arbitrary.yml"); !errors.Is(err, workflowdomain.ErrPlaybookNotAllowed) {
		t.Fatalf("unregistered path must be refused, got %v", err)
	}
}

// A manifest path must name the module that owns the playbook, because the module decides which
// roles the run may use; a playbook outside that layout is refused when the catalog loads.
func TestManifestCatalogRefusesPathsOutsideTheModuleLayout(t *testing.T) {
	for _, path := range []string{
		"ping.yml",
		"shared/ping.yml",
		"platform/slurm/ping.yml",
		"platform/slurm/roles/ping.yml",
		"shared/playbooks/nested/ping.yml",
		"shared/x/playbooks/ping.yml",
	} {
		t.Run(path, func(t *testing.T) {
			root := t.TempDir()
			manifest := writeManifest(t, root, map[string]string{"ping": path})
			if _, err := LoadManifestCatalog(manifest, root); !errors.Is(err, workflowdomain.ErrPlaybookNotAllowed) {
				t.Errorf("LoadManifestCatalog(%q) error = %v, want ErrPlaybookNotAllowed", path, err)
			}
		})
	}
}

// A run sees its own module's roles and the shared roles, never another module's.
func TestRolesPathConfinesARunToItsModule(t *testing.T) {
	root := filepath.FromSlash("/opt/modules")
	shared := filepath.Join(root, "shared", "roles")
	for _, tc := range []struct{ playbook, want string }{
		{"platform/slurm/playbooks/deploy-slurm.yml", filepath.Join(root, "platform", "slurm", "roles") + string(filepath.ListSeparator) + shared},
		{"software/nfs/playbooks/deploy-nfs.yml", filepath.Join(root, "software", "nfs", "roles") + string(filepath.ListSeparator) + shared},
		{"shared/playbooks/configure-ntp.yml", shared},
	} {
		got, err := rolesPath(root, tc.playbook)
		if err != nil || got != tc.want {
			t.Errorf("rolesPath(%q) = %q, %v; want %q", tc.playbook, got, err, tc.want)
		}
	}
	if _, err := rolesPath(root, "deploy-slurm.yml"); !errors.Is(err, workflowdomain.ErrPlaybookNotAllowed) {
		t.Errorf("rolesPath(flat path) error = %v, want ErrPlaybookNotAllowed", err)
	}
}

// The shipped manifest must reference real files: LoadManifestCatalog stats each path at
// startup, so a manifest entry with no playbook behind it would fail the API on boot.
// This guards the exporter playbooks in particular against being registered but missing.
func TestShippedManifestResolvesExporterPlaybooks(t *testing.T) {
	catalog, err := LoadManifestCatalog(shippedManifest, modulesRoot)
	if err != nil {
		t.Fatalf("load shipped manifest: %v", err)
	}
	for _, name := range []string{
		"uninstall-kubernetes",
		"install-exporters", "uninstall-exporters",
		"deploy-k8s-exporters", "remove-k8s-exporters",
	} {
		if _, err := catalog.Resolve(name); err != nil {
			t.Errorf("shipped manifest must register %q: %v", name, err)
		}
	}
}

func TestLocalRunnerDeletesEphemeralCredentials(t *testing.T) {
	root := t.TempDir()
	command := filepath.Join(root, "runner")
	rolesPathFile := filepath.Join(root, "roles-path")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$ANSIBLE_ROLES_PATH\" > %q\n", rolesPathFile)
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(root, "runtime")
	artifactRoot := filepath.Join(root, "artifacts")
	runner := NewLocalRunner(command, root, runtimeRoot, artifactRoot)

	operation := &workflowdomain.ExecutionOperation{
		ID: "operation-1", SiteID: "site-1", TargetServerIDs: []string{"server-1"},
		Execution: workflowdomain.Execution{RunID: "run-1", Playbook: "ping"},
		ExtraVars: map[string]any{"safe": true},
	}
	_, err := runner.Run(context.Background(), workflowdomain.RunnerInput{
		Operation: operation,
		Configuration: &workflowdomain.AutomationConfiguration{
			SSHUser: "ubuntu", SSHPort: 22, KnownHosts: "host ssh-ed25519 AAAA",
		},
		Credential: workflowdomain.AutomationCredential{
			SSHPrivateKey: "private-key", BecomePassword: "become-secret",
		},
		Inventory: map[string]any{
			"_meta": map[string]any{"hostvars": map[string]any{
				"server-1": map[string]any{"ansible_host": "192.0.2.1"},
			}},
		},
		PlaybookPath: "platform/slurm/playbooks/ping.yml",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	gotRolesPath, err := os.ReadFile(rolesPathFile)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := rolesPath(root, "platform/slurm/playbooks/ping.yml"); string(gotRolesPath) != want {
		t.Errorf("runner ANSIBLE_ROLES_PATH = %q, want %q", gotRolesPath, want)
	}
	entries, err := os.ReadDir(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("ephemeral run directory was not removed: %v", entries)
	}
}

func TestUninstallKubernetesPlaybookPreservesHostLifecycle(t *testing.T) {
	path := shippedFile(t, "uninstall-kubernetes.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read uninstall playbook: %v", err)
	}
	playbook := string(raw)
	for _, required := range []string{
		"cmd: /usr/local/bin/k0s reset",
		"path: /usr/local/bin/k0s",
		"- /etc/k0s",
		"path: /var/lib/k0s",
		"daemon_reload: true",
	} {
		if !strings.Contains(playbook, required) {
			t.Errorf("uninstall playbook missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"ansible.builtin.reboot",
		"name: conntrack",
		"state: absent\\n      name: conntrack",
		"failed_when: false",
	} {
		if strings.Contains(playbook, forbidden) {
			t.Errorf("uninstall playbook must preserve host lifecycle; found %q", forbidden)
		}
	}
}
