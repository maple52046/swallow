package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sssdRoleTasks parses one task file of the sssd_client role.
func sssdRoleTasks(t *testing.T, file string) []map[string]any {
	t.Helper()
	var tasks []map[string]any
	readYAML(t, shippedFile(t, filepath.Join("roles", "sssd_client", "tasks", file)), &tasks)
	return tasks
}

// The directory client changes how the host resolves accounts, sudo, and SSH keys only after SSSD
// read the Directory and the local accounts were checked against it, so a host that cannot reach
// the Directory, or whose accounts collide with it, keeps working as before (identity-and-access.md
// §8). The secret it receives never reaches a log.
func TestSSSDInstallChangesLoginPathsOnlyAfterTheDirectoryAnswers(t *testing.T) {
	tasks := sssdRoleTasks(t, "install.yml")
	probe := taskIndex(tasks, "ansible.builtin.fail", "sssd_directory_unreachable")
	conflicts := taskIndex(tasks, "ansible.builtin.include_tasks", "conflicts.yml")
	nsswitch := taskIndex(tasks, "ansible.builtin.replace", "nsswitch")
	sudoers := taskIndex(tasks, "ansible.builtin.lineinfile", "sudoers")
	dropin := taskIndex(tasks, "ansible.builtin.copy", "AuthorizedKeysCommand")
	for name, index := range map[string]int{"probe": probe, "conflicts": conflicts, "nsswitch": nsswitch, "sudoers": sudoers, "drop-in": dropin} {
		if index < 0 {
			t.Fatalf("install.yml has no %s task", name)
		}
	}
	if !(probe < conflicts && conflicts < nsswitch && conflicts < sudoers && conflicts < dropin) {
		t.Errorf("order: probe %d, conflicts %d, nsswitch %d, sudoers %d, drop-in %d; the Directory must answer and the conflicts be checked before any login path changes",
			probe, conflicts, nsswitch, sudoers, dropin)
	}
	config := taskIndex(tasks, "ansible.builtin.template", "sssd.conf")
	if config < 0 || tasks[config]["no_log"] != true {
		t.Errorf("the SSSD configuration holds the reader's password; its template task must set no_log")
	}
	if !strings.Contains(stringify(tasks[config]), `mode: 0600`) {
		t.Errorf("sssd.conf must be 0600: %s", stringify(tasks[config]))
	}
}

// The configuration reads only the contract's variables, verifies the Directory's certificate, and
// enforces Host Access.
func TestSSSDConfigurationUsesTheContract(t *testing.T) {
	data, err := os.ReadFile(shippedFile(t, filepath.Join("roles", "sssd_client", "templates", "sssd.conf.j2")))
	if err != nil {
		t.Fatal(err)
	}
	conf := string(data)
	for _, want := range []string{
		"swallow_directory_uri", "swallow_directory_base_dn", "swallow_directory_bind_dn",
		"swallow_directory_bind_password", "swallow_directory_id_start",
		"ldap_tls_reqcert = demand", "access_provider = ldap", "ldap_access_order = host",
		"sudo_provider = ldap", "ldap_user_ssh_public_key = sshPublicKey",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("sssd.conf.j2 lacks %q", want)
		}
	}
}

// Uninstall stops sshd and the name service from using the Directory before SSSD stops, so no
// login path points at a stopped service.
func TestSSSDRemovalUnwindsLoginPathsFirst(t *testing.T) {
	tasks := sssdRoleTasks(t, "remove.yml")
	dropin := taskIndex(tasks, "ansible.builtin.file", "sshd_dropin")
	nsswitch := taskIndex(tasks, "ansible.builtin.replace", "nsswitch")
	stop := taskIndex(tasks, "ansible.builtin.systemd", "Stop SSSD")
	if dropin < 0 || nsswitch < 0 || stop < 0 || !(dropin < stop && nsswitch < stop) {
		t.Errorf("order: drop-in %d, nsswitch %d, stop %d; sshd and nsswitch must stop using SSSD first", dropin, nsswitch, stop)
	}
}
