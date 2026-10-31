package domain

import (
	"context"
	"errors"
)

// DeploymentNetworkMode is Swallow's writable OS deployment addressing intent (glossary
// Network Configuration), a closed set of three values.
//
// The intent is provider-neutral: "let the provisioner assign and record an address", "let
// the network's own DHCP server assign it", or "use a caller-chosen address". How the
// provider realizes each is an adapter concern — the MAAS adapter fulfils Automatic with
// MAAS "auto-assign" (a provider-managed static IP recorded by MAAS, ADR 018) and DHCP with
// a MAAS DHCP link (ADR 058). The two differ on a subnet whose DHCP the provisioner does not
// serve: auto-assign allocates without knowing the site DHCP pool, while DHCP takes the
// site's address, gateway, and DNS.
type DeploymentNetworkMode string

const (
	// DeploymentNetworkAutomatic asks the provisioner to allocate and record a stable address.
	// It is the default and canonical value.
	DeploymentNetworkAutomatic DeploymentNetworkMode = "automatic"
	// DeploymentNetworkDHCP asks the network's own DHCP server to assign the address, gateway,
	// and DNS. The provider records only the address it observes, so on a site DHCP network
	// the address Swallow reports may be the one last seen rather than the current lease.
	DeploymentNetworkDHCP DeploymentNetworkMode = "dhcp"
	// DeploymentNetworkStatic requires a target-specific IP address.
	DeploymentNetworkStatic DeploymentNetworkMode = "static"
)

// NormalizeDeploymentNetworkMode resolves a raw, possibly-empty mode string to a canonical
// DeploymentNetworkMode. Empty becomes Automatic, the default; "automatic", "dhcp", and
// "static" are kept; anything else returns ok=false so the caller can reject it with its own
// bounded error. "dhcp" was an alias of Automatic between ADR 018 and ADR 058 and is now its
// own mode, so a stored "dhcp" now means DHCP.
func NormalizeDeploymentNetworkMode(raw string) (DeploymentNetworkMode, bool) {
	switch DeploymentNetworkMode(raw) {
	case "", DeploymentNetworkAutomatic:
		return DeploymentNetworkAutomatic, true
	case DeploymentNetworkDHCP:
		return DeploymentNetworkDHCP, true
	case DeploymentNetworkStatic:
		return DeploymentNetworkStatic, true
	default:
		return "", false
	}
}

// LinkMode is the NIC link mutation that realizes the deployment intent: provider
// auto-assign for Automatic, a DHCP link for DHCP, and a static link for Static. It is defined
// only for a mode NormalizeDeploymentNetworkMode accepted and returns ok=false otherwise, so a
// caller never configures a NIC from an unvalidated value.
func (m DeploymentNetworkMode) LinkMode() (NetworkLinkMode, bool) {
	switch m {
	case DeploymentNetworkAutomatic:
		return NetworkLinkAuto, true
	case DeploymentNetworkDHCP:
		return NetworkLinkDHCP, true
	case DeploymentNetworkStatic:
		return NetworkLinkStatic, true
	default:
		return "", false
	}
}

// NetworkConfigurationState describes an observed NIC subnet-link configuration.
// ProviderManaged is intentionally read-only: it represents a legacy provider mode
// that is not part of Swallow's deployment language.
type NetworkConfigurationState string

const (
	NetworkStateDHCP            NetworkConfigurationState = "dhcp"
	NetworkStateStatic          NetworkConfigurationState = "static"
	NetworkStateLinkOnly        NetworkConfigurationState = "link_only"
	NetworkStateUnconfigured    NetworkConfigurationState = "unconfigured"
	NetworkStateProviderManaged NetworkConfigurationState = "provider_managed"
	NetworkStateUnknown         NetworkConfigurationState = "unknown"
)

// PhysicalLinkState is independent from addressing configuration. Unknown is used
// when a provider cannot distinguish carrier state from administrative enablement.
type PhysicalLinkState string

const (
	PhysicalLinkUp      PhysicalLinkState = "up"
	PhysicalLinkDown    PhysicalLinkState = "down"
	PhysicalLinkUnknown PhysicalLinkState = "unknown"
)

