package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateBootISOName(t *testing.T) {
	if got, err := ValidateBootISOName("  south-rack "); err != nil || got != "south-rack" {
		t.Errorf("ValidateBootISOName trims: got %q, %v", got, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", 64), "line\nbreak"} {
		if _, err := ValidateBootISOName(bad); !errors.Is(err, ErrInvalidBootISO) {
			t.Errorf("ValidateBootISOName(%q) error = %v, want ErrInvalidBootISO", bad, err)
		}
	}
}
