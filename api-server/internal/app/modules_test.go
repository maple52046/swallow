package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// modulesRoot is api-server/modules relative to this package.
var modulesRoot = filepath.Join("..", "..", "modules")

// nonModuleDirs are directories under modules/<kind>/ that hold Go code shared by modules rather
// than a module; each is to be split into its modules.
var nonModuleDirs = map[string]bool{"platform/platformapi": true}

func testRegistry(t *testing.T) *moduledomain.Registry {
	t.Helper()
	registry, err := moduleRegistry()
	if err != nil {
		t.Fatalf("moduleRegistry() error = %v", err)
	}
	return registry
}

// allowlist reads the release's playbook allowlist as name -> path.
func allowlist(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(modulesRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Playbooks []struct{ Name, Path string } `json:"playbooks"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, entry := range manifest.Playbooks {
		entries[entry.Name] = entry.Path
	}
	return entries
}

// Every module directory declares itself and is compiled in, and every compiled module has a
// directory: a plugin cannot ship content the registry does not know about.
func TestEveryModuleDirectoryIsACompiledModule(t *testing.T) {
	registered := map[string]bool{}
	for _, module := range testRegistry(t).Modules() {
		registered[module.Manifest.Ref()] = true
		if _, err := os.Stat(filepath.Join(modulesRoot, module.Manifest.Ref(), "module.json")); err != nil {
			t.Errorf("module %s has no module.json in its directory: %v", module.Manifest.Ref(), err)
		}
	}
	dirs, err := filepath.Glob(filepath.Join(modulesRoot, "*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		ref := filepath.ToSlash(strings.TrimPrefix(dir, modulesRoot+string(filepath.Separator)))
		if strings.HasPrefix(ref, "shared/") || nonModuleDirs[ref] {
			continue
		}
		if !registered[ref] {
			t.Errorf("modules/%s is not a compiled module: add module.json and list it in compiledModules", ref)
		}
	}
}

// The allowlist and the manifests agree: a module's playbook is registered inside its own
// directory, and every registered playbook belongs to the module whose directory holds it, or to
// shared/.
func TestModulePlaybooksAgreeWithTheAllowlist(t *testing.T) {
	entries := allowlist(t)
	owners := map[string]string{}
	for _, module := range testRegistry(t).Modules() {
		ref := module.Manifest.Ref()
		for _, name := range module.Manifest.Playbooks {
			path, ok := entries[name]
			if !ok {
				t.Errorf("module %s runs playbook %q, which the allowlist does not register", ref, name)
				continue
			}
			if !strings.HasPrefix(path, ref+"/playbooks/") {
				t.Errorf("module %s playbook %q is at %s, outside the module", ref, name, path)
			}
			owners[name] = ref
		}
	}
	for name, path := range entries {
		if strings.HasPrefix(path, "shared/") {
			continue
		}
		if _, ok := owners[name]; !ok {
			t.Errorf("allowlisted playbook %q (%s) belongs to no module's manifest", name, path)
		}
	}
}

// Every Workflow kind is planned by the core or by exactly one module, and every kind a module
// claims is one the Workflow model accepts.
func TestEveryWorkflowKindHasOneOwner(t *testing.T) {
	registry := testRegistry(t)
	core := map[string]bool{}
	for _, kind := range coreWorkflowKinds {
		core[kind] = true
	}
	valid := map[string]bool{}
	for _, kind := range workflowdomain.ValidWorkflowKinds {
		valid[string(kind)] = true
		_, moduleOwned := registry.ForWorkflowKind(string(kind))
		if core[string(kind)] == moduleOwned {
			t.Errorf("Workflow kind %q: core-owned %v, module-owned %v; want exactly one owner", kind, core[string(kind)], moduleOwned)
		}
	}
	for _, module := range registry.Modules() {
		for _, kind := range module.Manifest.WorkflowKinds {
			if !valid[kind] {
				t.Errorf("module %s claims Workflow kind %q, which the Workflow model does not accept", module.Manifest.Ref(), kind)
			}
		}
	}
}

// Every completion type in the plan catalog is declared by the core or by exactly one module, and
// every type a module claims has a class in the catalog.
func TestEveryCompletionTypeHasOneOwner(t *testing.T) {
	registry := testRegistry(t)
	core := map[string]bool{}
	for _, completion := range coreCompletions {
		core[completion] = true
	}
	classes := completionClasses()
	var types []string
	for completion := range classes {
		types = append(types, completion)
	}
	sort.Strings(types)
	for _, completion := range types {
		_, moduleOwned := registry.ForCompletion(completion)
		if core[completion] == moduleOwned {
			t.Errorf("completion %q: core-owned %v, module-owned %v; want exactly one owner", completion, core[completion], moduleOwned)
		}
	}
	for _, module := range registry.Modules() {
		for _, completion := range module.Manifest.Completions {
			if _, ok := classes[completion]; !ok {
				t.Errorf("module %s claims completion %q, which has no class in the plan catalog", module.Manifest.Ref(), completion)
			}
		}
	}
}