// NetworkSubnet is a live provider-owned subnet reference available to one NIC, or one entry
// of a provisioner's subnet catalog. Swallow never stores it.
type NetworkSubnet struct {
	ID             string
	Name           string
	CIDR           string
	GatewayAddress string
	Managed        bool
	// ProvisionerDHCP reports that the provisioner itself serves DHCP on the subnet. False
	// means any DHCP there is the site's, which is when deployment suggests the DHCP mode.
	ProvisionerDHCP bool
	// DNSServers is the subnet's Subnet DNS in the provider's order, canonical IP strings;
	// empty when the subnet has none and the provisioner's own DNS applies (ADR 058).
	DNSServers []string
}

// NetworkLink is one provider-owned subnet association on a NIC.
type NetworkLink struct {
	ID             string
	State          NetworkConfigurationState
	ProviderMode   string
	SubnetID       string
	SubnetName     string
	CIDR           string
	IPAddress      string
	DefaultGateway bool
}

// NetworkInterface is a typed live NIC observation used by policy and presentation.
type NetworkInterface struct {
	ID               string
	Name             string
	MACAddress       string
	Boot             bool
	PhysicalState    PhysicalLinkState
	State            NetworkConfigurationState
	ProviderMode     string
	Links            []NetworkLink
	AvailableSubnets []NetworkSubnet
}

// MachineNetwork is the complete live network view needed for one Server workflow.
type MachineNetwork struct {
	MachineID  string
	Interfaces []NetworkInterface
}

// NetworkLinkMode describes an explicit NIC-level link mutation the adapter must perform.
// LinkOnly is available for manual configuration but never as an OS deployment mode; Auto is
// how Swallow's Automatic deployment intent is realized (provider auto-assign) and is not
// currently offered as a manual NIC mode; DHCP realizes the DHCP deployment intent and is
// also a manual NIC mode.
type NetworkLinkMode string

const (
	NetworkLinkDHCP     NetworkLinkMode = "dhcp"
	NetworkLinkStatic   NetworkLinkMode = "static"
	NetworkLinkLinkOnly NetworkLinkMode = "link_only"
	// NetworkLinkAuto requests provider auto-assign: the provider allocates and records a
	// stable address (MAAS "AUTO"), which the deployed OS receives as a fixed netplan
	// address. Realizes DeploymentNetworkAutomatic.
	NetworkLinkAuto NetworkLinkMode = "auto"
)

// NetworkLinkRequest addresses one interface and optionally one existing link.
// A non-empty LinkID means replace exactly that link; unrelated links are retained.
type NetworkLinkRequest struct {
	InterfaceID    string
	LinkID         string
	Mode           NetworkLinkMode
	SubnetID       string
	IPAddress      string
	DefaultGateway bool
}

// NetworkConfigurationProvider is the narrow typed boundary for provider network
// reads and writes. Implementations must never use a provider force option and must
// return the state observed after verifying a mutation.
type NetworkConfigurationProvider interface {
	ListNetworkSubnets(ctx context.Context) ([]NetworkSubnet, error)
	InspectNetwork(ctx context.Context, machineID string) (*MachineNetwork, error)
	ConfigureNetworkLink(ctx context.Context, machineID string, req NetworkLinkRequest) (*MachineNetwork, error)
	UnlinkNetwork(ctx context.Context, machineID, interfaceID, linkID string) (*MachineNetwork, error)
}

var (
	// ErrNetworkConfigurationUnsupported means the selected adapter cannot honour
	// Swallow's network intent and therefore cannot safely dispatch deployment.
	ErrNetworkConfigurationUnsupported = errors.New("provider does not support network configuration")
	// ErrInvalidNetworkConfiguration marks malformed mode, subnet, interface, or IP intent.
	ErrInvalidNetworkConfiguration = errors.New("invalid network configuration")
	// ErrNetworkConfigurationConflict marks a Server state or ambiguous choice that
	// must be resolved before a provider write.
	ErrNetworkConfigurationConflict = errors.New("network configuration conflict")
)
