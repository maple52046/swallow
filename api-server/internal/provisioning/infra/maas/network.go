package maas

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// InspectNetwork reads live machine interfaces and the provider subnets compatible
// with their VLANs. Nothing from this view is persisted by Swallow.
func (p *Provider) InspectNetwork(ctx context.Context, machineID string) (*provisioningdomain.MachineNetwork, error) {
	var machine machineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &machine); err != nil {
		return nil, translateError(err, machineID)
	}

	subnets, err := p.listNetworkSubnets(ctx)
	if err != nil {
		return nil, err
	}
	return toDomainNetwork(&machine, subnets), nil
}

// ListNetworkSubnets returns the live provider subnet catalog in Swallow language.
func (p *Provider) ListNetworkSubnets(ctx context.Context) ([]provisioningdomain.NetworkSubnet, error) {
	subnets, err := p.listNetworkSubnets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]provisioningdomain.NetworkSubnet, 0, len(subnets))
	for _, subnet := range subnets {
		out = append(out, provisioningdomain.NetworkSubnet{
			ID:             strconv.Itoa(subnet.ID),
			Name:           subnet.Name,
			CIDR:           subnet.CIDR,
			GatewayAddress: subnet.GatewayIP,
			Managed:        subnet.Managed,
		})
	}
	return out, nil
}

func (p *Provider) listNetworkSubnets(ctx context.Context) ([]subnetJSON, error) {
	var subnets []subnetJSON
	if err := p.client.get(ctx, "/subnets/", nil, &subnets); err != nil {
		return nil, translateError(err, "")
	}
	return subnets, nil
}

// ConfigureNetworkLink creates or replaces exactly one subnet link and verifies
// the result by reading the Machine again. A replacement deliberately unlinks only
// req.LinkID; MAAS force semantics are never used.
func (p *Provider) ConfigureNetworkLink(
	ctx context.Context,
	machineID string,
	req provisioningdomain.NetworkLinkRequest,
) (*provisioningdomain.MachineNetwork, error) {
	if strings.TrimSpace(req.InterfaceID) == "" || strings.TrimSpace(req.SubnetID) == "" {
		return nil, fmt.Errorf("%w: interfaceId and subnetId are required", provisioningdomain.ErrInvalidNetworkConfiguration)
	}
	mode, err := maasNetworkMode(req)
	if err != nil {
		return nil, err
	}
	if req.Mode == provisioningdomain.NetworkLinkStatic {
		address, parseErr := netip.ParseAddr(strings.TrimSpace(req.IPAddress))
		if parseErr != nil || !address.Is4() {
			return nil, fmt.Errorf("%w: enter a valid IPv4 address for static mode", provisioningdomain.ErrInvalidNetworkConfiguration)
		}
		req.IPAddress = address.String()
	} else if strings.TrimSpace(req.IPAddress) != "" {
		return nil, fmt.Errorf("%w: ipAddress is valid only for static mode", provisioningdomain.ErrInvalidNetworkConfiguration)
	}
	if req.Mode != provisioningdomain.NetworkLinkStatic && req.DefaultGateway {
		return nil, fmt.Errorf("%w: defaultGateway is supported only for static mode", provisioningdomain.ErrInvalidNetworkConfiguration)
	}

	before, err := p.InspectNetwork(ctx, machineID)
	if err != nil {
		return nil, err
	}
	iface, link, err := findNetworkSelection(before, req.InterfaceID, req.LinkID, req.SubnetID)
	if err != nil {
		return nil, err
	}
	if err := validateAddressInSubnet(iface, req); err != nil {
		return nil, err
	}
	if req.LinkID != "" && networkLinkMatches(link, req) {
		return before, nil
	}
	if req.LinkID != "" {
		if err := p.unlinkNetworkLink(ctx, machineID, iface.ID, req.LinkID); err != nil {
			return nil, err
		}
	}

	fields := map[string]string{
		"mode":   mode,
		"subnet": req.SubnetID,
	}
	if req.Mode == provisioningdomain.NetworkLinkStatic {
		if strings.TrimSpace(req.IPAddress) == "" {
			return nil, fmt.Errorf("%w: ipAddress is required for static mode", provisioningdomain.ErrInvalidNetworkConfiguration)
		}
		fields["ip_address"] = strings.TrimSpace(req.IPAddress)
	}
	if req.DefaultGateway {
		fields["default_gateway"] = "true"
	}

	if err := p.client.postOperation(
		ctx,
		networkInterfacePath(machineID, req.InterfaceID),
		"link_subnet",
		fields,
		nil,
	); err != nil {
		// A mutation-level 404 may identify the interface, subnet link, or requested
		// address. Preserve MAAS's response instead of claiming the Machine is gone.
		return nil, translateError(err, "")
	}

	after, err := p.InspectNetwork(ctx, machineID)
	if err != nil {
		return nil, err
	}
	if !networkContains(after, req) {
		return nil, &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: "MAAS accepted the network request but did not report the requested configuration.",
		}
	}
	return after, nil
}

