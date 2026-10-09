package maas

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// This file implements machine registration against MAAS (decision 055): the admin machine create,
// with the power settings in the same request and without the commissioning MAAS starts by default,
// so swallow's inspect-hardware Workflow owns commissioning.

// FindMachineByMAC implements provisioningdomain.MachineRegistrar. MAAS's machine list filter
// matches any of several mac_address values.
func (p *Provider) FindMachineByMAC(ctx context.Context, macs []string) (string, error) {
	query := url.Values{}
	for _, mac := range macs {
		if mac = strings.ToLower(strings.TrimSpace(mac)); mac != "" {
			query.Add("mac_address", mac)
		}
	}
	if len(query) == 0 {
		return "", nil
	}
	var machines []struct {
		SystemID string `json:"system_id"`
	}
	if err := p.client.get(ctx, "/machines/", query, &machines); err != nil {
		return "", translateError(err, "")
	}
	for _, machine := range machines {
		if machine.SystemID != "" {
			return machine.SystemID, nil
		}
	}
	return "", nil
}

// RegisterMachine implements provisioningdomain.MachineRegistrar through POST /machines/ (the admin
// create that takes mac_addresses), with commission=false. MAAS validates the power parameters
// against the driver as it does for an update.
func (p *Provider) RegisterMachine(ctx context.Context, registration provisioningdomain.MachineRegistration) (string, error) {
	if len(registration.MACAddresses) == 0 {
		return "", errors.New("register machine: at least one MAC address is required")
	}
	fields, err := powerUpdateFields(provisioningdomain.PowerDriverNone, registration.Power)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("hostname", registration.Hostname)
	values.Set("architecture", registration.Architecture+"/generic")
	values.Set("commission", "false")
	for _, mac := range registration.MACAddresses {
		values.Add("mac_addresses", mac)
	}
	for name, value := range fields {
		values.Set(name, value)
	}
	var created struct {
		SystemID string `json:"system_id"`
	}
	if err := p.client.postMultipartValues(ctx, "/machines/", values, &created); err != nil {
		return "", translatePowerParametersError(err, "", "register")
	}
	if created.SystemID == "" {
		return "", fmt.Errorf("register machine %s: MAAS returned no system_id", registration.Hostname)
	}
	return created.SystemID, nil
}
