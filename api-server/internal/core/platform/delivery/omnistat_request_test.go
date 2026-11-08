package delivery

import (
	"testing"
)

// A Slurm deploy's omnistat object carries hardwareCounters into the deploy input, false when
// omitted; the deviceMetricsExporter object, which shares its request type, has no such setting.
func TestDeployInputReadsTheOmnistatHardwareCounters(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "asked for", body: `{"siteId":"site-1","omnistat":{"enabled":true,"hardwareCounters":true}}`, want: true},
		{name: "omitted", body: `{"siteId":"site-1","omnistat":{"enabled":true}}`, want: false},
		{name: "turned off", body: `{"siteId":"site-1","omnistat":{"enabled":true,"hardwareCounters":false}}`, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input, err := DeployInputFromRequest([]byte(tc.body))
			if err != nil {
				t.Fatalf("DeployInputFromRequest(%s) error = %v", tc.body, err)
			}
			if input.Omnistat == nil || input.Omnistat.HardwareCounters != tc.want {
				t.Errorf("DeployInputFromRequest(%s) omnistat = %+v, want hardwareCounters %v", tc.body, input.Omnistat, tc.want)
			}
		})
	}
}
