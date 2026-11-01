package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// modulesRoot is api-server/modules relative to this package.
var modulesRoot = filepath.Join("..", "..", "modules")

// nonModuleDirs are directories under modules/<kind>/ that hold Go code shared by modules rather
// than a module; there are none.
var nonModuleDirs = map[string]bool{}

func testRegistry(t *testing.T) *moduledomain.Registry {
	t.Helper()
	registry, err := moduleRegistry()
	if err != nil {
		t.Fatalf("moduleRegistry() error = %v", err)
	}
	return registry
}

// testModules composes the compiled modules as a process does.
func testModules(t *testing.T) composedModules {
	t.Helper()
	modules, err := composeModules()
	if err != nil {
		t.Fatalf("composeModules() error = %v", err)
	}
	return modules
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

// Every compiled module's directory has an owner in the repository's CODEOWNERS, so review of a
// module change is assigned (core-and-extensions.md §10.1).
func TestEveryModuleHasACodeOwner(t *testing.T) {
	codeOwners, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "CODEOWNERS"))
	if err != nil {
		t.Fatalf("read CODEOWNERS: %v", err)
	}
	owned := map[string]bool{}
	for _, line := range strings.Split(string(codeOwners), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && !strings.HasPrefix(fields[0], "#") {
			owned[fields[0]] = true
		}
	}
	for _, module := range testRegistry(t).Modules() {
		if path := "/api-server/modules/" + module.Manifest.Ref() + "/"; !owned[path] {
			t.Errorf("CODEOWNERS has no owner for %s", path)
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

// Every Workflow kind the build accepts is planned by the core or by exactly one module, and every
// kind a module claims is one the build accepts.
func TestEveryWorkflowKindHasOneOwner(t *testing.T) {
	modules := testModules(t)
	core := map[string]bool{}
	for _, kind := range coreWorkflowKinds {
		core[kind] = true
	}
	for _, kind := range modules.workflowKinds.List() {
		_, moduleOwned := modules.registry.ForWorkflowKind(string(kind))
		if core[string(kind)] == moduleOwned {
			t.Errorf("Workflow kind %q: core-owned %v, module-owned %v; want exactly one owner", kind, core[string(kind)], moduleOwned)
		}
	}
	for _, module := range modules.registry.Modules() {
		for _, kind := range module.Manifest.WorkflowKinds {
			if !modules.workflowKinds.Valid(workflowdomain.WorkflowKind(kind)) {
				t.Errorf("module %s claims Workflow kind %q, which the build does not accept", module.Manifest.Ref(), kind)
			}
		}
	}
}

// Every Software module provides one catalog kind named by its id, whose Workflows require deployed
// targets, and the core keeps no kind of its own.
func TestEverySoftwareModuleProvidesItsKind(t *testing.T) {
	modules := testModules(t)
	var ids []string
	for _, module := range modules.registry.Modules() {
		if module.Manifest.Kind != moduledomain.KindSoftware {
			continue
		}
		ids = append(ids, module.Manifest.ID)
		kind := softwaredomain.Kind(module.Manifest.ID)
		if _, ok := modules.software.Lookup(kind); !ok {
			t.Errorf("software module %s is not in the catalog", module.Manifest.Ref())
		}
		for _, workflowKind := range []string{kind.InstallWorkflowKind(), kind.UninstallWorkflowKind()} {
			if got := modules.workflowKinds.RequiredProvisioningState(workflowdomain.WorkflowKind(workflowKind)); got != "deployed" {
				t.Errorf("%s requires %q, want deployed", workflowKind, got)
			}
		}
	}
	var kinds []string
	for _, entry := range modules.software.Entries() {
		kinds = append(kinds, string(entry.Kind))
	}
	if !slices.Equal(ids, kinds) {
		t.Errorf("catalog kinds = %v, want the Software modules %v", kinds, ids)
	}
}

// A Software module's spec schema reaches the catalog, so the core refuses a spec that breaks it
// before the module's own validation runs.
func TestSoftwareSpecSchemasAreChecked(t *testing.T) {
	modules := testModules(t)
	nfs, ok := modules.registry.Module(moduledomain.KindSoftware, "nfs")
	if !ok {
		t.Fatal("no software/nfs module")
	}
	if schema, err := nfs.Manifest.SpecSchema(); err != nil || schema == nil {
		t.Fatalf("software/nfs SpecSchema() = %v, %v; want its schema", schema, err)
	}
	err := modules.software.CheckSpec(softwaredomain.KindNFS, map[string]any{"exportPath": 7})
	if !errors.Is(err, softwaredomain.ErrSpecInvalid) || !strings.Contains(err.Error(), "spec.exportPath must be a string") {
		t.Errorf("CheckSpec(nfs, numeric exportPath) = %v, want ErrSpecInvalid naming spec.exportPath", err)
	}
	if err := modules.software.CheckSpec(softwaredomain.KindNFS, map[string]any{"exportPath": "/export/data"}); err != nil {
		t.Errorf("CheckSpec(nfs, valid) = %v", err)
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
