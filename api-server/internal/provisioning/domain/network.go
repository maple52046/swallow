package domain

import (
	"context"
	"errors"
)

// DeploymentNetworkMode is Swallow's writable OS deployment addressing intent.
//
// The intent is provider-neutral: "let the provisioner assign an address" versus "use a
// caller-chosen address". How the provider realizes automatic addressing is an adapter
// concern — the MAAS adapter fulfils it with MAAS "auto-assign" (a provider-managed static
// IP recorded by MAAS), which is stable and always known to the provider, rather than raw
// DHCP whose address is only known while a live lease is observed. See ADR 018.
type DeploymentNetworkMode string

const (
	// DeploymentNetworkAutomatic asks the provisioner to assign an address automatically.
	// It is the default and canonical value.
	DeploymentNetworkAutomatic DeploymentNetworkMode = "automatic"
	// DeploymentNetworkStatic requires a target-specific IP address.
	DeploymentNetworkStatic DeploymentNetworkMode = "static"
	// DeploymentNetworkDHCP is a deprecated one-release alias of DeploymentNetworkAutomatic,
	// kept so existing callers keep working; NormalizeDeploymentNetworkMode folds it into
	// Automatic. It named the provider protocol rather than the swallow intent (see ADR 018).
	DeploymentNetworkDHCP DeploymentNetworkMode = "dhcp"
)

// NormalizeDeploymentNetworkMode resolves a raw, possibly-empty or deprecated mode string to
// a canonical DeploymentNetworkMode. Empty and the deprecated "dhcp" alias both become
// Automatic; "static" stays Static; anything else returns ok=false so the caller can reject
// it with its own bounded error.
func NormalizeDeploymentNetworkMode(raw string) (DeploymentNetworkMode, bool) {
	switch DeploymentNetworkMode(raw) {
	case "", DeploymentNetworkAutomatic, DeploymentNetworkDHCP:
		return DeploymentNetworkAutomatic, true
	case DeploymentNetworkStatic:
		return DeploymentNetworkStatic, true
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

// NetworkSubnet is a live provider-owned subnet reference available to one NIC.
type NetworkSubnet struct {
	ID             string
	Name           string
	CIDR           string
	GatewayAddress string
	Managed        bool
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
// currently offered as a manual NIC mode.
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