// UnlinkNetwork removes one addressed subnet link and verifies that it is gone.
func (p *Provider) UnlinkNetwork(
	ctx context.Context,
	machineID, interfaceID, linkID string,
) (*provisioningdomain.MachineNetwork, error) {
	before, err := p.InspectNetwork(ctx, machineID)
	if err != nil {
		return nil, err
	}
	if _, _, err := findNetworkSelection(before, interfaceID, linkID, ""); err != nil {
		return nil, err
	}
	if err := p.unlinkNetworkLink(ctx, machineID, interfaceID, linkID); err != nil {
		return nil, err
	}
	after, err := p.InspectNetwork(ctx, machineID)
	if err != nil {
		return nil, err
	}
	if networkHasLink(after, interfaceID, linkID) {
		return nil, &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: "MAAS accepted the unlink request but still reports the subnet link.",
		}
	}
	return after, nil
}

func (p *Provider) unlinkNetworkLink(ctx context.Context, machineID, interfaceID, linkID string) error {
	err := p.client.postOperation(
		ctx,
		networkInterfacePath(machineID, interfaceID),
		"unlink_subnet",
		map[string]string{"id": linkID},
		nil,
	)
	if err != nil {
		// Preserve the network resource named by MAAS; the preceding live Machine read
		// is the boundary that owns ErrMachineNotFound.
		return translateError(err, "")
	}
	return nil
}

func networkInterfacePath(machineID, interfaceID string) string {
	return "/nodes/" + url.PathEscape(machineID) + "/interfaces/" + url.PathEscape(interfaceID) + "/"
}

func maasNetworkMode(req provisioningdomain.NetworkLinkRequest) (string, error) {
	switch req.Mode {
	case provisioningdomain.NetworkLinkDHCP:
		return "DHCP", nil
	case provisioningdomain.NetworkLinkStatic:
		return "STATIC", nil
	case provisioningdomain.NetworkLinkLinkOnly:
		return "LINK_UP", nil
	case provisioningdomain.NetworkLinkAuto:
		// MAAS "AUTO" allocates a static IP from the subnet's reserved space and records
		// it, so the deployed address is stable and always known to MAAS.
		return "AUTO", nil
	default:
		return "", fmt.Errorf("%w: mode must be automatic, dhcp, static, or link_only", provisioningdomain.ErrInvalidNetworkConfiguration)
	}
}

func findNetworkSelection(
	network *provisioningdomain.MachineNetwork,
	interfaceID, linkID, subnetID string,
) (*provisioningdomain.NetworkInterface, *provisioningdomain.NetworkLink, error) {
	for index := range network.Interfaces {
		iface := &network.Interfaces[index]
		if iface.ID != interfaceID {
			continue
		}
		if subnetID != "" {
			found := false
			for _, subnet := range iface.AvailableSubnets {
				if subnet.ID == subnetID {
					found = true
					break
				}
			}
			if !found {
				return nil, nil, fmt.Errorf("%w: subnet is not available on the selected interface", provisioningdomain.ErrInvalidNetworkConfiguration)
			}
		}
		if linkID == "" {
			return iface, nil, nil
		}
		for linkIndex := range iface.Links {
			if iface.Links[linkIndex].ID == linkID {
				return iface, &iface.Links[linkIndex], nil
			}
		}
		return nil, nil, fmt.Errorf("%w: network link not found", provisioningdomain.ErrInvalidNetworkConfiguration)
	}
	return nil, nil, fmt.Errorf("%w: network interface not found", provisioningdomain.ErrInvalidNetworkConfiguration)
}

func networkLinkMatches(link *provisioningdomain.NetworkLink, req provisioningdomain.NetworkLinkRequest) bool {
	if link == nil || link.SubnetID != req.SubnetID || link.DefaultGateway != req.DefaultGateway {
		return false
	}
	switch req.Mode {
	case provisioningdomain.NetworkLinkDHCP:
		return link.State == provisioningdomain.NetworkStateDHCP
	case provisioningdomain.NetworkLinkStatic:
		return link.State == provisioningdomain.NetworkStateStatic &&
			link.IPAddress == strings.TrimSpace(req.IPAddress)
	case provisioningdomain.NetworkLinkLinkOnly:
		return link.State == provisioningdomain.NetworkStateLinkOnly
	case provisioningdomain.NetworkLinkAuto:
		// A MAAS AUTO link is observed as provider-managed; the specific IP is chosen by
		// MAAS, so an existing auto link on the same subnet already satisfies the request.
		return link.State == provisioningdomain.NetworkStateProviderManaged
	default:
		return false
	}
}

func networkContains(network *provisioningdomain.MachineNetwork, req provisioningdomain.NetworkLinkRequest) bool {
	for _, iface := range network.Interfaces {
		if iface.ID != req.InterfaceID {
			continue
		}
		for index := range iface.Links {
			link := &iface.Links[index]
			if link.SubnetID == req.SubnetID && networkLinkMatches(link, req) {
				return true
			}
		}
	}
	return false
}

