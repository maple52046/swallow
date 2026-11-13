package tests

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The core must hold no vendor, product, Platform, or GPU-vendor knowledge
// (docs/development/core-and-extensions.md §2, §3.6). The import rules keep vendor SDKs out, but a
// vendor's name, constant, or wire format can still sit in an identifier, a string, or a struct
// tag. Existing violations are migrating out, so this is a ratchet: each core file's count of
// vendor terms is recorded in coreVendorTermsBaseline and may only fall (adding a new platform or
// vendor name is the one exception, core-and-extensions.md §3.6). A count that rises fails;
// a count that falls also fails until the baseline is rewritten with UPDATE_VENDOR_BASELINE=1, so a
// removed violation cannot come back unnoticed. Comments are not counted.

// coreVendorTermsBaseline records the per-file counts; it is rewritten, never edited by hand.
var coreVendorTermsBaseline = filepath.Join("testdata", "core_vendor_terms.json")

// coreVendorTerms are lower-case substrings that name a provider, product, Platform, or GPU vendor.
// "amd" is matched only as a word part that is not "amd64", which is a CPU architecture.
var coreVendorTerms = []string{
	"maas", "ansible", "temporal", "bson", "mongo", "prometheus", "promql", "alertmanager", "redfish",
	"libvirt", "virsh", "qemu", "ipxe", "grub", "k0s", "kubernetes", "kubectl", "helm", "slurm",
	"rocm", "amdgpu", "nvidia", "dcgm", "docker", "podman", "grafana", "omnistat", "ldap", "sssd",
}

// countVendorTerms returns how many identifiers, string literals, and struct tags in file name a
// vendor term.
func countVendorTerms(t *testing.T, path string) int {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Ident:
			if namesVendor(n.Name) {
				count++
			}
		case *ast.BasicLit:
			if n.Kind == token.STRING && namesVendor(n.Value) {
				count++
			}
		case *ast.Field:
			if n.Tag != nil && namesVendor(n.Tag.Value) {
				count++
			}
		}
		return true
	})
	return count
}

// namesVendor reports whether text names a vendor term.
func namesVendor(text string) bool {
	lower := strings.ToLower(text)
	for _, term := range coreVendorTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return strings.Contains(strings.ReplaceAll(lower, "amd64", ""), "amd")
}

// currentCoreVendorTerms counts every non-test Go file under internal/core, keyed by its path
// relative to the component root; files without vendor terms are omitted.
func currentCoreVendorTerms(t *testing.T) map[string]int {
	t.Helper()
	counts := map[string]int{}
	root := filepath.Join(componentRoot, "internal", "core")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if count := countVendorTerms(t, path); count > 0 {
			rel, err := filepath.Rel(componentRoot, path)
			if err != nil {
				return err
			}
			counts[filepath.ToSlash(rel)] = count
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

// TestCoreVendorTermsOnlyDecrease holds every core file's vendor-term count at or below the
// recorded baseline and requires the baseline to follow each reduction.
func TestCoreVendorTermsOnlyDecrease(t *testing.T) {
	current := currentCoreVendorTerms(t)
	if len(current) == 0 {
		t.Fatal("found no vendor terms in internal/core; is componentRoot right?")
	}
	if os.Getenv("UPDATE_VENDOR_BASELINE") == "1" {
		data, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(coreVendorTermsBaseline, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(coreVendorTermsBaseline)
	if err != nil {
		t.Fatalf("read %s (run with UPDATE_VENDOR_BASELINE=1 to create it): %v", coreVendorTermsBaseline, err)
	}
	baseline := map[string]int{}
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(current, baseline) {
		return
	}
	files := map[string]bool{}
	for file := range current {
		files[file] = true
	}
	for file := range baseline {
		files[file] = true
	}
	var rose, fell []string
	for file := range files {
		switch now, before := current[file], baseline[file]; {
		case now > before:
			rose = append(rose, fmt.Sprintf("%s: %d -> %d", file, before, now))
		case now < before:
			fell = append(fell, fmt.Sprintf("%s: %d -> %d", file, before, now))
		}
	}
	sort.Strings(rose)
	sort.Strings(fell)
	for _, change := range rose {
		t.Errorf("vendor terms in the core grew (move the vendor knowledge to an adapter or a module): %s", change)
	}
	if len(fell) > 0 {
		t.Errorf("vendor terms in the core fell; record it with UPDATE_VENDOR_BASELINE=1 go test ./tests/ -run TestCoreVendorTermsOnlyDecrease: %s",
			strings.Join(fell, ", "))
	}
}
