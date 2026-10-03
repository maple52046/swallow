package domain

import (
	"context"
	"errors"
)

// BMCConnection is a machine's BMC connection as the provisioner holds it: the power driver it
// uses and the address and account it reaches the BMC with (decision 047).
//
// The provisioner owns these facts; swallow reads them for one call and must never persist,
// log, or return Password through an API other than the admin-only live detail. Address is
// verbatim — a bare host for IPMI, sometimes a URL for Redfish — and callers derive what they
// need from it. NodeID is the provisioner's Redfish System hint (MAAS `node_id`), often empty.
type BMCConnection struct {
	PowerType string
	Address   string
	Username  string
	Password  string
	NodeID    string
}

// ErrMachineHasNoBMC means the machine has no BMC swallow could reach: it is a virtual machine,
// or the provisioner holds no power driver or address for it. It is a property of the machine,
// not a failure, so callers record it rather than retry.
var ErrMachineHasNoBMC = errors.New("machine has no BMC connection")

// BMCConnectionReader reads a machine's BMC connection from the provisioner, live, for a caller
// that drives the BMC itself (Redfish Boot Media). An adapter that sets
// ProviderCapabilities.BMCConnection must implement it.
//
// Implementations return ErrMachineNotFound for an unknown machine, ErrMachineHasNoBMC as
// described above, a *ProviderError{Kind: ProviderErrorAuth} when the integration credential may
// not read power parameters, and the usual unavailable errors otherwise.
type BMCConnectionReader interface {
	BMCConnection(ctx context.Context, machineID string) (*BMCConnection, error)
}
