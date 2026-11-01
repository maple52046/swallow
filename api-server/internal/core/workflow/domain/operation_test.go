package domain

import (
	"errors"
	"testing"
)

// Every Operation kind changes its target host and must respect the provider-owned lock.
func TestWorkflowKindRefusedWhenLocked(t *testing.T) {
	kinds := WorkflowKinds{}
	for _, kind := range BuiltInWorkflowKinds() {
		if !kinds.RefusedWhenLocked(kind) {
			t.Errorf("%s should be refused while its target is locked", kind)
		}
	}

	if kinds.RefusedWhenLocked("unknown") {
		t.Error("an invalid operation kind must not be treated as executable")
	}
}

// install-exporters must only run on a deployed host; uninstall must have no state
// requirement so a machine being retired or handed to k8s can still be cleaned.
func TestExporterKindProvisioningStateRequirement(t *testing.T) {
	kinds := WorkflowKinds{}
	if got := kinds.RequiredProvisioningState(WorkflowKindInstallExporters); got != "deployed" {
		t.Errorf("install-exporters required state = %q, want deployed", got)
	}
	if got := kinds.RequiredProvisioningState(WorkflowKindUninstallExporters); got != "" {
		t.Errorf("uninstall-exporters must have no required state, got %q", got)
	}
}

func TestExporterKindsAreValid(t *testing.T) {
	for _, kind := range []WorkflowKind{WorkflowKindInstallExporters, WorkflowKindUninstallExporters} {
		if !(WorkflowKinds{}).Valid(kind) {
			t.Errorf("%s should be a valid operation kind", kind)
		}
	}
}

// A module's kinds are accepted only once added, carry the state they were added with, and cannot
// redeclare a kind that is already accepted; adding never changes the original set.
func TestWorkflowKindsWithAddsModuleKinds(t *testing.T) {
	base := WorkflowKinds{}
	kinds, err := base.With("deployed", "configure-widget", "uninstall-widget")
	if err != nil {
		t.Fatalf("With: %v", err)
	}
	if base.Valid("configure-widget") {
		t.Error("With changed the set it was called on")
	}
	for _, kind := range []WorkflowKind{"configure-widget", "uninstall-widget"} {
		if !kinds.Valid(kind) || kinds.RequiredProvisioningState(kind) != "deployed" || !kinds.RefusedWhenLocked(kind) {
			t.Errorf("%s: valid %v, required %q", kind, kinds.Valid(kind), kinds.RequiredProvisioningState(kind))
		}
	}
	if got, want := len(kinds.List()), len(BuiltInWorkflowKinds())+2; got != want {
		t.Errorf("List has %d kinds, want %d", got, want)
	}
	if _, err := kinds.With("", "configure-widget"); !errors.Is(err, ErrWorkflowKindConflict) {
		t.Errorf("redeclaring a module kind: err = %v, want ErrWorkflowKindConflict", err)
	}
	if _, err := base.With("", WorkflowKindDeployOS); !errors.Is(err, ErrWorkflowKindConflict) {
		t.Errorf("redeclaring a built-in kind: err = %v, want ErrWorkflowKindConflict", err)
	}
}
