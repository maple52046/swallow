package ansible

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// holdRole is the shared role that holds the OS's own package maintenance while a playbook runs
// (decision 073).
const holdRole = "package_maintenance_hold"

// packageModules are the task modules that take the package manager's lock: they change packages or
// refresh the package index.
var packageModules = map[string]bool{
	"apt": true, "ansible.builtin.apt": true,
	"dnf": true, "ansible.builtin.dnf": true,
	"dnf5": true, "ansible.builtin.dnf5": true,
	"yum": true, "ansible.builtin.yum": true,
	"package": true, "ansible.builtin.package": true,
}

// repositoryModules refresh the package index, and so take the lock, unless update_cache is false.
var repositoryModules = map[string]bool{"apt_repository": true, "ansible.builtin.apt_repository": true}

var commandModules = []string{
	"command", "ansible.builtin.command", "shell", "ansible.builtin.shell", "raw", "ansible.builtin.raw",
}

// packageCommand matches a command line that changes packages or refreshes the package index.
var packageCommand = regexp.MustCompile(
	`\b(apt-get|apt|dpkg|dnf|yum)\s+(-\S+\s+)*(install|remove|purge|upgrade|dist-upgrade|update|-i|--install|-r|--remove|-P|--purge)\b`)

// commandLine renders a command module's arguments (a string, or cmd/argv) as one line.
func commandLine(value any) string {
	switch args := value.(type) {
	case string:
		return args
	case map[string]any:
		if cmd, ok := args["cmd"].(string); ok {
			return cmd
		}
		if argv, ok := args["argv"].([]any); ok {
			parts := make([]string, 0, len(argv))
			for _, part := range argv {
				parts = append(parts, fmt.Sprint(part))
			}
			return strings.Join(parts, " ")
		}
	}
	return fmt.Sprint(value)
}

// isPackageTask reports whether a task takes the package manager's lock, whichever way it is
// written: a package module, a repository module that refreshes the index, or a package command.
func isPackageTask(task map[string]any) bool {
	for key, value := range task {
		if packageModules[key] {
			return true
		}
		if repositoryModules[key] {
			args, _ := value.(map[string]any)
			return args == nil || args["update_cache"] != false
		}
	}
	for _, key := range commandModules {
		if value, ok := task[key]; ok && packageCommand.MatchString(commandLine(value)) {
			return true
		}
	}
	return false
}

// collectPackageTasks appends the package tasks of a task list, descending into blocks.
func collectPackageTasks(tasks []any, into *[]map[string]any) {
	for _, item := range tasks {
		task, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if isPackageTask(task) {
			*into = append(*into, task)
		}
		for _, key := range []string{"block", "rescue", "always"} {
			if nested, ok := task[key].([]any); ok {
				collectPackageTasks(nested, into)
			}
		}
	}
}

// packageTasksIn returns the package tasks of a role's task or handler file.
func packageTasksIn(t *testing.T, path string) []map[string]any {
	t.Helper()
	var tasks []any
	readYAML(t, path, &tasks)
	var found []map[string]any
	collectPackageTasks(tasks, &found)
	return found
}

// playPackageTasks returns the package tasks a playbook's plays list themselves, outside roles.
func playPackageTasks(plays []map[string]any) []map[string]any {
	var found []map[string]any
	for _, play := range plays {
		for _, key := range []string{"pre_tasks", "tasks", "post_tasks", "handlers"} {
			if tasks, ok := play[key].([]any); ok {
				collectPackageTasks(tasks, &found)
			}
		}
	}
	return found
}

