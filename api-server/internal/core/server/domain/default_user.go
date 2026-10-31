package domain

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// DefaultUserSource says where a Server's effective Server Default User comes from. The values
// are the published `defaultUser.source` of the Server projection.
type DefaultUserSource string

const (
	// DefaultUserSourceServer is a value an operator set on this Server (decision 045).
	DefaultUserSourceServer DefaultUserSource = "server"
	// DefaultUserSourceOSImage is the mirrored default user of the OS Image the Server was
	// deployed with (decision 039).
	DefaultUserSourceOSImage DefaultUserSource = "os_image"
)

// defaultUserPattern is the portable POSIX login-name shape (the useradd default). It is the one
// rule for every default user swallow accepts — on a Server and on an OS Image overlay — so a name
// that is valid in one place is valid in the other.
var defaultUserPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// ValidDefaultUser reports whether user is an acceptable default user name: lowercase letters,
// digits, '_' or '-', starting with a letter or '_', up to 32 characters. It does not check that
// the account exists on any host.
func ValidDefaultUser(user string) bool {
	return defaultUserPattern.MatchString(user)
}

// EffectiveDefaultUser is the single resolution of the Server Default User that automation logs in
// as (wait-for-ssh, the inventory's default_user, ansible_user) and the projection reports: the
// value set on this Server, else the deployed OS Image's default user, else "" with an empty source,
// meaning unknown — callers then fall back to probing candidate users.
func (s *Server) EffectiveDefaultUser() (string, DefaultUserSource) {
	if user := strings.TrimSpace(s.DefaultUser); user != "" {
		return user, DefaultUserSourceServer
	}
	if s.Provisioning != nil {
		if user := strings.TrimSpace(s.Provisioning.DeployedImageDefaultUser); user != "" {
			return user, DefaultUserSourceOSImage
		}
	}
	return "", ""
}

// SudoAccess is what a Server Default User can do with sudo, observed by running `sudo -n true`
// over a Deployment Key login. Automation needs root for host changes, so it tells the operator
// whether the Site become password matters (password_required) or whether the account cannot be
// used for them at all (unavailable).
type SudoAccess string

const (
	// SudoPasswordless means sudo works without a password.
	SudoPasswordless SudoAccess = "passwordless"
	// SudoPasswordRequired means sudo asks for a password; automation uses the Site become password.
	SudoPasswordRequired SudoAccess = "password_required"
	// SudoUnavailable means the account cannot use sudo (not a sudoer, or sudo is missing).
	SudoUnavailable SudoAccess = "unavailable"
)

// HostLoginTarget addresses one deployed Server for a direct SSH login. SiteID selects the Site's
// SSH port; Name is only used in operator-facing messages.
type HostLoginTarget struct {
	SiteID  string
	Address string
	Name    string
}

// HostAccess is the port for the SSH logins that setting a Server Default User performs from
// api-server itself, outside any Ansible run (decision 045).
//
// Implementations log in to the target's address on the Site SSH port without verifying host keys
// (like the login-user probe; the Ansible run keeps the authoritative host-key check), bound every
// login by a short timeout, and honour ctx cancellation. They must never log, persist, or return a
// password or private key. Errors are the sentinels below, possibly wrapped: ErrHostUnreachable for
// a connect or handshake failure, ErrHostPasswordRejected / ErrDeploymentKeyRejected when the host
// refused authentication, ErrDeploymentKeyMissing when the installation has no Deployment Key; any
// other error is an unexpected failure.
type HostAccess interface {
	// AuthorizeDeploymentKey logs in as user with password (password or keyboard-interactive
	// authentication) and ensures the Deployment Key's public key is in that account's
	// ~/.ssh/authorized_keys, adding it only when missing. The password is used for this one login.
	AuthorizeDeploymentKey(ctx context.Context, target HostLoginTarget, user, password string) error
	// CheckDeploymentKeyLogin logs in as user with the Deployment Key and reports its sudo access.
	CheckDeploymentKeyLogin(ctx context.Context, target HostLoginTarget, user string) (SudoAccess, error)
}

var (
	// ErrInvalidDefaultUser means the requested name is not a POSIX login name.
	ErrInvalidDefaultUser = errors.New("default user must be a POSIX login name (lowercase letters, digits, '_' or '-', up to 32 characters)")
	// ErrDefaultUserNotDeployed means the Server has no running OS swallow can log in to: it is not
	// deployed or has no reported address.
	ErrDefaultUserNotDeployed = errors.New("server is not a deployed host with a known address")
	// ErrHostUnreachable means the Server could not be reached over SSH (connect or handshake).
	ErrHostUnreachable = errors.New("server could not be reached over ssh")
	// ErrHostPasswordRejected means the host refused the one-time password for the account.
	ErrHostPasswordRejected = errors.New("server rejected the password")
	// ErrDeploymentKeyRejected means the host refused a Deployment Key login for the account.
	ErrDeploymentKeyRejected = errors.New("server rejected the deployment key")
	// ErrDeploymentKeyMissing means the installation has no Deployment Key to log in with.
	ErrDeploymentKeyMissing = errors.New("the installation has no deployment key")
)

// DefaultUserError explains a failed Server Default User change in operator terms — which Server,
// which account, and what to do — while Unwrap keeps the sentinel for status mapping.
type DefaultUserError struct {
	Err    error
	Server string
	User   string
}

// Error is the operator-facing sentence the API returns as error.message. It names only the Server
// and the account — never a password — so it is safe to show and log.
func (e *DefaultUserError) Error() string {
	switch {
	case errors.Is(e.Err, ErrDefaultUserNotDeployed):
		return fmt.Sprintf("Server %q is not a deployed host with a known address, so there is no OS account to use.", e.Server)
	case errors.Is(e.Err, ErrHostUnreachable):
		return fmt.Sprintf("Server %q could not be reached over SSH. Check that it is running and that the SSH port is open, then try again.", e.Server)
	case errors.Is(e.Err, ErrHostPasswordRejected):
		return fmt.Sprintf("Server %q rejected the password for %s. Check the password, and that the host allows password logins over SSH.", e.Server, e.User)
	case errors.Is(e.Err, ErrDeploymentKeyRejected):
		return fmt.Sprintf("Server %q rejected the Deployment Key for %s. Give the account's password once so swallow installs the key, or add the key to %s's authorized_keys yourself.", e.Server, e.User, e.User)
	case errors.Is(e.Err, ErrDeploymentKeyMissing):
		return "The installation has no Deployment Key. Create one with `swallow-api deployment-key ensure` first."
	default:
		return fmt.Sprintf("The default user of Server %q could not be set: %v", e.Server, e.Err)
	}
}

// Unwrap exposes the sentinel so delivery maps the status with errors.Is.
func (e *DefaultUserError) Unwrap() error { return e.Err }
