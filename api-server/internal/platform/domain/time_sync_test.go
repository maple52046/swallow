package domain

import (
	"errors"
	"testing"
)

func TestParseTimeSyncCheck(t *testing.T) {
	tests := []struct {
		raw     string
		want    TimeSyncCheck
		wantErr bool
	}{
		{raw: "", want: TimeSyncCheckEnforce},
		{raw: "enforce", want: TimeSyncCheckEnforce},
		{raw: "warn", want: TimeSyncCheckWarn},
		{raw: "skip", wantErr: true},
		{raw: "Warn", wantErr: true},
	}
	for _, tc := range tests {
		got, err := ParseTimeSyncCheck(tc.raw)
		if tc.wantErr {
			if !errors.Is(err, ErrInvalidDeployment) {
				t.Errorf("ParseTimeSyncCheck(%q) error = %v, want ErrInvalidDeployment", tc.raw, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseTimeSyncCheck(%q) = %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
}
