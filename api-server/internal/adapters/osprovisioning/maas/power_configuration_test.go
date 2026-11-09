package maas

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// Compile-time proof that the adapter offers the Power Configuration capability.
var (
	_ provisioningdomain.PowerConfigurationReader = (*Provider)(nil)
	_ provisioningdomain.PowerConfigurationWriter = (*Provider)(nil)
)

// powerFake serves one machine object and its power_parameters, and records the multipart fields
// of a machine update, empty ones included.
type powerFake struct {
	*fakeMAAS
	mu           sync.Mutex
	paramsReads  atomic.Int32
	updateFields map[string]string
	updates      int
}

func newPowerFake(t *testing.T, machine string, paramsStatus int, params string, updateStatus int, updateBody string) *powerFake {
	t.Helper()
	f := &powerFake{fakeMAAS: newFakeMAAS(t)}
	f.mux.HandleFunc("GET "+apiPrefix+"/machines/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("op") == "power_parameters" {
			f.paramsReads.Add(1)
			w.WriteHeader(paramsStatus)
			_, _ = w.Write([]byte(params))
			return
		}
		_, _ = w.Write([]byte(machine))
	})
	f.mux.HandleFunc("PUT "+apiPrefix+"/machines/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		fields := map[string]string{}
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			for name, values := range r.MultipartForm.Value {
				fields[name] = values[0]
			}
		}
		f.mu.Lock()
		f.updateFields, f.updates = fields, f.updates+1
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(updateStatus)
		_, _ = w.Write([]byte(updateBody))
	})
	return f
}

func TestPowerConfiguration_ReadsDriverAndParameters(t *testing.T) {
	fake := newPowerFake(t, `{"system_id":"abc123","power_type":"virsh","pod":null}`, http.StatusOK,
		`{"power_address":"qemu+ssh://maas@hv-01/system","power_id":"simple-pig","power_pass":"pw","k_g":"must-not-load"}`,
		http.StatusOK, `{}`)
	got, err := newTestProvider(t, fake.fakeMAAS).PowerConfiguration(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("PowerConfiguration error = %v", err)
	}
	want := provisioningdomain.PowerConfiguration{
		Driver: provisioningdomain.PowerDriverVirsh, Address: "qemu+ssh://maas@hv-01/system", PowerID: "simple-pig", Password: "pw",
	}
	if *got != want {
		t.Errorf("PowerConfiguration = %+v, want %+v", *got, want)
	}
}

// A machine without a driver has no parameters to read, and a VM-host member reports its host.
func TestPowerConfiguration_NoDriverAndVMHost(t *testing.T) {
	fake := newPowerFake(t, `{"system_id":"abc123","power_type":"","pod":null}`, http.StatusOK, `{}`, http.StatusOK, `{}`)
	got, err := newTestProvider(t, fake.fakeMAAS).PowerConfiguration(context.Background(), "abc123")
	if err != nil || got.Driver != provisioningdomain.PowerDriverNone || fake.paramsReads.Load() != 0 {
		t.Errorf("PowerConfiguration = %+v, %v after %d parameter reads; want no driver and no read", got, err, fake.paramsReads.Load())
	}

	fake = newPowerFake(t, `{"system_id":"abc123","power_type":"virsh","pod":{"name":"kvm-3"}}`, http.StatusOK,
		`{"power_address":"qemu+ssh://kvm-3/system","power_id":"vm"}`, http.StatusOK, `{}`)
	got, err = newTestProvider(t, fake.fakeMAAS).PowerConfiguration(context.Background(), "abc123")
	if err != nil || got.ManagedBy != "kvm-3" {
		t.Errorf("PowerConfiguration = %+v, %v; want ManagedBy kvm-3", got, err)
	}
}

