package maas

import (
	"context"
	"errors"
	"net/http"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

var _ provisioningdomain.MachineRegistrar = (*Provider)(nil)

// The machine create sends every MAC as its own mac_addresses part, the virsh power settings, and
// commission=false, so swallow's inspection owns commissioning.
func TestRegisterMachine_CreatesWithoutCommissioning(t *testing.T) {
	fake := newFakeMAAS(t)
	var fields map[string][]string
	fake.mux.HandleFunc("POST "+apiPrefix+"/machines/{$}", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			fields = r.MultipartForm.Value
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"system_id":"new123"}`))
	})
	id, err := newTestProvider(t, fake).RegisterMachine(context.Background(), provisioningdomain.MachineRegistration{
		Hostname: "lab-afde-mi308-1", Architecture: "amd64", MACAddresses: []string{"52:54:00:af:de:01", "52:54:00:af:de:02"},
		Power: provisioningdomain.PowerConfigurationChange{Driver: provisioningdomain.PowerDriverVirsh,
			Address: "qemu+ssh://ubuntu@10.170.168.22/system", PowerID: "lab-afde-mi308-1"},
	})
	if err != nil || id != "new123" {
		t.Fatalf("RegisterMachine = %q, %v; want new123", id, err)
	}
	want := map[string]string{
		"hostname": "lab-afde-mi308-1", "architecture": "amd64/generic", "commission": "false", "power_type": "virsh",
		"power_parameters_power_address": "qemu+ssh://ubuntu@10.170.168.22/system", "power_parameters_power_id": "lab-afde-mi308-1",
	}
	for name, value := range want {
		if got := fields[name]; len(got) != 1 || got[0] != value {
			t.Errorf("field %s = %v, want %q", name, got, value)
		}
	}
	if macs := fields["mac_addresses"]; len(macs) != 2 {
		t.Errorf("mac_addresses = %v, want both MACs as separate parts", macs)
	}
}

// A refused create carries MAAS's reason as a rejection.
func TestRegisterMachine_Refused(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.mux.HandleFunc("POST "+apiPrefix+"/machines/{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"hostname": ["Node with this Hostname already exists."]}`))
	})
	_, err := newTestProvider(t, fake).RegisterMachine(context.Background(), provisioningdomain.MachineRegistration{
		Hostname: "dup", Architecture: "amd64", MACAddresses: []string{"52:54:00:00:00:01"},
		Power: provisioningdomain.PowerConfigurationChange{Driver: provisioningdomain.PowerDriverVirsh, Address: "qemu+ssh://h/system", PowerID: "dup"},
	})
	var providerErr *provisioningdomain.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Fatalf("RegisterMachine error = %v, want a rejection", err)
	}
}

// The MAC lookup asks MAAS for any of the addresses and returns the first match.
func TestFindMachineByMAC(t *testing.T) {
	fake := newFakeMAAS(t)
	var asked []string
	fake.mux.HandleFunc("GET "+apiPrefix+"/machines/{$}", func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query()["mac_address"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"system_id":"abc123"}]`))
	})
	id, err := newTestProvider(t, fake).FindMachineByMAC(context.Background(), []string{"52:54:00:AF:DE:01", " "})
	if err != nil || id != "abc123" {
		t.Fatalf("FindMachineByMAC = %q, %v; want abc123", id, err)
	}
	if len(asked) != 1 || asked[0] != "52:54:00:af:de:01" {
		t.Errorf("asked for %v, want the normalized MAC", asked)
	}
}
