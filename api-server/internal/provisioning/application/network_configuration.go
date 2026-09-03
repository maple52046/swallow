package application

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

const networkInspectionWorkers = 4

// NetworkSuggestion is Swallow's non-authoritative deployment default derived
// from live provider state. Empty identifiers mean operator input is required.
type NetworkSuggestion struct {
	Mode           provisioningdomain.DeploymentNetworkMode
	InterfaceID    string
	SubnetID       string
	IPAddress      string
	DefaultGateway bool
}

// NetworkTarget is one Server's typed live Network Configuration plus edit policy.
type NetworkTarget struct {
	ServerID       string
	Editable       bool
	DisabledReason string
	Suggestion     NetworkSuggestion
	Network        *provisioningdomain.MachineNetwork
}

// NetworkInspectionResult preserves input order and carries actionable target issues.
type NetworkInspectionResult struct {
	Targets []NetworkTarget
	Issues  []DeploymentTargetIssue
}

// NetworkConfigurationService owns provider-neutral NIC policy while delegating
// provider vocabulary and transport to NetworkConfigurationProvider.
type NetworkConfigurationService struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
}

// NewNetworkConfigurationService creates the typed Server-network workflow.
func NewNetworkConfigurationService(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
) *NetworkConfigurationService {
	return &NetworkConfigurationService{servers: servers, providers: providers}
}

// Get reads one Server's live network without requiring it to be mutable.
func (s *NetworkConfigurationService) Get(ctx context.Context, serverID string) (*NetworkTarget, error) {
	server, networkProvider, err := s.resolve(ctx, serverID)
	if err != nil {
		return nil, err
	}
	network, err := networkProvider.InspectNetwork(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}
	target := makeNetworkTarget(server, network)
	return &target, nil
}

// Inspect reads up to one hundred same-Integration targets with bounded concurrency.
func (s *NetworkConfigurationService) Inspect(
	ctx context.Context,
	serverIDs []string,
) (*NetworkInspectionResult, error) {
	if len(serverIDs) == 0 || len(serverIDs) > maxDeploymentTargets {
		return nil, fmt.Errorf("%w: serverIds must contain between 1 and %d targets",
			provisioningdomain.ErrInvalidDeploymentBatch, maxDeploymentTargets)
	}
	seen := make(map[string]struct{}, len(serverIDs))
	servers := make([]*serverdomain.Server, len(serverIDs))
	integrationID := ""
	issues := make([]DeploymentTargetIssue, 0)
	for index, serverID := range serverIDs {
		if strings.TrimSpace(serverID) == "" {
			return nil, fmt.Errorf("%w: serverIds cannot contain an empty ID", provisioningdomain.ErrInvalidDeploymentBatch)
		}
		if _, duplicate := seen[serverID]; duplicate {
			return nil, fmt.Errorf("%w: serverIds must be unique", provisioningdomain.ErrInvalidDeploymentBatch)
		}
		seen[serverID] = struct{}{}
		server, err := s.servers.FindByID(ctx, serverID)
		if err != nil {
			return nil, err
		}
		servers[index] = server
		if integrationID == "" {
			integrationID = server.Source.IntegrationID
		} else if server.Source.IntegrationID != integrationID {
			issues = append(issues, DeploymentTargetIssue{
				ServerID: serverID,
				Code:     "integration_mismatch",
				Message:  "All targets must belong to the same provisioner integration.",
			})
		}
	}
	if len(issues) > 0 {
		return &NetworkInspectionResult{Targets: []NetworkTarget{}, Issues: issues}, nil
	}

	provider, err := s.providers.For(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	networkProvider, err := requireNetworkProvider(provider)
	if err != nil {
		return nil, err
	}

	type outcome struct {
		network *provisioningdomain.MachineNetwork
		err     error
	}
	outcomes := make([]outcome, len(servers))
	jobs := make(chan int)
	workerCount := networkInspectionWorkers
	if len(servers) < workerCount {
		workerCount = len(servers)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				outcomes[index].network, outcomes[index].err =
					networkProvider.InspectNetwork(ctx, servers[index].Source.ProviderMachineID)
			}
		}()
	}
	for index := range servers {
		jobs <- index
	}
	close(jobs)
	workers.Wait()

	result := &NetworkInspectionResult{
		Targets: make([]NetworkTarget, 0, len(servers)),
		Issues:  make([]DeploymentTargetIssue, 0),
	}
	for index, outcome := range outcomes {
		if outcome.err != nil {
			return nil, outcome.err
		}
		target := makeNetworkTarget(servers[index], outcome.network)
		result.Targets = append(result.Targets, target)
		if target.Suggestion.InterfaceID == "" {
			result.Issues = append(result.Issues, DeploymentTargetIssue{
				ServerID: servers[index].ID,
				Code:     "network_interface_required",
				Message:  "Select a network interface before deployment.",
			})
		} else if target.Suggestion.SubnetID == "" {
			result.Issues = append(result.Issues, DeploymentTargetIssue{
				ServerID: servers[index].ID,
				Code:     "network_selection_required",
				Message:  "Select a compatible subnet before deployment.",
			})
		}
	}
	return result, nil
}

