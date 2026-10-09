package domain

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// This file holds the Power Configuration model (decision 054): which power driver a provisioner
// uses for one machine, the driver's parameters, and the power adapters that give each driver
// family its validation and its family-only functions. It is pure model code: provider adapters
// under infra/ read and write the facts, and callers ask an adapter instead of branching on a
// driver name or on VM-host membership.

// PowerDriver is the provisioner's own name for how it reaches one machine's power, for MAAS the
// Machine's `power_type`. The values below are the ones swallow reasons about; a provisioner may
// report others (a VM host's own driver, for example), which are carried verbatim.
type PowerDriver string

const (
	// PowerDriverNone means the provisioner holds no driver: it can neither switch nor read power.
	PowerDriverNone PowerDriver = ""
	// PowerDriverIPMI drives a BMC over IPMI.
	PowerDriverIPMI PowerDriver = "ipmi"
	// PowerDriverRedfish drives a BMC over Redfish. It says nothing about Redfish Boot Media,
	// which is probed from the BMC whatever its driver (decision 047).
	PowerDriverRedfish PowerDriver = "redfish"
	// PowerDriverVirsh drives a libvirt virtual machine through a hypervisor URI.
	PowerDriverVirsh PowerDriver = "virsh"
	// PowerDriverManual means a person switches power; the provisioner cannot read the state.
	PowerDriverManual PowerDriver = "manual"
)

// PowerFamily groups the drivers swallow can write by what they reach. The family decides which
// parameters a configuration carries and which functions beyond switching power a machine has: only
// the bmc family has a BMC, so only it offers Redfish Boot Media.
type PowerFamily string

const (
	// PowerFamilyBMC is a physical machine's baseboard management controller.
	PowerFamilyBMC PowerFamily = "bmc"
	// PowerFamilyVirsh is a libvirt virtual machine on a hypervisor reached over SSH.
	PowerFamilyVirsh PowerFamily = "virsh"
)

// PowerControl classifies what a driver lets the provisioner do with power. It is what the
// enrollment wait and inspection need to know: only automatic control lets the provisioner report
// the power-off that ends enrollment and power a machine on to inspect it.
type PowerControl string

const (
	// PowerControlNone means no driver: the provisioner cannot switch or read power.
	PowerControlNone PowerControl = "none"
	// PowerControlManual means a person switches power and its state cannot be read.
	PowerControlManual PowerControl = "manual"
	// PowerControlAutomatic means the provisioner switches and reads power itself.
	PowerControlAutomatic PowerControl = "automatic"
)

// PowerControlOf classifies driver. Every driver that is neither absent nor manual is automatic,
// including drivers swallow does not write: the provisioner implements them, and a failing read is
// a connectivity problem the live power query reports, not a missing driver.
func PowerControlOf(driver PowerDriver) PowerControl {
	switch driver {
	case PowerDriverNone:
		return PowerControlNone
	case PowerDriverManual:
		return PowerControlManual
	default:
		return PowerControlAutomatic
	}
}

// PowerConfiguration is one machine's Power Configuration as the provisioner holds it: the driver
// and its parameters. The provisioner owns these facts (decision 054); swallow reads them for one
// call and must never persist or log them, and must never return Password through an API.
//
// Address is verbatim — a bare host or URL for a BMC, a libvirt URI for virsh — so callers that
// display it remove URL credentials first. Parameters that do not apply to Driver are empty.
type PowerConfiguration struct {
	Driver PowerDriver
	// Address is the BMC address or the hypervisor URI the provisioner connects to.
	Address string
	// PowerID is the libvirt domain name or UUID of a virsh driver.
	PowerID string
	// Username is a BMC account.
	Username string
	// Password is the driver's secret. It is in memory for one call only.
	Password string
	// NodeID is a BMC's Redfish System hint (MAAS `node_id`), often empty. swallow reads it but
	// does not write it.
	NodeID string
	// ManagedBy names the provisioner object whose own configuration powers this machine (a MAAS
	// VM host), so the machine's configuration is read-only in swallow. Empty otherwise.
	ManagedBy string
}