// A MAAS account that is not an administrator cannot read power parameters; that is a setup
// problem (auth), not a missing machine or a rejected request.
func TestPowerConfiguration_ForbiddenIsAuth(t *testing.T) {
	fake := newPowerFake(t, `{"system_id":"abc123","power_type":"ipmi","pod":null}`, http.StatusForbidden, `forbidden`, http.StatusOK, `{}`)
	_, err := newTestProvider(t, fake.fakeMAAS).PowerConfiguration(context.Background(), "abc123")
	var providerErr *provisioningdomain.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != provisioningdomain.ProviderErrorAuth {
		t.Fatalf("PowerConfiguration error = %v, want ProviderErrorAuth", err)
	}
}

func TestSetPowerConfiguration_MapsFieldsAndPassword(t *testing.T) {
	secret := "s3cret"
	empty := ""
	cases := []struct {
		name       string
		current    string
		change     provisioningdomain.PowerConfigurationChange
		want       map[string]string
		absentPass bool
	}{
		{
			name:    "a new virsh driver clears any previous password",
			current: "",
			change: provisioningdomain.PowerConfigurationChange{
				Driver: provisioningdomain.PowerDriverVirsh, Address: "qemu+ssh://maas@hv-01/system", PowerID: "simple-pig",
			},
			want: map[string]string{
				"power_type": "virsh", "power_parameters_power_address": "qemu+ssh://maas@hv-01/system",
				"power_parameters_power_id": "simple-pig", "power_parameters_power_pass": "",
			},
		},
		{
			name:    "an unchanged driver keeps the stored password",
			current: "ipmi",
			change: provisioningdomain.PowerConfigurationChange{
				Driver: provisioningdomain.PowerDriverIPMI, Address: "10.0.0.5", Username: "maas",
			},
			want: map[string]string{
				"power_type": "ipmi", "power_parameters_power_address": "10.0.0.5", "power_parameters_power_user": "maas",
			},
			absentPass: true,
		},
		{
			name:    "a given password replaces it",
			current: "virsh",
			change: provisioningdomain.PowerConfigurationChange{
				Driver: provisioningdomain.PowerDriverVirsh, Address: "qemu+ssh://h/system", PowerID: "vm", Password: &secret,
			},
			want: map[string]string{
				"power_type": "virsh", "power_parameters_power_address": "qemu+ssh://h/system",
				"power_parameters_power_id": "vm", "power_parameters_power_pass": "s3cret",
			},
		},
		{
			name:    "an empty password clears it",
			current: "ipmi",
			change: provisioningdomain.PowerConfigurationChange{
				Driver: provisioningdomain.PowerDriverIPMI, Address: "10.0.0.5", Password: &empty,
			},
			want: map[string]string{
				"power_type": "ipmi", "power_parameters_power_address": "10.0.0.5",
				"power_parameters_power_user": "", "power_parameters_power_pass": "",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newPowerFake(t, `{"system_id":"abc123","power_type":"`+tc.current+`","pod":null}`, http.StatusOK, `{}`, http.StatusOK, `{}`)
			if err := newTestProvider(t, fake.fakeMAAS).SetPowerConfiguration(context.Background(), "abc123", tc.change); err != nil {
				t.Fatalf("SetPowerConfiguration error = %v", err)
			}
			for name, want := range tc.want {
				if got, ok := fake.updateFields[name]; !ok || got != want {
					t.Errorf("field %s = %q (sent %v), want %q", name, got, ok, want)
				}
			}
			if _, sent := fake.updateFields["power_parameters_power_pass"]; tc.absentPass && sent {
				t.Error("sent a password field, want it omitted so MAAS keeps the stored one")
			}
			if len(fake.updateFields) != len(tc.want) {
				t.Errorf("sent fields %v, want exactly %v", fake.updateFields, tc.want)
			}
		})
	}
}

