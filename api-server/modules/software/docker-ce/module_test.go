package dockerce

import "testing"

// Only an explicit true enables the API; a legacy spec without the key must read as disabled.
func TestAPIEnabled(t *testing.T) {
	tests := []struct {
		name string
		spec map[string]any
		want bool
	}{
		{"explicit true", map[string]any{"enableApi": true}, true},
		{"explicit false", map[string]any{"enableApi": false}, false},
		{"legacy nil spec", nil, false},
		{"legacy spec without key", map[string]any{"version": "27"}, false},
		{"string true is not a boolean", map[string]any{"enableApi": "true"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := APIEnabled(tc.spec); got != tc.want {
				t.Errorf("APIEnabled(%v) = %v, want %v", tc.spec, got, tc.want)
			}
		})
	}
}

// The module loads with exactly the software hook its manifest declares.
func TestModuleDeclaresItsSoftwareHook(t *testing.T) {
	module, err := Module()
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if _, ok := module.Hooks["software"].(Software); !ok {
		t.Errorf("hooks = %v, want the software hook", module.Hooks)
	}
}