func networkHasLink(network *provisioningdomain.MachineNetwork, interfaceID, linkID string) bool {
	for _, iface := range network.Interfaces {
		if iface.ID != interfaceID {
			continue
		}
		for _, link := range iface.Links {
			if link.ID == linkID {
				return true
			}
		}
	}
	return false
}

func toDomainNetwork(machine *machineJSON, subnets []subnetJSON) *provisioningdomain.MachineNetwork {
	out := &provisioningdomain.MachineNetwork{
		MachineID:  machine.SystemID,
		Interfaces: make([]provisioningdomain.NetworkInterface, 0, len(machine.InterfaceSet)),
	}
	bootID := 0
	if machine.BootInterface != nil {
		bootID = machine.BootInterface.ID
	}
	gatewayLinkID := 0
	if machine.GatewayLinkIPv4 != nil {
		gatewayLinkID = machine.GatewayLinkIPv4.ID
	}

	for _, source := range machine.InterfaceSet {
		iface := provisioningdomain.NetworkInterface{
			ID:            strconv.Itoa(source.ID),
			Name:          source.Name,
			MACAddress:    source.MACAddress,
			Boot:          source.ID == bootID,
			PhysicalState: physicalLinkState(source.LinkConnected),
			Links:         make([]provisioningdomain.NetworkLink, 0, len(source.Links)),
		}
		for _, sourceLink := range source.Links {
			link := provisioningdomain.NetworkLink{
				ID:             strconv.Itoa(sourceLink.ID),
				State:          networkState(sourceLink.Mode),
				ProviderMode:   strings.ToUpper(strings.TrimSpace(sourceLink.Mode)),
				IPAddress:      sourceLink.IPAddress,
				DefaultGateway: sourceLink.ID == gatewayLinkID,
			}
			if sourceLink.Subnet != nil {
				link.SubnetID = strconv.Itoa(sourceLink.Subnet.ID)
				link.SubnetName = sourceLink.Subnet.Name
				link.CIDR = sourceLink.Subnet.CIDR
			}
			iface.Links = append(iface.Links, link)
		}
		iface.State, iface.ProviderMode = interfaceConfigurationState(iface.Links)
		for _, subnet := range subnets {
			if source.VLAN == nil || subnet.VLAN == nil || source.VLAN.ID != subnet.VLAN.ID {
				continue
			}
			iface.AvailableSubnets = append(iface.AvailableSubnets, provisioningdomain.NetworkSubnet{
				ID:             strconv.Itoa(subnet.ID),
				Name:           subnet.Name,
				CIDR:           subnet.CIDR,
				GatewayAddress: subnet.GatewayIP,
				Managed:        subnet.Managed,
			})
		}
		out.Interfaces = append(out.Interfaces, iface)
	}
	return out
}

func networkState(mode string) provisioningdomain.NetworkConfigurationState {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "dhcp":
		return provisioningdomain.NetworkStateDHCP
	case "static":
		return provisioningdomain.NetworkStateStatic
	case "link_up":
		return provisioningdomain.NetworkStateLinkOnly
	case "auto":
		return provisioningdomain.NetworkStateProviderManaged
	case "":
		return provisioningdomain.NetworkStateUnconfigured
	default:
		return provisioningdomain.NetworkStateUnknown
	}
}

func interfaceConfigurationState(links []provisioningdomain.NetworkLink) (provisioningdomain.NetworkConfigurationState, string) {
	if len(links) == 0 {
		return provisioningdomain.NetworkStateUnconfigured, ""
	}
	state := links[0].State
	providerMode := links[0].ProviderMode
	for _, link := range links[1:] {
		if link.State != state {
			return provisioningdomain.NetworkStateUnknown, ""
		}
		if link.ProviderMode != providerMode {
			providerMode = ""
		}
	}
	return state, providerMode
}

func physicalLinkState(value *bool) provisioningdomain.PhysicalLinkState {
	if value == nil {
		return provisioningdomain.PhysicalLinkUnknown
	}
	if *value {
		return provisioningdomain.PhysicalLinkUp
	}
	return provisioningdomain.PhysicalLinkDown
}

func validateAddressInSubnet(
	iface *provisioningdomain.NetworkInterface,
	req provisioningdomain.NetworkLinkRequest,
) error {
	if req.Mode != provisioningdomain.NetworkLinkStatic {
		return nil
	}
	address, err := netip.ParseAddr(req.IPAddress)
	if err != nil {
		return fmt.Errorf("%w: enter a valid IPv4 address for static mode", provisioningdomain.ErrInvalidNetworkConfiguration)
	}
	for _, subnet := range iface.AvailableSubnets {
		if subnet.ID != req.SubnetID || subnet.CIDR == "" {
			continue
		}
		prefix, prefixErr := netip.ParsePrefix(subnet.CIDR)
		if prefixErr == nil && !prefix.Contains(address) {
			return fmt.Errorf("%w: the static IP is outside the selected subnet", provisioningdomain.ErrInvalidNetworkConfiguration)
		}
		return nil
	}
	return nil
}
