package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// SubnetItem is one provisioner subnet as the Subnet DNS API reports it (provisioner-subnet-dns.md,
// decision 058). DNSServers is never nil, so the wire shape is always a list. Attached says whether
// machines are actually wired to it (SubnetAttachmentReader); the rest are host-internal networks.
type SubnetItem struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	CIDR            string   `json:"cidr"`
	GatewayAddress  string   `json:"gatewayAddress"`
	Managed         bool     `json:"managed"`
	ProvisionerDHCP bool     `json:"provisionerDhcp"`
	Attached        bool     `json:"attached"`
	DNSServers      []string `json:"dnsServers"`
}

// SubnetListItem is a provisioner Integration's subnet catalog, in the provisioner's order.
type SubnetListItem struct {
	IntegrationID string       `json:"integrationId"`
	Subnets       []SubnetItem `json:"subnets"`
}

// SubnetDNSUseCase lists the subnets of one provisioner Integration and writes a subnet's
// provisioner-owned Subnet DNS (decision 058).
//
// Both directions are live: the provisioner is the source of truth and swallow stores nothing, so
// a list always reflects the provisioner and a write is read back before it is reported. The
// setting belongs to a provisioner subnet, not to a Server, so no Server Lock applies, and writes
// are not serialized: two concurrent writes to one subnet leave whichever the provisioner applied
// last, which each read-back reports. Authorization (admin) is enforced by the route.
type SubnetDNSUseCase struct {
	providers provisioningdomain.ProviderFactory
}

// NewSubnetDNSUseCase wires the use case to the factory that resolves an Integration's provider.
func NewSubnetDNSUseCase(providers provisioningdomain.ProviderFactory) *SubnetDNSUseCase {
	return &SubnetDNSUseCase{providers: providers}
}

// List returns the Integration's subnets read live from its provisioner. A provisioner without the
// capability is refused as unsupported; the factory's errors (unknown Integration, not a
// provisioner, no credential) and provider errors pass through for the shared mapping.
func (uc *SubnetDNSUseCase) List(ctx context.Context, integrationID string) (*SubnetListItem, error) {
	ports, err := uc.ports(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	subnets, err := ports.lister.ListNetworkSubnets(ctx)
	if err != nil {
		return nil, err
	}
	attached, err := ports.attachments.AttachedSubnetIDs(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]SubnetItem, 0, len(subnets))
	for _, subnet := range subnets {
		items = append(items, toSubnetItem(subnet, attached[subnet.ID]))
	}
	return &SubnetListItem{IntegrationID: integrationID, Subnets: items}, nil
}

// SetDNSServers validates servers, writes them through as the subnet's Subnet DNS, and returns the
// subnet read back. An invalid list is ErrInvalidSubnetDNS before the provisioner is resolved or
// called. A subnet the provisioner does not list is ErrSubnetNotFound, checked before the write so
// a mistyped id never reaches the provider; one that disappears during the write is reported the
// same way. A refusal by the provisioner is its ProviderError.
func (uc *SubnetDNSUseCase) SetDNSServers(ctx context.Context, integrationID, subnetID string, servers []string) (*SubnetItem, error) {
	normalized, err := provisioningdomain.NormalizeSubnetDNSServers(servers)
	if err != nil {
		return nil, err
	}
	ports, err := uc.ports(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if _, err := findSubnet(ctx, ports.lister, subnetID); err != nil {
		return nil, err
	}
	if err := ports.writer.SetSubnetDNSServers(ctx, subnetID, normalized); err != nil {
		return nil, err
	}
	subnet, err := findSubnet(ctx, ports.lister, subnetID)
	if err != nil {
		return nil, err
	}
	attached, err := ports.attachments.AttachedSubnetIDs(ctx)
	if err != nil {
		return nil, err
	}
	item := toSubnetItem(*subnet, attached[subnet.ID])
	return &item, nil
}

// subnetDNSPorts are the three provider ports the Subnet DNS capability is made of.
type subnetDNSPorts struct {
	lister      provisioningdomain.NetworkConfigurationProvider
	writer      provisioningdomain.SubnetDNSWriter
	attachments provisioningdomain.SubnetAttachmentReader
}

// ports returns the Integration's subnet catalog, Subnet DNS writer, and attachment reader, or the
// unsupported refusal when its provider does not advertise the capability and implement all three.
func (uc *SubnetDNSUseCase) ports(ctx context.Context, integrationID string) (subnetDNSPorts, error) {
	provider, err := uc.providers.For(ctx, integrationID)
	if err != nil {
		return subnetDNSPorts{}, err
	}
	lister, canList := provider.(provisioningdomain.NetworkConfigurationProvider)
	writer, canWrite := provider.(provisioningdomain.SubnetDNSWriter)
	attachments, canAttach := provider.(provisioningdomain.SubnetAttachmentReader)
	if !provider.Capabilities().SubnetDNS || !canList || !canWrite || !canAttach {
		return subnetDNSPorts{}, unsupported("DNS servers for its subnets")
	}
	return subnetDNSPorts{lister: lister, writer: writer, attachments: attachments}, nil
}

// findSubnet reads the provisioner's live catalog and returns the subnet with id, or
// ErrSubnetNotFound.
func findSubnet(ctx context.Context, lister provisioningdomain.NetworkConfigurationProvider, id string) (*provisioningdomain.NetworkSubnet, error) {
	subnets, err := lister.ListNetworkSubnets(ctx)
	if err != nil {
		return nil, err
	}
	for index := range subnets {
		if subnets[index].ID == id {
			return &subnets[index], nil
		}
	}
	return nil, provisioningdomain.ErrSubnetNotFound
}

// toSubnetItem shapes a provisioner subnet into the API item, with an empty list for no servers.
func toSubnetItem(subnet provisioningdomain.NetworkSubnet, attached bool) SubnetItem {
	servers := append([]string{}, subnet.DNSServers...)
	return SubnetItem{
		ID:              subnet.ID,
		Name:            subnet.Name,
		CIDR:            subnet.CIDR,
		GatewayAddress:  subnet.GatewayAddress,
		Managed:         subnet.Managed,
		ProvisionerDHCP: subnet.ProvisionerDHCP,
		Attached:        attached,
		DNSServers:      servers,
	}
}
