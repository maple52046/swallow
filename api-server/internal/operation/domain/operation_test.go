package domain

import "testing"

// The exporter kinds are the ones swallow refuses to run against a locked machine, which
// is what keeps an operator-locked host off-limits to automatic install/uninstall.
func TestOperationKindRefusedWhenLocked(t *testing.T) {
	refused := map[OperationKind]bool{
		OperationKindInstallExporters:   true,
		OperationKindUninstallExporters: true,
		OperationKindInstallGPUDriver:   false,
		OperationKindDeployKubernetes:   false,
		OperationKindCustom:             false,
	}
	for kind, want := range refused {
		if got := kind.RefusedWhenLocked(); got != want {
			t.Errorf("%s RefusedWhenLocked() = %v, want %v", kind, got, want)
		}
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
