package maas

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// This file implements provisioningdomain.GroupingController against MAAS physical zones
// (decision 071). swallow owns the Zone catalog and each Server's Zone placement; these methods read
// MAAS's zones and make MAAS match. They speak zone names — swallow's currency. Create/delete are
// idempotent "ensure" operations so that a swallow write over MAAS's built-in `default` zone, or a
// repeated write, is not an error. MAAS resource pools are never written: swallow's Pools exist only
// in swallow (decision 082), and a machine's resource pool is only read with the machine.

// maasDefaultZone is the name of MAAS's built-in zone. MAAS refuses to delete it and puts a new
// machine in it.
const maasDefaultZone = "default"

// zonePath is the MAAS endpoint for one physical zone, addressed by its name.
func zonePath(name string) string {
	return "/zones/" + url.PathEscape(name) + "/"
}

// zoneJSON is the subset of a MAAS physical zone swallow reads.
type zoneJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListZones returns every MAAS physical zone; the built-in `default` is marked Default.
func (p *Provider) ListZones(ctx context.Context) ([]provisioningdomain.ProviderGroup, error) {
	var zones []zoneJSON
	if err := p.client.get(ctx, "/zones/", nil, &zones); err != nil {
		return nil, translateError(err, "")
	}
	out := make([]provisioningdomain.ProviderGroup, 0, len(zones))
	for _, zone := range zones {
		out = append(out, provisioningdomain.ProviderGroup{
			Name: zone.Name, Description: zone.Description, Default: zone.Name == maasDefaultZone,
		})
	}
	return out, nil
}

// DefaultZoneName implements provisioningdomain.GroupingController: MAAS's built-in zone is named
// `default`.
func (p *Provider) DefaultZoneName() string {
	return maasDefaultZone
}

// EnsureZone creates the named MAAS physical zone unless it already exists.
//
// Existence is checked with a read first rather than by interpreting a create rejection,
// because MAAS phrases the "already exists" validation error as free text that is not safe to
// pattern-match. A zone MAAS already holds (including the built-in `default`) is treated as
// satisfied, so a swallow create over it succeeds.
func (p *Provider) EnsureZone(ctx context.Context, name, description string) error {
	exists, err := p.zoneExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := p.client.postMultipart(ctx, "/zones/", map[string]string{
		"name":        name,
		"description": description,
	}, nil); err != nil {
		return translateError(err, "")
	}
	return nil
}

// RenameZone changes a MAAS zone's name (and description) from currentName to newName. When the
// current zone is absent — for instance it was never realized in this provider — the new zone is
// ensured instead so the provider ends in the requested state.
func (p *Provider) RenameZone(ctx context.Context, currentName, newName, description string) error {
	exists, err := p.zoneExists(ctx, currentName)
	if err != nil {
		return err
	}
	if !exists {
		return p.EnsureZone(ctx, newName, description)
	}
	if err := p.client.putMultipart(ctx, zonePath(currentName), map[string]string{
		"name":        newName,
		"description": description,
	}, nil); err != nil {
		return translateError(err, "")
	}
	return nil
}

// DeleteZone removes the named MAAS zone, treating an absent zone as already satisfied. A MAAS
// refusal — for example deleting a zone that still owns machines — is surfaced as a provider
// rejection by translateError rather than swallowed.
func (p *Provider) DeleteZone(ctx context.Context, name string) error {
	exists, err := p.zoneExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := p.client.delete(ctx, zonePath(name)); err != nil {
		return translateError(err, "")
	}
	return nil
}

// SetMachineZone assigns machineID to the named zone via the MAAS machine update (PUT), the same
// path the MAAS CLI's "machine update zone=" uses. It returns the machine as MAAS reports it
// afterwards so the caller can echo the effective zone without a second read.
func (p *Provider) SetMachineZone(ctx context.Context, machineID, zoneName string) (*provisioningdomain.Machine, error) {
	if zoneName == "" {
		return nil, &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: "A zone name is required: a MAAS machine always belongs to a zone.",
		}
	}
	var out machineJSON
	if err := p.client.putMultipart(ctx, machinePath(machineID), map[string]string{
		"zone": zoneName,
	}, &out); err != nil {
		return nil, translateError(err, machineID)
	}
	return toDomainMachine(&out), nil
}

// zoneExists reports whether MAAS holds a zone with this name. A 404 is a definite "no"; any
// other transport or API failure is propagated so an ensure/delete does not proceed on a guess.
func (p *Provider) zoneExists(ctx context.Context, name string) (bool, error) {
	err := p.client.get(ctx, zonePath(name), nil, nil)
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, translateError(err, "")
}

// isNotFound reports whether err is a MAAS HTTP 404. It inspects the adapter's own apiError so a
// missing zone can be distinguished from a transport failure without going through translateError,
// which would map a machine-scoped 404 onto ErrMachineNotFound.
func isNotFound(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}
