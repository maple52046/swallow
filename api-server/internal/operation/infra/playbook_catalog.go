package infra

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

type playbookManifest struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Playbooks     []playbookManifestEntry `json:"playbooks"`
}

type playbookManifestEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ManifestCatalog is an immutable allowlist shipped with the release.
type ManifestCatalog struct {
	root  string
	paths map[string]string
}

// LoadManifestCatalog validates the manifest and every referenced path.
func LoadManifestCatalog(manifestPath, projectRoot string) (*ManifestCatalog, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read playbook manifest: %w", err)
	}
	var manifest playbookManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("parse playbook manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported playbook manifest schemaVersion %d", manifest.SchemaVersion)
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	catalog := &ManifestCatalog{root: root, paths: make(map[string]string, len(manifest.Playbooks))}
	for _, entry := range manifest.Playbooks {
		if entry.Name == "" || entry.Path == "" {
			return nil, fmt.Errorf("playbook manifest entries require name and path")
		}
		if _, exists := catalog.paths[entry.Name]; exists {
			return nil, fmt.Errorf("duplicate playbook name %q", entry.Name)
		}
		clean := filepath.Clean(entry.Path)
		if filepath.IsAbs(clean) || clean == ".." || len(clean) >= 3 && clean[:3] == ".."+string(filepath.Separator) {
			return nil, fmt.Errorf("%w: %q escapes project root", operationdomain.ErrPlaybookNotAllowed, entry.Path)
		}
		full := filepath.Join(root, clean)
		if info, statErr := os.Stat(full); statErr != nil || info.IsDir() {
			return nil, fmt.Errorf("manifest playbook %q: %w", entry.Name, statErr)
		}
		catalog.paths[entry.Name] = clean
	}
	return catalog, nil
}

// Resolve returns a manifest-owned relative path, never an operator path.
func (c *ManifestCatalog) Resolve(name string) (string, error) {
	path, ok := c.paths[name]
	if !ok {
		return "", fmt.Errorf("%w: %s", operationdomain.ErrPlaybookNotAllowed, name)
	}
	return path, nil
}

// ProjectRoot is the immutable playbook bundle root.
func (c *ManifestCatalog) ProjectRoot() string { return c.root }
