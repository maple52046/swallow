package maas

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// Compile-time proof that the adapter offers the Server Enrollment capabilities.
var (
	_ provisioningdomain.EnrollmentSettler    = (*Provider)(nil)
	_ provisioningdomain.ExistingHostEnroller = (*Provider)(nil)
)

// onMachineAndPower serves GET /machines/{id}/ as machineBody and op=query_power_state as
// powerBody with powerStatus, counting the live power queries in queries.
func (f *fakeMAAS) onMachineAndPower(machineBody string, powerStatus int, powerBody string, queries *atomic.Int32) {
	f.mux.HandleFunc("GET "+apiPrefix+"/machines/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("op") == "query_power_state" {
			queries.Add(1)
			w.WriteHeader(powerStatus)
			_, _ = w.Write([]byte(powerBody))
			return
		}
		_, _ = w.Write([]byte(machineBody))
	})
}

// MAAS powers a host off when enlistment ends, so a New Machine is settled only once it is off;
// a failed live power read falls back to the state MAAS recorded. A Machine whose enlistment
// script set finished without an automatic driver can never report that power-off (decision 054),
// while one still running its script set may yet get its BMC driver.
func TestObserveEnrollment(t *testing.T) {
	machine := func(power, powerType, commissioningStatus string) string {
		body := `{"system_id": "abc123", "status": 0, "status_name": "New", "power_state": "` + power +
			`", "power_type": "` + powerType + `"`
		if commissioningStatus != "" {
			body += `, "commissioning_status": ` + commissioningStatus
		}
		return body + "}"
	}
	cases := []struct {
		name        string
		machine     string
		powerStatus int
		powerBody   string
		want        provisioningdomain.EnrollmentState
		wantControl provisioningdomain.PowerControl
		wantQueries int32
	}{
		{"new and live off", machine("on", "ipmi", "2"), http.StatusOK, `{"state": "off"}`,
			provisioningdomain.EnrollmentSettled, provisioningdomain.PowerControlAutomatic, 1},
		{"new and live on", machine("off", "ipmi", "2"), http.StatusOK, `{"state": "on"}`,
			provisioningdomain.EnrollmentEnrolling, provisioningdomain.PowerControlAutomatic, 1},
		{"new, live read fails, recorded off", machine("off", "virsh", "2"), http.StatusServiceUnavailable, `{}`,
			provisioningdomain.EnrollmentSettled, provisioningdomain.PowerControlAutomatic, 1},
		{"new, live read fails, recorded unknown", machine("unknown", "virsh", "2"), http.StatusServiceUnavailable, `{}`,
			provisioningdomain.EnrollmentEnrolling, provisioningdomain.PowerControlAutomatic, 1},
		{"commissioning has nothing to settle", `{"system_id": "abc123", "status": 1, "status_name": "Commissioning", "power_state": "on", "power_type": "ipmi"}`,
			http.StatusOK, `{"state": "on"}`, provisioningdomain.EnrollmentSettled, provisioningdomain.PowerControlAutomatic, 0},
		{"no driver while enlistment runs is still enrolling", machine("unknown", "", "1"), http.StatusOK, `{}`,
			provisioningdomain.EnrollmentEnrolling, provisioningdomain.PowerControlNone, 0},
		{"no driver after enlistment passed is unobservable", machine("unknown", "", "2"), http.StatusOK, `{}`,
			provisioningdomain.EnrollmentPowerUnobservable, provisioningdomain.PowerControlNone, 0},
		{"no driver without any script set is unobservable", machine("unknown", "", "-1"), http.StatusOK, `{}`,
			provisioningdomain.EnrollmentPowerUnobservable, provisioningdomain.PowerControlNone, 0},
		{"manual driver after enlistment is unobservable", machine("unknown", "manual", "3"), http.StatusOK, `{}`,
			provisioningdomain.EnrollmentPowerUnobservable, provisioningdomain.PowerControlManual, 0},
		{"no driver but recorded off is settled", machine("off", "", "2"), http.StatusOK, `{}`,
			provisioningdomain.EnrollmentSettled, provisioningdomain.PowerControlNone, 0},
		{"no script status reported keeps waiting", machine("unknown", "", ""), http.StatusOK, `{}`,
			provisioningdomain.EnrollmentEnrolling, provisioningdomain.PowerControlNone, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeMAAS(t)
			var queries atomic.Int32
			fake.onMachineAndPower(tc.machine, tc.powerStatus, tc.powerBody, &queries)
			got, err := newTestProvider(t, fake).ObserveEnrollment(context.Background(), "abc123")
			if err != nil || got.State != tc.want || got.Control != tc.wantControl {
				t.Errorf("ObserveEnrollment = %+v, %v; want state %q control %q", got, err, tc.want, tc.wantControl)
			}
			if queries.Load() != tc.wantQueries {
				t.Errorf("live power queries = %d, want %d", queries.Load(), tc.wantQueries)
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
