package ansible

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

type playbookManifest struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Playbooks     []playbookManifestEntry `json:"playbooks"`
}

// playbookManifestEntry is one allowlisted playbook. FetchesArtifacts (optional, false when
// absent) marks a playbook whose hosts install or download the artifacts its modules declare, so
// it receives the trusted variable that says where to fetch them from.
type playbookManifestEntry struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	FetchesArtifacts bool   `json:"fetchesArtifacts,omitempty"`
}

// ManifestCatalog is an immutable allowlist shipped with the release. Its root is the modules
// root: every manifest path is a module's playbook (see playbookModule), and the run takes its
// roles from that module and the shared roles only (see rolesPath).
type ManifestCatalog struct {
	root             string
	paths            map[string]string
	fetchesArtifacts map[string]bool
}

// LoadManifestCatalog validates the manifest and every referenced path. An unknown key is refused
// rather than ignored: a misspelled or retired flag would otherwise silently run a playbook without
// the trusted variable it reads.
func LoadManifestCatalog(manifestPath, projectRoot string) (*ManifestCatalog, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read playbook manifest: %w", err)
	}
	var manifest playbookManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse playbook manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported playbook manifest schemaVersion %d", manifest.SchemaVersion)
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	catalog := &ManifestCatalog{root: root, paths: make(map[string]string, len(manifest.Playbooks)), fetchesArtifacts: map[string]bool{}}
	for _, entry := range manifest.Playbooks {
		if entry.Name == "" || entry.Path == "" {
			return nil, fmt.Errorf("playbook manifest entries require name and path")
		}
		if _, exists := catalog.paths[entry.Name]; exists {
			return nil, fmt.Errorf("duplicate playbook name %q", entry.Name)
		}
		clean := filepath.Clean(entry.Path)
		if filepath.IsAbs(clean) || clean == ".." || len(clean) >= 3 && clean[:3] == ".."+string(filepath.Separator) {
			return nil, fmt.Errorf("%w: %q escapes project root", workflowdomain.ErrPlaybookNotAllowed, entry.Path)
		}
		if _, err := playbookModule(clean); err != nil {
			return nil, fmt.Errorf("manifest playbook %q: %w", entry.Name, err)
		}
		full := filepath.Join(root, clean)
		if info, statErr := os.Stat(full); statErr != nil || info.IsDir() {
			return nil, fmt.Errorf("manifest playbook %q: %w", entry.Name, statErr)
		}
		catalog.paths[entry.Name] = clean
		if entry.FetchesArtifacts {
			catalog.fetchesArtifacts[entry.Name] = true
		}
	}
	return catalog, nil
}

// FetchesArtifacts implements workflowdomain.PlaybookCatalog from the entry's fetchesArtifacts
// flag.
func (c *ManifestCatalog) FetchesArtifacts(name string) bool {
	return c.fetchesArtifacts[name]
}

// Resolve returns a manifest-owned relative path, never a user path.
func (c *ManifestCatalog) Resolve(name string) (string, error) {
	path, ok := c.paths[name]
	if !ok {
		return "", fmt.Errorf("%w: %s", workflowdomain.ErrPlaybookNotAllowed, name)
	}
	return path, nil
}

// ProjectRoot is the immutable playbook bundle root.
func (c *ManifestCatalog) ProjectRoot() string { return c.root }

// sharedModule is the content directory every run may take roles from. Playbooks that belong to
// no single module (diagnostics, NTP, host exporters) live there too.
const sharedModule = "shared"

// playbookModule returns the module directory, relative to the modules root, that owns a
// manifest path. A path must be shared/playbooks/<file>.yml or <kind>/<id>/playbooks/<file>.yml,
// so the owning module, and with it the roles a run may use, follows from the path alone.
func playbookModule(path string) (string, error) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	switch {
	case len(parts) == 3 && parts[0] == sharedModule && parts[1] == "playbooks":
		return sharedModule, nil
	case len(parts) == 4 && parts[0] != sharedModule && parts[2] == "playbooks":
		return parts[0] + "/" + parts[1], nil
	}
	return "", fmt.Errorf("%w: %q is not <module>/playbooks/<file>.yml", workflowdomain.ErrPlaybookNotAllowed, path)
}

// rolesPath is the ANSIBLE_ROLES_PATH for a run of playbook, a manifest path under root: the
// owning module's roles, then the shared roles. Listing only those keeps a module from using
// another module's roles at run time, and replaces Ansible's default search of user and system
// role directories. Ansible skips an entry that does not exist, so a module without roles of its
// own needs no special case.
func rolesPath(root, playbook string) (string, error) {
	module, err := playbookModule(playbook)
	if err != nil {
		return "", err
	}
	var dirs []string
	if module != sharedModule {
		dirs = append(dirs, filepath.Join(root, filepath.FromSlash(module), "roles"))
	}
	dirs = append(dirs, filepath.Join(root, sharedModule, "roles"))
	return strings.Join(dirs, string(filepath.ListSeparator)), nil
}
