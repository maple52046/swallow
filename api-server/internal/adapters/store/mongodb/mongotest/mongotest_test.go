package mongotest

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// Only test databases older than an hour are stale; swallow's own databases and young test
// databases are never returned.
func TestStaleDatabases(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	old := fmt.Sprintf("swallow_workflow_repo_test_%d", now.Add(-2*time.Hour).UnixNano())
	legacy := fmt.Sprintf("swallow_claim_tx_test_%d", now.Add(-3*time.Hour).UnixNano())
	young := fmt.Sprintf("swallow_workflow_repo_test_%d", now.Add(-time.Minute).UnixNano())
	names := []string{"swallow", "swallow_dev", "admin", "swallow_test_notime", old, legacy, young}

	got := StaleDatabases(names, now)
	want := []string{old, legacy}
	if !slices.Equal(got, want) {
		t.Errorf("StaleDatabases() = %v, want %v", got, want)
	}
}
