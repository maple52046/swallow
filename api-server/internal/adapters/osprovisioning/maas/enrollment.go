package maas

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// This file implements Server Enrollment against MAAS (decisions 053 and 054): telling when MAAS's
// own enlistment of a network-booted machine has finished, and handing a host that keeps its OS
// what `swallow servers enroll --provisioner=maas` needs. All MAAS vocabulary stays here and in the
// CLI's host-side adapter.

// MAAS script statuses (maascommon ScriptStatus) of a commissioning script set that is still in
// progress. A machine without any script set reports -1. Enlistment of a New Machine runs as a
// commissioning script set, and its 30-maas-01-bmc-config script is where MAAS sets a physical
// machine's BMC power driver, so while the set is in progress a missing driver may still appear.
const (
	scriptStatusPending         = 0
	scriptStatusRunning         = 1
	scriptStatusInstalling      = 7
	scriptStatusApplyingNetconf = 10
)

// enrollmentMachineJSON is the machine object as the enrollment observation reads it: the fleet
// fields plus the power driver and the numeric state of the current commissioning script set.
// CommissioningStatus is a pointer so a MAAS that omits it is told apart from one that reports -1.
type enrollmentMachineJSON struct {
	machineJSON
	PowerType           string `json:"power_type"`
	CommissioningStatus *int   `json:"commissioning_status"`
}

// enlistmentInProgress reports whether the machine's commissioning script set — enlistment, for a
// New Machine — may still be running. A MAAS that reports no numeric status is treated as still
// running, which keeps the pre-observation behavior (wait, then time out) instead of declaring a
// physical machine's power missing while its BMC driver may yet be configured.
func (m *enrollmentMachineJSON) enlistmentInProgress() bool {
	if m.CommissioningStatus == nil {
		return true
	}
	switch *m.CommissioningStatus {
	case scriptStatusPending, scriptStatusRunning, scriptStatusInstalling, scriptStatusApplyingNetconf:
		return true
	default:
		return false
	}
}

// ObserveEnrollment implements provisioningdomain.EnrollmentSettler.
//
// MAAS creates the New Machine while its enlistment environment still runs on the host and powers
// the host off when enlistment ends; commissioning it before that power-off races the enlistment
// boot. A Machine that has left New has nothing left to settle. For a New Machine with an automatic
// power driver the power state is read live; when that read fails (a busy BMC, an unreachable
// hypervisor) the state MAAS last recorded is used, and an unknown state is still enrolling. A
// driver that is absent or manual is not queried — MAAS cannot read it — and once the enlistment
// script set has finished such a Machine's power-off can never be observed unless MAAS already
// recorded it off.
func (p *Provider) ObserveEnrollment(ctx context.Context, machineID string) (provisioningdomain.EnrollmentObservation, error) {
	var m enrollmentMachineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &m); err != nil {
		return provisioningdomain.EnrollmentObservation{}, translateError(err, machineID)
	}
	machine := toDomainMachine(&m.machineJSON)
	driver := powerDriverOf(m.PowerType)
	observation := provisioningdomain.EnrollmentObservation{
		State:   provisioningdomain.EnrollmentEnrolling,
		Driver:  driver,
		Control: provisioningdomain.PowerControlOf(driver),
	}
	if machine.Status != provisioningdomain.MachineStatusNew {
		observation.State = provisioningdomain.EnrollmentSettled
		return observation, nil
	}
	power := machine.PowerState
	if observation.Control == provisioningdomain.PowerControlAutomatic {
		live, err := p.QueryPowerState(ctx, machineID)
		if err == nil && live != provisioningdomain.PowerStateUnknown && live != provisioningdomain.PowerStateError {
			power = live
		}
	}
	switch {
	case power == provisioningdomain.PowerStateOff:
		observation.State = provisioningdomain.EnrollmentSettled
	case observation.Control != provisioningdomain.PowerControlAutomatic && !m.enlistmentInProgress():
		observation.State = provisioningdomain.EnrollmentPowerUnobservable
	}
	return observation, nil
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
