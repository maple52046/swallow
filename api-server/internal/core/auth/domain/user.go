package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Role is a User's installation-wide User Role (glossary User Role; decision 082). The set is
// closed: admin administers everything, user may only manage its own account, and everything else
// a user may do comes from its Pool Memberships. Session tokens and API Keys carry the role, but the
// authorization layer reads the User's current role on every request.
type Role string

const (
	// RoleAdmin may do everything, everywhere.
	RoleAdmin Role = "admin"
	// RoleUser may manage only its own account; Pool Memberships grant the rest.
	RoleUser Role = "user"
)

// Valid reports whether r is one of the User Role values.
func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleUser
}

// PasswordHash is a bcrypt hash of a User's password. swallow verifies sign-ins against it and
// projects the same hash into the Directory (as `{CRYPT}`) for password-protected sudo on the
// Servers, so it must stay a crypt(3)-compatible bcrypt string.
type PasswordHash string

// MinPasswordLength and MaxPasswordBytes bound a password: bcrypt ignores everything after 72
// bytes, so a longer password would silently match any password sharing its first 72 bytes.
const (
	MinPasswordLength = 8
	MaxPasswordBytes  = 72
)

// HashPassword returns the bcrypt hash of raw. It does not validate raw; callers that accept a new
// password from a person call ValidatePassword first.
func HashPassword(raw string) (PasswordHash, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return PasswordHash(b), nil
}

// Matches reports whether raw is the password h was made from.
func (h PasswordHash) Matches(raw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(h), []byte(raw)) == nil
}

// ValidatePassword enforces the password rule for a new password: at least MinPasswordLength
// characters and at most MaxPasswordBytes bytes. It returns ErrInvalidPassword.
func ValidatePassword(raw string) error {
	if len([]rune(raw)) < MinPasswordLength {
		return fmt.Errorf("%w: it must have at least %d characters", ErrInvalidPassword, MinPasswordLength)
	}
	if len(raw) > MaxPasswordBytes {
		return fmt.Errorf("%w: it must have at most %d bytes", ErrInvalidPassword, MaxPasswordBytes)
	}
	return nil
}

// usernamePattern is the POSIX login-name rule the username follows, because the username is also
// the User's Linux login on the Servers the Directory reaches (decision 082).
var usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// systemUsernames are login names a User may not take because the supported OS Images have local
// accounts with them: system accounts and the automation accounts the images create. A User with
// one of these names would collide with a local account of the same name on every Server. The
// service accounts swallow's modules create are reserved by the composition root, which knows the
// modules (ValidateUsername's serviceAccounts).
var systemUsernames = map[string]bool{
	"root": true, "daemon": true, "bin": true, "sys": true, "sync": true, "games": true, "man": true,
	"lp": true, "mail": true, "news": true, "uucp": true, "proxy": true, "www-data": true, "backup": true,
	"list": true, "irc": true, "gnats": true, "nobody": true, "messagebus": true, "syslog": true,
	"sshd": true, "_apt": true, "polkitd": true, "tss": true, "uuidd": true, "tcpdump": true,
	"landscape": true, "fwupd-refresh": true, "usbmux": true, "dnsmasq": true, "lxd": true,
	"ubuntu": true, "cloud-user": true, "centos": true, "rocky": true, "almalinux": true, "ec2-user": true,
	"chrony": true, "_chrony": true, "ntp": true,
}

// ValidateUsername enforces the username rule: the POSIX login-name pattern, not a system account,
// not one of serviceAccounts (the accounts swallow's modules create on Servers), not a systemd-*
// name, and not a swallow-* name (swallow's group names). It returns ErrInvalidUsername or
// ErrReservedUsername.
func ValidateUsername(username string, serviceAccounts map[string]bool) error {
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("%w: use 1 to 32 lowercase letters, digits, '_' or '-', starting with a letter or '_'", ErrInvalidUsername)
	}
	if systemUsernames[username] || serviceAccounts[username] ||
		strings.HasPrefix(username, "systemd-") || strings.HasPrefix(username, "swallow-") {
		return fmt.Errorf("%w: %q is a system or service account", ErrReservedUsername, username)
	}
	return nil
}

// User is a person's swallow account (glossary User; decision 082): the identity that signs in,
// owns Sessions, API Keys, and Access Keys, and is the person's Linux login on the managed Servers.
//
// Invariants: Username follows ValidateUsername and is unique; Role is valid; UIDNumber, once
// allocated (non-zero), never changes and is never given to another User, even after this one is
// deleted. A User created before POSIX identities existed has UIDNumber 0 until the API's start
// allocates one.
type User struct {
	ID           string
	Username     string
	PasswordHash PasswordHash
	Role         Role
	UIDNumber    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

var (
	// ErrUserNotFound is a domain miss: no User matches.
	ErrUserNotFound = errors.New("user not found")
	// ErrInvalidCredentials means a sign-in's username or password is wrong; callers never say which.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidUsername means a username breaks the login-name rule.
	ErrInvalidUsername = errors.New("invalid username")
	// ErrReservedUsername means a username is a system or service account's name.
	ErrReservedUsername = errors.New("reserved username")
	// ErrUsernameTaken means another User already has the username.
	ErrUsernameTaken = errors.New("username already taken")
	// ErrInvalidPassword means a new password breaks the password rule.
	ErrInvalidPassword = errors.New("invalid password")
	// ErrInvalidRole means a value is not a User Role.
	ErrInvalidRole = errors.New("invalid role")
	// ErrLastAdmin means a change would leave the installation without an admin.
	ErrLastAdmin = errors.New("the last admin cannot be demoted or deleted")
	// ErrDeleteSelf means an admin tried to delete their own account.
	ErrDeleteSelf = errors.New("an admin cannot delete their own account")
	// ErrWrongPassword means a password change named the wrong current password.
	ErrWrongPassword = errors.New("the current password is wrong")
)
