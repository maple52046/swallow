package domain

import "testing"

// Every Operation kind changes its target host and must respect the provider-owned lock.
func TestWorkflowKindRefusedWhenLocked(t *testing.T) {
	for _, kind := range ValidWorkflowKinds {
		if !kind.RefusedWhenLocked() {
			t.Errorf("%s should be refused while its target is locked", kind)
		}
	}

	if WorkflowKind("unknown").RefusedWhenLocked() {
		t.Error("an invalid operation kind must not be treated as executable")
	}
}

// install-exporters must only run on a deployed host; uninstall must have no state
// requirement so a machine being retired or handed to k8s can still be cleaned.
func TestExporterKindProvisioningStateRequirement(t *testing.T) {
	if got := WorkflowKindInstallExporters.RequiredProvisioningState(); got != "deployed" {
		t.Errorf("install-exporters required state = %q, want deployed", got)
	}
	if got := WorkflowKindUninstallExporters.RequiredProvisioningState(); got != "" {
		t.Errorf("uninstall-exporters must have no required state, got %q", got)
	}
}

func TestExporterKindsAreValid(t *testing.T) {
	for _, kind := range []WorkflowKind{WorkflowKindInstallExporters, WorkflowKindUninstallExporters} {
		if !kind.Valid() {
			t.Errorf("%s should be a valid operation kind", kind)
		}
	}
}
