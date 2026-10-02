package infra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
)

const (
	// hostDialTimeout bounds the TCP connect, so an address that drops packets fails fast.
	hostDialTimeout = 5 * time.Second
	// hostSessionTimeout bounds one whole login — handshake plus the single command — so a host
	// that accepts the connection but never answers cannot hold the HTTP request.
	hostSessionTimeout = 30 * time.Second
	// defaultSSHPort is used when the Site has no Automation Configuration or no port set.
	defaultSSHPort = 22
)

// authorizeKeyScript appends the public key read from stdin to the login account's
// ~/.ssh/authorized_keys unless a line with the same key type and material is already there (a
// manually added copy with another comment counts as present). It creates the directory and file
// with the permissions sshd's StrictModes requires, repairs a missing final newline before
// appending, and restores SELinux labels where restorecon exists (RHEL family). It runs under
// `sh -c` so it works whatever the account's login shell is, and contains no single quote so it can
// be single-quoted for that shell.
const authorizeKeyScript = `set -e
umask 077
IFS= read -r key
set -f
set -- $key
match="$1 $2"
dir="$HOME/.ssh"
file="$dir/authorized_keys"
mkdir -p "$dir"
chmod 700 "$dir"
touch "$file"
chmod 600 "$file"
if ! grep -qF -- "$match" "$file"; then
  if [ -s "$file" ] && [ -n "$(tail -c 1 "$file")" ]; then printf "\n" >> "$file"; fi
  printf "%s\n" "$key" >> "$file"
fi
if command -v restorecon >/dev/null 2>&1; then restorecon -R "$dir" >/dev/null 2>&1 || true; fi`

// sudoCheckScript exits 0 when the account may run commands as root without a password (root
// itself always may), and otherwise reports sudo's own refusal on its output.
const sudoCheckScript = `if [ "$(id -u)" -eq 0 ]; then exit 0; fi
sudo -n true`

// DeploymentKeyReader supplies the installation's Deployment Key private key (PEM) for one login.
// ok=false with a nil error means no Deployment Key exists. The value must not be logged or kept.
type DeploymentKeyReader interface {
	DeploymentPrivateKey(ctx context.Context) (privateKey string, ok bool, err error)
}

// SSHPortResolver returns the SSH port automation uses for a Site (its Automation Configuration's
// port), or 0 when the Site sets none.
type SSHPortResolver interface {
	SSHPort(ctx context.Context, siteID string) (int, error)
}

// SSHHostAccess implements serverdomain.HostAccess with x/crypto/ssh: the direct logins that
// setting a Server Default User performs from api-server (decision 045).
//
// Trust and secrecy: host keys are not verified, matching the login-user probe (the Ansible run
// keeps the authoritative check), so a one-time password goes to whatever answers at the Server's
// address — acceptable only on the internal networks swallow targets. Passwords and the private key
// are held only for the duration of one call and never logged, wrapped into errors, or returned.
// Each login uses its own connection, closed before returning; it is safe for concurrent use.
type SSHHostAccess struct {
	keys  DeploymentKeyReader
	ports SSHPortResolver
}

// NewSSHHostAccess builds the adapter over the Deployment Key and the Site SSH port. ports may be
// nil, in which case every login uses port 22.
func NewSSHHostAccess(keys DeploymentKeyReader, ports SSHPortResolver) *SSHHostAccess {
	return &SSHHostAccess{keys: keys, ports: ports}
}

// AuthorizeDeploymentKey logs in with password (offering both password and keyboard-interactive
// authentication, since PAM-backed sshd often allows only the latter) and runs authorizeKeyScript
// with the Deployment Key's public key on stdin.
func (a *SSHHostAccess) AuthorizeDeploymentKey(ctx context.Context, target serverdomain.HostLoginTarget, user, password string) error {
	signer, err := a.deploymentSigner(ctx)
	if err != nil {
		return err
	}
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " swallow-deployment-key"
	answerAll := ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i := range answers {
			answers[i] = password
		}
		return answers, nil
	})
	auth := []ssh.AuthMethod{ssh.Password(password), answerAll}
	output, err := a.run(ctx, target, user, auth, serverdomain.ErrHostPasswordRejected, authorizeKeyScript, publicKey+"\n")
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		// The script's output is the shell's own message (for example a read-only home); it never
		// echoes stdin, so it carries no secret.
		return fmt.Errorf("adding the deployment key to authorized_keys failed (exit %d): %s", exitErr.ExitStatus(), strings.TrimSpace(output))
	}
	return err
}

