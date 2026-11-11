package ansible

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// libvirtRoleTasks parses one task file of the libvirt role.
func libvirtRoleTasks(t *testing.T, file string) []map[string]any {
	t.Helper()
	var tasks []map[string]any
	readYAML(t, shippedFile(t, filepath.Join("roles", "libvirt", "tasks", file)), &tasks)
	return tasks
}

// taskIndex returns the index of the first task that uses module and whose YAML text contains
// marker ("" matches any), or -1.
func taskIndex(tasks []map[string]any, module, marker string) int {
	for i, task := range tasks {
		body, ok := task[module]
		if !ok {
			continue
		}
		if marker == "" || strings.Contains(stringify(task["name"])+" "+stringify(body)+" "+stringify(task["when"]), marker) {
			return i
		}
	}
	return -1
}

// stringify flattens a parsed YAML value into text for substring checks: a mapping becomes
// "key: value" pairs in key order, a list its items, and a scalar its printed form (false, 300).
func stringify(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, stringify(item))
		}
		return strings.Join(parts, " ")
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+": "+stringify(v[key]))
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(v)
	}
}

// The deploy playbook points the host at its artifact sources before the role, the uninstall
// playbook runs the role's removal branch, and the role checks the host before it makes KVM
// available, installs, and verifies, in that order.
func TestLibvirtPlaybooksDriveTheRole(t *testing.T) {
	deploy := slurmPlaybookFile(t, "deploy-libvirt.yml")
	sources, role := strings.Index(deploy, "role: artifact_sources"), strings.Index(deploy, "role: libvirt")
	if sources < 0 || role < 0 || sources > role {
		t.Error("deploy-libvirt.yml must point the host at its artifact sources before the libvirt role runs")
	}
	uninstall := slurmPlaybookFile(t, "uninstall-libvirt.yml")
	if !strings.Contains(uninstall, "role: libvirt") || !strings.Contains(uninstall, "libvirt_state: absent") {
		t.Error("uninstall-libvirt.yml must run the libvirt role with libvirt_state: absent")
	}

	main := slurmPlaybookFile(t, "roles", "libvirt", "tasks", "main.yml")
	order := []string{"preflight.yml", "kvm.yml", "install.yml", "verify.yml"}
	last := -1
	for _, file := range order {
		at := strings.Index(main, file)
		if at < 0 || at < last {
			t.Errorf("the libvirt role must include %v in that order; %s is missing or out of order", order, file)
		}
		last = at
	}
	if !strings.Contains(main, "remove.yml") || strings.Index(main, "libvirt_state == 'absent'") > strings.Index(main, "remove.yml") {
		t.Error("the libvirt role's removal branch must include remove.yml")
	}
}

// The preflight refuses a host before anything changes on it, so it holds no package task, and every
// package the role installs comes after the checks.
func TestLibvirtPreflightInstallsNothing(t *testing.T) {
	for i, task := range libvirtRoleTasks(t, "preflight.yml") {
		for key := range task {
			if packageModules[key] {
				t.Errorf("preflight.yml task %d (%v) uses %s; the preflight must not change packages", i, task["name"], key)
			}
		}
	}
	for _, marker := range []string{"[libvirt_unsupported_os]", "[libvirt_unsupported_architecture]", "[libvirt_kvm_unavailable]"} {
		if taskIndex(libvirtRoleTasks(t, "preflight.yml"), "ansible.builtin.fail", marker) < 0 {
			t.Errorf("preflight.yml must refuse with %s", marker)
		}
	}
}

// The running kernel's modules package is installed only when the kernel has no KVM module (a RAM
// deployment's image), after the role looked for it, and /dev/kvm is required afterwards.
func TestLibvirtInstallsKernelModulesOnlyWhenKVMIsMissing(t *testing.T) {
	tasks := libvirtRoleTasks(t, "kvm.yml")
	modinfo := taskIndex(tasks, "ansible.builtin.command", "modinfo")
	install := taskIndex(tasks, "ansible.builtin.apt", "linux-modules-{{ ansible_facts['kernel'] }}")
	if modinfo < 0 || install < 0 || modinfo > install {
		t.Fatalf("kvm.yml must look for the KVM module (task %d) before installing linux-modules (task %d)", modinfo, install)
	}
	if when := stringify(tasks[install]["when"]); when != "libvirt_kvm_modinfo.rc != 0" {
		t.Errorf("the linux-modules install runs when %q, want only when modinfo found no KVM module", when)
	}
	if taskIndex(tasks, "ansible.builtin.fail", "[libvirt_kvm_modules_unavailable]") < install {
		t.Error("kvm.yml must report a kernel whose modules package is not available, after trying to install it")
	}
	devKVM := taskIndex(tasks, "ansible.builtin.stat", "/dev/kvm")
	if devKVM < install || taskIndex(tasks, "ansible.builtin.fail", "[libvirt_kvm_unavailable]") < devKVM {
		t.Error("kvm.yml must refuse with [libvirt_kvm_unavailable] when /dev/kvm is missing after loading the module")
	}
}

