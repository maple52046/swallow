package ansible

import (
	"os"
	"path/filepath"
	"testing"
)

// The usesLocalRepository flag of a manifest entry is what makes acceptance require the Site's Local
// Repository; an entry without it, and a name the manifest does not list, do not.
func TestManifestCatalogReportsPlaybooksThatUseTheLocalRepository(t *testing.T) {
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
	body := `{"schemaVersion": 1, "playbooks": [
		{"name": "deploy", "path": "shared/playbooks/deploy.yml", "usesLocalRepository": true},
		{"name": "ping", "path": "shared/playbooks/ping.yml"}
	]}`
	if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadManifestCatalog(manifest, root)
	if err != nil {
		t.Fatalf("LoadManifestCatalog: %v", err)
	}
	for name, want := range map[string]bool{"deploy": true, "ping": false, "unlisted": false} {
		if got := catalog.UsesLocalRepository(name); got != want {
			t.Errorf("UsesLocalRepository(%q) = %v, want %v", name, got, want)
		}
	}
}
