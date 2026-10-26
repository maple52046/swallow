package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeGPUDriverSettings(t *testing.T) {
	settings, err := NormalizeGPUDriverSettings(GPUDriverSettings{ModuleOptions: map[string]string{
		" pcie_p2p ": " 0 ", "vm_fragment_size": "-1", "ip_block_mask": "0xffffffff",
	}})
	if err != nil {
		t.Fatalf("NormalizeGPUDriverSettings() error = %v", err)
	}
	if settings.ModuleOptions["pcie_p2p"] != "0" || settings.ModuleOptions["vm_fragment_size"] != "-1" || len(settings.ModuleOptions) != 3 {
		t.Errorf("normalized = %v", settings.ModuleOptions)
	}
	if empty, err := NormalizeGPUDriverSettings(GPUDriverSettings{}); err != nil || !empty.Empty() || empty.ModuleOptions == nil {
		t.Errorf("empty request = %+v, %v; want an empty, non-nil option map", empty, err)
	}

	tooMany := map[string]string{}
	for i := 0; i <= MaxGPUDriverModuleOptions; i++ {
		tooMany["option_"+strings.Repeat("x", i)] = "1"
	}
	for name, options := range map[string]map[string]string{
		"upper-case name":        {"PCIe_P2P": "0"},
		"name with a dash":       {"pcie-p2p": "0"},
		"empty value":            {"pcie_p2p": " "},
		"value with a space":     {"pcie_p2p": "0 install"},
		"value with a newline":   {"pcie_p2p": "0\noptions evil x=1"},
		"value with a semicolon": {"pcie_p2p": "0;reboot"},
		"same name twice":        {"pcie_p2p": "0", " pcie_p2p": "1"},
		"too many options":       tooMany,
	} {
		if _, err := NormalizeGPUDriverSettings(GPUDriverSettings{ModuleOptions: options}); !errors.Is(err, ErrInvalidGPUDriver) {
			t.Errorf("%s: error = %v, want ErrInvalidGPUDriver", name, err)
		}
	}
}
