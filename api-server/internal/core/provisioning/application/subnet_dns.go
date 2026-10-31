package application

import (
	"context"
	"errors"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// SubnetItem is one provisioner subnet as the Subnet DNS API reports it (provisioner-subnet-dns.md,
// decisions 058 and 071). The subnet's own facts (name, CIDR, gateway, management, DHCP, and
// whether machines are attached) are the provisioner's, read live; its DNS servers are a
// swallow-owned setting with a generation, a spec, and a status. Attached says whether machines
// are actually wired to it (SubnetAttachmentReader); the rest are host-internal networks.
type SubnetItem struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	CIDR            string              `json:"cidr"`
	GatewayAddress  string              `json:"gatewayAddress"`
	Managed         bool                `json:"managed"`
	ProvisionerDHCP bool                `json:"provisionerDhcp"`
	Attached        bool                `json:"attached"`
	Generation      int64               `json:"generation"`
	Spec            SubnetDNSSpecItem   `json:"spec"`
	Status          SubnetDNSStatusItem `json:"status"`
}

// SubnetDNSSpecItem is the DNS servers swallow wants on the subnet; never nil on the wire.
type SubnetDNSSpecItem struct {
	DNSServers []string `json:"dnsServers"`
}

// SubnetDNSStatusItem is the published status: the Subnet DNS controller's judgement and the DNS
// servers the provisioner reports now (read with the subnet list).
type SubnetDNSStatusItem struct {
	ObservedGeneration int64                       `json:"observedGeneration"`
	Conditions         []convergence.ConditionItem `json:"conditions"`
	DNSServers         []string                    `json:"dnsServers"`
}

// SubnetListItem is a provisioner Integration's subnet catalog, in the provisioner's order.
type SubnetListItem struct {
	IntegrationID string       `json:"integrationId"`
	Subnets       []SubnetItem `json:"subnets"`
}

// SubnetDNSUseCase lists the subnets of one provisioner Integration and writes a subnet's Subnet
// DNS as swallow-owned desired state (decision 071).
//
// The subnet catalog is read live, because the provisioner owns its subnets. Each subnet's DNS
// servers are a stored setting: a subnet listed without one is adopted from this read, which
// never writes to the provisioner, and a write stores the spec for the Subnet DNS controller in the
// worker to realize. The setting belongs to a provisioner subnet, not to a Server, so no Server Lock
// applies. Authorization (admin) is enforced by the route.
type SubnetDNSUseCase struct {
	providers provisioningdomain.ProviderFactory
	settings  provisioningdomain.SubnetDNSStore
	now       func() time.Time
}

// NewSubnetDNSUseCase wires the use case to the factory that resolves an Integration's provider and
// the setting store.
func NewSubnetDNSUseCase(providers provisioningdomain.ProviderFactory, settings provisioningdomain.SubnetDNSStore) *SubnetDNSUseCase {
	return &SubnetDNSUseCase{providers: providers, settings: settings, now: func() time.Time { return time.Now().UTC() }}
}

// List returns the Integration's subnets read live, each with its DNS setting. A provisioner
// without the capability is refused as unsupported; the factory's errors (unknown Integration,
// not a provisioner, no credential) and provider errors pass through for the shared mapping.
func (uc *SubnetDNSUseCase) List(ctx context.Context, integrationID string) (*SubnetListItem, error) {
	ports, err := subnetDNSPortsFor(ctx, uc.providers, integrationID)
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
	stored, err := uc.settings.ListByIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	bySubnet := make(map[string]*provisioningdomain.SubnetDNSSetting, len(stored))
	for _, setting := range stored {
		bySubnet[setting.SubnetID] = setting
	}
	items := make([]SubnetItem, 0, len(subnets))
	for _, subnet := range subnets {
		setting, ok := bySubnet[subnet.ID]
		if !ok {
			if setting, err = uc.adopt(ctx, integrationID, subnet); err != nil {
				return nil, err
			}
		}
		items = append(items, toSubnetItem(subnet, attached[subnet.ID], setting))
	}
	return &SubnetListItem{IntegrationID: integrationID, Subnets: items}, nil
}