// CheckDeploymentKeyLogin logs in with the Deployment Key and classifies sudoCheckScript's result:
// success is passwordless; a refusal that mentions a password is password_required; any other
// non-zero exit (not a sudoer, sudo missing) is unavailable.
func (a *SSHHostAccess) CheckDeploymentKeyLogin(ctx context.Context, target serverdomain.HostLoginTarget, user string) (serverdomain.SudoAccess, error) {
	signer, err := a.deploymentSigner(ctx)
	if err != nil {
		return "", err
	}
	output, err := a.run(ctx, target, user, []ssh.AuthMethod{ssh.PublicKeys(signer)}, serverdomain.ErrDeploymentKeyRejected, sudoCheckScript, "")
	var exitErr *ssh.ExitError
	switch {
	case err == nil:
		return serverdomain.SudoPasswordless, nil
	case errors.As(err, &exitErr):
		if strings.Contains(strings.ToLower(output), "password") {
			return serverdomain.SudoPasswordRequired, nil
		}
		return serverdomain.SudoUnavailable, nil
	default:
		return "", err
	}
}

// deploymentSigner opens the Deployment Key for one login.
func (a *SSHHostAccess) deploymentSigner(ctx context.Context) (ssh.Signer, error) {
	privateKey, ok, err := a.keys.DeploymentPrivateKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("read deployment key: %w", err)
	}
	if !ok {
		return nil, serverdomain.ErrDeploymentKeyMissing
	}
	signer, err := sshprobe.ParseSigner(privateKey)
	if err != nil {
		// The parse error never contains key material, only its shape.
		return nil, fmt.Errorf("parse deployment key: %w", err)
	}
	return signer, nil
}

// run performs one login as user and executes script under `sh -c`, feeding stdin, and returns the
// command's combined output. A connect or handshake failure is ErrHostUnreachable; an
// authentication rejection is authRejected. A command that ran but exited non-zero returns its
// *ssh.ExitError so callers can inspect the output. The connection is closed on return and when ctx
// ends, which also aborts a command in progress.
func (a *SSHHostAccess) run(
	ctx context.Context,
	target serverdomain.HostLoginTarget,
	user string,
	auth []ssh.AuthMethod,
	authRejected error,
	script, stdin string,
) (string, error) {
	address := net.JoinHostPort(target.Address, strconv.Itoa(a.port(ctx, target.SiteID)))
	dialer := net.Dialer{Timeout: hostDialTimeout}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return "", fmt.Errorf("%w: %v", serverdomain.ErrHostUnreachable, err)
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	_ = connection.SetDeadline(time.Now().Add(hostSessionTimeout))

	config := &ssh.ClientConfig{
		User: user,
		Auth: auth,
		// Not verified by design (see SSHHostAccess); the handshake still negotiates encryption.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         hostDialTimeout,
	}
	clientConn, channels, requests, err := ssh.NewClientConn(connection, address, config)
	if err != nil {
		if sshprobe.IsAuthenticationError(err) {
			return "", authRejected
		}
		return "", fmt.Errorf("%w: %v", serverdomain.ErrHostUnreachable, err)
	}
	client := ssh.NewClient(clientConn, channels, requests)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("%w: open session: %v", serverdomain.ErrHostUnreachable, err)
	}
	defer session.Close()
	var output bytes.Buffer
	session.Stdout = &output
	session.Stderr = &output
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}
	err = session.Run("sh -c '" + script + "'")
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			return output.String(), err
		}
		return "", fmt.Errorf("%w: %v", serverdomain.ErrHostUnreachable, err)
	}
	return output.String(), nil
}

// port is the Site SSH port, or 22 when the Site sets none or it cannot be read; a wrong port then
// surfaces as ErrHostUnreachable rather than blocking the request on configuration.
func (a *SSHHostAccess) port(ctx context.Context, siteID string) int {
	if a.ports == nil {
		return defaultSSHPort
	}
	port, err := a.ports.SSHPort(ctx, siteID)
	if err != nil || port <= 0 {
		return defaultSSHPort
	}
	return port
}
