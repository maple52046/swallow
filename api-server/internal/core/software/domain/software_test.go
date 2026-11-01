package domain

import "testing"

// Only an explicit true enables the API; a legacy spec without the key must read as disabled.
func TestDockerAPIEnabled(t *testing.T) {
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
			if got := DockerAPIEnabled(tc.spec); got != tc.want {
				t.Errorf("DockerAPIEnabled(%v) = %v, want %v", tc.spec, got, tc.want)
			}
		})
	}
}