// CreateLink adds one explicit subnet link after enforcing Ready-only policy.
func (s *NetworkConfigurationService) CreateLink(
	ctx context.Context,
	serverID string,
	req provisioningdomain.NetworkLinkRequest,
) (*NetworkTarget, error) {
	req.LinkID = ""
	return s.configure(ctx, serverID, req)
}

// ReplaceLink replaces exactly one addressed subnet link.
func (s *NetworkConfigurationService) ReplaceLink(
	ctx context.Context,
	serverID string,
	req provisioningdomain.NetworkLinkRequest,
) (*NetworkTarget, error) {
	if strings.TrimSpace(req.LinkID) == "" {
		return nil, fmt.Errorf("%w: linkId is required", provisioningdomain.ErrInvalidNetworkConfiguration)
	}
	return s.configure(ctx, serverID, req)
}

// DeleteLink removes exactly one addressed subnet link.
func (s *NetworkConfigurationService) DeleteLink(
	ctx context.Context,
	serverID, interfaceID, linkID string,
) (*NetworkTarget, error) {
	server, networkProvider, err := s.resolveMutable(ctx, serverID)
	if err != nil {
		return nil, err
	}
	network, err := networkProvider.UnlinkNetwork(
		ctx, server.Source.ProviderMachineID, interfaceID, linkID)
	if err != nil {
		return nil, err
	}
	if err := s.refreshAddresses(ctx, server); err != nil {
		return nil, err
	}
	target := makeNetworkTarget(server, network)
	return &target, nil
}

func (s *NetworkConfigurationService) configure(
	ctx context.Context,
	serverID string,
	req provisioningdomain.NetworkLinkRequest,
) (*NetworkTarget, error) {
	if err := validateNetworkLinkRequest(&req); err != nil {
		return nil, err
	}
	server, networkProvider, err := s.resolveMutable(ctx, serverID)
	if err != nil {
		return nil, err
	}
	network, err := networkProvider.ConfigureNetworkLink(
		ctx, server.Source.ProviderMachineID, req)
	if err != nil {
		return nil, err
	}
	if err := s.refreshAddresses(ctx, server); err != nil {
		return nil, err
	}
	target := makeNetworkTarget(server, network)
	return &target, nil
}

func (s *NetworkConfigurationService) resolve(
	ctx context.Context,
	serverID string,
) (*serverdomain.Server, provisioningdomain.NetworkConfigurationProvider, error) {
	server, err := s.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, nil, err
	}
	networkProvider, err := requireNetworkProvider(provider)
	if err != nil {
		return nil, nil, err
	}
	return server, networkProvider, nil
}

func (s *NetworkConfigurationService) resolveMutable(
	ctx context.Context,
	serverID string,
) (*serverdomain.Server, provisioningdomain.NetworkConfigurationProvider, error) {
	server, networkProvider, err := s.resolve(ctx, serverID)
	if err != nil {
		return nil, nil, err
	}
	if reason := networkDisabledReason(server); reason != "" {
		return nil, nil, fmt.Errorf("%w: %s", provisioningdomain.ErrNetworkConfigurationConflict, reason)
	}
	return server, networkProvider, nil
}

func requireNetworkProvider(
	provider provisioningdomain.OSProvisioningProvider,
) (provisioningdomain.NetworkConfigurationProvider, error) {
	if !provider.Capabilities().NetworkConfiguration {
		return nil, provisioningdomain.ErrNetworkConfigurationUnsupported
	}
	networkProvider, ok := provider.(provisioningdomain.NetworkConfigurationProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q advertises network configuration without implementing it", provider.Name())
	}
	return networkProvider, nil
}

func makeNetworkTarget(
	server *serverdomain.Server,
	network *provisioningdomain.MachineNetwork,
) NetworkTarget {
	reason := networkDisabledReason(server)
	return NetworkTarget{
		ServerID:       server.ID,
		Editable:       reason == "",
		DisabledReason: reason,
		Suggestion:     suggestNetwork(network),
		Network:        network,
	}
}

