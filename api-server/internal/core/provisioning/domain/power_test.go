package domain

import (
	"errors"
	"testing"
)

func TestPowerControlOf(t *testing.T) {
	cases := []struct {
		driver PowerDriver
		want   PowerControl
	}{
		{PowerDriverNone, PowerControlNone},
		{PowerDriverManual, PowerControlManual},
		{PowerDriverIPMI, PowerControlAutomatic},
		{PowerDriverVirsh, PowerControlAutomatic},
		// A driver swallow does not write is still the provisioner's own automatic driver.
		{PowerDriver("lxd"), PowerControlAutomatic},
	}
	for _, tc := range cases {
		if got := PowerControlOf(tc.driver); got != tc.want {
			t.Errorf("PowerControlOf(%q) = %q, want %q", tc.driver, got, tc.want)
		}
	}
}

// The registry resolves each supported driver to its family, lists the writable drivers in
// family order, and knows nothing about drivers no family claims.
func TestDefaultPowerAdapters(t *testing.T) {
	registry := DefaultPowerAdapters()
	for driver, family := range map[PowerDriver]PowerFamily{
		PowerDriverIPMI: PowerFamilyBMC, PowerDriverRedfish: PowerFamilyBMC, PowerDriverVirsh: PowerFamilyVirsh,
	} {
		adapter, ok := registry.ForDriver(driver)
		if !ok || adapter.Family() != family {
			t.Errorf("ForDriver(%q) = %v, %v; want family %q", driver, adapter, ok, family)
		}
	}
	for _, driver := range []PowerDriver{PowerDriverNone, PowerDriverManual, "lxd"} {
		if _, ok := registry.ForDriver(driver); ok {
			t.Errorf("ForDriver(%q) found an adapter, want none", driver)
		}
	}
	want := []PowerDriverOption{
		{PowerDriverIPMI, PowerFamilyBMC}, {PowerDriverRedfish, PowerFamilyBMC}, {PowerDriverVirsh, PowerFamilyVirsh},
	}
	got := registry.Options()
	if len(got) != len(want) {
		t.Fatalf("Options() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Options()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// Only a bmc-family driver of a machine that is not a VM-host member has a BMC; Redfish is the
// BMC's function, not a family of its own.
func TestPowerAdapterRegistryBMCAdapter(t *testing.T) {
	registry := DefaultPowerAdapters()
	cases := []struct {
		name   string
		config PowerConfiguration
		want   bool
	}{
		{"ipmi", PowerConfiguration{Driver: PowerDriverIPMI, Address: "10.0.0.5"}, true},
		{"redfish driver", PowerConfiguration{Driver: PowerDriverRedfish, Address: "https://10.0.0.5"}, true},
		{"virsh", PowerConfiguration{Driver: PowerDriverVirsh, Address: "qemu+ssh://u@h/system"}, false},
		{"no driver", PowerConfiguration{}, false},
		{"unknown driver", PowerConfiguration{Driver: "lxd"}, false},
		{"VM-host member with a BMC driver", PowerConfiguration{Driver: PowerDriverIPMI, Address: "10.0.0.5", ManagedBy: "kvm-3"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, got := registry.BMCAdapter(tc.config); got != tc.want {
				t.Errorf("BMCAdapter(%+v) = %v, want %v", tc.config, got, tc.want)
			}
		})
	}
}

func TestBMCPowerAdapterBMCAccess(t *testing.T) {
	bmc, _ := DefaultPowerAdapters().BMCAdapter(PowerConfiguration{Driver: PowerDriverIPMI})
	access, ok := bmc.BMCAccess(PowerConfiguration{
		Driver: PowerDriverIPMI, Address: " 10.0.0.5 ", Username: "maas", Password: "pw", NodeID: "Self",
	})
	want := BMCAccess{Driver: PowerDriverIPMI, Address: "10.0.0.5", Username: "maas", Password: "pw", SystemHint: "Self"}
	if !ok || access != want {
		t.Errorf("BMCAccess = %+v, %v; want %+v", access, ok, want)
	}
	if _, ok := bmc.BMCAccess(PowerConfiguration{Driver: PowerDriverIPMI}); ok {
		t.Error("BMCAccess of a configuration without an address reported a BMC")
	}
}

func TestVirshPowerAdapterNormalize(t *testing.T) {
	virsh, _ := DefaultPowerAdapters().ForDriver(PowerDriverVirsh)
	valid := PowerConfigurationChange{Driver: PowerDriverVirsh, Address: " qemu+ssh://maas@tainan-ci.lab/system ", PowerID: " simple-pig "}
	got, err := virsh.Normalize(valid)
	if err != nil {
		t.Fatalf("Normalize(valid) error = %v", err)
	}
	if got.Address != "qemu+ssh://maas@tainan-ci.lab/system" || got.PowerID != "simple-pig" {
		t.Errorf("Normalize(valid) = %+v, want trimmed address and power ID", got)
	}
	for _, address := range []string{"qemu+ssh://tainan-ci:2222/system", "qemu+ssh://[fd00::1]/system"} {
		if _, err := virsh.Normalize(PowerConfigurationChange{Driver: PowerDriverVirsh, Address: address, PowerID: "vm"}); err != nil {
			t.Errorf("Normalize(%q) error = %v, want accepted", address, err)
		}
	}

	refused := []struct {
		name   string
		change PowerConfigurationChange
	}{
		{"missing address", PowerConfigurationChange{PowerID: "vm"}},
		{"local socket", PowerConfigurationChange{Address: "qemu:///system", PowerID: "vm"}},
		{"session URI", PowerConfigurationChange{Address: "qemu+ssh://u@h/session", PowerID: "vm"}},
		{"query parameters", PowerConfigurationChange{Address: "qemu+ssh://u@h/system?keyfile=/k", PowerID: "vm"}},
		{"fragment", PowerConfigurationChange{Address: "qemu+ssh://u@h/system#x", PowerID: "vm"}},
		{"password in URI", PowerConfigurationChange{Address: "qemu+ssh://u:secret@h/system", PowerID: "vm"}},
		{"no host", PowerConfigurationChange{Address: "qemu+ssh:///system", PowerID: "vm"}},
		{"missing power ID", PowerConfigurationChange{Address: "qemu+ssh://u@h/system"}},
		{"power ID with spaces", PowerConfigurationChange{Address: "qemu+ssh://u@h/system", PowerID: "my vm"}},
		{"username", PowerConfigurationChange{Address: "qemu+ssh://u@h/system", PowerID: "vm", Username: "u"}},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			tc.change.Driver = PowerDriverVirsh
			if _, err := virsh.Normalize(tc.change); !errors.Is(err, ErrInvalidPowerConfiguration) {
				t.Errorf("Normalize(%+v) error = %v, want ErrInvalidPowerConfiguration", tc.change, err)
			}
		})
	}
}

func TestBMCPowerAdapterNormalize(t *testing.T) {
	bmc, _ := DefaultPowerAdapters().ForDriver(PowerDriverIPMI)
	got, err := bmc.Normalize(PowerConfigurationChange{Driver: PowerDriverIPMI, Address: " 10.0.0.5 ", Username: " maas "})
	if err != nil || got.Address != "10.0.0.5" || got.Username != "maas" {
		t.Errorf("Normalize = %+v, %v; want trimmed address and username", got, err)
	}
	for name, change := range map[string]PowerConfigurationChange{
		"missing address":      {Driver: PowerDriverIPMI},
		"power ID":             {Driver: PowerDriverIPMI, Address: "10.0.0.5", PowerID: "vm"},
		"address with spaces":  {Driver: PowerDriverIPMI, Address: "10.0.0.5 10.0.0.6"},
		"username with spaces": {Driver: PowerDriverIPMI, Address: "10.0.0.5", Username: "a b"},
	} {
		if _, err := bmc.Normalize(change); !errors.Is(err, ErrInvalidPowerConfiguration) {
			t.Errorf("Normalize(%s) error = %v, want ErrInvalidPowerConfiguration", name, err)
		}
	}
}

func TestRedactPowerAddress(t *testing.T) {
	cases := map[string]string{
		"qemu+ssh://maas@tainan-ci/system":                       "qemu+ssh://maas@tainan-ci/system",
		"qemu+ssh://maas:secret@tainan-ci/system?keyfile=/k#x":   "qemu+ssh://maas@tainan-ci/system",
		"https://admin:pw@192.0.2.20/redfish/v1?token=t#session": "https://admin@192.0.2.20/redfish/v1",
		"10.0.0.5":                  "10.0.0.5",
		"admin:pw@10.0.0.5?x=1":     "10.0.0.5",
		"":                          "",
		"  qemu+ssh://u@h/system  ": "qemu+ssh://u@h/system",
	}
	for in, want := range cases {
		if got := RedactPowerAddress(in); got != want {
			t.Errorf("RedactPowerAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
