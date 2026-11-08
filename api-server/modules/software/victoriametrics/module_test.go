package victoriametrics

import (
	"errors"
	"reflect"
	"testing"

	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

// The module loads with exactly the software hook its manifest declares, and its spec schema lists
// the catalog entry's spec fields.
func TestModuleDeclaresItsSoftwareHookAndSchema(t *testing.T) {
	module, err := Module()
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if _, ok := module.Hooks[softwaredomain.HookSoftware].(Software); !ok {
		t.Errorf("hooks = %v, want the software hook", module.Hooks)
	}
	schema, err := module.Manifest.SpecSchema()
	if err != nil || schema == nil {
		t.Fatalf("SpecSchema() = %v, %v; want the module's schema", schema, err)
	}
	if got, want := schema.Names(), []string{softwaredomain.SpecListen, softwaredomain.SpecRetention}; !reflect.DeepEqual(got, want) {
		t.Errorf("schema properties = %v, want the catalog's spec fields %v", got, want)
	}
}

// A retention is bounded at 100 years in its own unit; a unit VictoriaMetrics reads differently (a
// bare number, m) or a value below a day is refused before any Workflow exists.
func TestValidateSpecBoundsTheRetention(t *testing.T) {
	tests := []struct {
		retention string
		wantErr   bool
	}{
		{retention: "", wantErr: false},
		{retention: "1M", wantErr: false},
		{retention: " 90d ", wantErr: false},
		{retention: "36500d", wantErr: false},
		{retention: "36501d", wantErr: true},
		{retention: "5214w", wantErr: false},
		{retention: "5215w", wantErr: true},
		{retention: "1200M", wantErr: false},
		{retention: "1201M", wantErr: true},
		{retention: "100y", wantErr: false},
		{retention: "101y", wantErr: true},
		{retention: "12h", wantErr: true},
		{retention: "1m", wantErr: true},
		{retention: "30", wantErr: true},
		{retention: "0d", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.retention, func(t *testing.T) {
			err := Software{}.ValidateSpec(nil, map[string]any{softwaredomain.SpecRetention: tc.retention})
			if tc.wantErr && !errors.Is(err, softwaredomain.ErrSpecInvalid) {
				t.Errorf("ValidateSpec(retention %q) = %v, want ErrSpecInvalid", tc.retention, err)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateSpec(retention %q) = %v, want accepted", tc.retention, err)
			}
		})
	}
}

// The core and the dashboard check the retention's format with the schema's pattern and the hook
// with its own; both must accept exactly the same values, or a value would pass one and fail the
// other.
func TestRetentionFormatAgreesWithTheSpecSchema(t *testing.T) {
	module, err := Module()
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	schema, err := module.Manifest.SpecSchema()
	if err != nil {
		t.Fatalf("SpecSchema: %v", err)
	}
	for _, retention := range []string{"1d", "7d", "2w", "1M", "18M", "1y", "99999d", "0d", "01d", "1m", "1h", "12", "1D", "1W", "1Y", "1d2h", "d", "100000d"} {
		schemaAccepts := schema.Validate(map[string]any{softwaredomain.SpecRetention: retention}) == nil
		if hookAccepts := retentionPattern.MatchString(retention); schemaAccepts != hookAccepts {
			t.Errorf("retention %q: schema accepts %v, hook accepts %v", retention, schemaAccepts, hookAccepts)
		}
	}
}

// The recorded spec always names the pinned release and the listen address and retention that were
// applied, whether the request gave them or not.
func TestNormalizeSpecRecordsTheReleaseAndDefaults(t *testing.T) {
	tests := []struct {
		name string
		spec map[string]any
		want map[string]any
	}{
		{
			name: "omitted spec",
			spec: nil,
			want: map[string]any{"version": Version, "listen": "loopback", "retention": DefaultRetention},
		},
		{
			name: "chosen listen address and retention",
			spec: map[string]any{"listen": "management", "retention": "90d"},
			want: map[string]any{"version": Version, "listen": "management", "retention": "90d"},
		},
		{
			name: "blank values take the defaults",
			spec: map[string]any{"listen": " ", "retention": ""},
			want: map[string]any{"version": Version, "listen": "loopback", "retention": DefaultRetention},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Software{}).NormalizeSpec(tc.spec); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("NormalizeSpec(%v) = %v, want %v", tc.spec, got, tc.want)
			}
		})
	}
}

// The playbook receives the recorded release, listen address, retention, and the module's port; an
// uninstall of a record without a spec still gets complete variables.
func TestInstallVarsPassTheRecordedSpec(t *testing.T) {
	tests := []struct {
		name string
		spec map[string]any
		want map[string]any
	}{
		{
			name: "recorded spec",
			spec: map[string]any{"version": Version, "listen": "management", "retention": "1y"},
			want: map[string]any{
				"swallow_victoriametrics_version": Version, "swallow_victoriametrics_listen": "management",
				"swallow_victoriametrics_retention": "1y", "swallow_victoriametrics_port": Port,
			},
		},
		{
			name: "record without a spec",
			spec: nil,
			want: map[string]any{
				"swallow_victoriametrics_version": Version, "swallow_victoriametrics_listen": "loopback",
				"swallow_victoriametrics_retention": DefaultRetention, "swallow_victoriametrics_port": Port,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Software{}).InstallVars(nil, tc.spec); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("InstallVars(%v) = %v, want %v", tc.spec, got, tc.want)
			}
		})
	}
}