func networkDisabledReason(server *serverdomain.Server) string {
	switch {
	case server.Absent:
		return "The Server is absent from its provisioner."
	case server.Provisioning == nil || server.Provisioning.State != string(provisioningdomain.MachineStatusReady):
		return "Network configuration is available only while the Server is Ready."
	case server.Provisioning.Locked:
		return "Unlock the Server before changing network configuration."
	default:
		return ""
	}
}

func suggestNetwork(network *provisioningdomain.MachineNetwork) NetworkSuggestion {
	suggestion := NetworkSuggestion{Mode: provisioningdomain.DeploymentNetworkDHCP}
	var selected *provisioningdomain.NetworkInterface
	for index := range network.Interfaces {
		if network.Interfaces[index].Boot {
			selected = &network.Interfaces[index]
			break
		}
	}
	if selected == nil && len(network.Interfaces) == 1 {
		selected = &network.Interfaces[0]
	}
	if selected == nil {
		return suggestion
	}
	suggestion.InterfaceID = selected.ID

	staticLinks := make([]provisioningdomain.NetworkLink, 0, 1)
	for _, link := range selected.Links {
		if link.State == provisioningdomain.NetworkStateStatic {
			staticLinks = append(staticLinks, link)
		}
	}
	if len(staticLinks) > 0 {
		suggestion.Mode = provisioningdomain.DeploymentNetworkStatic
	}
	if len(staticLinks) == 1 {
		suggestion.SubnetID = staticLinks[0].SubnetID
		suggestion.IPAddress = staticLinks[0].IPAddress
		suggestion.DefaultGateway = staticLinks[0].DefaultGateway
		return suggestion
	}
	if len(staticLinks) > 1 {
		return suggestion
	}
	subnets := make(map[string]struct{})
	for _, link := range selected.Links {
		if link.SubnetID != "" {
			subnets[link.SubnetID] = struct{}{}
		}
	}
	if len(subnets) == 1 {
		for subnetID := range subnets {
			suggestion.SubnetID = subnetID
		}
		return suggestion
	}
	managed := ""
	for _, subnet := range selected.AvailableSubnets {
		if !subnet.Managed {
			continue
		}
		if managed != "" {
			return suggestion
		}
		managed = subnet.ID
	}
	suggestion.SubnetID = managed
	return suggestion
}

func (s *NetworkConfigurationService) refreshAddresses(
	ctx context.Context,
	server *serverdomain.Server,
) error {
	provider, err := s.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return err
	}
	machine, err := provider.GetMachine(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return err
	}
	server.Observed.Addresses = append([]string(nil), machine.IPAddresses...)
	server.UpdatedAt = time.Now().UTC()
	return s.servers.Upsert(ctx, server)
}

func validateNetworkLinkRequest(req *provisioningdomain.NetworkLinkRequest) error {
	req.InterfaceID = strings.TrimSpace(req.InterfaceID)
	req.LinkID = strings.TrimSpace(req.LinkID)
	req.SubnetID = strings.TrimSpace(req.SubnetID)
	req.IPAddress = strings.TrimSpace(req.IPAddress)
	if req.InterfaceID == "" || req.SubnetID == "" {
		return fmt.Errorf("%w: interfaceId and subnetId are required", provisioningdomain.ErrInvalidNetworkConfiguration)
	}
	switch req.Mode {
	case provisioningdomain.NetworkLinkDHCP:
		if req.IPAddress != "" {
			return fmt.Errorf("%w: ipAddress is valid only for static mode", provisioningdomain.ErrInvalidNetworkConfiguration)
		}
		if req.DefaultGateway {
			return fmt.Errorf("%w: defaultGateway is supported only for static mode", provisioningdomain.ErrInvalidNetworkConfiguration)
		}
	case provisioningdomain.NetworkLinkStatic:
		address, err := netip.ParseAddr(req.IPAddress)
		if err != nil || !address.Is4() {
			return fmt.Errorf("%w: enter a valid IPv4 address for static mode", provisioningdomain.ErrInvalidNetworkConfiguration)
		}
		req.IPAddress = address.String()
	case provisioningdomain.NetworkLinkLinkOnly:
		if req.IPAddress != "" || req.DefaultGateway {
			return fmt.Errorf("%w: link_only cannot set an IP address or default gateway", provisioningdomain.ErrInvalidNetworkConfiguration)
		}
	default:
		return fmt.Errorf("%w: mode must be dhcp, static, or link_only", provisioningdomain.ErrInvalidNetworkConfiguration)
	}
	return nil
}
