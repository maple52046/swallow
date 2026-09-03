package maas

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

const networkSubnetsJSON = `[
  {"id": 7, "name": "fabric-a", "cidr": "10.20.0.0/24", "gateway_ip": "10.20.0.1", "managed": true, "vlan": {"id": 3}}
]`

func networkMachineJSON(mode, ip string, linkID int) string {
	machine := map[string]any{
		"system_id":      "abc123",
		"boot_interface": map[string]any{"id": 42},
		"interface_set": []any{map[string]any{
			"id": 42, "name": "eno1", "mac_address": "02:00:00:00:00:01",
			"enabled": true, "link_connected": true, "vlan": map[string]any{"id": 3},
			"links": []any{},
		}},
	}
	if mode != "" {
		machine["interface_set"].([]any)[0].(map[string]any)["links"] = []any{map[string]any{
			"id": linkID, "mode": mode, "ip_address": ip,
			"subnet": map[string]any{"id": 7, "name": "fabric-a", "cidr": "10.20.0.0/24"},
		}}
	}
	body, _ := json.Marshal(machine)
	return string(body)
}

func TestInspectNetworkSeparatesProviderModeFromPhysicalLink(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onGetMachine(http.StatusOK, networkMachineJSON("AUTO", "10.20.0.20", 91))
	fake.respond("GET "+apiPrefix+"/subnets/{$}", http.StatusOK, networkSubnetsJSON)

	network, err := newTestProvider(t, fake).InspectNetwork(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("InspectNetwork: %v", err)
	}
	if len(network.Interfaces) != 1 {
		t.Fatalf("interfaces = %d, want 1", len(network.Interfaces))
	}
	iface := network.Interfaces[0]
	if !iface.Boot || iface.PhysicalState != provisioningdomain.PhysicalLinkUp {
		t.Errorf("interface identity/link = %+v", iface)
	}
	if iface.State != provisioningdomain.NetworkStateProviderManaged || iface.ProviderMode != "AUTO" {
		t.Errorf("provider mode translation = %q/%q", iface.State, iface.ProviderMode)
	}
	if len(iface.AvailableSubnets) != 1 || iface.AvailableSubnets[0].ID != "7" {
		t.Errorf("available subnets = %+v", iface.AvailableSubnets)
	}
}

func TestConfigureNetworkLinkReplacesOnlySelectedLinkWithoutForce(t *testing.T) {
	fake := newFakeMAAS(t)
	reads := 0
	fake.mux.HandleFunc("GET "+apiPrefix+"/machines/{id}/{$}", func(w http.ResponseWriter, _ *http.Request) {
		reads++
		w.Header().Set("Content-Type", "application/json")
		if reads == 1 {
			_, _ = w.Write([]byte(networkMachineJSON("STATIC", "10.20.0.30", 91)))
			return
		}
		_, _ = w.Write([]byte(networkMachineJSON("DHCP", "10.20.0.40", 92)))
	})
	fake.respond("GET "+apiPrefix+"/subnets/{$}", http.StatusOK, networkSubnetsJSON)
	operations := make([]string, 0, 2)
	forms := make([]map[string]string, 0, 2)
	fake.mux.HandleFunc("POST "+apiPrefix+"/nodes/{machine}/interfaces/{interface}/{$}", func(w http.ResponseWriter, r *http.Request) {
		operations = append(operations, r.URL.Query().Get("op"))
		_ = r.ParseMultipartForm(1 << 20)
		form := map[string]string{}
		if r.MultipartForm != nil {
			for key, values := range r.MultipartForm.Value {
				if len(values) > 0 {
					form[key] = values[0]
				}
			}
		}
		forms = append(forms, form)
		w.WriteHeader(http.StatusOK)
	})

	_, err := newTestProvider(t, fake).ConfigureNetworkLink(context.Background(), "abc123", provisioningdomain.NetworkLinkRequest{
		InterfaceID: "42", LinkID: "91", Mode: provisioningdomain.NetworkLinkDHCP,
		SubnetID: "7", DefaultGateway: false,
	})
	if err != nil {
		t.Fatalf("ConfigureNetworkLink: %v", err)
	}
	if len(operations) != 2 || operations[0] != "unlink_subnet" || operations[1] != "link_subnet" {
		t.Fatalf("operations = %v, want unlink_subnet then link_subnet", operations)
	}
	if forms[0]["id"] != "91" || forms[1]["mode"] != "DHCP" || forms[1]["subnet"] != "7" {
		t.Errorf("forms = %+v", forms)
	}
	for _, form := range forms {
		if _, exists := form["force"]; exists {
			t.Fatalf("force must never be sent: %+v", form)
		}
	}
}

