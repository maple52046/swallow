package libvirt

import (
	"errors"
	"testing"

	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

// The module loads with exactly the software hook its manifest declares and no spec schema, because
// the kind takes no spec fields.
func TestModuleDeclaresItsSoftwareHookWithoutASchema(t *testing.T) {
	module, err := Module()
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if _, ok := module.Hooks[softwaredomain.HookSoftware].(Software); !ok {
		t.Errorf("hooks = %v, want the software hook", module.Hooks)
	}
	if module.Manifest.ID != string(Kind) {
		t.Errorf("manifest id = %q, want the kind %q", module.Manifest.ID, Kind)
	}
	schema, err := module.Manifest.SpecSchema()
	if err != nil || schema != nil {
		t.Errorf("SpecSchema() = %v, %v; want no schema", schema, err)
	}
}

// The catalog entry offers no spec field, no role, and no restriction of its own.
func TestEntryHasNoSpecFieldsOrRestrictions(t *testing.T) {
	entry := Software{}.Entry()
	if entry.Kind != Kind || entry.Label != "libvirt (QEMU/KVM)" {
		t.Errorf("Entry() kind, label = %q, %q; want %q, %q", entry.Kind, entry.Label, Kind, "libvirt (QEMU/KVM)")
	}
	if entry.SpecFields == nil || len(entry.SpecFields) != 0 {
		t.Errorf("Entry().SpecFields = %#v, want an empty list", entry.SpecFields)
	}
	if len(entry.Roles) != 0 || len(entry.MutuallyExclusiveWith) != 0 || entry.RefusedForKubernetesMembers ||
		entry.RefusedForClaimedServers || entry.Exporter != nil || entry.RequiredAccelerator != "" {
		t.Errorf("Entry() = %+v, want no roles, exclusions, refusals, exporter, or accelerator", entry)
	}
}

// Any spec field is refused before a Workflow exists; an empty or missing spec is accepted and
// recorded as an empty spec with no trusted variables.
func TestSpecTakesNoFields(t *testing.T) {
	software := Software{}
	if err := software.ValidateSpec(nil, map[string]any{"version": "10.0"}); !errors.Is(err, softwaredomain.ErrSpecInvalid) {
		t.Errorf("ValidateSpec(version) = %v, want ErrSpecInvalid", err)
	}
	for _, spec := range []map[string]any{nil, {}} {
		if err := software.ValidateSpec(nil, spec); err != nil {
			t.Errorf("ValidateSpec(%v) = %v, want accepted", spec, err)
		}
	}
	if got := software.NormalizeSpec(nil); got == nil || len(got) != 0 {
		t.Errorf("NormalizeSpec(nil) = %#v, want an empty spec", got)
	}
	if got := software.InstallVars(nil, map[string]any{}); len(got) != 0 {
		t.Errorf("InstallVars() = %v, want no variables", got)
	}
}
