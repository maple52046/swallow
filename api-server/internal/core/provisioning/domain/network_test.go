package domain

import "testing"

// NormalizeDeploymentNetworkMode is the single place that resolves the wire value into one of
// the three deployment addressing intents, so every caller stays consistent. Since ADR 058
// "dhcp" is its own mode rather than an alias of Automatic.
func TestNormalizeDeploymentNetworkMode(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  DeploymentNetworkMode
		wantK bool
	}{
		{name: "empty defaults to automatic", raw: "", want: DeploymentNetworkAutomatic, wantK: true},
		{name: "automatic stays automatic", raw: "automatic", want: DeploymentNetworkAutomatic, wantK: true},
		{name: "dhcp is its own mode", raw: "dhcp", want: DeploymentNetworkDHCP, wantK: true},
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

// Each deployment intent configures the NIC with its own link mode; Automatic and DHCP must
// not collapse into one, because they behave differently on a site DHCP network.
func TestDeploymentNetworkModeLinkMode(t *testing.T) {
	tests := []struct {
		mode   DeploymentNetworkMode
		want   NetworkLinkMode
		wantOK bool
	}{
		{mode: DeploymentNetworkAutomatic, want: NetworkLinkAuto, wantOK: true},
		{mode: DeploymentNetworkDHCP, want: NetworkLinkDHCP, wantOK: true},
		{mode: DeploymentNetworkStatic, want: NetworkLinkStatic, wantOK: true},
		{mode: "", want: "", wantOK: false},
		{mode: "link_only", want: "", wantOK: false},
	}
	for _, test := range tests {
		got, ok := test.mode.LinkMode()
		if got != test.want || ok != test.wantOK {
			t.Errorf("DeploymentNetworkMode(%q).LinkMode() = (%q,%v), want (%q,%v)",
				test.mode, got, ok, test.want, test.wantOK)
		}
	}
}
