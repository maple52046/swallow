package maas

import (
	"context"

	provisioningdomain "github.com/AFDEAPAC/swallow/internal/provisioning/domain"
)

// This file implements the optional provider capabilities against MAAS. Each is a thin
// wrapper over a MAAS named operation, sharing one helper so that a new action is one
// line plus its documented intent, and so error translation is identical to deploy and
// release.

// action invokes a state-changing MAAS operation and returns the machine as MAAS reports
// it immediately afterwards, translated into the provider-neutral shape.
func (p *Provider) action(ctx context.Context, machineID, operation string) (*provisioningdomain.Machine, error) {
	var out machineJSON
	if err := p.client.postOperation(ctx, machinePath(machineID), operation, nil, &out); err != nil {
		return nil, translateError(err, machineID)
	}
	return toDomainMachine(&out), nil
}

// --- PowerController ---

func (p *Provider) PowerOn(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "power_on")
}

func (p *Provider) PowerOff(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "power_off")
}

// QueryPowerState reads the live BMC power state rather than MAAS's cached value, which
// is why it is a distinct call from reading the machine's power_state field.
func (p *Provider) QueryPowerState(ctx context.Context, machineID string) (provisioningdomain.PowerState, error) {
	var out struct {
		State string `json:"state"`
	}
	if err := p.client.getOperation(ctx, machinePath(machineID), "query_power_state", &out); err != nil {
		return provisioningdomain.PowerStateUnknown, translateError(err, machineID)
	}
	if state, ok := maasPowerStateToDomain[out.State]; ok {
		return state, nil
	}
	return provisioningdomain.PowerStateUnknown, nil
}

// --- HardwareValidator ---

func (p *Provider) Commission(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "commission")
}

func (p *Provider) Test(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "test")
}

func (p *Provider) Abort(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "abort")
}

func (p *Provider) OverrideFailedTesting(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "override_failed_testing")
}

// --- OperatorStateController ---

func (p *Provider) Lock(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "lock")
}

func (p *Provider) Unlock(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "unlock")
}

func (p *Provider) MarkBroken(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "mark_broken")
}

func (p *Provider) MarkFixed(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "mark_fixed")
}

func (p *Provider) EnterRescueMode(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "rescue_mode")
}

func (p *Provider) ExitRescueMode(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.action(ctx, machineID, "exit_rescue_mode")
}
