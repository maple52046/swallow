package infra

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

func TestManifestCatalogAllowsOnlyRegisteredFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ping.yml"), []byte("---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifest, []byte(`{
		"schemaVersion": 1,
		"playbooks": [{"name": "ping", "path": "ping.yml"}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog, err := LoadManifestCatalog(manifest, root)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if path, err := catalog.Resolve("ping"); err != nil || path != "ping.yml" {
		t.Fatalf("resolve registered playbook: path=%q err=%v", path, err)
	}
	if _, err := catalog.Resolve("../arbitrary.yml"); !errors.Is(err, operationdomain.ErrPlaybookNotAllowed) {
		t.Fatalf("unregistered path must be refused, got %v", err)
	}
}

// The shipped manifest must reference real files: LoadManifestCatalog stats each path at
// startup, so a manifest entry with no playbook behind it would fail the API on boot.
// This guards the exporter playbooks in particular against being registered but missing.
func TestShippedManifestResolvesExporterPlaybooks(t *testing.T) {
	// Manifest lives at automation/manifest.json; its paths are relative to the
	// playbook bundle root automation/playbooks (see config defaults).
	automation := filepath.Join("..", "..", "..", "automation")
	manifest := filepath.Join(automation, "manifest.json")
	projectRoot := filepath.Join(automation, "playbooks")

	catalog, err := LoadManifestCatalog(manifest, projectRoot)
	if err != nil {
		t.Fatalf("load shipped manifest: %v", err)
	}
	for _, name := range []string{
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
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(root, "runtime")
	artifactRoot := filepath.Join(root, "artifacts")
	runner := NewLocalRunner(command, root, runtimeRoot, artifactRoot)

	operation := &operationdomain.ExecutionOperation{
		ID: "operation-1", SiteID: "site-1", TargetServerIDs: []string{"server-1"},
		Execution: operationdomain.Execution{RunID: "run-1", Playbook: "ping"},
		ExtraVars: map[string]any{"safe": true},
	}
	_, err := runner.Run(context.Background(), operationdomain.RunnerInput{
		Operation: operation,
		Configuration: &operationdomain.AutomationConfiguration{
			SSHUser: "ubuntu", SSHPort: 22, KnownHosts: "host ssh-ed25519 AAAA",
		},
		Credential: operationdomain.AutomationCredential{
			SSHPrivateKey: "private-key", BecomePassword: "become-secret",
		},
		Inventory: map[string]any{
			"_meta": map[string]any{"hostvars": map[string]any{
				"server-1": map[string]any{"ansible_host": "192.0.2.1"},
			}},
		},
		PlaybookPath: "ping.yml",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	entries, err := os.ReadDir(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("ephemeral run directory was not removed: %v", entries)
	}
}
