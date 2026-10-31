package domain

import "testing"

// TestDeployTargetEphemeralMapping pins the one-to-one boundary mapping between the deploy-target
// vocabulary and the internal ephemeral flag, so the compatibility bridge cannot silently drift.
func TestDeployTargetEphemeralMapping(t *testing.T) {
	if DeployTargetDisk.Ephemeral() {
		t.Error("disk must map to ephemeral=false")
	}
	if !DeployTargetRAM.Ephemeral() {
		t.Error("ram must map to ephemeral=true")
	}
	if DeployTargetForEphemeral(false) != DeployTargetDisk {
		t.Error("ephemeral=false must map back to disk")
	}
	if DeployTargetForEphemeral(true) != DeployTargetRAM {
		t.Error("ephemeral=true must map back to ram")
	}
}

// TestParseDeployTarget checks case-insensitive parsing and rejection of unknown values.
func TestParseDeployTarget(t *testing.T) {
	cases := []struct {
		in    string
		want  DeployTarget
		valid bool
	}{
		{"disk", DeployTargetDisk, true},
		{"RAM", DeployTargetRAM, true},
		{" Disk ", DeployTargetDisk, true},
		{"ephemeral", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, valid := ParseDeployTarget(tc.in)
		if valid != tc.valid || (valid && got != tc.want) {
			t.Errorf("ParseDeployTarget(%q) = (%q,%v), want (%q,%v)", tc.in, got, valid, tc.want, tc.valid)
		}
	}
}

func TestResolveEphemeral(t *testing.T) {
	ram, disk, bogus := "RAM", "disk", "floppy"
	yes, no := true, false
	tests := []struct {
		name         string
		deployTarget *string
		ephemeral    *bool
		want         *bool
		ok           bool
	}{
		{name: "neither names no mode", ok: true},
		{name: "ephemeral alias alone", ephemeral: &yes, want: &yes, ok: true},
		{name: "deployTarget ram", deployTarget: &ram, want: &yes, ok: true},
		{name: "deployTarget wins over the alias", deployTarget: &disk, ephemeral: &yes, want: &no, ok: true},
		{name: "unknown deployTarget", deployTarget: &bogus, ephemeral: &yes},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveEphemeral(tc.deployTarget, tc.ephemeral)
			if ok != tc.ok || (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("ResolveEphemeral() = %v, %v; want %v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
