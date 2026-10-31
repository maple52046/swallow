package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	"github.com/maple52046/swallow/internal/core/provisioning/domain/settingstest"
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

// The list reports every subnet in the provisioner's order, adopting each subnet's DNS servers as
// its spec, with empty lists (never null) for a subnet without DNS servers.
func TestSubnetDNSList(t *testing.T) {
	provider := &subnetProvider{capable: true, subnets: siteSubnets(), attached: map[string]bool{"1": true}}
	settings := settingstest.NewSubnetDNSStore()
	uc := NewSubnetDNSUseCase(fixedProviders{provider: provider}, settings)
	list, err := uc.List(context.Background(), "maas-a")
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if list.IntegrationID != "maas-a" || len(list.Subnets) != 2 {
		t.Fatalf("List = %+v, want two subnets of maas-a", list)
	}
	site, maas := list.Subnets[0], list.Subnets[1]
	if site.ID != "1" || site.ProvisionerDHCP || !site.Attached || site.Spec.DNSServers == nil || len(site.Spec.DNSServers) != 0 || site.Status.DNSServers == nil {
		t.Errorf("site subnet = %+v, want attached, no provisioner DHCP, and empty, non-nil DNS lists", site)
	}
	if !maas.ProvisionerDHCP || maas.Generation != 1 || strings.Join(maas.Spec.DNSServers, ",") != "192.168.100.1" {
		t.Errorf("MAAS subnet = %+v, want its DNS server adopted at generation 1", maas)
	}
	if stored, _ := settings.List(context.Background()); len(stored) != 2 || len(provider.writes) != 0 {
		t.Errorf("settings %d, writes %v; want both subnets adopted without a write", len(stored), provider.writes)
	}
}

// A write is normalized and stored as the spec; the provisioner is not called, and the status
// still reports what the provisioner holds now.
func TestSubnetDNSSetDNSServers(t *testing.T) {
	provider := &subnetProvider{capable: true, subnets: siteSubnets(), attached: map[string]bool{"1": true}}
	uc := NewSubnetDNSUseCase(fixedProviders{provider: provider}, settingstest.NewSubnetDNSStore())
	item, err := uc.SetDNSServers(context.Background(), "maas-a", "1", []string{" 165.204.124.158", "165.204.219.249"})
	if err != nil {
		t.Fatalf("SetDNSServers error = %v", err)
	}
	if len(provider.writes) != 0 {
		t.Errorf("writes = %v, want none: the controller writes the provisioner", provider.writes)
	}
	if item.ID != "1" || !item.Attached || item.Generation != 1 || strings.Join(item.Spec.DNSServers, ",") != "165.204.124.158,165.204.219.249" || len(item.Status.DNSServers) != 0 {
		t.Errorf("SetDNSServers = %+v, want the normalized spec and the provisioner's current (empty) list", item)
	}
}

// Refusals happen before any write: an invalid list before the provisioner is even resolved, an
// unknown subnet after reading the catalog, and a provisioner without the capability.
func TestSubnetDNSSetDNSServersRefusals(t *testing.T) {
	provider := &subnetProvider{capable: true, subnets: siteSubnets()}
	settings := settingstest.NewSubnetDNSStore()
	uc := NewSubnetDNSUseCase(fixedProviders{provider: provider}, settings)
	if _, err := uc.SetDNSServers(context.Background(), "maas-a", "1", []string{"dns.example.com"}); !errors.Is(err, provisioningdomain.ErrInvalidSubnetDNS) {
		t.Errorf("invalid server error = %v, want ErrInvalidSubnetDNS", err)
	}
	if _, err := uc.SetDNSServers(context.Background(), "maas-a", "99", []string{"10.0.0.53"}); !errors.Is(err, provisioningdomain.ErrSubnetNotFound) {
		t.Errorf("unknown subnet error = %v, want ErrSubnetNotFound", err)
	}
	if stored, _ := settings.List(context.Background()); len(stored) != 0 {
		t.Errorf("settings = %d, want none after refusals", len(stored))
	}

	unsupportedUC := NewSubnetDNSUseCase(fixedProviders{provider: &subnetProvider{subnets: siteSubnets()}}, settings)
	var provErr *provisioningdomain.ProviderError
	if _, err := unsupportedUC.List(context.Background(), "maas-a"); !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("List without the capability error = %v, want ProviderErrorRejected", err)
	}
	if _, err := unsupportedUC.SetDNSServers(context.Background(), "maas-a", "1", nil); !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("SetDNSServers without the capability error = %v, want ProviderErrorRejected", err)
	}
}

// The controller adopts every subnet, converges a spec change at once, corrects drift at resync,
// and drops the setting of a subnet the provisioner no longer has.
func TestSubnetDNSController(t *testing.T) {
	provider := &subnetProvider{capable: true, subnets: siteSubnets()}
	settings := settingstest.NewSubnetDNSStore()
	controller := NewSubnetDNSController(fixedIntegrations{"maas-a"}, fixedProviders{provider: provider}, settings, nil)
	reconcile := func(key string, trigger convergence.Trigger) {
		t.Helper()
		if err := controller.Reconcile(context.Background(), convergence.Request{Key: key, Trigger: trigger}); err != nil {
			t.Fatalf("reconcile %s: %v", key, err)
		}
	}
	reconcile("subnets:maas-a", convergence.TriggerResync)
	if stored, _ := settings.List(context.Background()); len(stored) != 2 || len(provider.writes) != 0 {
		t.Fatalf("adopted %d settings with writes %v, want 2 and none", len(stored), provider.writes)
	}

	if _, err := settings.PutSpec(context.Background(), "maas-a", "1", []string{"10.0.0.53"}); err != nil {
		t.Fatal(err)
	}
	reconcile("subnet-dns:maas-a/1", convergence.TriggerChange)
	if strings.Join(provider.writes, ";") != "1=10.0.0.53" {
		t.Fatalf("writes = %v, want subnet 1 written at once", provider.writes)
	}

	provider.subnets[0].DNSServers = []string{"8.8.8.8"}
	reconcile("subnet-dns:maas-a/1", convergence.TriggerChange)
	setting, _ := settings.Get(context.Background(), "maas-a", "1")
	if !convergence.IsTrue(setting.Status.Conditions, convergence.ConditionDrifted) || len(provider.writes) != 1 {
		t.Fatalf("drift on a change trigger: %+v, writes %v", setting.Status, provider.writes)
	}
	reconcile("subnet-dns:maas-a/1", convergence.TriggerResync)
	if len(provider.writes) != 2 || strings.Join(provider.subnets[0].DNSServers, ",") != "10.0.0.53" {
		t.Fatalf("after resync: writes %v, subnet %v", provider.writes, provider.subnets[0].DNSServers)
	}

	provider.subnets = provider.subnets[1:]
	reconcile("subnets:maas-a", convergence.TriggerResync)
	if _, err := settings.Get(context.Background(), "maas-a", "1"); !errors.Is(err, provisioningdomain.ErrSettingNotFound) {
		t.Errorf("the setting of a removed subnet must go, got %v", err)
	}
}
