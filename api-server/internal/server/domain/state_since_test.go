package domain

import (
	"testing"
	"time"
)

func TestNextStateSince(t *testing.T) {
	earlier := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	now := earlier.Add(90 * time.Second)
	tests := []struct {
		name     string
		previous *ProvisioningStatus
		state    string
		want     time.Time
	}{
		{name: "first observation starts now", previous: nil, state: "releasing", want: now},
		{name: "unchanged state keeps its start", previous: &ProvisioningStatus{State: "releasing", StateSince: earlier}, state: "releasing", want: earlier},
		{name: "changed state starts now", previous: &ProvisioningStatus{State: "deployed", StateSince: earlier}, state: "releasing", want: now},
		{name: "unrecorded start is recorded now", previous: &ProvisioningStatus{State: "releasing"}, state: "releasing", want: now},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextStateSince(tc.previous, tc.state, now); !got.Equal(tc.want) {
				t.Errorf("NextStateSince(%+v, %q) = %v, want %v", tc.previous, tc.state, got, tc.want)
			}
		})
	}
}
