package config

import "testing"

// The Node Exporter controller is on unless the environment turns it off, and a value that is not
// a boolean stops the load instead of leaving the controller on.
func TestApplyEnv_NodeExporterEnsure(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    bool
		wantErr bool
	}{
		{name: "unset keeps the default", value: "", want: true},
		{name: "false turns it off", value: "false", want: false},
		{name: "true keeps it on", value: "true", want: true},
		{name: "zero turns it off", value: "0", want: false},
		{name: "padded value", value: " false ", want: false},
		{name: "malformed value", value: "off", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SWALLOW_WORKER_NODE_EXPORTER_ENSURE", tc.value)
			cfg := DefaultConfig()
			err := applyEnv(&cfg)
			if tc.wantErr {
				if err == nil {
					t.Errorf("applyEnv(%q) error = nil, want an error", tc.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyEnv(%q) error = %v", tc.value, err)
			}
			if cfg.API.NodeExporterEnsure != tc.want {
				t.Errorf("applyEnv(%q) NodeExporterEnsure = %v, want %v", tc.value, cfg.API.NodeExporterEnsure, tc.want)
			}
		})
	}
}
