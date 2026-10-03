package maas

import (
	"context"
	"errors"
	"net/http"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// bmcMachineJSON is the part of a machine object needed to decide whether it has a BMC at all:
// a VM-host member (`pod`) is a virtual machine, and an empty `power_type` means MAAS has no
// power driver configured.
type bmcMachineJSON struct {
	PowerType string     `json:"power_type"`
	Pod       *namedJSON `json:"pod"`
}

// BMCConnection reads one machine's BMC connection live from MAAS for swallow's Redfish Boot
// Media (decision 047). It costs two calls: the machine object, then the admin-only
// `power_parameters` operation, decoded through the same allowlist as the live detail so no
// K_g, token, or unknown field is ever held.
//
// A virtual machine, a machine without a power driver, and one whose parameters carry no
// address all map to ErrMachineHasNoBMC. A 401/403 on power_parameters means the integration's
// MAAS account is not an administrator and maps to ProviderErrorAuth.
func (p *Provider) BMCConnection(ctx context.Context, machineID string) (*provisioningdomain.BMCConnection, error) {
	var m bmcMachineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &m); err != nil {
		return nil, translateError(err, machineID)
	}
	if nameOf(m.Pod) != "" || cleanField(m.PowerType) == "" {
		return nil, provisioningdomain.ErrMachineHasNoBMC
	}
	var params powerParametersJSON
	if err := p.client.getOperation(ctx, machinePath(machineID), "power_parameters", &params); err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
			return nil, &provisioningdomain.ProviderError{
				Kind:   provisioningdomain.ProviderErrorAuth,
				Detail: "The provisioner credential may not read this machine's BMC parameters.",
				Err:    err,
			}
		}
		return nil, translateError(err, machineID)
	}
	address := cleanField(params.PowerAddress)
	if address == "" {
		return nil, provisioningdomain.ErrMachineHasNoBMC
	}
	return &provisioningdomain.BMCConnection{
		PowerType: cleanField(m.PowerType),
		Address:   address,
		Username:  cleanField(params.PowerUser),
		Password:  cleanField(params.PowerPass),
		NodeID:    cleanField(params.NodeID),
	}, nil
}
