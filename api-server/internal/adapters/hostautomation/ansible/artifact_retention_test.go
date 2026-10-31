package ansible

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestArtifactRetentionRemovesOnlyExpiredDirectories(t *testing.T) {
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

	expired, err := ExpiredArtifacts(root, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expired, []string{"old-run"}) {
		t.Fatalf("expired = %v, want [old-run]", expired)
	}
	if err := RemoveArtifacts(root, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old artifact still exists: %v", err)
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Fatalf("fresh artifact was removed: %v", err)
	}
}