// roleTaskFiles lists a role's task and handler files.
func roleTaskFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	for _, sub := range []string{"tasks", "handlers"} {
		matches, err := filepath.Glob(filepath.Join(dir, sub, "*.yml"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	return files
}

// playUsesRole reports whether a play lists the role in its roles: section, where the hold must be
// taken so it runs before any task of the play.
func playUsesRole(play map[string]any, name string) bool {
	entries, _ := play["roles"].([]any)
	for _, entry := range entries {
		if roleEntryName(entry) == name {
			return true
		}
	}
	return false
}

// playReleasesHold reports whether a play includes the hold role's release tasks.
func playReleasesHold(play map[string]any) bool {
	tasks, _ := play["tasks"].([]any)
	for _, item := range tasks {
		task, _ := item.(map[string]any)
		for _, key := range roleTaskKeys {
			if args, ok := task[key].(map[string]any); ok && args["name"] == holdRole && args["tasks_from"] == "release" {
				return true
			}
		}
	}
	return false
}

// Every shipped playbook that changes packages holds the OS's own package maintenance for its whole
// run (decision 073): its first play takes the hold on every target before anything else, and its
// last play releases it. Without the hold, unattended-upgrades can start mid-run, take the package
// lock, and fail whichever install it collides with (the apt module does not wait for it).
func TestPackageChangingPlaybooksHoldPackageMaintenance(t *testing.T) {
	seen := map[string]bool{}
	holding := 0
	for _, entry := range readShippedManifest(t).Playbooks {
		if seen[entry.Path] {
			continue
		}
		seen[entry.Path] = true
		var plays []map[string]any
		readYAML(t, filepath.Join(modulesRoot, entry.Path), &plays)
		changes := len(playPackageTasks(plays)) > 0
		resolved, _, _ := roleClosure(t, entry.Path)
		for _, dir := range resolved {
			for _, file := range roleTaskFiles(t, dir) {
				changes = changes || len(packageTasksIn(t, file)) > 0
			}
		}
		if !changes {
			continue
		}
		holding++
		first, last := plays[0], plays[len(plays)-1]
		if first["hosts"] != "all" || first["gather_facts"] != false || !playUsesRole(first, holdRole) {
			t.Errorf("%s changes packages, so its first play must take the hold on every host before "+
				"anything else (hosts: all, gather_facts: false, roles: [%s])", entry.Path, holdRole)
		}
		if len(plays) < 3 || last["hosts"] != "all" || !playReleasesHold(last) {
			t.Errorf("%s changes packages, so its last play must release the hold on every host "+
				"(hosts: all, include_role %s tasks_from: release)", entry.Path, holdRole)
		}
	}
	if holding == 0 {
		t.Fatal("found no shipped playbook that changes packages; is the package task detection right?")
	}
}

// Every package task in the shipped content retries. The hold keeps the OS's own maintenance away,
// but a mirror can still fail for a moment and something outside swallow can still hold the lock.
func TestShippedPackageTasksRetry(t *testing.T) {
	check := func(file string, tasks []map[string]any) {
		for _, task := range tasks {
			if task["until"] == nil || task["retries"] == nil {
				rel, _ := filepath.Rel(modulesRoot, file)
				t.Errorf("%s: package task %q must retry (register, until: ... is succeeded, retries, delay)",
					rel, task["name"])
			}
		}
	}
	for _, module := range moduleDirs(t) {
		roles, err := filepath.Glob(filepath.Join(module, "roles", "*"))
		if err != nil {
			t.Fatal(err)
		}
		for _, dir := range roles {
			for _, file := range roleTaskFiles(t, dir) {
				check(file, packageTasksIn(t, file))
			}
		}
		playbooks, err := filepath.Glob(filepath.Join(module, "playbooks", "*.yml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range playbooks {
			var plays []map[string]any
			readYAML(t, file, &plays)
			check(file, playPackageTasks(plays))
		}
	}
}

// The hold changes only runtime state: it stops and starts timers, never enables, disables, or masks
// them, so a reboot or the lease's own expiry always gives the operator's update policy back.
func TestPackageMaintenanceHoldOnlyChangesRuntimeState(t *testing.T) {
	dir := roleDir(t, holdRole)
	script, err := os.ReadFile(filepath.Join(dir, "files", "swallow-package-hold.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{`"enable"`, `"disable"`, `"mask"`, `"--now"`} {
		if strings.Contains(string(script), verb) {
			t.Errorf("swallow-package-hold.py runs systemctl %s; the hold may only stop and start units", verb)
		}
	}
	for _, file := range roleTaskFiles(t, dir) {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"enabled:", "masked:"} {
			if strings.Contains(string(raw), key) {
				t.Errorf("%s sets %s; the hold may only change runtime state", filepath.Base(file), key)
			}
		}
	}
}

// fakeSystemctl answers the systemctl calls the hold makes. A unit's active state lives in
// $FAKE/units/<unit>; a unit without a file is inactive, like a unit systemd does not have.
const fakeSystemctl = `#!/bin/sh
cmd=$1; shift
case $cmd in
is-active) for u in "$@"; do if [ -f "$FAKE/units/$u" ]; then cat "$FAKE/units/$u"; else echo inactive; fi; done ;;
stop) for u in "$@"; do case $u in
    swallow-package-hold-expiry-*) rm -f "$FAKE/units/$u" ;;
    *) if [ -f "$FAKE/units/$u" ]; then echo inactive > "$FAKE/units/$u"; fi ;;
  esac; done ;;
start) for u in "$@"; do echo active > "$FAKE/units/$u"; done ;;
list-units) for f in "$FAKE"/units/swallow-package-hold-expiry-*.timer; do
    if [ -f "$f" ]; then echo "${f##*/} loaded active waiting expiry"; fi; done ;;
esac
exit 0
`

// fakeSystemdRun loads the transient timer named by --unit.
const fakeSystemdRun = `#!/bin/sh
for a in "$@"; do case $a in --unit=*) echo active > "$FAKE/units/${a#--unit=}.timer" ;; esac; done
`

// holdHost runs the hold script against fake systemd tools and a scratch root.
type holdHost struct {
	t      *testing.T
	dir    string
	root   string
	script string
	env    []string
}

func newHoldHost(t *testing.T, units map[string]string) *holdHost {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not available")
	}
	dir := t.TempDir()
	host := &holdHost{
		t:      t,
		dir:    dir,
		root:   filepath.Join(dir, "root"),
		script: filepath.Join(roleDir(t, holdRole), "files", "swallow-package-hold.py"),
	}
	for _, sub := range []string{"bin", "units", filepath.Join("root", "proc")} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	host.write(filepath.Join("bin", "systemctl"), fakeSystemctl, 0o755)
	host.write(filepath.Join("bin", "systemd-run"), fakeSystemdRun, 0o755)
	host.write(filepath.Join("root", "proc", "locks"), "", 0o644)
	for unit, state := range units {
		host.write(filepath.Join("units", unit), state+"\n", 0o644)
	}
	host.env = append(os.Environ(),
		"FAKE="+dir,
		"PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SWALLOW_PACKAGE_HOLD_ROOT="+host.root,
	)
	return host
}

func (h *holdHost) write(rel, content string, mode os.FileMode) {
	h.t.Helper()
	path := filepath.Join(h.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		h.t.Fatal(err)
	}
}

// run executes one hold command and returns its stdout, stderr, and exit status.
func (h *holdHost) run(args ...string) (stdout, stderr string, status int) {
	h.t.Helper()
	cmd := exec.Command("python3", append([]string{h.script}, args...)...)
	cmd.Env = h.env
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		status = exit.ExitCode()
	case err != nil:
		h.t.Fatalf("run hold %v: %v", args, err)
	}
	return out.String(), errOut.String(), status
}

// mustRun executes one hold command that must succeed and decodes its JSON report into report.
func (h *holdHost) mustRun(report any, args ...string) {
	h.t.Helper()
	stdout, stderr, status := h.run(args...)
	if status != 0 {
		h.t.Fatalf("hold %v exit = %d (stderr %q), want 0", args, status, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), report); err != nil {
		h.t.Fatalf("hold %v printed %q: %v", args, stdout, err)
	}
}

// unitState returns a unit's state in the fake systemd; a unit it never saw is inactive.
func (h *holdHost) unitState(unit string) string {
	h.t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.dir, "units", unit))
	if os.IsNotExist(err) {
		return "inactive"
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

// expiryDeadlines returns the deadlines of the loaded expiry timers.
func (h *holdHost) expiryDeadlines() []int64 {
	h.t.Helper()
	matches, err := filepath.Glob(filepath.Join(h.dir, "units", "swallow-package-hold-expiry-*.timer"))
	if err != nil {
		h.t.Fatal(err)
	}
	deadlines := make([]int64, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(match), "swallow-package-hold-expiry-"), ".timer")
		deadline, err := strconv.ParseInt(name, 10, 64)
		if err != nil {
			h.t.Fatalf("expiry timer %s: %v", match, err)
		}
		deadlines = append(deadlines, deadline)
	}
	sort.Slice(deadlines, func(i, j int) bool { return deadlines[i] < deadlines[j] })
	return deadlines
}

