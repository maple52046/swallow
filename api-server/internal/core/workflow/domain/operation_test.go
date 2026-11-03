package domain

import (
	"errors"
	"testing"
)

// The host exporters are Managed Software (the Node Exporter module) and OS provisioning installs
// the Node Exporter on every Server, so the core no longer accepts the kinds that installed them as
// plain playbooks or as Kubernetes DaemonSets.
func TestRetiredExporterKindsAreNotAccepted(t *testing.T) {
	for _, kind := range []WorkflowKind{"install-exporters", "uninstall-exporters", "deploy-k8s-exporters", "remove-k8s-exporters"} {
		if (WorkflowKinds{}).Valid(kind) {
			t.Errorf("%s is a retired kind and must not be accepted", kind)
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
		if !kinds.Valid(kind) || kinds.RequiredProvisioningState(kind) != "deployed" {
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
