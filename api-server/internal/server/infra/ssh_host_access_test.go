package infra

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// testHost is an in-process SSH server that runs each exec request with /bin/sh in a temporary
// HOME, so the adapter's real scripts change a real authorized_keys file. Publickey logins are
// accepted only for keys in that file, which is how the tests prove the key was installed.
type testHost struct {
	address  string
	port     int
	home     string
	bin      string
	password string
	// keyboardOnly refuses plain password authentication, like a PAM-only sshd.
	keyboardOnly bool
}

func startTestHost(t *testing.T, password string, keyboardOnly bool) *testHost {
	t.Helper()
	host := &testHost{home: t.TempDir(), bin: t.TempDir(), password: password, keyboardOnly: keyboardOnly}
	host.setSudo(t, "exit 0")
	writeScript(t, filepath.Join(host.bin, "id"), "echo 1000")

	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if host.authorized(key) {
				return nil, nil
			}
			return nil, errors.New("key not authorized")
		},
		KeyboardInteractiveCallback: func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := challenge("", "", []string{"Password: "}, []bool{false})
			if err == nil && len(answers) == 1 && answers[0] == host.password {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		},
	}
	if !keyboardOnly {
		config.PasswordCallback = func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == host.password {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		}
	}
	config.AddHostKey(hostSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	host.address = "127.0.0.1"
	host.port = listener.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go host.serve(connection, config)
		}
	}()
	return host
}

func (h *testHost) authorized(key ssh.PublicKey) bool {
	raw, err := os.ReadFile(filepath.Join(h.home, ".ssh", "authorized_keys"))
	if err != nil {
		return false
	}
	for len(raw) > 0 {
		parsed, _, _, rest, err := ssh.ParseAuthorizedKey(raw)
		if err != nil {
			return false
		}
		if bytes.Equal(parsed.Marshal(), key.Marshal()) {
			return true
		}
		raw = rest
	}
	return false
}

func (h *testHost) setSudo(t *testing.T, body string) {
	t.Helper()
	writeScript(t, filepath.Join(h.bin, "sudo"), body)
}

// serve handles one connection: every session's exec request runs under /bin/sh with the test
// HOME and the fake sudo/id first on PATH, and reports the exit status.
func (h *testHost) serve(connection net.Conn, config *ssh.ServerConfig) {
	_, channels, requests, err := ssh.NewServerConn(connection, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer channel.Close()
			for request := range channelRequests {
				if request.Type != "exec" {
					_ = request.Reply(false, nil)
					continue
				}
				var payload struct{ Command string }
				if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
					_ = request.Reply(false, nil)
					continue
				}
				_ = request.Reply(true, nil)
				command := exec.Command("/bin/sh", "-c", payload.Command)
				command.Env = []string{"HOME=" + h.home, "PATH=" + h.bin + ":" + os.Getenv("PATH")}
				command.Stdin = channel
				command.Stdout = channel
				command.Stderr = channel.Stderr()
				status := uint32(0)
				if err := command.Run(); err != nil {
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) {
						status = uint32(exitErr.ExitCode())
					} else {
						status = 127
					}
				}
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				return
			}
		}()
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

type staticKey struct {
	pem string
	ok  bool
}

func (k staticKey) DeploymentPrivateKey(context.Context) (string, bool, error) {
	return k.pem, k.ok, nil
}

type staticPort int

func (p staticPort) SSHPort(context.Context, string) (int, error) { return int(p), nil }

func newDeploymentKey(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate deployment key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatalf("marshal deployment key: %v", err)
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatalf("deployment public key: %v", err)
	}
	return string(pem.EncodeToMemory(block)), sshPublic
}

func newAccess(t *testing.T, host *testHost) (*SSHHostAccess, ssh.PublicKey, serverdomain.HostLoginTarget) {
	t.Helper()
	privatePEM, public := newDeploymentKey(t)
	access := NewSSHHostAccess(staticKey{pem: privatePEM, ok: true}, staticPort(host.port))
	return access, public, serverdomain.HostLoginTarget{SiteID: "site-a", Address: host.address, Name: "lab"}
}

func authorizedKeyLines(t *testing.T, host *testHost) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(host.home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

// The password login installs the key once (repeating it is a no-op), with sshd-acceptable
// permissions, after which a Deployment Key login works.
func TestAuthorizeDeploymentKeyInstallsOnceAndEnablesKeyLogin(t *testing.T) {
	host := startTestHost(t, "s3cret", false)
	access, public, target := newAccess(t, host)
	ctx := context.Background()

	if _, err := access.CheckDeploymentKeyLogin(ctx, target, "amd"); !errors.Is(err, serverdomain.ErrDeploymentKeyRejected) {
		t.Fatalf("CheckDeploymentKeyLogin before install error = %v, want ErrDeploymentKeyRejected", err)
	}
	for i := 0; i < 2; i++ {
		if err := access.AuthorizeDeploymentKey(ctx, target, "amd", "s3cret"); err != nil {
			t.Fatalf("AuthorizeDeploymentKey (run %d) error = %v", i+1, err)
		}
	}
	lines := authorizedKeyLines(t, host)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public)))) {
		t.Errorf("authorized_keys = %q, want exactly the Deployment Key once", lines)
	}
	for path, want := range map[string]os.FileMode{
		filepath.Join(host.home, ".ssh"):                    0o700,
		filepath.Join(host.home, ".ssh", "authorized_keys"): 0o600,
	} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
			t.Errorf("mode of %s = %v (%v), want %v", path, info.Mode().Perm(), err, want)
		}
	}
	if sudo, err := access.CheckDeploymentKeyLogin(ctx, target, "amd"); err != nil || sudo != serverdomain.SudoPasswordless {
		t.Errorf("CheckDeploymentKeyLogin after install = %q, %v; want passwordless", sudo, err)
	}
}