// leaseDeadline reads the deadline the hold recorded for a lease.
func (h *holdHost) leaseDeadline(lease string) int64 {
	h.t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.root, "run", "swallow", "package-hold", "leases", lease))
	if err != nil {
		h.t.Fatal(err)
	}
	var doc struct{ Deadline int64 }
	if err := json.Unmarshal(raw, &doc); err != nil {
		h.t.Fatal(err)
	}
	return doc.Deadline
}

// makeLeasesDue moves every lease's deadline into the past, as if the expiry timer had fired late.
func (h *holdHost) makeLeasesDue() {
	h.t.Helper()
	dir := filepath.Join(h.root, "run", "swallow", "package-hold", "leases")
	entries, err := os.ReadDir(dir)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), []byte(`{"deadline": 1}`), 0o600); err != nil {
			h.t.Fatal(err)
		}
	}
}

// Two overlapping leases share one hold: the timers stay stopped until the last lease ends, one
// expiry timer always tracks the latest deadline, and only the timers the hold stopped start again,
// so a timer the operator had stopped stays stopped.
func TestPackageHoldGivesBackOnlyTheTimersItStopped(t *testing.T) {
	host := newHoldHost(t, map[string]string{
		"apt-daily.timer":         "inactive",
		"apt-daily-upgrade.timer": "active",
	})
	now := time.Now().Unix()

	var acquired struct{ Stopped, Held []string }
	host.mustRun(&acquired, "acquire", "first", "3600", "0", "60")
	if got, want := acquired.Stopped, []string{"apt-daily-upgrade.timer"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("first acquire stopped %v, want %v", got, want)
	}
	if got := host.unitState("apt-daily-upgrade.timer"); got != "inactive" {
		t.Errorf("apt-daily-upgrade.timer = %s while held, want inactive", got)
	}

	host.mustRun(&acquired, "acquire", "second", "7200", "0", "60")
	if len(acquired.Stopped) != 0 || fmt.Sprint(acquired.Held) != "[apt-daily-upgrade.timer]" {
		t.Errorf("second acquire stopped %v holding %v, want nothing new and [apt-daily-upgrade.timer]",
			acquired.Stopped, acquired.Held)
	}
	if got := host.expiryDeadlines(); len(got) != 1 || got[0] < now+7200 {
		t.Errorf("expiry timers %v after a 7200s lease, want one at or after %d", got, now+7200)
	}

	var released struct{ Leases []string }
	host.mustRun(&released, "release", "second", "60")
	if got := host.expiryDeadlines(); len(got) != 1 || got[0] < now+3600 || got[0] > now+3600+60 {
		t.Errorf("expiry timers %v after releasing the longer lease, want one at the first lease's deadline %d",
			got, now+3600)
	}

	var expired struct{ Leases, Started []string }
	host.mustRun(&expired, "expire")
	if len(expired.Started) != 0 || host.unitState("apt-daily-upgrade.timer") != "inactive" {
		t.Errorf("expire with live leases %v started %v, want nothing started", expired.Leases, expired.Started)
	}

	host.makeLeasesDue()
	host.mustRun(&expired, "expire")
	if fmt.Sprint(expired.Started) != "[apt-daily-upgrade.timer]" {
		t.Errorf("expire with every lease due started %v, want [apt-daily-upgrade.timer]", expired.Started)
	}
	if got := host.unitState("apt-daily-upgrade.timer"); got != "active" {
		t.Errorf("apt-daily-upgrade.timer = %s after the hold ended, want active", got)
	}
	if got := host.unitState("apt-daily.timer"); got != "inactive" {
		t.Errorf("apt-daily.timer = %s after the hold ended, want the operator's inactive kept", got)
	}
	if got := host.expiryDeadlines(); len(got) != 0 {
		t.Errorf("expiry timers %v after the hold ended, want none", got)
	}
}