// A VM-host member is refused without a write; a MAAS validation refusal keeps MAAS's explanation.
func TestSetPowerConfiguration_Refusals(t *testing.T) {
	change := provisioningdomain.PowerConfigurationChange{
		Driver: provisioningdomain.PowerDriverVirsh, Address: "qemu+ssh://h/system", PowerID: "vm",
	}
	fake := newPowerFake(t, `{"system_id":"abc123","power_type":"virsh","pod":{"name":"kvm-3"}}`, http.StatusOK, `{}`, http.StatusOK, `{}`)
	err := newTestProvider(t, fake.fakeMAAS).SetPowerConfiguration(context.Background(), "abc123", change)
	if !errors.Is(err, provisioningdomain.ErrPowerConfigurationManaged) || fake.updates != 0 {
		t.Errorf("SetPowerConfiguration on a VM-host member = %v after %d updates, want ErrPowerConfigurationManaged and no write", err, fake.updates)
	}

	fake = newPowerFake(t, `{"system_id":"abc123","power_type":"","pod":null}`, http.StatusOK, `{}`,
		http.StatusBadRequest, `{"power_parameters": ["power_id: This field is required."]}`)
	err = newTestProvider(t, fake.fakeMAAS).SetPowerConfiguration(context.Background(), "abc123", change)
	var providerErr *provisioningdomain.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Fatalf("SetPowerConfiguration error = %v, want ProviderErrorRejected", err)
	}
	if want := "MAAS refused the request: power_parameters: power_id: This field is required."; providerErr.Detail != want {
		t.Errorf("detail = %q, want %q", providerErr.Detail, want)
	}
}

// verifyFake is a MAAS machine for the verified write: it serves the machine, its power
// parameters, and a power query answered with queryStatus and queryBody, and records every machine
// update in order, empty fields included.
type verifyFake struct {
	*fakeMAAS
	mu      sync.Mutex
	updates []map[string]string
	// hang makes the power query never answer, like MAAS retrying a BMC that does not respond.
	hang atomic.Bool
}

func newVerifyFake(t *testing.T, powerType, params string, queryStatus int, queryBody string) *verifyFake {
	t.Helper()
	f := &verifyFake{fakeMAAS: newFakeMAAS(t)}
	f.mux.HandleFunc("GET "+apiPrefix+"/machines/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("op") {
		case "power_parameters":
			_, _ = w.Write([]byte(params))
		case "query_power_state":
			if f.hang.Load() {
				<-r.Context().Done()
				return
			}
			w.WriteHeader(queryStatus)
			_, _ = w.Write([]byte(queryBody))
		default:
			_, _ = w.Write([]byte(`{"system_id":"abc123","power_type":"` + powerType + `","pod":null}`))
		}
	})
	f.mux.HandleFunc("PUT "+apiPrefix+"/machines/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		fields := map[string]string{}
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			for name, values := range r.MultipartForm.Value {
				fields[name] = values[0]
			}
		}
		f.mu.Lock()
		f.updates = append(f.updates, fields)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	return f
}

var _ provisioningdomain.PowerConfigurationVerifier = (*Provider)(nil)

// A configuration MAAS reads the power through is kept, and the state it read is returned.
func TestSetVerifiedPowerConfiguration_KeepsAReadableConfiguration(t *testing.T) {
	fake := newVerifyFake(t, "", `{}`, http.StatusOK, `{"state":"on"}`)
	secret := "right"
	state, err := newTestProvider(t, fake.fakeMAAS).SetVerifiedPowerConfiguration(context.Background(), "abc123",
		provisioningdomain.PowerConfigurationChange{Driver: provisioningdomain.PowerDriverIPMI, Address: "10.0.0.5", Username: "maas", Password: &secret})
	if err != nil || state != provisioningdomain.PowerStateOn {
		t.Fatalf("SetVerifiedPowerConfiguration = %q, %v; want on", state, err)
	}
	if len(fake.updates) != 1 || fake.updates[0]["power_type"] != "ipmi" {
		t.Errorf("updates = %v, want the one ipmi write", fake.updates)
	}
}

