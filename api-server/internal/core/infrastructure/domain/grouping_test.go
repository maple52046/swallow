package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"trims surrounding whitespace", "  rack-a  ", "rack-a", false},
		{"rejects empty", "", "", true},
		{"rejects whitespace only", "   ", "", true},
		{"rejects over length bound", strings.Repeat("a", maxGroupNameLen+1), "", true},
		{"accepts at length bound", strings.Repeat("a", maxGroupNameLen), strings.Repeat("a", maxGroupNameLen), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateName(tc.input)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidGroup) {
					t.Fatalf("ValidateName(%q) error = %v, want ErrInvalidGroup", tc.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateName(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ValidateName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