// RedactPowerAddress returns a Power Configuration address safe to display: a password in the URL,
// the query, and the fragment are removed because they can carry credentials or tokens. A user name
// is kept — for a libvirt URI it is the SSH account the provisioner connects as, which the operator
// needs to see and is not a secret. A value that is not a URL is cut at its first '?' or '#', and
// anything up to its last '@' is dropped, because a bare "user:password@host" cannot be told apart
// from a user name alone.
func RedactPowerAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
		if parsed.User != nil {
			parsed.User = url.User(parsed.User.Username())
		}
		parsed.RawQuery, parsed.ForceQuery, parsed.Fragment = "", false, ""
		return parsed.String()
	}
	address, _, _ = strings.Cut(address, "?")
	address, _, _ = strings.Cut(address, "#")
	if at := strings.LastIndexByte(address, '@'); at >= 0 {
		address = address[at+1:]
	}
	return address
}

// PowerConfigurationChange is an operator's replacement of one machine's Power Configuration.
//
// Every non-secret parameter of Driver is replaced as given, so an empty Username removes the
// account. Password is write-only and three-valued: nil keeps the password the provisioner holds
// when Driver is unchanged and clears it when Driver changes (a previous driver's secret must not
// leak into the new one); a non-nil value replaces it, and a pointer to "" clears it.
type PowerConfigurationChange struct {
	Driver   PowerDriver
	Address  string
	PowerID  string
	Username string
	Password *string
}

var (
	// ErrInvalidPowerConfiguration marks a Power Configuration change that swallow refuses before
	// calling the provisioner: an unsupported driver, or a parameter missing, malformed, or not
	// applicable to the driver. Errors wrapping it carry the operator-facing reason.
	ErrInvalidPowerConfiguration = errors.New("invalid power configuration")
	// ErrPowerConfigurationManaged marks a write to a machine whose power the provisioner takes
	// from another object (a MAAS VM host); that object's configuration must change instead.
	ErrPowerConfigurationManaged = errors.New("the machine's power is managed by its provisioner VM host")
)

// PowerAdapter is the code model of one power driver family (decision 054). It knows the drivers
// of its family and validates an operator's configuration for them; it never switches power, which
// the provisioner does through its own driver.
//
// Functions only some families have are optional extensions reached by a type assertion, such as
// BMCPowerAdapter. Callers ask the adapter for an extension instead of comparing driver names, so
// a new family adds its functions without touching every caller. Implementations are stateless and
// safe for concurrent use.
type PowerAdapter interface {
	// Family is the family this adapter serves.
	Family() PowerFamily
	// Drivers lists the provisioner drivers of the family, in display order.
	Drivers() []PowerDriver
	// Normalize validates change, whose Driver is one of Drivers, and returns its canonical form
	// (trimmed parameters). A refusal wraps ErrInvalidPowerConfiguration with the reason.
	Normalize(change PowerConfigurationChange) (PowerConfigurationChange, error)
}

// BMCAccess is how swallow reaches a BMC out of band for one call, for the Redfish functions that
// swallow drives itself (Boot Media, decision 047). It carries the password in memory only.
type BMCAccess struct {
	Driver     PowerDriver
	Address    string
	Username   string
	Password   string
	SystemHint string
}

// BMCPowerAdapter is the extension of the bmc family: a machine with a BMC can be reached out of
// band, independently of how the provisioner switches its power. A virsh machine's adapter does not
// implement it, which is how callers learn that the machine has no BMC.
type BMCPowerAdapter interface {
	PowerAdapter
	// BMCAccess returns the out-of-band access config describes, or false when config holds no
	// BMC address.
	BMCAccess(config PowerConfiguration) (BMCAccess, bool)
}

// PowerDriverOption is one driver an operator may choose when writing a Power Configuration.
type PowerDriverOption struct {
	Driver PowerDriver
	Family PowerFamily
}

