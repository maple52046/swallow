package infra

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// oci.conf must keep the invariants the lab proved (decision 066): the same ContainerPath string on
// every node (PMIx passes expanded paths between nodes, so no %n), no MountSpoolDir (the ranks are
// told the host path of the PMIx server directory, so the spool must appear at that same path inside
// the container), the crun path Ubuntu's AppArmor profile grants user namespaces to, and slurmd's
// NOTIFY_SOCKET kept away from crun. Uninstall removes the file and the probe bundle.
func TestOCIConfKeepsTheCrossNodeInvariants(t *testing.T) {
	conf := slurmPlaybookFile(t, "roles", "slurm_config", "templates", "oci.conf.j2")
	for _, want := range []string{
		`ContainerPath="{{ slurm_oci_container_path }}"`,
		`RunTimeEnvExclude="^(SLURM_CONF|SLURM_CONF_SERVER|NOTIFY_SOCKET)="`,
		`{{ slurm_oci_crun }} --rootless=true`,
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("oci.conf.j2 does not contain %q", want)
		}
	}
	if strings.Contains(conf, "MountSpoolDir") {
		t.Error("oci.conf must not move the step spool away from its host path (MountSpoolDir)")
	}
	playbook := slurmPlaybookFile(t, "deploy-slurm.yml")
	for _, want := range []string{
		"slurm_oci_crun: /usr/bin/crun",
		"slurm_oci_container_path: /run/user/%U/slurm-oci",
	} {
		if !strings.Contains(playbook, want) {
			t.Errorf("deploy-slurm.yml does not set %q", want)
		}
	}
	if strings.Contains(playbook, "slurm_oci_container_path: ") && strings.Contains(
		strings.SplitN(strings.SplitN(playbook, "slurm_oci_container_path: ", 2)[1], "\n", 2)[0], "%n") {
		t.Error("ContainerPath must not contain the node name (%n)")
	}
	uninstall := slurmPlaybookFile(t, "uninstall-slurm.yml")
	for _, want := range []string{"/etc/slurm/oci.conf", "/var/lib/swallow/oci"} {
		if !strings.Contains(uninstall, want) {
			t.Errorf("uninstall-slurm.yml does not remove %s", want)
		}
	}
}

// fakeCrunSpec is a crun stand-in that answers `spec --rootless` with a minimal rootless spec.
const fakeCrunSpec = `#!/bin/sh
cat > config.json <<'EOF'
{"ociVersion":"1.0.0","process":{"terminal":true,"args":["sh"],"cwd":"/"},"root":{"path":"rootfs","readonly":true},
 "mounts":[],"linux":{"namespaces":[{"type":"pid"},{"type":"network"},{"type":"ipc"},{"type":"uts"},{"type":"user"},{"type":"cgroup"},{"type":"mount"}]}}
EOF
`

func runOCIProbeBundle(t *testing.T, bundle, crun string) bool {
	t.Helper()
	script := filepath.Join("..", "..", "..", "automation", "playbooks", "roles", "slurm_oci", "files", "oci-probe-bundle.py")
	out, err := exec.Command("python3", script, "--bundle", bundle, "--crun", crun).Output()
	if err != nil {
		t.Fatalf("oci-probe-bundle: %v", err)
	}
	var report struct{ Changed bool }
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("parse %q: %v", out, err)
	}
	return report.Changed
}

// The probe bundle keeps the host network (MPI ranks of different nodes and slurmstepd's loopback PMIx
// server), a tmpfs /run (the runtime mounts the step spool at its host path there) with the host's
// MUNGE socket (PMIx authenticates ranks with it), the node's /dev for the device cgroup to judge, and
// is rewritten only when it differs, so a rerun reports no change.
func TestOCIProbeBundleIsHostNetworkedAndIdempotent(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not available")
	}
	dir := t.TempDir()
	crun := filepath.Join(dir, "crun")
	if err := os.WriteFile(crun, []byte(fakeCrunSpec), 0o755); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(dir, "probe")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if !runOCIProbeBundle(t, bundle, crun) {
		t.Error("the first run must report a change")
	}
	if runOCIProbeBundle(t, bundle, crun) {
		t.Error("a rerun over an unchanged bundle must report no change")
	}

	raw, err := os.ReadFile(filepath.Join(bundle, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Root   struct{ Path string }
		Mounts []struct{ Destination, Source string }
		Linux  struct{ Namespaces []struct{ Type string } }
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	for _, namespace := range spec.Linux.Namespaces {
		if namespace.Type == "network" {
			t.Error("the probe bundle must use the host network namespace")
		}
	}
	if spec.Root.Path != filepath.Join(bundle, "rootfs") {
		t.Errorf("root = %q, want the bundle's own rootfs", spec.Root.Path)
	}
	mounts := map[string]string{}
	for _, mount := range spec.Mounts {
		mounts[mount.Destination] = mount.Source
	}
	for destination, source := range map[string]string{"/dev": "/dev", "/run": "tmpfs", "/run/munge": "/run/munge"} {
		if mounts[destination] != source {
			t.Errorf("mount %s = %q, want %q", destination, mounts[destination], source)
		}
	}
}
