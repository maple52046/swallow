package ansible

import (
	"os"
	"path/filepath"
	"testing"
)

// writeCatalogFixture writes the playbooks deploy and ping under a fresh root and a manifest of
// body, returning the manifest's path and the root.
func writeCatalogFixture(t *testing.T, body string) (string, string) {
	t.Helper()
	root := t.TempDir()
	for _, path := range []string{"shared/playbooks/deploy.yml", "shared/playbooks/ping.yml"} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return manifest, root
}

// The fetchesArtifacts flag of a manifest entry is what makes acceptance resolve where the Site's
// artifacts come from; an entry without it, and a name the manifest does not list, do not.
func TestManifestCatalogReportsPlaybooksThatFetchArtifacts(t *testing.T) {
	manifest, root := writeCatalogFixture(t, `{"schemaVersion": 1, "playbooks": [
		{"name": "deploy", "path": "shared/playbooks/deploy.yml", "fetchesArtifacts": true},
		{"name": "ping", "path": "shared/playbooks/ping.yml"}
	]}`)
	catalog, err := LoadManifestCatalog(manifest, root)
	if err != nil {
		t.Fatalf("LoadManifestCatalog: %v", err)
	}
	for name, want := range map[string]bool{"deploy": true, "ping": false, "unlisted": false} {
		if got := catalog.FetchesArtifacts(name); got != want {
			t.Errorf("FetchesArtifacts(%q) = %v, want %v", name, got, want)
		}
	}
}

// A key the manifest schema does not have, such as the retired usesLocalRepository flag, stops the
// load instead of leaving a fetching playbook without the trusted variable it reads.
func TestManifestCatalogRefusesUnknownKeys(t *testing.T) {
	for name, body := range map[string]string{
		"retired entry flag": `{"schemaVersion": 1, "playbooks": [
			{"name": "deploy", "path": "shared/playbooks/deploy.yml", "usesLocalRepository": true}]}`,
		"unknown top-level key": `{"schemaVersion": 1, "playbook": [], "playbooks": [
			{"name": "deploy", "path": "shared/playbooks/deploy.yml"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			manifest, root := writeCatalogFixture(t, body)
			if _, err := LoadManifestCatalog(manifest, root); err == nil {
				t.Errorf("LoadManifestCatalog(%s) = nil error, want the unknown key refused", body)
			}
		})
	}
}