// PowerAdapterRegistry resolves a driver to the adapter of its family. It is immutable after
// construction and safe for concurrent use.
type PowerAdapterRegistry struct {
	adapters []PowerAdapter
	byDriver map[PowerDriver]PowerAdapter
}

// NewPowerAdapterRegistry registers adapters in order. A driver claimed by two adapters belongs to
// the first, so the order is also the precedence.
func NewPowerAdapterRegistry(adapters ...PowerAdapter) *PowerAdapterRegistry {
	registry := &PowerAdapterRegistry{byDriver: map[PowerDriver]PowerAdapter{}}
	for _, adapter := range adapters {
		registry.adapters = append(registry.adapters, adapter)
		for _, driver := range adapter.Drivers() {
			if _, claimed := registry.byDriver[driver]; !claimed {
				registry.byDriver[driver] = adapter
			}
		}
	}
	return registry
}

// DefaultPowerAdapters is the registry of every family swallow supports: bmc, then virsh.
func DefaultPowerAdapters() *PowerAdapterRegistry {
	return NewPowerAdapterRegistry(bmcPowerAdapter{}, virshPowerAdapter{})
}

// ForDriver returns the adapter of driver's family, or false for no driver and for a driver no
// registered family knows; such a machine has no family functions and cannot be written.
func (r *PowerAdapterRegistry) ForDriver(driver PowerDriver) (PowerAdapter, bool) {
	adapter, ok := r.byDriver[driver]
	return adapter, ok
}

// BMCAdapter returns the BMC extension that applies to config, or false when the machine has no
// BMC. This is the one place that decides it: the driver's family must implement BMCPowerAdapter,
// and a machine powered through a provisioner VM host (ManagedBy set) is a virtual machine, which
// never has a BMC whatever driver the VM host uses.
func (r *PowerAdapterRegistry) BMCAdapter(config PowerConfiguration) (BMCPowerAdapter, bool) {
	if config.ManagedBy != "" {
		return nil, false
	}
	adapter, known := r.ForDriver(config.Driver)
	if !known {
		return nil, false
	}
	bmc, ok := adapter.(BMCPowerAdapter)
	return bmc, ok
}

// Options lists every driver a write may choose, family by family in registration order.
func (r *PowerAdapterRegistry) Options() []PowerDriverOption {
	var options []PowerDriverOption
	for _, adapter := range r.adapters {
		for _, driver := range adapter.Drivers() {
			if r.byDriver[driver] == adapter {
				options = append(options, PowerDriverOption{Driver: driver, Family: adapter.Family()})
			}
		}
	}
	return options
}

// PowerConfigurationReader reads one machine's Power Configuration live from the provisioner,
// including its password, for in-process callers only: the Power Configuration use case strips the
// password before answering, and Boot Media passes it to the BMC for one call. An adapter that sets
// ProviderCapabilities.PowerConfiguration must implement it.
//
// Implementations return ErrMachineNotFound for an unknown machine, a configuration with an empty
// Driver (and no parameters) for a machine without a driver, a *ProviderError{Kind:
// ProviderErrorAuth} when the integration credential may not read power parameters, and the usual
// unavailable errors otherwise.
type PowerConfigurationReader interface {
	PowerConfiguration(ctx context.Context, machineID string) (*PowerConfiguration, error)
}

// PowerConfigurationWriter writes an operator's Power Configuration through to the provisioner. An
// adapter that sets ProviderCapabilities.PowerConfiguration must implement it.
//
// change has already been normalized by its family's adapter. Implementations apply the Password
// semantics of PowerConfigurationChange, never switch power, and return ErrMachineNotFound,
// ErrPowerConfigurationManaged for a machine powered through a VM host, a *ProviderError{Kind:
// ProviderErrorRejected} carrying the provisioner's explanation when it refuses the configuration,
// ProviderErrorAuth for a credential that may not write power parameters, and the usual
// unavailable errors otherwise.
type PowerConfigurationWriter interface {
	SetPowerConfiguration(ctx context.Context, machineID string, change PowerConfigurationChange) error
}
