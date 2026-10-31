package tests

import (
	"bufio"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The api-server layout (docs/development/architecture-spec.md, Target Layout) keeps the core free
// of adapters, modules, and vendor SDKs, keeps adapters and modules from reaching into each other,
// and keeps shared helpers beneath everything. These tests check the imports of every non-test
// source file, so a dependency that breaks the layout fails go test rather than waiting for
// review. Test files are exempt: a test may wire real adapters to exercise a boundary.

// componentRoot is the api-server Go module, relative to this package.
const componentRoot = ".."

// internalAreas are the only directories allowed directly under internal/.
var internalAreas = []string{"adapters", "app", "core", "shared"}

// coreForbiddenVendors are SDK import prefixes the core must not depend on; their types belong to
// the store, executor, and host access adapters.
var coreForbiddenVendors = []string{"go.mongodb.org/", "go.temporal.io/", "golang.org/x/crypto/ssh"}

// under reports whether path is prefix or lies beneath it.
func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// segments returns the first n slash-separated segments of path, or "" when it has fewer.
func segments(path string, n int) string {
	parts := strings.Split(path, "/")
	if len(parts) < n {
		return ""
	}
	return strings.Join(parts[:n], "/")
}

// layoutRule returns the layout rule an import breaks, or "" when it is allowed. dir is the
// importing package's directory relative to the component root; imported is the import path with
// the module prefix removed when it is one of this module's packages (local reports which).
func layoutRule(dir, imported string, local bool) string {
	switch {
	case under(dir, "internal/core"):
		if !local {
			for _, vendor := range coreForbiddenVendors {
				if strings.HasPrefix(imported, vendor) {
					return "core must not import vendor SDK " + vendor
				}
			}
			if strings.HasPrefix(imported, "github.com/gofiber/") && segments(dir, 4) != "" &&
				strings.Split(dir, "/")[3] != "delivery" {
				return "core may import Fiber only in a delivery package"
			}
			return ""
		}
		for _, outer := range []string{"internal/adapters", "internal/app", "modules", "config", "bootstrap", "cmd"} {
			if under(imported, outer) {
				return "core must not import " + outer
			}
		}
		if under(imported, "internal/core") && segments(dir, 4) != "" && segments(imported, 4) != "" {
			from, to := strings.Split(dir, "/")[3], strings.Split(imported, "/")[3]
			if from == "domain" && (to == "application" || to == "delivery") {
				return "a domain package must not import " + to
			}
			if from == "application" && to == "delivery" {
				return "an application package must not import delivery"
			}
		}
	case under(dir, "internal/adapters"):
		if !local {
			return ""
		}
		if own := segments(dir, 4); under(imported, "internal/adapters") && !under(imported, own) {
			return "an adapter must not import another adapter"
		}
		for _, outer := range []string{"internal/app", "modules"} {
			if under(imported, outer) {
				return "an adapter must not import " + outer
			}
		}
	case under(dir, "modules"):
		if !local {
			return ""
		}
		if under(imported, "internal/core") || under(imported, "internal/shared") || under(imported, segments(dir, 3)) {
			return ""
		}
		return "a module may import only the core, shared helpers, and its own packages"
	case under(dir, "internal/shared"):
		if !local {
			return ""
		}
		for _, outer := range []string{"internal/core", "internal/adapters", "internal/app", "modules"} {
			if under(imported, outer) {
				return "shared helpers must not import " + outer
			}
		}
	}
	return ""
}

// modulePath reads the Go module path from the component's go.mod.
func modulePath(t *testing.T) string {
	t.Helper()
	file, err := os.Open(filepath.Join(componentRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if path, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), "module "); ok {
			return strings.TrimSpace(path)
		}
	}
	t.Fatal("go.mod declares no module path")
	return ""
}

