package application

import (
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// Both writers of the provisioning axis, reconcile and the projection after a lifecycle action,
// mirror the machine's power control, so a Server's list entry can tell "no power driver" from
// "power unreadable" whichever of them wrote last.
func TestProvisioningProjectionMirrorsPowerControl(t *testing.T) {
	machine := &provisioningdomain.Machine{
		ID:           "m-1",
		Status:       provisioningdomain.MachineStatusDeployed,
		PowerState:   provisioningdomain.PowerStateUnknown,
		PowerControl: provisioningdomain.PowerControlNone,
	}

	reconciled := &serverdomain.Server{ID: "srv-1"}
	apply(reconciled, serverdomain.Source{}, machine, "maas-a",
		func(*provisioningdomain.Machine) deployedImage { return deployedImage{} },
		func([]string, []string) []string { return nil })
	if got := reconciled.Provisioning.PowerControl; got != "none" {
		t.Errorf("apply PowerControl = %q, want %q", got, "none")
	}

	acted := &serverdomain.Server{ID: "srv-1"}
	updateProvisioningProjection(acted, machine)
	if got := acted.Provisioning.PowerControl; got != "none" {
		t.Errorf("updateProvisioningProjection PowerControl = %q, want %q", got, "none")
	}
}
