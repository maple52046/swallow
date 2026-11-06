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

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
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

// Every compiled module's artifact declaration is valid on its own, the declarations agree with each
// other (the registry builds), and the registry reports exactly the modules that declare something.
func TestCompiledModuleArtifactsAreValid(t *testing.T) {
	registry := testRegistry(t)
	declared := map[string]bool{}
	for _, entry := range registry.Artifacts() {
		declared[entry.ModuleID] = true
	}
	for _, module := range registry.Modules() {
		artifacts := module.Manifest.Artifacts
		if err := artifacts.Validate(); err != nil {
			t.Errorf("module %s artifacts: %v", module.Manifest.Ref(), err)
		}
		declares := len(artifacts.Sources)+len(artifacts.Files)+len(artifacts.Images)+len(artifacts.Python) > 0
		if declared[module.Manifest.ID] != declares {
			t.Errorf("Artifacts() lists module %s: %v, want %v", module.Manifest.Ref(), declared[module.Manifest.ID], declares)
		}
	}
}

// Every OS package source a compiled module declares reaches the Local Repository's declarations
// with its signing key, so a Site that fetches from the upstreams can verify each one; the shipped
// keys are the ones confirmed against their upstreams.
func TestComposedDeclarationsCarryTheSigningKeyOfEverySource(t *testing.T) {
	keys := map[string]localrepodomain.SigningKey{}
	for _, declaration := range testModules(t).artifacts {
		for _, source := range declaration.Sources {
			if source.Format != localrepodomain.FormatDeb && source.Format != localrepodomain.FormatRPM {
				continue
			}
			key, ok := declaration.SigningKeys[source.Name]
			if !ok || key.URL == "" || key.Fingerprint == "" {
				t.Errorf("module %s source %s has no signing key in its composed declaration (%+v)", declaration.ModuleID, source.Name, declaration.SigningKeys)
			}
			keys[source.Name] = key
		}
	}
	for name, want := range map[string]localrepodomain.SigningKey{
		"docker-ce": {URL: "https://download.docker.com/linux/ubuntu/gpg", Fingerprint: "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"},
		"grafana":   {URL: "https://apt.grafana.com/gpg.key", Fingerprint: "B53AE77BADB630A683046005963FA27710458545"},
	} {
		if got := keys[name]; got != want {
			t.Errorf("source %s signing key = %+v, want %+v", name, got, want)
		}
	}
}

// The AMD module declares every image a GPU Operator release pins, so a release added to the core's
// catalog cannot deploy images the Local Repository was never asked to fetch.
func TestAMDModuleDeclaresTheGPUOperatorImages(t *testing.T) {
	amd, ok := testRegistry(t).Module(moduledomain.KindAccelerator, "amd")
	if !ok {
		t.Fatal("no accelerator/amd module")
	}
	images := amd.Manifest.Artifacts.Images
	for _, release := range platformdomain.GPUOperatorReleases() {
		pinned := []string{release.Images.DevicePlugin, release.Images.NodeLabeller, release.Images.Utils, release.Images.MetricsExporter}
		for _, image := range release.DRADriverImages {
			pinned = append(pinned, image)
		}
		for _, image := range pinned {
			if !slices.Contains(images, image) {
				t.Errorf("GPU Operator %s pins %s, which accelerator/amd does not declare", release.Version, image)
			}
		}
	}
}

// The Slurm module declares exactly the Python pins Omnistat installs within, so the Local
// Repository fetches the same environment pip resolves on every node.
func TestSlurmModuleDeclaresTheOmnistatConstraints(t *testing.T) {
	slurm, ok := testRegistry(t).Module(moduledomain.KindPlatform, "slurm")
	if !ok {
		t.Fatal("no platform/slurm module")
	}
	data, err := os.ReadFile(filepath.Join(modulesRoot, "platform", "slurm", "roles", "omnistat", "files", "omnistat-1.15.0-constraints.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var pins []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			pins = append(pins, line)
		}
	}
	if got := slurm.Manifest.Artifacts.Python; !slices.Equal(got, pins) {
		t.Errorf("platform/slurm python = %v, want the Omnistat constraints %v", got, pins)
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
