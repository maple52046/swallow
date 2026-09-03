package domain

import "testing"

// Every Operation kind changes its target host and must respect the provider-owned lock.
func TestOperationKindRefusedWhenLocked(t *testing.T) {
	for _, kind := range ValidOperationKinds {
		if !kind.RefusedWhenLocked() {
			t.Errorf("%s should be refused while its target is locked", kind)
		}
	}

	if OperationKind("unknown").RefusedWhenLocked() {
		t.Error("an invalid operation kind must not be treated as executable")
	}
}

// install-exporters must only run on a deployed host; uninstall must have no state
// requirement so a machine being retired or handed to k8s can still be cleaned.
func TestExporterKindProvisioningStateRequirement(t *testing.T) {
	if got := OperationKindInstallExporters.RequiredProvisioningState(); got != "deployed" {
		t.Errorf("install-exporters required state = %q, want deployed", got)
	}
	if got := OperationKindUninstallExporters.RequiredProvisioningState(); got != "" {
		t.Errorf("uninstall-exporters must have no required state, got %q", got)
	}
}

func TestExporterKindsAreValid(t *testing.T) {
	for _, kind := range []OperationKind{OperationKindInstallExporters, OperationKindUninstallExporters} {
		if !kind.Valid() {
			t.Errorf("%s should be a valid operation kind", kind)
		}
	}
}
