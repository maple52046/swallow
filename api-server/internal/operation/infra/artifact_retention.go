package infra

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// PruneArtifacts removes completed runner artifact directories older than cutoff.
// It only examines direct children so a malformed artifact cannot escape the root.
func PruneArtifacts(root string, cutoff time.Time) (int, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return removed, err
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
