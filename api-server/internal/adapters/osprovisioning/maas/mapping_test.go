package maas

import (
	"encoding/json"
	"testing"
)

// The Deploy path treats an explicit ephemeral_deploy=false as evidence that MAAS
// ignored an ephemeral request, and silence as no evidence at all. That distinction only
// survives while the field is a pointer, so it is pinned here: flattening it to a plain
// bool would make every MAAS that predates the field look like it had refused.
func TestMachineJSON_EphemeralAbsenceIsDistinctFromFalse(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantReported bool
		wantValue    bool
	}{
		{
			name:         "reported true",
			body:         `{"system_id":"abc","ephemeral_deploy":true}`,
			wantReported: true,
			wantValue:    true,
		},
		{
			name:         "reported false",
			body:         `{"system_id":"abc","ephemeral_deploy":false}`,
			wantReported: true,
			wantValue:    false,
		},
		{
			name:         "not reported at all",
			body:         `{"system_id":"abc"}`,
			wantReported: false,
		},
		{
			name:         "reported null",
			body:         `{"system_id":"abc","ephemeral_deploy":null}`,
			wantReported: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parsed machineJSON
			if err := json.Unmarshal([]byte(tc.body), &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			reported := parsed.EphemeralDeploy != nil
			if reported != tc.wantReported {
				t.Fatalf("reported: got %v, want %v", reported, tc.wantReported)
			}
			if reported && *parsed.EphemeralDeploy != tc.wantValue {
				t.Errorf("value: got %v, want %v", *parsed.EphemeralDeploy, tc.wantValue)
			}

			// The domain type carries no notion of "unknown", so an unreported field
			// has to land as false. That is why the pointer is checked before the
			// domain value is trusted as a contradiction.
			machine := toDomainMachine(&parsed)
			if machine.Ephemeral != tc.wantValue {
				t.Errorf("domain machine: got %v, want %v", machine.Ephemeral, tc.wantValue)
			}
		})
	}
}

func TestToDomainMachine_CarriesTheProviderKernelLabel(t *testing.T) {
	var parsed machineJSON
	if err := json.Unmarshal([]byte(`{"system_id":"abc","hwe_kernel":"ga-24.04"}`), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := toDomainMachine(&parsed).HWEKernel; got != "ga-24.04" {
		t.Errorf("got %q, want ga-24.04", got)
	}
}
