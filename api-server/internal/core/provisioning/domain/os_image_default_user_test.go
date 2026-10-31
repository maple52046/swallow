package domain

import "testing"

func TestValidDefaultUser(t *testing.T) {
	for user, want := range map[string]bool{
		"ubuntu":                            true,
		"cloud-user":                        true,
		"_svc":                              true,
		"":                                  false,
		"Cloud":                             false,
		"cloud user":                        false,
		"1ops":                              false,
		"root;id":                           false,
		"a23456789012345678901234567890123": false,
	} {
		if got := ValidDefaultUser(user); got != want {
			t.Errorf("ValidDefaultUser(%q) = %v, want %v", user, got, want)
		}
	}
}

func TestEffectiveDefaultUser(t *testing.T) {
	tests := []struct {
		name, overlay, providerOS, want string
	}{
		{name: "overlay wins", overlay: "ops", providerOS: "ubuntu", want: "ops"},
		{name: "ubuntu built-in", providerOS: "ubuntu", want: "ubuntu"},
		{name: "rhel built-in", providerOS: "RHEL", want: "cloud-user"},
		{name: "custom has no built-in", providerOS: "custom", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveDefaultUser(tc.overlay, tc.providerOS); got != tc.want {
				t.Errorf("EffectiveDefaultUser(%q, %q) = %q, want %q", tc.overlay, tc.providerOS, got, tc.want)
			}
		})
	}
}
