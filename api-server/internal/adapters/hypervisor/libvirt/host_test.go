package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// fakeExit is a command that exited non-zero.
type fakeExit struct {
	status int
	stderr string
}

func (e *fakeExit) Error() string       { return e.stderr }
func (e *fakeExit) ExitStatus() int     { return e.status }
func (e *fakeExit) ErrorOutput() string { return e.stderr }

// fakeAccess answers scripts by the first rule whose marker the script contains, and records
// every script with the stdin it streamed.
type fakeAccess struct {
	rules   []rule
	scripts []string
	stdin   []string
}

type rule struct {
	marker string
	answer func(stdin string) (string, error)
}

func (f *fakeAccess) RunWithDeploymentKey(_ context.Context, _ serverdomain.HostLoginTarget, _ string, script string, stdin io.Reader, _ time.Duration) (string, error) {
	input := ""
	if stdin != nil {
		raw, _ := io.ReadAll(stdin)
		input = string(raw)
	}
	f.scripts, f.stdin = append(f.scripts, script), append(f.stdin, input)
	for _, r := range f.rules {
		if strings.Contains(script, r.marker) {
			return r.answer(input)
		}
	}
	return "", nil
}

func (f *fakeAccess) AuthorizePublicKey(context.Context, serverdomain.HostLoginTarget, string, string) error {
	return nil
}

var login = serverdomain.HypervisorLogin{ServerID: "hv-1", Name: "tainan-ci", Account: "ubuntu",
	Target: serverdomain.HostLoginTarget{Address: "10.170.168.22", Name: "tainan-ci"}}

func answer(out string) func(string) (string, error) {
	return func(string) (string, error) { return out, nil }
}

func TestListDomains(t *testing.T) {
	access := &fakeAccess{rules: []rule{{marker: "list --all --name", answer: answer(
		domainMarker + "\nshut off\n" + tainanDomain + "\n" + domainMarker + "\nrunning\n" + strings.Replace(tainanDomain, "lab-afde-mi308-1", "lab-afde-mi308-2", 1) + "\n")}}}
	machines, err := NewHost(access).ListDomains(context.Background(), login)
	if err != nil {
		t.Fatalf("ListDomains error = %v", err)
	}
	if len(machines) != 2 || machines[0].Name != "lab-afde-mi308-1" || !machines[0].ShutOff() || machines[1].State != "running" || !machines[0].CDROM {
		t.Errorf("machines = %+v", machines)
	}
}

func TestExplainClassifiesFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"unreachable", serverdomain.ErrHostUnreachable, serverdomain.ErrHostUnreachable},
		{"key rejected", serverdomain.ErrDeploymentKeyRejected, serverdomain.ErrDeploymentKeyRejected},
		{"no virsh", &fakeExit{127, "sh: 1: virsh: not found"}, serverdomain.ErrLibvirtUnavailable},
		{"no libvirt access", &fakeExit{1, "error: failed to connect to the hypervisor\nerror: Failed to connect socket to '/var/run/libvirt/libvirt-sock': Permission denied"}, serverdomain.ErrLibvirtUnavailable},
		{"no domain", &fakeExit{1, "error: failed to get domain 'nope'"}, serverdomain.ErrDomainNotFound},
		{"refused", &fakeExit{1, "error: unsupported configuration: boot order 1 is already used"}, serverdomain.ErrLibvirtRefused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			access := &fakeAccess{rules: []rule{{marker: "domstate", answer: func(string) (string, error) { return "", tc.err }}}}
			_, err := NewHost(access).Domain(context.Background(), login, "nope")
			var hypervisorErr *serverdomain.HypervisorError
			if !errors.As(err, &hypervisorErr) || !errors.Is(err, tc.want) {
				t.Fatalf("Domain error = %v, want a HypervisorError wrapping %v", err, tc.want)
			}
		})
	}
}

// Applying uploads the ISO when the volume differs, then defines the domain with the volume on the
// CD-ROM booting first. An identical volume is not uploaded again.
func TestApplyBootMediaUploadsAndDefines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "swallow-ipxe.iso")
	content := []byte("iso content")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	volumePath := "/var/lib/libvirt/images/swallow-ipxe-iso-1.iso"
	current := tainanDomain
	stored := ""
	access := &fakeAccess{rules: []rule{
		{marker: "vol-upload", answer: func(stdin string) (string, error) {
			stored = stdin
			return digest + "\n", nil
		}},
		{marker: "pool-info", answer: func(string) (string, error) {
			if stored == "" {
				return "", nil
			}
			return digest + "\n", nil
		}},
		{marker: "vol-path", answer: answer(volumePath + "\n")},
		{marker: "define --file", answer: func(stdin string) (string, error) {
			current = stdin
			return "", nil
		}},
		{marker: "domstate", answer: func(string) (string, error) { return "shut off\n" + current, nil }},
	}}
	host := NewHost(access)
	iso := serverdomain.BootISOFile{ID: "iso-1", Name: "rack", Path: path, Size: int64(len(content))}
	state, err := host.ApplyBootMedia(context.Background(), login, "lab-afde-mi308-1", iso)
	if err != nil {
		t.Fatalf("ApplyBootMedia error = %v", err)
	}
	if stored != string(content) {
		t.Errorf("uploaded %q, want the ISO file", stored)
	}
	if !state.Ready() || state.MediaImage != volumePath {
		t.Errorf("state = %+v, want the volume ready", state)
	}
	if !strings.Contains(current, volumePath) || !strings.Contains(current, "qemu:commandline") {
		t.Errorf("defined domain lacks the volume or the qemu namespace:\n%s", current)
	}

	uploads := 0
	for _, script := range access.scripts {
		if strings.Contains(script, "vol-upload") {
			uploads++
		}
	}
	if _, err := host.ApplyBootMedia(context.Background(), login, "lab-afde-mi308-1", iso); err != nil {
		t.Fatalf("second ApplyBootMedia error = %v", err)
	}
	again := 0
	for _, script := range access.scripts {
		if strings.Contains(script, "vol-upload") {
			again++
		}
	}
	if uploads != 1 || again != 1 {
		t.Errorf("uploads = %d then %d, want the identical volume reused", uploads, again)
	}
}

// Scripts quote domain names, so a name with shell characters cannot run anything.
func TestQuoteDomainNames(t *testing.T) {
	access := &fakeAccess{rules: []rule{{marker: "domstate", answer: answer("shut off\n")}}}
	_ = NewHost(access).DestroyDomain(context.Background(), login, "vm'; rm -rf / #")
	if len(access.scripts) != 1 || !strings.Contains(access.scripts[0], `'vm'\''; rm -rf / #'`) {
		t.Errorf("script = %q, want the name single-quoted", access.scripts)
	}
}
