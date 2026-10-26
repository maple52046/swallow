package infra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The slurmrestd role is a small IaC convergence loop: declare the systemd override, apply a
// configuration-triggered restart before validating runtime, repair a stale live process only
// when its declared listener is absent, and preserve systemd diagnostics if convergence fails.
func TestSlurmRestdRoleConvergesBeforeListenerVerification(t *testing.T) {
	root := filepath.Join("..", "..", "..", "automation", "playbooks", "roles", "slurm_slurmrestd")
	tasksRaw, err := os.ReadFile(filepath.Join(root, "tasks", "main.yml"))
	if err != nil {
		t.Fatalf("read slurmrestd tasks: %v", err)
	}
	tasks := string(tasksRaw)

	required := []string{
		"ansible.builtin.template:",
		"src: override.conf.j2",
		"register: slurm_slurmrestd_override",
		"Converge slurmrestd service to the declared unit",
		"state: \"{{ 'restarted' if slurm_slurmrestd_override.changed else 'started' }}\"",
		"Probe the desired slurmrestd TCP listener",
		"failed_when: false",
		"Reconcile slurmrestd runtime drift",
		"slurm_slurmrestd_listener_probe.msg",
		"Wait for slurmrestd to accept TCP connections",
		"Capture slurmrestd systemd status for diagnostics",
		"argv: [systemctl, status, slurmrestd, --no-pager, --full]",
		"argv: [journalctl, -u, slurmrestd, --no-pager, --lines=30]",
		"slurmrestd did not converge to the declared TCP listener",
	}
	for _, value := range required {
		if !strings.Contains(tasks, value) {
			t.Errorf("slurmrestd tasks missing %q", value)
		}
	}

	declaration := strings.Index(tasks, "Declare the slurmrestd TCP and JWT runtime")
	service := strings.Index(tasks, "Converge slurmrestd service to the declared unit")
	probe := strings.Index(tasks, "Probe the desired slurmrestd TCP listener")
	reconcile := strings.Index(tasks, "Reconcile slurmrestd runtime drift")
	verify := strings.Index(tasks, "Wait for slurmrestd to accept TCP connections")
	if declaration == -1 || service == -1 || probe == -1 || reconcile == -1 || verify == -1 {
		t.Fatal("slurmrestd role must declare, apply, probe, reconcile, and verify its runtime")
	}
	if !(declaration < service && service < probe && probe < reconcile && reconcile < verify) {
		t.Errorf("slurmrestd convergence order is invalid: declaration=%d service=%d probe=%d reconcile=%d verify=%d",
			declaration, service, probe, reconcile, verify)
	}

	templateRaw, err := os.ReadFile(filepath.Join(root, "templates", "override.conf.j2"))
	if err != nil {
		t.Fatalf("read slurmrestd systemd template: %v", err)
	}
	template := string(templateRaw)
	for _, value := range []string{
		"ExecStart=",
		"Environment=SLURM_JWT=daemon",
		"Environment=SLURM_CONF={{ slurm_conf_dir }}/slurm.conf",
		"ExecStart=/usr/sbin/slurmrestd -a rest_auth/jwt 0.0.0.0:{{ slurmrestd_port }}",
	} {
		if !strings.Contains(template, value) {
			t.Errorf("slurmrestd systemd template missing %q", value)
		}
	}
}