// SetDNSServers validates servers and stores them as the subnet's spec, returning the subnet with
// its setting. An invalid list is ErrInvalidSubnetDNS before the provisioner is resolved. A subnet
// the provisioner does not list is ErrSubnetNotFound, so a mistyped id never becomes a setting. The
// provisioner is not written.
func (uc *SubnetDNSUseCase) SetDNSServers(ctx context.Context, integrationID, subnetID string, servers []string) (*SubnetItem, error) {
	normalized, err := provisioningdomain.NormalizeSubnetDNSServers(servers)
	if err != nil {
		return nil, err
	}
	ports, err := subnetDNSPortsFor(ctx, uc.providers, integrationID)
	if err != nil {
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
	setting, err := uc.settings.PutSpec(ctx, integrationID, subnetID, normalized)
	if err != nil {
		return nil, err
	}
	item := toSubnetItem(*subnet, attached[subnet.ID], setting)
	return &item, nil
}

// adopt stores the subnet's current DNS servers as its setting, or returns the one another
// adopter stored first.
func (uc *SubnetDNSUseCase) adopt(ctx context.Context, integrationID string, subnet provisioningdomain.NetworkSubnet) (*provisioningdomain.SubnetDNSSetting, error) {
	setting := AdoptedSubnetDNS(integrationID, subnet, uc.now())
	adopted, err := uc.settings.Adopt(ctx, setting)
	if err != nil {
		return nil, err
	}
	if adopted {
		return setting, nil
	}
	setting, err = uc.settings.Get(ctx, integrationID, subnet.ID)
	if errors.Is(err, provisioningdomain.ErrSettingNotFound) {
		return AdoptedSubnetDNS(integrationID, subnet, uc.now()), nil
	}
	return setting, err
}

// AdoptedSubnetDNS is the setting the first observation of a subnet adopts: its current DNS
// servers as the spec at generation 1, realized by definition.
func AdoptedSubnetDNS(integrationID string, subnet provisioningdomain.NetworkSubnet, now time.Time) *provisioningdomain.SubnetDNSSetting {
	servers := append([]string(nil), subnet.DNSServers...)
	return &provisioningdomain.SubnetDNSSetting{
		IntegrationID: integrationID, SubnetID: subnet.ID, Generation: 1, Servers: servers,
		Status: provisioningdomain.SubnetDNSStatus{
			ObservedGeneration: 1, Conditions: adoptedConditions(now), Observed: servers, ObservedAt: &now,
		},
	}
}

// subnetDNSPorts are the three provider ports the Subnet DNS capability is made of.
type subnetDNSPorts struct {
	lister      provisioningdomain.NetworkConfigurationProvider
	writer      provisioningdomain.SubnetDNSWriter
	attachments provisioningdomain.SubnetAttachmentReader
}

// subnetDNSPortsFor returns the Integration's subnet catalog, Subnet DNS writer, and attachment
// reader, or the unsupported refusal when its provider does not advertise the capability and
// implement all three.
func subnetDNSPortsFor(ctx context.Context, providers provisioningdomain.ProviderFactory, integrationID string) (subnetDNSPorts, error) {
	provider, err := providers.For(ctx, integrationID)
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

// toSubnetItem shapes a provisioner subnet and its setting into the API item, with empty lists for
// no servers.
func toSubnetItem(subnet provisioningdomain.NetworkSubnet, attached bool, setting *provisioningdomain.SubnetDNSSetting) SubnetItem {
	return SubnetItem{
		ID:              subnet.ID,
		Name:            subnet.Name,
		CIDR:            subnet.CIDR,
		GatewayAddress:  subnet.GatewayAddress,
		Managed:         subnet.Managed,
		ProvisionerDHCP: subnet.ProvisionerDHCP,
		Attached:        attached,
		Generation:      setting.Generation,
		Spec:            SubnetDNSSpecItem{DNSServers: append([]string{}, setting.Servers...)},
		Status: SubnetDNSStatusItem{
			ObservedGeneration: setting.Status.ObservedGeneration,
			Conditions:         convergence.ConditionItems(setting.Status.Conditions),
			DNSServers:         append([]string{}, subnet.DNSServers...),
		},
	}
}