// Each rule refuses the dependency it names and still allows the dependencies the layout needs.
func TestLayoutRuleRefusesEachForbiddenDirection(t *testing.T) {
	for _, tc := range []struct {
		name, dir, imported string
		local, forbidden    bool
	}{
		{"core to adapter", "internal/core/server/application", "internal/adapters/bmc/redfish", true, true},
		{"core to module", "internal/core/platform/application", "modules/platform/platformapi", true, true},
		{"core to composition root", "internal/core/workflow/domain", "internal/app", true, true},
		{"core to config", "internal/core/site/application", "config", true, true},
		{"core to MongoDB", "internal/core/site/domain", "go.mongodb.org/mongo-driver/bson", false, true},
		{"core to Temporal", "internal/core/workflow/application", "go.temporal.io/sdk/client", false, true},
		{"core to SSH", "internal/core/sshkey/application", "golang.org/x/crypto/ssh", false, true},
		{"Fiber outside delivery", "internal/core/site/application", "github.com/gofiber/fiber/v2", false, true},
		{"domain to application", "internal/core/site/domain", "internal/core/server/application", true, true},
		{"application to delivery", "internal/core/site/application", "internal/core/site/delivery", true, true},
		{"adapter to another adapter", "internal/adapters/hypervisor/libvirt", "internal/adapters/hostaccess/sshaccess", true, true},
		{"adapter to module", "internal/adapters/store/mongodb/platform", "modules/platform/platformapi", true, true},
		{"module to adapter", "modules/platform/platformapi", "internal/adapters/store/mongodb", true, true},
		{"module to another module", "modules/software/docker-ce/dockerengine", "modules/platform/platformapi", true, true},
		{"shared to core", "internal/shared/apierror", "internal/core/site/domain", true, true},

		{"Fiber in delivery", "internal/core/site/delivery", "github.com/gofiber/fiber/v2", false, false},
		{"application to another context's domain", "internal/core/platform/application", "internal/core/site/domain", true, false},
		{"core to shared", "internal/core/site/delivery", "internal/shared/apierror", true, false},
		{"store context to store client", "internal/adapters/store/mongodb/server", "internal/adapters/store/mongodb", true, false},
		{"adapter to core", "internal/adapters/osprovisioning/maas", "internal/core/provisioning/domain", true, false},
		{"adapter to SDK", "internal/adapters/store/mongodb/site", "go.mongodb.org/mongo-driver/mongo", false, false},
		{"module to core", "modules/platform/platformapi", "internal/core/platform/domain", true, false},
		{"module to its own package", "modules/software/docker-ce/dockerengine", "modules/software/docker-ce", true, false},
		{"composition root to anything", "internal/app", "internal/adapters/bmc/redfish", true, false},
	} {
		if rule := layoutRule(tc.dir, tc.imported, tc.local); (rule != "") != tc.forbidden {
			t.Errorf("%s: layoutRule(%q, %q) = %q, want forbidden=%v", tc.name, tc.dir, tc.imported, rule, tc.forbidden)
		}
	}
}

func TestLayoutInternalHoldsOnlyItsAreas(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(componentRoot, "internal"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		if entry.IsDir() {
			got = append(got, entry.Name())
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(internalAreas, ",") {
		t.Errorf("internal/ holds %v, want only %v: code belongs in core, an adapter, shared, or app", got, internalAreas)
	}
}

func TestLayoutImportDirections(t *testing.T) {
	module := modulePath(t)
	fset := token.NewFileSet()
	scanned := map[string]int{}
	err := filepath.WalkDir(componentRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != componentRoot && (name == "testdata" || name == "bin" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(componentRoot, path)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		scanned[segments(dir, 2)]++
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			local := under(imported, module)
			if local {
				imported = strings.TrimPrefix(strings.TrimPrefix(imported, module), "/")
			}
			if rule := layoutRule(dir, imported, local); rule != "" {
				t.Errorf("%s imports %q: %s", filepath.ToSlash(rel), imported, rule)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, area := range []string{"internal/core", "internal/adapters", "internal/shared", "modules/platform", "modules/software"} {
		if scanned[area] == 0 {
			t.Errorf("scanned no Go source under %s; is componentRoot right?", area)
		}
	}
}
