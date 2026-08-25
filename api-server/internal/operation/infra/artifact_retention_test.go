package infra

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneArtifactsRemovesOnlyExpiredDirectories(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old-run")
	freshDir := filepath.Join(root, "fresh-run")
	if err := os.Mkdir(oldDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(freshDir, 0o750); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	removed, err := PruneArtifacts(root, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old artifact still exists: %v", err)
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Fatalf("fresh artifact was removed: %v", err)
	}
}
