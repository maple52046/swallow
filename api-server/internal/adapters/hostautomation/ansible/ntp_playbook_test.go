package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The configure-ntp playbook (decision 065) is named by the deployment launchers rather than a site
// mapping, so a manifest that fails to register it would only surface when an OS deployment is
// accepted. This pins the registration and the playbooks' contract: the trusted swallow_ntp_servers
// variable, the client the OS already has before installing one, one client at a time, and a
// bounded sync wait that fails with a stable code; and the platform playbooks checking every clock
// before they install anything.
func TestConfigureNTPPlaybookContract(t *testing.T) {
	automation := filepath.Join("..", "..", "..", "..", "automation")
	catalog, err := LoadManifestCatalog(
		filepath.Join(automation, "manifest.json"),
		filepath.Join(automation, "playbooks"),
	)
	if err != nil {
		t.Fatalf("load shipped manifest: %v", err)
	}
	if _, err := catalog.Resolve("configure-ntp"); err != nil {
		t.Fatalf("shipped manifest must register configure-ntp: %v", err)
	}
	if _, err := catalog.Resolve("check-time-sync"); err == nil {
		t.Errorf("check-time-sync must not be a playbook: the platform playbooks check time themselves")
	}

	root := filepath.Join(automation, "playbooks")
	tasks := filepath.Join("roles", "ntp_client", "tasks", "main.yml")
	cases := []struct {
		path     string
		required []string
	}{
		{path: "configure-ntp.yml", required: []string{"swallow_ntp_servers", "role: ntp_client"}},
		{path: filepath.Join("roles", "ntp_client", "defaults", "main.yml"), required: []string{
			"swallow_ntp_servers", "ntp_client_sync_tries", "ntp_client_sync_interval",
		}},
		{path: filepath.Join("roles", "ntp_client", "vars", "main.yml"), required: []string{
			"timesyncd: [systemd-timesyncd]", "chrony: [chrony, chronyd]", "ntpd: [ntpsec, ntp, ntpd]",
		}},
		{path: tasks, required: []string{
			"[ntp_servers_invalid]",
			"ansible.builtin.service_facts",
			"/usr/sbin/chronyd", "/usr/sbin/ntpd",
			"Stop the other time clients",
			"--property=NTPMessage",
			"- waitsync",
			"ntpq -pn",
			"[ntp_not_synchronized]",
		}},
		{path: filepath.Join("roles", "ntp_client", "templates", "timesyncd.conf.j2"), required: []string{
			"[Time]", "NTP=\n", "FallbackNTP=",
		}},
		{path: filepath.Join("roles", "ntp_client", "templates", "chrony.conf.j2"), required: []string{
			"{% for server in ntp_client_servers %}", "iburst", "makestep",
		}},
		{path: filepath.Join("roles", "ntp_client", "templates", "ntp.conf.j2"), required: []string{
			"{% for server in ntp_client_servers %}", "iburst", "tinker panic 0",
		}},
		// The platforms check every node's clock with the trusted mode and bound before installing.
		{path: "deploy-kubernetes.yml", required: []string{"role: time_sync_check"}},
		{path: "deploy-slurm.yml", required: []string{"role: time_sync_check"}},
		{path: filepath.Join("roles", "time_sync_check", "defaults", "main.yml"), required: []string{
			"swallow_ntp_servers", "swallow_time_sync_check | default('enforce')", "swallow_time_sync_max_offset",
		}},
		{path: filepath.Join("roles", "time_sync_check", "tasks", "main.yml"), required: []string{
			"--property=NTPSynchronized", "ntp_offset.py", "[time_not_synchronized]", "[time_sync_warning]",
			"time_sync_check_mode == 'enforce'", "any_errors_fatal: true",
		}},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(filepath.Join(root, tc.path))
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}
		content := string(raw)
		for _, required := range tc.required {
			if !strings.Contains(content, required) {
				t.Errorf("%s missing %q", tc.path, required)
			}
		}
	}

	// Installing a client is only the fallback for a host with none.
	raw, err := os.ReadFile(filepath.Join(root, tasks))
	if err != nil {
		t.Fatalf("read %s: %v", tasks, err)
	}
	content := string(raw)
	install := strings.Index(content, "- name: Install chrony on a host without a time client")
	if install < 0 || !strings.Contains(content[install:], "when: ntp_client_kind == 'none'") ||
		strings.Count(content, "name: chrony") != 2 {
		t.Errorf("%s must install chrony only in the block for a host without a time client", tasks)
	}

	// The platform check runs before the platform's first package or binary is installed.
	for _, playbook := range []struct{ path, before string }{
		{path: "deploy-kubernetes.yml", before: "role: k0s_binary"},
		{path: "deploy-slurm.yml", before: "role: slurm_preflight"},
	} {
		raw, err := os.ReadFile(filepath.Join(root, playbook.path))
		if err != nil {
			t.Fatalf("read %s: %v", playbook.path, err)
		}
		check, next := strings.Index(string(raw), "role: time_sync_check"), strings.Index(string(raw), playbook.before)
		if check < 0 || next < 0 || check > next {
			t.Errorf("%s must run time_sync_check before %s", playbook.path, playbook.before)
		}
	}

	// The templates must not read DHCP-offered or distribution sources: only the installation's server.
	for _, name := range []string{"chrony.conf.j2", "ntp.conf.j2", "timesyncd.conf.j2"} {
		template, err := os.ReadFile(filepath.Join(root, "roles", "ntp_client", "templates", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, forbidden := range []string{"sourcedir", "confdir", "ntp.ubuntu.com", "pool.ntp.org"} {
			if strings.Contains(string(template), forbidden) {
				t.Errorf("%s must use only the installation's NTP server, found %q", name, forbidden)
			}
		}
	}
}
