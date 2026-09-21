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