// A file without a final newline must not get the key glued onto its last line, and a manually
// added copy with another comment counts as present.
func TestAuthorizeDeploymentKeyRepairsNewlineAndRecognisesExistingKey(t *testing.T) {
	host := startTestHost(t, "s3cret", false)
	access, public, target := newAccess(t, host)
	if err := os.MkdirAll(filepath.Join(host.home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOperatorKeyMaterial0000000000000000000000 operator"
	if err := os.WriteFile(filepath.Join(host.home, ".ssh", "authorized_keys"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := access.AuthorizeDeploymentKey(ctx, target, "amd", "s3cret"); err != nil {
		t.Fatalf("AuthorizeDeploymentKey error = %v", err)
	}
	lines := authorizedKeyLines(t, host)
	if len(lines) != 2 || lines[0] != existing {
		t.Fatalf("authorized_keys = %q, want the operator line intact plus the Deployment Key", lines)
	}

	manual := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public))) + " added-by-hand"
	if err := os.WriteFile(filepath.Join(host.home, ".ssh", "authorized_keys"), []byte(manual+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := access.AuthorizeDeploymentKey(ctx, target, "amd", "s3cret"); err != nil {
		t.Fatalf("AuthorizeDeploymentKey error = %v", err)
	}
	if lines := authorizedKeyLines(t, host); len(lines) != 1 || lines[0] != manual {
		t.Errorf("authorized_keys = %q, want the hand-added copy only", lines)
	}
}

// PAM-only hosts accept the password only through keyboard-interactive.
func TestAuthorizeDeploymentKeyUsesKeyboardInteractive(t *testing.T) {
	host := startTestHost(t, "s3cret", true)
	access, _, target := newAccess(t, host)
	if err := access.AuthorizeDeploymentKey(context.Background(), target, "amd", "s3cret"); err != nil {
		t.Fatalf("AuthorizeDeploymentKey error = %v", err)
	}
}

func TestHostAccessErrors(t *testing.T) {
	host := startTestHost(t, "s3cret", false)
	access, _, target := newAccess(t, host)
	ctx := context.Background()

	if err := access.AuthorizeDeploymentKey(ctx, target, "amd", "wrong"); !errors.Is(err, serverdomain.ErrHostPasswordRejected) {
		t.Errorf("AuthorizeDeploymentKey with a wrong password error = %v, want ErrHostPasswordRejected", err)
	}

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()
	unreachable := NewSSHHostAccess(access.keys, staticPort(closedPort))
	if _, err := unreachable.CheckDeploymentKeyLogin(ctx, target, "amd"); !errors.Is(err, serverdomain.ErrHostUnreachable) {
		t.Errorf("CheckDeploymentKeyLogin on a closed port error = %v, want ErrHostUnreachable", err)
	}

	noKey := NewSSHHostAccess(staticKey{}, staticPort(host.port))
	if _, err := noKey.CheckDeploymentKeyLogin(ctx, target, "amd"); !errors.Is(err, serverdomain.ErrDeploymentKeyMissing) {
		t.Errorf("CheckDeploymentKeyLogin without a Deployment Key error = %v, want ErrDeploymentKeyMissing", err)
	}
}

// sudo's own refusal decides between "asks for a password" and "cannot use sudo".
func TestCheckDeploymentKeyLoginClassifiesSudo(t *testing.T) {
	host := startTestHost(t, "s3cret", false)
	access, _, target := newAccess(t, host)
	ctx := context.Background()
	if err := access.AuthorizeDeploymentKey(ctx, target, "amd", "s3cret"); err != nil {
		t.Fatalf("AuthorizeDeploymentKey error = %v", err)
	}
	tests := []struct {
		name string
		sudo string
		want serverdomain.SudoAccess
	}{
		{name: "passwordless", sudo: "exit 0", want: serverdomain.SudoPasswordless},
		{name: "password required", sudo: "echo 'sudo: a password is required' >&2; exit 1", want: serverdomain.SudoPasswordRequired},
		{name: "not a sudoer", sudo: "echo 'Sorry, user amd may not run sudo on lab.' >&2; exit 1", want: serverdomain.SudoUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host.setSudo(t, tc.sudo)
			got, err := access.CheckDeploymentKeyLogin(ctx, target, "amd")
			if err != nil || got != tc.want {
				t.Errorf("CheckDeploymentKeyLogin = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

// The password must never surface in an error, even when the install script fails.
func TestAuthorizeDeploymentKeyFailureDoesNotLeakPassword(t *testing.T) {
	host := startTestHost(t, "s3cret-value", false)
	access, _, target := newAccess(t, host)
	// A read-only home makes the script fail after login.
	if err := os.Chmod(host.home, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(host.home, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions, so the script cannot be made to fail this way")
	}
	err := access.AuthorizeDeploymentKey(context.Background(), target, "amd", "s3cret-value")
	if err == nil {
		t.Fatal("AuthorizeDeploymentKey on a read-only home succeeded, want an error")
	}
	if strings.Contains(err.Error(), "s3cret-value") {
		t.Errorf("error %q contains the password", err.Error())
	}
}
