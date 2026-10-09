package maas

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// This file implements the Power Configuration capability against MAAS (decision 054): the
// Machine's `power_type` is the driver, its admin-only `power_parameters` hold the driver's
// parameters, and a machine update writes both. MAAS vocabulary stays here.

// powerMachineJSON is the part of a machine object the Power Configuration needs: the driver and
// whether a VM host (`pod`) owns the machine's power.
type powerMachineJSON struct {
	PowerType string     `json:"power_type"`
	Pod       *namedJSON `json:"pod"`
}

// powerDriverOf normalizes MAAS's power_type. It is lower-cased and trimmed only; placeholder
// cleaning (cleanField) must not apply, because every non-empty value is a real driver name.
func powerDriverOf(powerType string) provisioningdomain.PowerDriver {
	return provisioningdomain.PowerDriver(strings.ToLower(strings.TrimSpace(powerType)))
}

// PowerConfiguration implements provisioningdomain.PowerConfigurationReader. It costs the machine
// read plus, when a driver is set, the admin-only power_parameters operation, decoded through the
// same allowlist as the live detail so no K_g, token, or unknown field is ever held. The password
// is returned for in-process use; callers must not log or return it.
func (p *Provider) PowerConfiguration(ctx context.Context, machineID string) (*provisioningdomain.PowerConfiguration, error) {
	var m powerMachineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &m); err != nil {
		return nil, translateError(err, machineID)
	}
	config := &provisioningdomain.PowerConfiguration{Driver: powerDriverOf(m.PowerType), ManagedBy: nameOf(m.Pod)}
	if config.Driver == provisioningdomain.PowerDriverNone {
		return config, nil
	}
	var params powerParametersJSON
	if err := p.client.getOperation(ctx, machinePath(machineID), "power_parameters", &params); err != nil {
		return nil, translatePowerParametersError(err, machineID, "read")
	}
	config.Address = strings.TrimSpace(params.PowerAddress)
	config.PowerID = strings.TrimSpace(params.PowerID)
	config.Username = strings.TrimSpace(params.PowerUser)
	config.Password = params.PowerPass
	config.NodeID = strings.TrimSpace(params.NodeID)
	return config, nil
}

// SetPowerConfiguration implements provisioningdomain.PowerConfigurationWriter through the MAAS
// machine update (PUT), the path the MAAS CLI's `machine update power_type=… power_parameters_…=`
// uses. MAAS validates the parameters against the driver (it needs a connected rack controller to
// do so) and fills every parameter the request omits from the stored ones, which is what keeps a
// password the operator did not resend. A VM-host member is refused before the write: its power
// comes from the VM host, and rewriting it on the machine would detach it from that host.
func (p *Provider) SetPowerConfiguration(ctx context.Context, machineID string, change provisioningdomain.PowerConfigurationChange) error {
	var m powerMachineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &m); err != nil {
		return translateError(err, machineID)
	}
	if host := nameOf(m.Pod); host != "" {
		return fmt.Errorf("%w: change the power settings of VM host %q in the provisioner", provisioningdomain.ErrPowerConfigurationManaged, host)
	}
	fields, err := powerUpdateFields(powerDriverOf(m.PowerType), change)
	if err != nil {
		return err
	}
	if err := p.client.putMultipartExact(ctx, machinePath(machineID), fields, nil); err != nil {
		return translatePowerParametersError(err, machineID, "write")
	}
	return nil
}

// powerUpdateFields maps a normalized change onto MAAS machine-update fields. Every non-secret
// parameter of the driver is sent, empty or not, so the change replaces rather than merges them;
// the password is omitted only when it must be kept (nil for an unchanged driver), so MAAS fills
// it from the stored parameters.
func powerUpdateFields(current provisioningdomain.PowerDriver, change provisioningdomain.PowerConfigurationChange) (map[string]string, error) {
	fields := map[string]string{
		"power_type":                     string(change.Driver),
		"power_parameters_power_address": change.Address,
	}
	switch change.Driver {
	case provisioningdomain.PowerDriverVirsh:
		fields["power_parameters_power_id"] = change.PowerID
	case provisioningdomain.PowerDriverIPMI, provisioningdomain.PowerDriverRedfish:
		fields["power_parameters_power_user"] = change.Username
	default:
		return nil, fmt.Errorf("%w: driver %q cannot be written to MAAS", provisioningdomain.ErrInvalidPowerConfiguration, change.Driver)
	}
	switch {
	case change.Password != nil:
		fields["power_parameters_power_pass"] = *change.Password
	case current != change.Driver:
		fields["power_parameters_power_pass"] = ""
	}
	return fields, nil
}

// translatePowerParametersError keeps the meaning of a refused power-parameter call explicit: MAAS
// answers 401/403 when the integration's account is not an administrator, which is a setup problem
// the operator must fix in the Integration, not a rejected configuration.
func translatePowerParametersError(err error, machineID, verb string) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorAuth,
			Detail: "The provisioner credential may not " + verb + " this machine's power parameters; its MAAS account must be an administrator.",
			Err:    err,
		}
	}
	return translateError(err, machineID)
}
