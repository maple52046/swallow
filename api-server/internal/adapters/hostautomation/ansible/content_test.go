package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// modulesRoot is the shipped host automation content (api-server/modules) relative to this
// package, so the content tests read exactly what the release bundles.
var modulesRoot = filepath.Join("..", "..", "..", "..", "modules")

// shippedManifest is the release's playbook manifest; its paths are relative to modulesRoot.
var shippedManifest = filepath.Join(modulesRoot, "manifest.json")

// moduleDirs lists every directory that holds a module's playbooks/ and roles/: modules/shared
// and modules/<kind>/<id>.
func moduleDirs(t *testing.T) []string {
	t.Helper()
	dirs := []string{filepath.Join(modulesRoot, sharedModule)}
	kinds, err := filepath.Glob(filepath.Join(modulesRoot, "*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range kinds {
		if strings.HasPrefix(dir, filepath.Join(modulesRoot, sharedModule)+string(filepath.Separator)) {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// roleDir returns the directory of the shipped role name in whichever module owns it, so a test
// names a role without hard-coding that ownership. Exactly one module may define a role.
func roleDir(t *testing.T, name string) string {
	t.Helper()
	var found []string
	for _, module := range moduleDirs(t) {
		dir := filepath.Join(module, "roles", name)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			found = append(found, dir)
		}
	}
	if len(found) != 1 {
		t.Fatalf("role %q is defined in %d modules (%v), want exactly one", name, len(found), found)
	}
	return found[0]
}

// shippedFile maps a content path written as the playbook bundle addresses it, either a
// playbook file name (deploy-slurm.yml) or roles/<role>/<file>, onto the module that ships it.
func shippedFile(t *testing.T, rel string) string {
	t.Helper()
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if parts[0] == "roles" && len(parts) > 2 {
		return filepath.Join(append([]string{roleDir(t, parts[1])}, parts[2:]...)...)
	}
	if len(parts) != 1 {
		t.Fatalf("shippedFile(%q): want a playbook file name or roles/<role>/<file>", rel)
	}
	var found []string
	for _, module := range moduleDirs(t) {
		path := filepath.Join(module, "playbooks", rel)
		if _, err := os.Stat(path); err == nil {
			found = append(found, path)
		}
	}
	if len(found) != 1 {
		t.Fatalf("playbook %q is shipped by %d modules (%v), want exactly one", rel, len(found), found)
	}
	return found[0]
}
