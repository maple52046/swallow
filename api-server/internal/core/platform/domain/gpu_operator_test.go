package domain

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestEveryGPUOperatorReleaseShipsItsChart keeps the catalog and the bundled chart archives in
// step: the playbook copies gpu-operator-charts-<version>.tgz from the role's files, so a release
// without its archive would fail on every target at run time.
func TestEveryGPUOperatorReleaseShipsItsChart(t *testing.T) {
	defaults := 0
	for _, release := range GPUOperatorReleases() {
		if release.Default {
			defaults++
		}
		path := filepath.Join("..", "..", "..", "..", "automation", "playbooks", "roles", "amd_gpu_operator", "files",
			"gpu-operator-charts-"+release.Version+".tgz")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("release %s has no bundled chart: %v", release.Version, err)
		}
		if _, ok := release.DRADriverImages[release.DefaultDRADriverVersion]; !ok {
			t.Errorf("release %s default DRA driver %s has no image", release.Version, release.DefaultDRADriverVersion)
		}
	}
	if defaults != 1 {
		t.Errorf("catalog has %d default releases, want exactly 1", defaults)
	}
}

func TestParseKubernetesMinorVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  KubernetesMinorVersion
		ok    bool
	}{
		{name: "k0s release", input: "v1.36.3+k0s.2", want: KubernetesMinorVersion{Major: 1, Minor: 36}, ok: true},
		{name: "without v prefix or suffix", input: "1.34.0", want: KubernetesMinorVersion{Major: 1, Minor: 34}, ok: true},
		{name: "major only", input: "v1", ok: false},
		{name: "not a number", input: "latest", ok: false},
		{name: "empty", input: "", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseKubernetesMinorVersion(tc.input)
			if ok != tc.ok || got != tc.want {
				t.Errorf("ParseKubernetesMinorVersion(%q) = %v, %v; want %v, %v", tc.input, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestNormalizeGPUOperatorSettingsAppliesDefaults(t *testing.T) {
	kubernetes := KubernetesMinorVersion{Major: 1, Minor: 36}
	got, err := NormalizeGPUOperatorSettings(GPUOperatorSettings{Enabled: true, NodeLabeller: true}, kubernetes, true)
	if err != nil {
		t.Fatalf("NormalizeGPUOperatorSettings: %v", err)
	}
	want := GPUOperatorSettings{
		Enabled: true, Version: "v1.5.1", AllocationMode: GPUAllocationDevicePlugin,
		ResourceNaming: GPUResourceNamingSingle, NodeLabeller: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeGPUOperatorSettings defaults = %+v, want %+v", got, want)
	}

	dra, err := NormalizeGPUOperatorSettings(GPUOperatorSettings{
		Enabled: true, AllocationMode: GPUAllocationDRA,
	}, kubernetes, true)
	if err != nil {
		t.Fatalf("NormalizeGPUOperatorSettings(dra): %v", err)
	}
	if dra.DRADriverVersion != "v1.0.1" {
		t.Errorf("NormalizeGPUOperatorSettings(dra) = %+v, want the default DRA driver v1.0.1", dra)
	}

	plugin, err := NormalizeGPUOperatorSettings(GPUOperatorSettings{
		Enabled: true, AllocationMode: GPUAllocationDevicePlugin, DRADriverVersion: "v1.0.1",
	}, kubernetes, true)
	if err != nil {
		t.Fatalf("NormalizeGPUOperatorSettings(device-plugin): %v", err)
	}
	if plugin.DRADriverVersion != "" {
		t.Errorf("device-plugin DRADriverVersion = %q, want it cleared", plugin.DRADriverVersion)
	}
}

func TestNormalizeGPUOperatorSettingsRejectsWhatCannotBeApplied(t *testing.T) {
	current := KubernetesMinorVersion{Major: 1, Minor: 36}
	tests := []struct {
		name       string
		settings   GPUOperatorSettings
		kubernetes KubernetesMinorVersion
		hasGPU     bool
	}{
		{name: "unknown release", settings: GPUOperatorSettings{Enabled: true, Version: "v9.9.9"}, kubernetes: current, hasGPU: true},
		{name: "kubernetes below the release floor", settings: GPUOperatorSettings{Enabled: true}, kubernetes: KubernetesMinorVersion{Major: 1, Minor: 28}, hasGPU: true},
		{name: "DRA below resource.k8s.io/v1", settings: GPUOperatorSettings{Enabled: true, AllocationMode: GPUAllocationDRA}, kubernetes: KubernetesMinorVersion{Major: 1, Minor: 33}, hasGPU: true},
		{name: "unverified DRA driver", settings: GPUOperatorSettings{Enabled: true, AllocationMode: GPUAllocationDRA, DRADriverVersion: "v0.1.0"}, kubernetes: current, hasGPU: true},
		{name: "unknown allocation mode", settings: GPUOperatorSettings{Enabled: true, AllocationMode: "time-slicing"}, kubernetes: current, hasGPU: true},
		{name: "unknown resource naming", settings: GPUOperatorSettings{Enabled: true, ResourceNaming: "per-gpu"}, kubernetes: current, hasGPU: true},
		{name: "no AMD GPU node", settings: GPUOperatorSettings{Enabled: true}, kubernetes: current},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeGPUOperatorSettings(tc.settings, tc.kubernetes, tc.hasGPU)
			if !errors.Is(err, ErrInvalidGPUOperator) {
				t.Errorf("NormalizeGPUOperatorSettings(%+v) error = %v, want ErrInvalidGPUOperator", tc.settings, err)
			}
		})
	}
}

func TestNormalizeGPUOperatorSettingsPassesAnUninstallThrough(t *testing.T) {
	settings := GPUOperatorSettings{Version: "v1.5.1", AllocationMode: GPUAllocationDRA}
	got, err := NormalizeGPUOperatorSettings(settings, KubernetesMinorVersion{}, false)
	if err != nil || !reflect.DeepEqual(got, settings) {
		t.Errorf("NormalizeGPUOperatorSettings(disabled) = %+v, %v; want the settings unchanged", got, err)
	}
}

func TestGPUOperatorManaged(t *testing.T) {
	var none *GPUOperator
	if none.Managed() {
		t.Error("nil GPUOperator is managed, want unmanaged")
	}
	if (&GPUOperator{State: GPUOperatorAbsent}).Managed() {
		t.Error("absent GPUOperator is managed, want unmanaged")
	}
	if !(&GPUOperator{State: GPUOperatorFailed}).Managed() {
		t.Error("failed GPUOperator is unmanaged, want managed: the cluster may still run it")
	}
}
