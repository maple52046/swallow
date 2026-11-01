package domain

import (
	"errors"
	"testing"
)

// fakeSoftware is a Software with a fixed entry and no spec logic.
type fakeSoftware struct{ entry CatalogEntry }

func (f fakeSoftware) Entry() CatalogEntry                                { return f.entry }
func (fakeSoftware) ValidateSpec(map[string][]Role, map[string]any) error { return nil }
func (fakeSoftware) NormalizeSpec(spec map[string]any) map[string]any     { return CloneSpec(spec) }
func (fakeSoftware) InstallVars(map[string][]Role, map[string]any) map[string]any {
	return map[string]any{}
}

// The catalog holds each kind once, orders entries by kind, and labels an unknown kind by its slug.
func TestCatalogOrdersKindsAndLabelsThem(t *testing.T) {
	catalog, err := NewCatalog(
		fakeSoftware{CatalogEntry{Kind: "runtime-b", Label: "Runtime B", MutuallyExclusiveWith: []Kind{"runtime-a"}}},
		fakeSoftware{CatalogEntry{Kind: "runtime-a", Label: "Runtime A", MutuallyExclusiveWith: []Kind{"runtime-b"}}},
		fakeSoftware{CatalogEntry{
			Kind: "monitor", Label: "Monitor", InstallMethods: []InstallMethod{InstallMethodContainer, InstallMethodNative},
			ContainerRuntimes: []Kind{"runtime-a", "runtime-b"},
		}},
	)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	var kinds []Kind
	for _, entry := range catalog.Entries() {
		kinds = append(kinds, entry.Kind)
	}
	if len(kinds) != 3 || kinds[0] != "monitor" || kinds[1] != "runtime-a" || kinds[2] != "runtime-b" {
		t.Errorf("Entries kinds = %v, want [monitor runtime-a runtime-b]", kinds)
	}
	if _, ok := catalog.Lookup("runtime-a"); !ok {
		t.Error("Lookup(runtime-a) found nothing")
	}
	if got := catalog.Label("runtime-a"); got != "Runtime A" {
		t.Errorf("Label(runtime-a) = %q", got)
	}
	if got := catalog.Label("missing"); got != "missing" {
		t.Errorf("Label(missing) = %q, want the slug", got)
	}
}

// A catalog whose kinds are inconsistent refuses to form, so a build with such modules cannot start.
func TestCatalogRefusesInconsistentKinds(t *testing.T) {
	tests := map[string][]Software{
		"no label": {fakeSoftware{CatalogEntry{Kind: "a"}}},
		"twice":    {fakeSoftware{CatalogEntry{Kind: "a", Label: "A"}}, fakeSoftware{CatalogEntry{Kind: "a", Label: "A"}}},
		"unknown exclusive kind": {
			fakeSoftware{CatalogEntry{Kind: "a", Label: "A", MutuallyExclusiveWith: []Kind{"b"}}},
		},
		"unknown container runtime": {fakeSoftware{CatalogEntry{
			Kind: "a", Label: "A", InstallMethods: []InstallMethod{InstallMethodContainer}, ContainerRuntimes: []Kind{"b"},
		}}},
		"runtimes without the container method": {
			fakeSoftware{CatalogEntry{Kind: "b", Label: "B"}},
			fakeSoftware{CatalogEntry{Kind: "a", Label: "A", InstallMethods: []InstallMethod{InstallMethodNative}, ContainerRuntimes: []Kind{"b"}}},
		},
	}
	for name, software := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCatalog(software...); !errors.Is(err, ErrInvalidCatalog) {
				t.Errorf("NewCatalog error = %v, want ErrInvalidCatalog", err)
			}
		})
	}
}

// A kind's Workflow kinds and playbooks are named after it; they are persisted, so they never change.
func TestKindWorkflowNames(t *testing.T) {
	kind := Kind("docker-ce")
	got := []string{kind.InstallWorkflowKind(), kind.InstallPlaybook(), kind.UninstallWorkflowKind(), kind.UninstallPlaybook()}
	want := []string{"configure-docker-ce", "deploy-docker-ce", "uninstall-docker-ce", "uninstall-docker-ce"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("names = %v, want %v", got, want)
			break
		}
	}
}

// fakeSchema lists names and refuses any spec that sets "bad".
type fakeSchema struct{ names []string }

func (f fakeSchema) Names() []string { return f.names }
func (fakeSchema) Validate(spec map[string]any) error {
	if _, bad := spec["bad"]; bad {
		return errors.New("spec.bad is not a setting of this module")
	}
	return nil
}

// A kind's spec schema must list exactly its catalog fields; a spec that breaks it is ErrSpecInvalid,
// and a kind without a schema is left to its Software.
func TestCatalogChecksSpecsAgainstAttachedSchemas(t *testing.T) {
	catalog, err := NewCatalog(
		fakeSoftware{CatalogEntry{Kind: "share", Label: "Share", SpecFields: []string{"path", "options"}}},
		fakeSoftware{CatalogEntry{Kind: "plain", Label: "Plain", SpecFields: []string{"version"}}},
	)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	if err := catalog.AttachSpecSchema("share", fakeSchema{names: []string{"path"}}); !errors.Is(err, ErrInvalidCatalog) {
		t.Errorf("schema missing a catalog field: error = %v, want ErrInvalidCatalog", err)
	}
	if err := catalog.AttachSpecSchema("missing", fakeSchema{}); !errors.Is(err, ErrInvalidCatalog) {
		t.Errorf("schema for an unknown kind: error = %v, want ErrInvalidCatalog", err)
	}
	if err := catalog.AttachSpecSchema("share", fakeSchema{names: []string{"options", "path"}}); err != nil {
		t.Fatalf("AttachSpecSchema: %v", err)
	}
	if err := catalog.CheckSpec("share", map[string]any{"bad": true}); !errors.Is(err, ErrSpecInvalid) {
		t.Errorf("CheckSpec(share, bad) = %v, want ErrSpecInvalid", err)
	}
	if err := catalog.CheckSpec("share", map[string]any{"path": "/x"}); err != nil {
		t.Errorf("CheckSpec(share, valid) = %v", err)
	}
	if err := catalog.CheckSpec("plain", map[string]any{"bad": true}); err != nil {
		t.Errorf("CheckSpec(plain) = %v, want nil for a kind without a schema", err)
	}
}