// The role is safe on a host that already runs virtual machines: it never reboots, never restarts
// or reloads a daemon, and never changes libvirt's networks, the host's network configuration, or the
// kernel command line.
func TestLibvirtRoleLeavesARunningHypervisorAlone(t *testing.T) {
	for _, file := range []string{"main.yml", "preflight.yml", "kvm.yml", "install.yml", "verify.yml", "remove.yml"} {
		content := slurmPlaybookFile(t, "roles", "libvirt", "tasks", file)
		for _, forbidden := range []string{
			"ansible.builtin.reboot", "state: restarted", "state: reloaded",
			"net-destroy", "net-undefine", "net-define", "net-autostart", "netplan", "grub",
		} {
			if strings.Contains(content, forbidden) {
				t.Errorf("libvirt role %s contains %q; the role must leave a running hypervisor alone", file, forbidden)
			}
		}
	}
	install := libvirtRoleTasks(t, "install.yml")
	daemon := taskIndex(install, "ansible.builtin.systemd", "libvirtd")
	if daemon < 0 || stringify(install[daemon]["ansible.builtin.systemd"]) == "" ||
		!strings.Contains(stringify(install[daemon]["ansible.builtin.systemd"]), "state: started") {
		t.Error("install.yml must keep libvirtd with state: started, which leaves a running daemon alone")
	}
}

// Uninstall refuses while libvirt defines virtual machines, or cannot say whether it does, before it
// removes any package, and it removes rather than purges, so disk images under /var/lib/libvirt stay.
func TestLibvirtUninstallRefusesWhileVirtualMachinesRemain(t *testing.T) {
	tasks := libvirtRoleTasks(t, "remove.yml")
	remove := taskIndex(tasks, "ansible.builtin.apt", "")
	if remove < 0 {
		t.Fatal("remove.yml must remove the packages")
	}
	for _, marker := range []string{"[libvirt_domains_unknown]", "[libvirt_domains_present]"} {
		if at := taskIndex(tasks, "ansible.builtin.fail", marker); at < 0 || at > remove {
			t.Errorf("remove.yml must refuse with %s before removing any package", marker)
		}
	}
	apt := stringify(tasks[remove]["ansible.builtin.apt"])
	if !strings.Contains(apt, "state: absent") || !strings.Contains(apt, "purge: false") {
		t.Errorf("remove.yml removes packages with %q, want state: absent and purge: false", apt)
	}
	if content := slurmPlaybookFile(t, "roles", "libvirt", "tasks", "remove.yml"); strings.Contains(content, "ansible.builtin.file") {
		t.Error("remove.yml must not delete files; disk images under /var/lib/libvirt stay")
	}
}

// The preflight refuses an unverified OS release, a non-x86_64 CPU, and a CPU without hardware
// virtualization before anything changes on the host, and accepts Ubuntu 24.04 on an Intel or AMD
// CPU that offers it.
func TestLibvirtPreflightRefusesHostsThatCannotRunKVM(t *testing.T) {
	t.Parallel()
	run := newRoleTasksRun(t, "libvirt", "preflight", false)
	scratch := t.TempDir()
	writeScratch(t, scratch, "amd", "processor\t: 0\nflags\t\t: fpu vme svm lm\n")
	writeScratch(t, scratch, "intel", "processor\t: 0\nflags\t\t: fpu vme vmx lm\n")
	writeScratch(t, scratch, "none", "processor\t: 0\nflags\t\t: fpu vme lm\n")
	tests := []struct {
		name         string
		distribution string
		version      string
		architecture string
		cpuinfo      string
		want         string
	}{
		{name: "AMD", distribution: "Ubuntu", version: "24.04", architecture: "x86_64", cpuinfo: "amd"},
		{name: "Intel", distribution: "Ubuntu", version: "24.04", architecture: "x86_64", cpuinfo: "intel"},
		{name: "older Ubuntu", distribution: "Ubuntu", version: "22.04", architecture: "x86_64", cpuinfo: "amd", want: "[libvirt_unsupported_os]"},
		{name: "RHEL family", distribution: "Rocky", version: "9.4", architecture: "x86_64", cpuinfo: "amd", want: "[libvirt_unsupported_os]"},
		{name: "arm64", distribution: "Ubuntu", version: "24.04", architecture: "aarch64", cpuinfo: "amd", want: "[libvirt_unsupported_architecture]"},
		{name: "no virtualization", distribution: "Ubuntu", version: "24.04", architecture: "x86_64", cpuinfo: "none", want: "[libvirt_kvm_unavailable]"},
	}
	for _, tc := range tests {
		out, changed, failed := run.run(map[string]any{
			"libvirt_os_distribution": tc.distribution,
			"libvirt_os_version":      tc.version,
			"libvirt_architecture":    tc.architecture,
			"libvirt_cpuinfo_path":    filepath.Join(scratch, tc.cpuinfo),
		})
		if changed != 0 {
			t.Errorf("%s: preflight changed %d tasks, want none", tc.name, changed)
		}
		if tc.want == "" && failed != 0 {
			t.Errorf("%s: preflight failed %d tasks, want accepted:\n%s", tc.name, failed, out)
		}
		if tc.want != "" && (failed == 0 || !strings.Contains(out, tc.want)) {
			t.Errorf("%s: preflight failed %d tasks, want %s:\n%s", tc.name, failed, tc.want, out)
		}
	}
}
