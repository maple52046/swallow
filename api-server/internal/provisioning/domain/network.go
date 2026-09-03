package domain

import (
	"context"
	"errors"
)

// DeploymentNetworkMode is Swallow's writable OS deployment addressing intent.
type DeploymentNetworkMode string

const (
	// DeploymentNetworkDHCP explicitly requests dynamic addressing.
	DeploymentNetworkDHCP DeploymentNetworkMode = "dhcp"
	// DeploymentNetworkStatic requires a target-specific IP address.
	DeploymentNetworkStatic DeploymentNetworkMode = "static"
)

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

// NetworkLinkMode describes an explicit NIC-level mutation. LinkOnly is available
// for manual configuration but never as an OS deployment mode.
type NetworkLinkMode string

const (
	NetworkLinkDHCP     NetworkLinkMode = "dhcp"
	NetworkLinkStatic   NetworkLinkMode = "static"
	NetworkLinkLinkOnly NetworkLinkMode = "link_only"
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
