package maas

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
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
// password the user did not resend. A VM-host member is refused before the write: its power
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

// powerCheckWindow bounds the power read that decides whether a written configuration is kept. A
// working driver answers within seconds. A BMC that does not answer (a wrong address, a network
// the rack cannot reach, or a BMC that silently drops a wrong password) makes ipmipower report a
// connection timeout, which MAAS retries four times with 4-32 s pauses for over two minutes
// before failing; waiting for that would leave the user staring at the dialog, so the check gives
// up first and says why. It is a variable only so tests can shorten it.
var powerCheckWindow = 20 * time.Second

// SetVerifiedPowerConfiguration implements provisioningdomain.PowerConfigurationVerifier: it
// snapshots the machine's power_type and power_parameters, writes the change like
// SetPowerConfiguration, and asks MAAS to query the power through it (query_power_state, which
// reaches the BMC or hypervisor and changes nothing) for at most powerCheckWindow. A state other
// than on or off, a failed query, or no answer in time restores the snapshot and refuses the
// change with the reason, so a wrong address or password is never left behind. MAAS may keep
// querying after the window closes; that query reads only the power, and the restored
// configuration is what MAAS keeps. The snapshot holds the previous password (and any K_g) in
// memory for that one write and nowhere else; reasons have both passwords redacted.
func (p *Provider) SetVerifiedPowerConfiguration(
	ctx context.Context, machineID string, change provisioningdomain.PowerConfigurationChange,
) (provisioningdomain.PowerState, error) {
	unknown := provisioningdomain.PowerStateUnknown
	var m powerMachineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &m); err != nil {
		return unknown, translateError(err, machineID)
	}
	if host := nameOf(m.Pod); host != "" {
		return unknown, fmt.Errorf("%w: change the power settings of VM host %q in the provisioner", provisioningdomain.ErrPowerConfigurationManaged, host)
	}
	fields, err := powerUpdateFields(powerDriverOf(m.PowerType), change)
	if err != nil {
		return unknown, err
	}
	previous, previousPassword, err := p.powerSnapshot(ctx, machineID, m.PowerType)
	if err != nil {
		return unknown, err
	}
	if err := p.client.putMultipartExact(ctx, machinePath(machineID), fields, nil); err != nil {
		return unknown, translatePowerParametersError(err, machineID, "write")
	}

	var out struct {
		State string `json:"state"`
	}
	queryCtx, cancel := context.WithTimeout(ctx, powerCheckWindow)
	queryErr := p.client.getOperation(queryCtx, machinePath(machineID), "query_power_state", &out)
	timedOut := errors.Is(queryCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	cancel()
	state, known := maasPowerStateToDomain[strings.ToLower(out.State)]
	if queryErr == nil && known && (state == provisioningdomain.PowerStateOn || state == provisioningdomain.PowerStateOff) {
		return state, nil
	}
	reason := powerQueryReason(queryErr, out.State)
	if timedOut {
		reason = fmt.Sprintf("the BMC or hypervisor did not answer within %d seconds: check the address and the password "+
			"(some BMCs do not answer a wrong one), and that the provisioner reaches that network", int(powerCheckWindow/time.Second))
	}
	for _, secret := range []string{fields["power_parameters_power_pass"], previousPassword} {
		if secret != "" {
			reason = strings.ReplaceAll(reason, secret, "[REDACTED]")
		}
	}

	if err := p.client.putMultipartExact(ctx, machinePath(machineID), previous, nil); err != nil {
		return unknown, &provisioningdomain.ProviderError{
			Kind: provisioningdomain.ProviderErrorUnavailable,
			Detail: "The provisioner could not read the power through the new configuration (" + reason +
				"), and putting the previous configuration back failed, so the new one is still set. Fix or replace it.",
			Err: err,
		}
	}
	return unknown, fmt.Errorf("%w (%s); the previous configuration was kept", provisioningdomain.ErrPowerConfigurationUnverified, reason)
}

// powerSnapshot returns the machine-update fields that put its current power driver and
// parameters back, and the stored password so reasons can be redacted. Scalar parameters are
// restored verbatim (an empty driver included, which is how MAAS clears one). List-valued ones,
// such as IPMI workaround flags, are left out: an unchanged driver keeps its stored values because
// the change never sends them, and a changed driver returns to MAAS's defaults for them.
func (p *Provider) powerSnapshot(ctx context.Context, machineID, powerType string) (map[string]string, string, error) {
	fields := map[string]string{"power_type": powerType}
	if strings.TrimSpace(powerType) == "" {
		return fields, "", nil
	}
	var params map[string]any
	if err := p.client.getOperation(ctx, machinePath(machineID), "power_parameters", &params); err != nil {
		return nil, "", translatePowerParametersError(err, machineID, "read")
	}
	for name, value := range params {
		switch v := value.(type) {
		case string:
			fields["power_parameters_"+name] = v
		case bool:
			fields["power_parameters_"+name] = strconv.FormatBool(v)
		case float64:
			fields["power_parameters_"+name] = strconv.FormatFloat(v, 'f', -1, 64)
		case nil:
			fields["power_parameters_"+name] = ""
		}
	}
	// A driver without a password reports none (or not a string); there is then nothing to redact.
	password, _ := params["power_pass"].(string)
	return fields, password, nil
}

// powerQueryReason is MAAS's own explanation of a failed power query, which names what to fix
// (a refused login, an unreachable address), or the state it read when that was neither on nor off.
func powerQueryReason(err error, state string) string {
	var apiErr *apiError
	switch {
	case errors.As(err, &apiErr) && strings.TrimSpace(apiErr.Body) != "":
		return strings.TrimSpace(apiErr.Body)
	case err != nil:
		return "the provisioner did not answer the power query: " + err.Error()
	case strings.TrimSpace(state) == "":
		return "it read no power state"
	default:
		return "it read the power state as " + state
	}
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
// the user must fix in the Integration, not a rejected configuration.
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
