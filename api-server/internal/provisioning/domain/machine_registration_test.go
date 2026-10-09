package domain

import (
	"errors"
	"testing"
)

func TestMachineHostnameOf(t *testing.T) {
	cases := map[string]string{
		"lab-afde-mi308-1": "lab-afde-mi308-1",
		"Lab_VM.01":        "lab-vm-01",
		"-edge-":           "edge",
		"___":              "",
		"a234567890123456789012345678901234567890123456789012345678901234567890": "a23456789012345678901234567890123456789012345678901234567890123",
	}
	for domain, want := range cases {
		if got := MachineHostnameOf(domain); got != want {
			t.Errorf("MachineHostnameOf(%q) = %q, want %q", domain, got, want)
		}
	}
}

func TestMachineArchitectureOf(t *testing.T) {
	for arch, want := range map[string]string{"x86_64": "amd64", "aarch64": "arm64", "ppc64le": ""} {
		got, ok := MachineArchitectureOf(arch)
		if got != want || ok != (want != "") {
			t.Errorf("MachineArchitectureOf(%q) = %q, %v; want %q", arch, got, ok, want)
		}
	}
}

func TestVirshAddressRoundTrip(t *testing.T) {
	address := VirshAddress("ubuntu", "10.170.168.22")
	if address != "qemu+ssh://ubuntu@10.170.168.22/system" {
		t.Fatalf("VirshAddress = %q", address)
	}
	account, host, err := VirshHostOf(address)
	if err != nil || account != "ubuntu" || host != "10.170.168.22" {
		t.Errorf("VirshHostOf = %q, %q, %v", account, host, err)
	}
	if _, host, err := VirshHostOf(VirshAddress("ubuntu", "fd00::22")); err != nil || host != "fd00::22" {
		t.Errorf("VirshHostOf(IPv6) = %q, %v", host, err)
	}
	if _, _, err := VirshHostOf("qemu:///system"); !errors.Is(err, ErrInvalidPowerConfiguration) {
		t.Errorf("VirshHostOf(local) error = %v, want ErrInvalidPowerConfiguration", err)
	}
}
