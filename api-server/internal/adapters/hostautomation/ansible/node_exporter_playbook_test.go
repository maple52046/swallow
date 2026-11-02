package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Node Exporter is the OS distribution's package run by the package's own service, so the host
// needs no container runtime: the role installs prometheus-node-exporter (Debian family) or
// node_exporter (RHEL family), never runs a container, removes swallow's containerised unit that
// holds the same port before the package's service starts, and checks the exporter answers on the
// port swallow's Prometheus discovery scrapes.
func TestNodeExporterRoleInstallsTheOSPackage(t *testing.T) {
	raw, err := os.ReadFile(shippedFile(t, filepath.Join("roles", "node_exporter", "tasks", "main.yml")))
	if err != nil {
		t.Fatalf("read node_exporter role: %v", err)
	}
	role := string(raw)
	for _, required := range []string{
		"'Debian': 'prometheus-node-exporter'",
		"'RedHat': 'node_exporter'",
		"node_exporter_state == 'present'",
		"node_exporter_state == 'absent'",
		"node_exporter_unsupported_os",
		"node_exporter_package_unavailable",
		"/etc/systemd/system/{{ node_exporter_legacy_unit }}",
		"http://127.0.0.1:{{ node_exporter_port }}/metrics",
	} {
		if !strings.Contains(role, required) {
			t.Errorf("node_exporter role missing %q", required)
		}
	}
	if strings.Contains(role, "docker run") {
		t.Error("node_exporter role must not run the exporter in a container")
	}
	legacy := strings.Index(role, "Remove the containerised node-exporter unit")
	install := strings.Index(role, "Install the node-exporter package (Debian family)")
	if legacy < 0 || install < 0 || legacy > install {
		t.Error("the containerised unit must be removed before the package is installed")
	}
}

// The deploy playbook passes the trusted port to the role; the uninstall playbook runs the same
// role with its removal branch.
func TestNodeExporterPlaybooksDriveTheRole(t *testing.T) {
	for name, required := range map[string][]string{
		"deploy-node-exporter.yml":    {"role: node_exporter", "swallow_node_exporter_port"},
		"uninstall-node-exporter.yml": {"role: node_exporter", "node_exporter_state: absent"},
	} {
		raw, err := os.ReadFile(shippedFile(t, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, want := range required {
			if !strings.Contains(string(raw), want) {
				t.Errorf("%s missing %q", name, want)
			}
		}
	}
}
