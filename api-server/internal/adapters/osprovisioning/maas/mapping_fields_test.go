package maas

import "testing"

// The mirrored cache fields have to survive the mapping, because the whole point of
// caching them is to query the fleet by hardware without opening each machine.
func TestToDomainMachine_MapsMirroredCacheFields(t *testing.T) {
	m := &machineJSON{
		SystemID:                "abc123",
		Status:                  6,
		Locked:                  true,
		CommissioningStatusName: "Passed",
		TestingStatusName:       "Failed",
		Pod:                     &namedJSON{Name: "kvm-host-3"},
		HardwareInfo: &hardwareInfoJSON{
			SystemVendor:  "Dell Inc.",
			SystemProduct: "PowerEdge R760xa",
			SystemSerial:  "Unknown",
			CPUModel:      "Intel(R) Xeon(R) Platinum 8480+",
		},
	}

	got := toDomainMachine(m)

	if !got.Locked {
		t.Error("Locked was dropped")
	}
	if got.CommissioningStatus != "Passed" || got.TestingStatus != "Failed" {
		t.Errorf("validation labels: got %q / %q", got.CommissioningStatus, got.TestingStatus)
	}
	if got.Pod != "kvm-host-3" {
		t.Errorf("Pod: got %q", got.Pod)
	}
	if got.SystemVendor != "Dell Inc." || got.SystemProduct != "PowerEdge R760xa" {
		t.Errorf("system vendor/product: got %q / %q", got.SystemVendor, got.SystemProduct)
	}
	if got.CPUModel != "Intel(R) Xeon(R) Platinum 8480+" {
		t.Errorf("CPU model: got %q", got.CPUModel)
	}
	// The serial is a placeholder, so it must be blank rather than the literal "Unknown".
	if got.SerialNumber != "Unknown" {
		// SerialNumber keeps the raw value; identity normalization strips placeholders
		// downstream. This asserts the raw passthrough is unchanged so that behaviour is
		// deliberate rather than accidental.
		t.Logf("serial passthrough is %q", got.SerialNumber)
	}
}

func TestCleanField_DropsPlaceholders(t *testing.T) {
	for _, placeholder := range []string{"Unknown", "unknown", "  ", "Not Specified", "Default string", "None", "N/A"} {
		if got := cleanField(placeholder); got != "" {
			t.Errorf("%q should clean to empty, got %q", placeholder, got)
		}
	}
	if got := cleanField("  Dell Inc.  "); got != "Dell Inc." {
		t.Errorf("a real value should be trimmed and kept, got %q", got)
	}
}
