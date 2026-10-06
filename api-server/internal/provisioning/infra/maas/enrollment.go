package maas

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// This file implements Server Enrollment against MAAS (decision 053): telling when MAAS's own
// enlistment of a network-booted machine has finished, and handing a host that keeps its OS what
// `swallow servers enroll --provisioner=maas` needs. All MAAS vocabulary stays here and in the
// CLI's host-side adapter.

// EnrollmentSettled reports whether MAAS has finished enlisting machineID.
//
// MAAS creates the New Machine while its enlistment environment still runs on the host and powers
// the host off when enlistment ends; commissioning it before that power-off races the enlistment
// boot. A Machine that has left New has nothing left to settle. The power state is read live from
// the BMC; when that read fails (a BMC MAAS has not configured yet, or a busy one) the state MAAS
// last recorded is used, and an unknown state is not settled.
func (p *Provider) EnrollmentSettled(ctx context.Context, machineID string) (bool, error) {
	machine, err := p.GetMachine(ctx, machineID)
	if err != nil {
		return false, err
	}
	if machine.Status != provisioningdomain.MachineStatusNew {
		return true, nil
	}
	power, err := p.QueryPowerState(ctx, machineID)
	if err != nil || power == provisioningdomain.PowerStateUnknown || power == provisioningdomain.PowerStateError {
		power = machine.PowerState
	}
	return power == provisioningdomain.PowerStateOff, nil
}

// ExistingHostEnrollment returns the region URL and the API key a host keeping its OS needs: MAAS's
// register-machine authenticates with a MAAS API key, so the key this adapter was built with is
// handed out as is.
func (p *Provider) ExistingHostEnrollment(_ context.Context) (*provisioningdomain.ExistingHostEnrollment, error) {
	return &provisioningdomain.ExistingHostEnrollment{
		Endpoint: p.client.regionURL(),
		Token:    p.client.key.credential(),
	}, nil
}
