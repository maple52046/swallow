package domain

import (
	"context"
	"time"
)

// SyncState is the closed set of realization states of one SSH Key in one provisioner
// Integration (glossary SSH Key).
type SyncState string

const (
	// SyncPending means no realization has run since the key or the Integration changed.
	SyncPending SyncState = "pending"
	// SyncSynced means the provisioner holds this key's current material.
	SyncSynced SyncState = "synced"
	// SyncFailed means the last realization attempt failed; Error explains why.
	SyncFailed SyncState = "failed"
	// SyncUnsupported means the provisioner cannot hold SSH keys at all.
	SyncUnsupported SyncState = "unsupported"
)

// ProviderSync records the realization of one SSH Key in one provisioner Integration.
//
// Fingerprint is the key material the record describes. When the Deployment Key is replaced its
// ID stays the same but its fingerprint changes, so a record whose Fingerprint differs from the
// key's describes the previous material and the key reads as pending until the new material is
// realized. ProviderKeyID is the provider key swallow registered or adopted for Fingerprint's
// material; RetiredProviderKeyIDs are provider keys swallow registered for earlier material that
// still have to be removed. These recorded ids are the only handles swallow ever uses to remove a
// key from a provisioner, which is what keeps operator-added keys safe.
type ProviderSync struct {
	KeyID                 string
	IntegrationID         string
	SiteID                string
	Fingerprint           string
	ProviderKeyID         string
	RetiredProviderKeyIDs []string
	State                 SyncState
	SyncedAt              *time.Time
	Error                 string
	UpdatedAt             time.Time
}

// Provisioner identifies one provisioner Integration swallow realizes keys into.
type Provisioner struct {
	IntegrationID string
	SiteID        string
}

// RegisteredKey is one key a provisioner holds, in swallow's vocabulary.
type RegisteredKey struct {
	ProviderKeyID string
	PublicKey     string
}

// KeyRegistrar is one provisioner's key list. Implementations translate provider errors into
// errors whose message is safe to show an operator; RemoveKey treats a missing key as satisfied.
type KeyRegistrar interface {
	ListKeys(ctx context.Context) ([]RegisteredKey, error)
	AddKey(ctx context.Context, publicKey string) (RegisteredKey, error)
	RemoveKey(ctx context.Context, providerKeyID string) error
}

// ProvisionerKeys resolves the provisioners swallow realizes keys into.
//
// ListProvisioners returns every enabled provisioner Integration across all Sites; a paused
// Integration is left alone until an operator re-enables it. Registrar returns the key registrar
// for one Integration and
// ok=false (with a nil error) when its provisioner cannot hold SSH keys; an error means the
// provisioner could not be built (for example a missing credential) and is recorded as a failed
// sync rather than as unsupported.
type ProvisionerKeys interface {
	ListProvisioners(ctx context.Context) ([]Provisioner, error)
	Registrar(ctx context.Context, integrationID string) (KeyRegistrar, bool, error)
}

// SyncRepository persists ProviderSync records, keyed by (KeyID, IntegrationID).
//
// Upsert replaces the record for its key pair. Delete of a missing record is not an error. List
// returns every record, including orphans whose key was deleted, so realization can remove their
// provider keys before dropping them. Production implementations must be durable.
type SyncRepository interface {
	List(ctx context.Context) ([]ProviderSync, error)
	Upsert(ctx context.Context, sync ProviderSync) error
	Delete(ctx context.Context, keyID, integrationID string) error
}

// KeyRepository persists SSH Keys and the Deployment Key's sealed private key.
//
// Implementations must enforce the SSHKey invariants durably (unique fingerprint, a single
// Deployment Key, unique Access Key names per owner) so concurrent writers cannot violate them,
// and map violations to ErrDuplicateKey, ErrDeploymentKeyExists, and ErrDuplicateName. Private
// key material is sealed at rest and returned only by DeploymentPrivateKey; no other method
// reads or returns it.
type KeyRepository interface {
	// List returns the Deployment Key (when present) and ownerUserID's Access Keys.
	List(ctx context.Context, ownerUserID string) ([]*SSHKey, error)
	// ListAll returns every SSH Key; realization uses it to build the desired key set.
	ListAll(ctx context.Context) ([]*SSHKey, error)
	// FindByID returns one key or ErrKeyNotFound.
	FindByID(ctx context.Context, id string) (*SSHKey, error)
	// FindDeployment returns the Deployment Key or ErrDeploymentKeyNotFound.
	FindDeployment(ctx context.Context) (*SSHKey, error)
	// CreateAccess stores a new Access Key.
	CreateAccess(ctx context.Context, key *SSHKey) error
	// CreateDeployment stores the Deployment Key and seals privateKeyPEM with it.
	CreateDeployment(ctx context.Context, key *SSHKey, privateKeyPEM string) error
	// ReplaceDeployment overwrites the Deployment Key's material, name, and sealed private key,
	// keeping its ID and CreatedAt.
	ReplaceDeployment(ctx context.Context, key *SSHKey, privateKeyPEM string) error
	// DeploymentPrivateKey opens the Deployment Key's sealed private key. It is called only by
	// automation immediately before it authenticates to a host.
	DeploymentPrivateKey(ctx context.Context) (string, error)
	// DeleteAccess removes one of ownerUserID's Access Keys, or returns ErrKeyNotFound.
	DeleteAccess(ctx context.Context, ownerUserID, id string) error
}
