package domain

import "testing"

// NormalizeDeploymentNetworkMode is the single place that folds an empty value and the
// deprecated "dhcp" alias into the canonical Automatic mode, so every caller stays
// consistent and the deprecated wire value never leaks downstream.
func TestNormalizeDeploymentNetworkMode(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  DeploymentNetworkMode
		wantK bool
	}{
		{name: "empty defaults to automatic", raw: "", want: DeploymentNetworkAutomatic, wantK: true},
		{name: "automatic stays automatic", raw: "automatic", want: DeploymentNetworkAutomatic, wantK: true},
		{name: "deprecated dhcp alias folds to automatic", raw: "dhcp", want: DeploymentNetworkAutomatic, wantK: true},
		{name: "static stays static", raw: "static", want: DeploymentNetworkStatic, wantK: true},
		{name: "unknown is rejected", raw: "bridge", want: "", wantK: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := NormalizeDeploymentNetworkMode(test.raw)
			if ok != test.wantK || got != test.want {
				t.Fatalf("NormalizeDeploymentNetworkMode(%q) = (%q,%v), want (%q,%v)",
					test.raw, got, ok, test.want, test.wantK)
			}
		})
	}
}
