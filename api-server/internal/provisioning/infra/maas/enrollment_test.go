package maas

import (
	"context"
	"net/http"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// Compile-time proof that the adapter offers the Server Enrollment capabilities.
var (
	_ provisioningdomain.EnrollmentSettler    = (*Provider)(nil)
	_ provisioningdomain.ExistingHostEnroller = (*Provider)(nil)
)

// onMachineAndPower serves GET /machines/{id}/ as machineBody and op=query_power_state as
// powerBody with powerStatus.
func (f *fakeMAAS) onMachineAndPower(machineBody string, powerStatus int, powerBody string) {
	f.mux.HandleFunc("GET "+apiPrefix+"/machines/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("op") == "query_power_state" {
			w.WriteHeader(powerStatus)
			_, _ = w.Write([]byte(powerBody))
			return
		}
		_, _ = w.Write([]byte(machineBody))
	})
}

// MAAS powers a host off when enlistment ends, so a New Machine is settled only once it is off;
// a failed live power read falls back to the state MAAS recorded.
func TestEnrollmentSettled(t *testing.T) {
	newMachine := func(power string) string {
		return `{"system_id": "abc123", "status": 0, "status_name": "New", "power_state": "` + power + `"}`
	}
	cases := []struct {
		name        string
		machine     string
		powerStatus int
		powerBody   string
		want        bool
	}{
		{"new and live off", newMachine("on"), http.StatusOK, `{"state": "off"}`, true},
		{"new and live on", newMachine("off"), http.StatusOK, `{"state": "on"}`, false},
		{"new, live read fails, recorded off", newMachine("off"), http.StatusServiceUnavailable, `{}`, true},
		{"new, live read fails, recorded unknown", newMachine("unknown"), http.StatusServiceUnavailable, `{}`, false},
		{"commissioning has nothing to settle", `{"system_id": "abc123", "status": 1, "status_name": "Commissioning", "power_state": "on"}`, http.StatusOK, `{"state": "on"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeMAAS(t)
			fake.onMachineAndPower(tc.machine, tc.powerStatus, tc.powerBody)
			got, err := newTestProvider(t, fake).EnrollmentSettled(context.Background(), "abc123")
			if err != nil || got != tc.want {
				t.Errorf("EnrollmentSettled = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// The enrollment hands out the region URL, without the API suffix, and the adapter's own API key.
func TestExistingHostEnrollment(t *testing.T) {
	fake := newFakeMAAS(t)
	enrollment, err := newTestProvider(t, fake).ExistingHostEnrollment(context.Background())
	if err != nil {
		t.Fatalf("ExistingHostEnrollment error = %v", err)
	}
	wantEndpoint := fake.server.URL + "/MAAS"
	if enrollment.Endpoint != wantEndpoint || enrollment.Token != "ck:tk:ts" {
		t.Errorf("enrollment = %q, %q; want %q and the API key", enrollment.Endpoint, enrollment.Token, wantEndpoint)
	}
}