// fileLockKey renders path's device and inode the way /proc/locks lists them (hex major:minor).
func fileLockKey(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("python3", "-c",
		`import os, sys; s = os.stat(sys.argv[1]); print("%02x:%02x:%d" % (os.major(s.st_dev), os.minor(s.st_dev), s.st_ino))`,
		path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// acquire waits for package maintenance that was already running (a maintenance service, or a
// process holding a package lock) instead of interrupting it, and fails with
// [package_maintenance_busy] when it outlasts the wait; the lease then ends after the grace. A
// process only waiting for the lock does not count.
func TestPackageHoldWaitsForRunningMaintenance(t *testing.T) {
	tests := []struct {
		name     string
		units    map[string]string
		locks    func(t *testing.T, host *holdHost) string
		wantBusy string
	}{
		{
			name:     "maintenance service still running",
			units:    map[string]string{"apt-daily-upgrade.service": "activating"},
			wantBusy: "apt-daily-upgrade.service",
		},
		{
			name: "package lock held",
			locks: func(t *testing.T, host *holdHost) string {
				host.write(filepath.Join("root", "proc", "4242", "comm"), "unattended-upgr\n", 0o644)
				return "1: POSIX  ADVISORY  WRITE 4242 " + fileLockKey(t, filepath.Join(host.root, "var", "lib", "dpkg", "lock-frontend")) + " 0 EOF\n"
			},
			wantBusy: "pid 4242 unattended-upgr",
		},
		{
			name: "process waiting for the lock",
			locks: func(t *testing.T, host *holdHost) string {
				return "1: -> POSIX  ADVISORY  WRITE 4243 " + fileLockKey(t, filepath.Join(host.root, "var", "lib", "dpkg", "lock-frontend")) + " 0 EOF\n"
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host := newHoldHost(t, tc.units)
			host.write(filepath.Join("root", "var", "lib", "dpkg", "lock-frontend"), "", 0o640)
			if tc.locks != nil {
				host.write(filepath.Join("root", "proc", "locks"), tc.locks(t, host), 0o644)
			}
			now := time.Now().Unix()
			_, stderr, status := host.run("acquire", "deploy", "3600", "0", "30")
			if tc.wantBusy == "" {
				if status != 0 {
					t.Errorf("acquire exit = %d (stderr %q), want 0", status, stderr)
				}
				return
			}
			if status != 75 || !strings.HasPrefix(stderr, "[package_maintenance_busy]") || !strings.Contains(stderr, tc.wantBusy) {
				t.Errorf("acquire = exit %d, stderr %q; want exit 75 and [package_maintenance_busy] naming %q",
					status, stderr, tc.wantBusy)
			}
			if got := host.leaseDeadline("deploy"); got > now+30+5 {
				t.Errorf("lease deadline after a busy acquire = %d, want within the 30s grace of %d", got, now)
			}
		})
	}
}