// A configuration MAAS cannot read the power through is refused with MAAS's reason, the password
// redacted, after the previous driver and every scalar parameter are written back.
func TestSetVerifiedPowerConfiguration_RestoresWhenPowerIsUnreadable(t *testing.T) {
	cases := []struct {
		name        string
		powerType   string
		params      string
		queryStatus int
		queryBody   string
		wantRestore map[string]string
		wantReason  string
	}{
		{
			name: "a wrong password on an IPMI driver", powerType: "ipmi",
			params:      `{"power_address":"10.0.0.5","power_user":"maas","power_pass":"old","cipher_suite_id":"3","workaround_flags":["opensesspriv"]}`,
			queryStatus: http.StatusServiceUnavailable, queryBody: `Failed to query power for maas with wrong: password invalid`,
			wantRestore: map[string]string{
				"power_type": "ipmi", "power_parameters_power_address": "10.0.0.5", "power_parameters_power_user": "maas",
				"power_parameters_power_pass": "old", "power_parameters_cipher_suite_id": "3",
			},
			wantReason: "password invalid",
		},
		{
			name: "no driver before, an unknown state after", powerType: "",
			params: `{}`, queryStatus: http.StatusOK, queryBody: `{"state":"error"}`,
			wantRestore: map[string]string{"power_type": ""},
			wantReason:  "it read the power state as error",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newVerifyFake(t, tc.powerType, tc.params, tc.queryStatus, tc.queryBody)
			wrong := "wrong"
			_, err := newTestProvider(t, fake.fakeMAAS).SetVerifiedPowerConfiguration(context.Background(), "abc123",
				provisioningdomain.PowerConfigurationChange{Driver: provisioningdomain.PowerDriverIPMI, Address: "10.0.0.6", Username: "maas", Password: &wrong})
			if !errors.Is(err, provisioningdomain.ErrPowerConfigurationUnverified) {
				t.Fatalf("SetVerifiedPowerConfiguration error = %v, want ErrPowerConfigurationUnverified", err)
			}
			if !strings.Contains(err.Error(), tc.wantReason) || strings.Contains(err.Error(), "wrong") {
				t.Errorf("error = %q, want the reason %q with the password redacted", err, tc.wantReason)
			}
			if len(fake.updates) != 2 {
				t.Fatalf("updates = %v, want the write then the restore", fake.updates)
			}
			restore := fake.updates[1]
			if len(restore) != len(tc.wantRestore) {
				t.Errorf("restore fields = %v, want exactly %v", restore, tc.wantRestore)
			}
			for name, value := range tc.wantRestore {
				if got, ok := restore[name]; !ok || got != value {
					t.Errorf("restore %s = %q (present %v), want %q", name, got, ok, value)
				}
			}
		})
	}
}

// A BMC that does not answer keeps MAAS retrying for minutes, so the check gives up after its
// window, puts the previous configuration back, and says what to check rather than "timeout".
func TestSetVerifiedPowerConfiguration_GivesUpOnAnUnansweredQuery(t *testing.T) {
	previous := powerCheckWindow
	powerCheckWindow = 100 * time.Millisecond
	t.Cleanup(func() { powerCheckWindow = previous })

	fake := newVerifyFake(t, "", `{}`, http.StatusOK, `{"state":"on"}`)
	fake.hang.Store(true)
	wrong := "wrong"
	_, err := newTestProvider(t, fake.fakeMAAS).SetVerifiedPowerConfiguration(context.Background(), "abc123",
		provisioningdomain.PowerConfigurationChange{Driver: provisioningdomain.PowerDriverIPMI, Address: "10.0.0.6", Username: "maas", Password: &wrong})
	if !errors.Is(err, provisioningdomain.ErrPowerConfigurationUnverified) {
		t.Fatalf("SetVerifiedPowerConfiguration error = %v, want ErrPowerConfigurationUnverified", err)
	}
	if !strings.Contains(err.Error(), "did not answer within") {
		t.Errorf("error = %q, want it to say the BMC did not answer in time", err)
	}
	if len(fake.updates) != 2 || fake.updates[1]["power_type"] != "" {
		t.Errorf("updates = %v, want the write then the previous (no) driver put back", fake.updates)
	}
}
