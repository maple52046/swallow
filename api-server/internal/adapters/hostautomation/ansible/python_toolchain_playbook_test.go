package ansible

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// shippedContentLine is one non-comment line of a shipped playbook or role file, named as the
// bundle addresses its file (bundlePath).
type shippedContentLine struct {
	file   string // the file as the bundle addresses it
	role   string // the role the file belongs to, or "" for a playbook
	number int
	text   string
}

// shippedContentLines returns every line of the shipped playbooks and role files that is not a
// comment, skipping the data roles copy as they are (unscannedContent).
func shippedContentLines(t *testing.T) []shippedContentLine {
	t.Helper()
	var lines []shippedContentLine
	for _, module := range moduleDirs(t) {
		for _, sub := range []string{"playbooks", "roles"} {
			root := filepath.Join(module, sub)
			if _, err := os.Stat(root); os.IsNotExist(err) {
				continue
			}
			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() || unscannedContent(path) {
					return err
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				file, role := bundlePath(module, path), ""
				if parts := strings.Split(filepath.ToSlash(file), "/"); parts[0] == "roles" && len(parts) > 1 {
					role = parts[1]
				}
				for number, text := range strings.Split(string(raw), "\n") {
					trimmed := strings.TrimSpace(text)
					if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "{#") || strings.HasPrefix(trimmed, "//") {
						continue
					}
					lines = append(lines, shippedContentLine{file: file, role: role, number: number + 1, text: text})
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	return lines
}

// pythonIndexSetting matches content that chooses a Python package index: the run's index itself, an
// index environment variable of pip or uv, or an index option.
var pythonIndexSetting = regexp.MustCompile(
	`swallow_artifacts\.python_index|\b(?:PIP|UV)_\w*INDEX\w*|\bPIP_TRUSTED_HOST\b|--(?:extra-|default-)?index(?:-url)?\b`)

// Hosts reach the Site's Python index through the configuration artifact_sources writes (pip.conf,
// uv.toml), so a role that installs Python packages follows the Site's choice without knowing about
// the Local Repository (decision 080). No other role or playbook reads the run's index or sets one.
func TestOnlyArtifactSourcesChoosesAPythonIndex(t *testing.T) {
	for _, line := range shippedContentLines(t) {
		if line.role == artifactSourcesRole {
			continue
		}
		if match := pythonIndexSetting.FindString(line.text); match != "" {
			t.Errorf("%s:%d sets a Python index (%s); leave it to the host configuration %s writes",
				line.file, line.number, match, artifactSourcesRole)
		}
	}
}

// osPythonToolchain matches content that builds a Python environment with the OS's own tools: the
// venv module, or the packages it and source builds need.
var osPythonToolchain = regexp.MustCompile(`-m\s+venv\b|\bpython3-venv\b|\bpython3-dev\b|\bbuild-essential\b`)

// A role builds Python environments with the toolchain python_toolchain finds or supplies (any
// CPython 3.12 and uv) from wheels, so compute nodes get no compilers or Python headers for it
// (decision 080).
func TestNoRoleBuildsPythonWithTheOSToolchain(t *testing.T) {
	for _, line := range shippedContentLines(t) {
		if match := osPythonToolchain.FindString(line.text); match != "" {
			t.Errorf("%s:%d uses %s; build the environment with uv from %s instead", line.file, line.number, match, pythonToolchainRole)
		}
	}
}
