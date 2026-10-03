package maas

import (
	"context"
	"errors"
	"net/http"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// onMachineAndPowerParameters answers the machine object and its power_parameters operation,
// which share one path and differ only by the op query parameter.
func (f *fakeMAAS) onMachineAndPowerParameters(machine string, paramsStatus int, params string) {
	f.mux.HandleFunc("GET "+apiPrefix+"/machines/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("op") == "power_parameters" {
			w.WriteHeader(paramsStatus)
			_, _ = w.Write([]byte(params))
			return
		}
		_, _ = w.Write([]byte(machine))
	})
}

func TestBMCConnection_ReadsAllowlistedPowerParameters(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onMachineAndPowerParameters(`{"system_id":"abc123","power_type":"ipmi","pod":null}`, http.StatusOK,
		`{"power_address":"10.170.168.230","power_user":"maas","power_pass":"s3cret","k_g":"must-not-be-read","node_id":""}`)
	connection, err := newTestProvider(t, fake).BMCConnection(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("BMCConnection: %v", err)
	}
	want := provisioningdomain.BMCConnection{PowerType: "ipmi", Address: "10.170.168.230", Username: "maas", Password: "s3cret"}
	if *connection != want {
		t.Errorf("BMCConnection = %+v, want %+v", *connection, want)
	}
}

func TestBMCConnection_MachinesWithoutBMC(t *testing.T) {
	cases := []struct {
		name, machine, params string
	}{
		{"virtual machine", `{"power_type":"virsh","pod":{"name":"lab-host"}}`, `{"power_address":"qemu+ssh://host"}`},
		{"no power driver", `{"power_type":"","pod":null}`, `{}`},
		{"no address", `{"power_type":"redfish","pod":null}`, `{"power_user":"admin"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeMAAS(t)
			fake.onMachineAndPowerParameters(tc.machine, http.StatusOK, tc.params)
			if _, err := newTestProvider(t, fake).BMCConnection(context.Background(), "abc123"); !errors.Is(err, provisioningdomain.ErrMachineHasNoBMC) {
				t.Errorf("BMCConnection error = %v, want ErrMachineHasNoBMC", err)
			}
		})
	}
}

func TestBMCConnection_NonAdminCredentialIsAuthError(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onMachineAndPowerParameters(`{"power_type":"ipmi","pod":null}`, http.StatusForbidden, `forbidden`)
	_, err := newTestProvider(t, fake).BMCConnection(context.Background(), "abc123")
	var providerErr *provisioningdomain.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != provisioningdomain.ProviderErrorAuth {
		t.Errorf("BMCConnection error = %v, want a ProviderErrorAuth", err)
	}
}
