// Package domain defines swallow's Access model for SSH Keys (decision 039): the single
// system-owned Deployment Key and the public-key-only Access Keys owned by a User, plus the
// per-provisioner record of whether each key has been realized there.
//
// swallow owns these records outright. A provisioner's own key list is a realization target
// reached through the ProvisionerKeys port, never the source of truth. This package holds the
// entities, value sets, invariants, persistence and realization ports, and domain errors; it must
// not import MongoDB, Fiber, x/crypto/ssh, or any provisioning adapter. Private key material never
// appears in these types: the Deployment Key's private key crosses only the repository's
// dedicated write and read methods.
package domain

import (
	"strings"
	"time"
)

// Purpose is the closed set of roles an SSH Key plays (glossary SSH Key).
type Purpose string

const (
	// PurposeDeployment marks the single system-owned Deployment Key whose private key swallow
	// holds and uses for SSH readiness and Ansible execution.
	PurposeDeployment Purpose = "deployment"
	// PurposeAccess marks a User's Access Key: public key only, used by a person to log in.
	PurposeAccess Purpose = "access"
)

// DefaultDeploymentKeyName is the name the Deployment Key gets when swallow generates it at
// first start. It also becomes the public key's comment so the key is recognizable in a
// provisioner's UI.
const DefaultDeploymentKeyName = "swallow-deployment"

// MaxNameLength bounds an SSH Key name so a list stays readable and a provider comment stays
// short.
const MaxNameLength = 100

// SSHKey is one swallow-owned SSH public key.
//
// Invariants: Fingerprint is the OpenSSH SHA256 fingerprint of PublicKey and is unique across
// every SSH Key; exactly one key has PurposeDeployment and it has no OwnerUserID; every
// PurposeAccess key has an OwnerUserID and a name unique (case-insensitively) among that
// owner's Access Keys. The Deployment Key keeps its ID when it is replaced, so its sync records
// stay addressable; only its material and Fingerprint change.
type SSHKey struct {
	ID          string
	Name        string
	Purpose     Purpose
	OwnerUserID string
	// KeyType is the SSH algorithm name, e.g. "ssh-ed25519".
	KeyType     string
	Fingerprint string
	// PublicKey is one authorized_keys line: "<type> <base64> <comment>".
	PublicKey string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PublicKeyMaterial is the parsed, validated form of a public key: the pieces swallow stores
// and compares. It is produced only by the KeyMaterial port, so a value of this type has already
// passed algorithm and strength checks.
type PublicKeyMaterial struct {
	KeyType     string
	Fingerprint string
	// AuthorizedKey is the normalized authorized_keys line including its comment.
	AuthorizedKey string
}

// GeneratedKeyPair is a freshly generated key pair. PrivateKeyPEM is an unencrypted OpenSSH
// private key; callers either seal it (Deployment Key) or hand it to the operator once and drop
// it (Access Key). It must never be logged.
type GeneratedKeyPair struct {
	Public        PublicKeyMaterial
	PrivateKeyPEM string
}

// KeyMaterial parses and generates SSH key material. Implementations own every cryptographic
// detail so the application layer never touches an SSH library.
//
// ParsePublicKey and ParsePrivateKey return ErrInvalidKey for malformed input, ErrUnsupportedKey
// for an algorithm or strength swallow does not accept, and ParsePrivateKey returns
// ErrPassphraseProtected for an encrypted private key, because swallow uses the Deployment Key
// unattended. Implementations must be safe for concurrent use and must use crypto/rand.
type KeyMaterial interface {
	// Generate creates an ed25519 key pair whose public key carries comment.
	Generate(comment string) (GeneratedKeyPair, error)
	// ParsePublicKey validates one authorized_keys line (no options, exactly one key).
	ParsePublicKey(line string) (PublicKeyMaterial, error)
	// ParsePrivateKey validates a PEM private key and derives its public key with comment.
	ParsePrivateKey(privateKeyPEM, comment string) (PublicKeyMaterial, error)
}

// NormalizeName trims a key name and reports whether it is acceptable: non-empty and at most
// MaxNameLength characters. It is the single name rule for every key-writing use case.
func NormalizeName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, name != "" && len([]rune(name)) <= MaxNameLength
}

// SameKeyMaterial reports whether two authorized_keys lines carry the same key, ignoring the
// comment and surrounding whitespace. Providers may rewrite or drop the comment, so key identity
// is the algorithm name plus the base64 blob, never the whole line. Both lines must be free of
// authorized_keys options, which swallow never stores.
func SameKeyMaterial(a, b string) bool {
	ka, kb := keyMaterial(a), keyMaterial(b)
	return ka != "" && ka == kb
}

// keyMaterial returns "<type> <base64>" for an authorized_keys line, or "" when the line has
// fewer than two fields.
func keyMaterial(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	return fields[0] + " " + fields[1]
}
