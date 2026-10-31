package maas

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// This file implements provisioningdomain.GroupingController against MAAS physical zones and
// resource pools (decision 029). swallow owns the Zone/Pool catalog; these methods only realize
// that intent in MAAS. They speak group names — swallow's currency — and hide MAAS's internal
// resource-pool ids. Create/delete are idempotent "ensure" operations so that a swallow write
// over MAAS's built-in `default` zone/pool, or a repeated write, is not an error.

// zonePath is the MAAS endpoint for one physical zone, addressed by its name.
func zonePath(name string) string {
	return "/zones/" + url.PathEscape(name) + "/"
}

// resourcePoolPath is the MAAS endpoint for one resource pool, addressed by its numeric id.
// Unlike zones, MAAS keys resource pools by id, so callers resolve a name to an id first.
func resourcePoolPath(id int) string {
	return "/resourcepools/" + strconv.Itoa(id) + "/"
}

// resourcePoolJSON is the subset of a MAAS resource pool swallow reads: the numeric id used in
// its endpoint path and the name swallow matches on.
type resourcePoolJSON struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
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

// EnsurePool creates the named MAAS resource pool unless it already exists.
func (p *Provider) EnsurePool(ctx context.Context, name, description string) error {
	_, found, err := p.resourcePoolID(ctx, name)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	if err := p.client.postMultipart(ctx, "/resourcepools/", map[string]string{
		"name":        name,
		"description": description,
	}, nil); err != nil {
		return translateError(err, "")
	}
	return nil
}

// RenamePool changes a MAAS resource pool's name (and description). MAAS keys resource pools by
// id, so the current name is resolved to an id first; an absent pool is (re)ensured under the new
// name so the provider ends in the requested state.
func (p *Provider) RenamePool(ctx context.Context, currentName, newName, description string) error {
	id, found, err := p.resourcePoolID(ctx, currentName)
	if err != nil {
		return err
	}
	if !found {
		return p.EnsurePool(ctx, newName, description)
	}
	if err := p.client.putMultipart(ctx, resourcePoolPath(id), map[string]string{
		"name":        newName,
		"description": description,
	}, nil); err != nil {
		return translateError(err, "")
	}
	return nil
}

// DeletePool removes the named MAAS resource pool, treating an absent pool as satisfied. MAAS
// refuses to delete a pool that still owns machines or the built-in default; that refusal is
// surfaced as a provider rejection.
func (p *Provider) DeletePool(ctx context.Context, name string) error {
	id, found, err := p.resourcePoolID(ctx, name)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err := p.client.delete(ctx, resourcePoolPath(id)); err != nil {
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

// SetMachinePool assigns machineID to the named resource pool via the MAAS machine update (PUT),
// the same path the MAAS CLI's "machine update pool=" uses.
func (p *Provider) SetMachinePool(ctx context.Context, machineID, poolName string) (*provisioningdomain.Machine, error) {
	if poolName == "" {
		return nil, &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: "A resource pool name is required: a MAAS machine always belongs to a pool.",
		}
	}
	var out machineJSON
	if err := p.client.putMultipart(ctx, machinePath(machineID), map[string]string{
		"pool": poolName,
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

// resourcePoolID resolves a resource pool name to its MAAS id, reporting found=false when no pool
// has that name. MAAS offers no name-addressed pool endpoint, so the collection is listed and
// matched by name. The found flag is separate from the id because MAAS's built-in default pool has
// id 0, so a zero id is a real pool, not an absence.
func (p *Provider) resourcePoolID(ctx context.Context, name string) (id int, found bool, err error) {
	var pools []resourcePoolJSON
	if err := p.client.get(ctx, "/resourcepools/", nil, &pools); err != nil {
		return 0, false, translateError(err, "")
	}
	for _, pool := range pools {
		if pool.Name == name {
			return pool.ID, true, nil
		}
	}
	return 0, false, nil
}

// isNotFound reports whether err is a MAAS HTTP 404. It inspects the adapter's own apiError so a
// missing zone can be distinguished from a transport failure without going through translateError,
// which would map a machine-scoped 404 onto ErrMachineNotFound.
func isNotFound(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}
