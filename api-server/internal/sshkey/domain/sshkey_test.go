package domain

import "testing"

func TestSameKeyMaterialIgnoresComment(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "same key, different comment", a: "ssh-ed25519 AAAA alice", b: "ssh-ed25519 AAAA renamed in maas", want: true},
		{name: "same key, no comment", a: " ssh-ed25519 AAAA ", b: "ssh-ed25519 AAAA bob", want: true},
		{name: "different blob", a: "ssh-ed25519 AAAA", b: "ssh-ed25519 BBBB", want: false},
		{name: "different type", a: "ssh-rsa AAAA", b: "ssh-ed25519 AAAA", want: false},
		{name: "malformed never matches", a: "ssh-ed25519", b: "ssh-ed25519", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameKeyMaterial(tc.a, tc.b); got != tc.want {
				t.Errorf("SameKeyMaterial(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	if got, ok := NormalizeName("  laptop  "); !ok || got != "laptop" {
		t.Errorf("NormalizeName(padded) = %q, %v; want laptop, true", got, ok)
	}
	if _, ok := NormalizeName("   "); ok {
		t.Errorf("NormalizeName(blank) accepted a blank name")
	}
	long := make([]rune, MaxNameLength+1)
	for i := range long {
		long[i] = '鍵'
	}
	if _, ok := NormalizeName(string(long)); ok {
		t.Errorf("NormalizeName accepted a name longer than %d characters", MaxNameLength)
	}
}
