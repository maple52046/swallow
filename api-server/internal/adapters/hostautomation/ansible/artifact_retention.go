package ansible

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ExpiredArtifacts returns the run ids (directory names) of the runner artifact directories older
// than cutoff. It only examines direct children so a malformed artifact cannot escape the root.
func ExpiredArtifacts(root string, cutoff time.Time) ([]string, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var expired []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if info.ModTime().Before(cutoff) {
			expired = append(expired, entry.Name())
		}
	}
	return expired, nil
}

// RemoveArtifacts removes the artifact directories of the given runs, which ExpiredArtifacts
// returned.
func RemoveArtifacts(root string, runIDs []string) error {
	for _, runID := range runIDs {
		if err := os.RemoveAll(filepath.Join(root, filepath.Base(runID))); err != nil {
			return err
		}
	}
	return nil
}
