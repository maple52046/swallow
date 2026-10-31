package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// subnetProvider is a provisioner holding a subnet catalog. capable switches the advertised
// capability; writes records every subnet id and server list it was given, which then become the
// subnet's DNSServers the way a provider applies them.
type subnetProvider struct {
	provisioningdomain.OSProvisioningProvider
	capable  bool
	subnets  []provisioningdomain.NetworkSubnet
	attached map[string]bool
	writes   []string
	setErr   error
}

func (p *subnetProvider) AttachedSubnetIDs(context.Context) (map[string]bool, error) {
	return p.attached, nil
}

func (p *subnetProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{SubnetDNS: p.capable}
}

func (p *subnetProvider) ListNetworkSubnets(context.Context) ([]provisioningdomain.NetworkSubnet, error) {
	return append([]provisioningdomain.NetworkSubnet(nil), p.subnets...), nil
}

func (p *subnetProvider) InspectNetwork(context.Context, string) (*provisioningdomain.MachineNetwork, error) {
	return nil, errors.New("not used")
}

func (p *subnetProvider) ConfigureNetworkLink(context.Context, string, provisioningdomain.NetworkLinkRequest) (*provisioningdomain.MachineNetwork, error) {
	return nil, errors.New("not used")
}

func (p *subnetProvider) UnlinkNetwork(context.Context, string, string, string) (*provisioningdomain.MachineNetwork, error) {
	return nil, errors.New("not used")
}

func (p *subnetProvider) SetSubnetDNSServers(_ context.Context, subnetID string, servers []string) error {
	if p.setErr != nil {
		return p.setErr
	}
	p.writes = append(p.writes, subnetID+"="+strings.Join(servers, ","))
	for index := range p.subnets {
		if p.subnets[index].ID == subnetID {
			p.subnets[index].DNSServers = servers
		}
	}
	return nil
}

func siteSubnets() []provisioningdomain.NetworkSubnet {
	return []provisioningdomain.NetworkSubnet{
		{ID: "1", Name: "site", CIDR: "10.170.168.0/23", GatewayAddress: "10.170.169.254", Managed: true},
		{ID: "2", Name: "maas", CIDR: "192.168.100.0/24", Managed: true, ProvisionerDHCP: true, DNSServers: []string{"192.168.100.1"}},
	}
}

// The list reports every subnet in the provisioner's order with an empty list, never null, for a
// subnet without DNS servers, and whether each is attached.
func TestSubnetDNSList(t *testing.T) {
	uc := NewSubnetDNSUseCase(fixedProviders{provider: &subnetProvider{capable: true, subnets: siteSubnets(), attached: map[string]bool{"1": true}}})
	list, err := uc.List(context.Background(), "maas-a")
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if list.IntegrationID != "maas-a" || len(list.Subnets) != 2 {
		t.Fatalf("List = %+v, want two subnets of maas-a", list)
	}
	site, maas := list.Subnets[0], list.Subnets[1]
	if site.ID != "1" || site.ProvisionerDHCP || !site.Attached || site.DNSServers == nil || len(site.DNSServers) != 0 {
		t.Errorf("site subnet = %+v, want attached, no provisioner DHCP, and an empty, non-nil DNS list", site)
	}
	if !maas.ProvisionerDHCP || maas.Attached || strings.Join(maas.DNSServers, ",") != "192.168.100.1" {
		t.Errorf("MAAS subnet = %+v, want provisioner DHCP, not attached per the reader, and its DNS server", maas)
	}
}

// A write is normalized, written through once, and reported as read back from the provisioner.
func TestSubnetDNSSetDNSServers(t *testing.T) {
	provider := &subnetProvider{capable: true, subnets: siteSubnets(), attached: map[string]bool{"1": true}}
	uc := NewSubnetDNSUseCase(fixedProviders{provider: provider})
	item, err := uc.SetDNSServers(context.Background(), "maas-a", "1", []string{" 165.204.124.158", "165.204.219.249"})
	if err != nil {
		t.Fatalf("SetDNSServers error = %v", err)
	}
	if strings.Join(provider.writes, ";") != "1=165.204.124.158,165.204.219.249" {
		t.Errorf("writes = %v, want one normalized write to subnet 1", provider.writes)
	}
	if item.ID != "1" || !item.Attached || strings.Join(item.DNSServers, ",") != "165.204.124.158,165.204.219.249" {
		t.Errorf("SetDNSServers = %+v, want attached subnet 1 with the written servers", item)
	}

	item, err = uc.SetDNSServers(context.Background(), "maas-a", "1", []string{})
	if err != nil || item.DNSServers == nil || len(item.DNSServers) != 0 || provider.writes[1] != "1=" {
		t.Errorf("clearing = %+v, %v, writes %v; want an empty list written and read back", item, err, provider.writes)
	}
}

// Refusals happen before any write: an invalid list before the provisioner is even resolved, an
// unknown subnet after reading the catalog, and a provisioner without the capability.
func TestSubnetDNSSetDNSServersRefusals(t *testing.T) {
	provider := &subnetProvider{capable: true, subnets: siteSubnets()}
	uc := NewSubnetDNSUseCase(fixedProviders{provider: provider})
	if _, err := uc.SetDNSServers(context.Background(), "maas-a", "1", []string{"dns.example.com"}); !errors.Is(err, provisioningdomain.ErrInvalidSubnetDNS) {
		t.Errorf("invalid server error = %v, want ErrInvalidSubnetDNS", err)
	}
	if _, err := uc.SetDNSServers(context.Background(), "maas-a", "99", []string{"10.0.0.53"}); !errors.Is(err, provisioningdomain.ErrSubnetNotFound) {
		t.Errorf("unknown subnet error = %v, want ErrSubnetNotFound", err)
	}
	if len(provider.writes) != 0 {
		t.Errorf("writes = %v, want none after refusals", provider.writes)
	}

	unsupportedUC := NewSubnetDNSUseCase(fixedProviders{provider: &subnetProvider{subnets: siteSubnets()}})
	var provErr *provisioningdomain.ProviderError
	if _, err := unsupportedUC.List(context.Background(), "maas-a"); !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("List without the capability error = %v, want ProviderErrorRejected", err)
	}
	if _, err := unsupportedUC.SetDNSServers(context.Background(), "maas-a", "1", nil); !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("SetDNSServers without the capability error = %v, want ProviderErrorRejected", err)
	}
}
