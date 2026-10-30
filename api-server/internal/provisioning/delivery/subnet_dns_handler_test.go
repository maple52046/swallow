package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/apicontract"
)

// subnetHandlerProvider holds one site subnet and replaces its DNS servers on write.
type subnetHandlerProvider struct {
	provisioningdomain.OSProvisioningProvider
	subnet provisioningdomain.NetworkSubnet
	writes int
}

func (p *subnetHandlerProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{SubnetDNS: true}
}

func (p *subnetHandlerProvider) ListNetworkSubnets(context.Context) ([]provisioningdomain.NetworkSubnet, error) {
	return []provisioningdomain.NetworkSubnet{p.subnet}, nil
}

func (p *subnetHandlerProvider) InspectNetwork(context.Context, string) (*provisioningdomain.MachineNetwork, error) {
	return nil, errors.New("not used")
}

func (p *subnetHandlerProvider) ConfigureNetworkLink(context.Context, string, provisioningdomain.NetworkLinkRequest) (*provisioningdomain.MachineNetwork, error) {
	return nil, errors.New("not used")
}

func (p *subnetHandlerProvider) UnlinkNetwork(context.Context, string, string, string) (*provisioningdomain.MachineNetwork, error) {
	return nil, errors.New("not used")
}

func (p *subnetHandlerProvider) AttachedSubnetIDs(context.Context) (map[string]bool, error) {
	return map[string]bool{p.subnet.ID: true}, nil
}

func (p *subnetHandlerProvider) SetSubnetDNSServers(_ context.Context, _ string, servers []string) error {
	p.subnet.DNSServers = servers
	p.writes++
	return nil
}

// newSubnetHandlerApp mounts the handlers at their production paths so the contract validator
// matches each response to its documented operation.
func newSubnetHandlerApp(t *testing.T, provider *subnetHandlerProvider) *fiber.App {
	t.Helper()
	handler := &ProvisioningHandler{}
	handler.AttachSubnetDNS(application.NewSubnetDNSUseCase(proxyHandlerProviders{provider: provider}))
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	app.Get("/api/v1/provisioning/integrations/:id/subnets", handler.ListSubnets)
	app.Put("/api/v1/provisioning/integrations/:id/subnets/:subnetId/dns-servers", handler.SetSubnetDNSServers)
	return app
}

func doSubnetRequest(t *testing.T, app *fiber.App, method, path, body string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp, decoded
}

func siteHandlerSubnet() provisioningdomain.NetworkSubnet {
	return provisioningdomain.NetworkSubnet{ID: "1", Name: "site", CIDR: "10.170.168.0/23", GatewayAddress: "10.170.169.254", Managed: true}
}

// The list is uncached and shaped by provisioner-subnet-dns.md: an empty DNS list is [], not null,
// and an unknown Integration is 404.
func TestListSubnets(t *testing.T) {
	app := newSubnetHandlerApp(t, &subnetHandlerProvider{subnet: siteHandlerSubnet()})
	resp, body := doSubnetRequest(t, app, http.MethodGet, "/api/v1/provisioning/integrations/maas-a/subnets", "")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status = %d, cache = %q; want 200 no-store", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	subnets, _ := body["subnets"].([]any)
	if body["integrationId"] != "maas-a" || len(subnets) != 1 {
		t.Fatalf("body = %v, want one subnet of maas-a", body)
	}
	subnet, _ := subnets[0].(map[string]any)
	servers, isList := subnet["dnsServers"].([]any)
	if subnet["cidr"] != "10.170.168.0/23" || subnet["provisionerDhcp"] != false || subnet["attached"] != true || !isList || len(servers) != 0 {
		t.Errorf("subnet = %v, want the attached site subnet without provisioner DHCP and dnsServers []", subnet)
	}

	resp, _ = doSubnetRequest(t, app, http.MethodGet, "/api/v1/provisioning/integrations/other/subnets", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown Integration status = %d, want 404", resp.StatusCode)
	}
}

// The write answers with the subnet read back; a missing field, an invalid address, and an
// unknown subnet are refused with the contract's codes and nothing written.
func TestSetSubnetDNSServers(t *testing.T) {
	provider := &subnetHandlerProvider{subnet: siteHandlerSubnet()}
	app := newSubnetHandlerApp(t, provider)
	path := "/api/v1/provisioning/integrations/maas-a/subnets/1/dns-servers"

	resp, body := doSubnetRequest(t, app, http.MethodPut, path, `{"dnsServers":["165.204.124.158","165.204.219.249"]}`)
	servers, _ := body["dnsServers"].([]any)
	if resp.StatusCode != http.StatusOK || len(servers) != 2 || servers[0] != "165.204.124.158" {
		t.Fatalf("write = %d %v, want 200 with both servers", resp.StatusCode, body)
	}

	for _, test := range []struct {
		name, path, body string
		status           int
	}{
		{name: "missing field", path: path, body: `{}`, status: http.StatusBadRequest},
		{name: "not an array", path: path, body: `{"dnsServers":"10.0.0.53"}`, status: http.StatusBadRequest},
		{name: "not an address", path: path, body: `{"dnsServers":["dns.example.com"]}`, status: http.StatusBadRequest},
		{name: "unknown subnet", path: "/api/v1/provisioning/integrations/maas-a/subnets/99/dns-servers", body: `{"dnsServers":[]}`, status: http.StatusNotFound},
	} {
		resp, body := doSubnetRequest(t, app, http.MethodPut, test.path, test.body)
		if resp.StatusCode != test.status {
			t.Errorf("%s: status = %d (%v), want %d", test.name, resp.StatusCode, body, test.status)
		}
	}
	if provider.writes != 1 {
		t.Errorf("writes = %d, want only the accepted one", provider.writes)
	}
}
