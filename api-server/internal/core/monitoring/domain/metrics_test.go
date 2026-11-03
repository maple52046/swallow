package domain

import (
	"errors"
	"slices"
	"testing"
)

// Ids are validated rather than escaped, because an adapter places them in a label matcher.
func TestNewServerSet_RejectsUnsafeIDs(t *testing.T) {
	for _, ids := range [][]string{
		{"srv-1", ""},
		{`srv-1"} or up{`},
		{"srv 1"},
		{"srv|2"},
	} {
		if _, err := NewServerSet(ids); !errors.Is(err, ErrInvalidServerID) {
			t.Errorf("NewServerSet(%q) error = %v, want ErrInvalidServerID", ids, err)
		}
	}
}

// The set keeps the caller's order and cannot be changed through the slices it was built from or
// returns.
func TestNewServerSet_KeepsOrderAndOwnsItsIDs(t *testing.T) {
	ids := []string{"srv-b", "srv_a", "SRV-3"}
	servers, err := NewServerSet(ids)
	if err != nil {
		t.Fatalf("NewServerSet(%q): %v", ids, err)
	}
	ids[0] = "changed"
	got := servers.IDs()
	got[1] = "changed"
	if want := []string{"srv-b", "srv_a", "SRV-3"}; !slices.Equal(servers.IDs(), want) {
		t.Errorf("IDs() = %q, want %q", servers.IDs(), want)
	}
}

// Every named metric has a distinct name and one of the defined scope aggregates.
func TestNamedMetrics_AreDistinctWithKnownAggregates(t *testing.T) {
	seen := map[string]bool{}
	for _, metric := range NamedMetrics() {
		if seen[metric.Name] {
			t.Errorf("metric %q is listed twice", metric.Name)
		}
		seen[metric.Name] = true
		switch metric.Aggregate {
		case ScopeAverage, ScopeSum, ScopeMaximum:
		default:
			t.Errorf("metric %q has scope aggregate %q, want average, sum, or maximum", metric.Name, metric.Aggregate)
		}
	}
}