func TestConfigureNetworkLinkRejectsInvalidStaticBeforeUnlink(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onGetMachine(http.StatusOK, networkMachineJSON("STATIC", "10.20.0.30", 91))
	fake.respond("GET "+apiPrefix+"/subnets/{$}", http.StatusOK, networkSubnetsJSON)
	provider := newTestProvider(t, fake)

	_, err := provider.ConfigureNetworkLink(context.Background(), "abc123", provisioningdomain.NetworkLinkRequest{
		InterfaceID: "42", LinkID: "91", Mode: provisioningdomain.NetworkLinkStatic,
		SubnetID: "7", IPAddress: "not-an-ip",
	})
	if err == nil {
		t.Fatal("expected invalid Static IP to be rejected")
	}
	if fake.lastOperation != "" {
		t.Fatalf("provider mutation occurred before validation: %q", fake.lastOperation)
	}
}

func TestConfigureNetworkLinkPreservesStaticIPAllocationRejection(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onGetMachine(http.StatusOK, networkMachineJSON("", "", 0))
	fake.respond("GET "+apiPrefix+"/subnets/{$}", http.StatusOK, networkSubnetsJSON)
	fake.mux.HandleFunc("POST "+apiPrefix+"/nodes/{machine}/interfaces/{interface}/{$}", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Static IP address 10.20.0.25 is already allocated.", http.StatusNotFound)
	})

	_, err := newTestProvider(t, fake).ConfigureNetworkLink(
		context.Background(),
		"abc123",
		provisioningdomain.NetworkLinkRequest{
			InterfaceID: "42", Mode: provisioningdomain.NetworkLinkStatic,
			SubnetID: "7", IPAddress: "10.20.0.25",
		},
	)

	var providerErr *provisioningdomain.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("ConfigureNetworkLink error = %v, want ProviderError", err)
	}
	if errors.Is(err, provisioningdomain.ErrMachineNotFound) {
		t.Fatalf("ConfigureNetworkLink error = %v, must not claim the Machine is missing", err)
	}
	if providerErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Fatalf("ProviderError kind = %q, want %q", providerErr.Kind, provisioningdomain.ProviderErrorRejected)
	}
	if !strings.Contains(providerErr.Detail, "Static IP address 10.20.0.25 is already allocated") {
		t.Fatalf("ProviderError detail = %q, want the MAAS allocation reason", providerErr.Detail)
	}
}

func TestConfigureNetworkLinkRejectsDefaultGatewayForDHCPBeforeProviderRead(t *testing.T) {
	fake := newFakeMAAS(t)
	provider := newTestProvider(t, fake)

	_, err := provider.ConfigureNetworkLink(
		context.Background(),
		"abc123",
		provisioningdomain.NetworkLinkRequest{
			InterfaceID: "42", Mode: provisioningdomain.NetworkLinkDHCP,
			SubnetID: "7", DefaultGateway: true,
		},
	)
	if err == nil {
		t.Fatal("expected DHCP default gateway to be rejected")
	}
	if fake.lastOperation != "" {
		t.Fatalf("provider request occurred before validation: %q", fake.lastOperation)
	}
}
