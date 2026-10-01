// Package application coordinates swallow's SSH Key use cases (decision 039): listing keys with
// their provisioner sync status, importing and generating Access Keys, replacing or regenerating
// the Deployment Key, creating the Deployment Key at installation, and realizing every key in
// each key-capable provisioner.
//
// The service depends only on domain ports, so it runs without MongoDB, HTTP, an SSH library, or a
// live provisioner. Authorization is the caller's job: delivery admits only admins and passes the
// caller's user id, which scopes every Access Key operation. Private key material flows through
// here only as opaque strings handed straight to the repository or returned once to the caller of
// GenerateAccessKey; it is never logged or kept.
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/maple52046/swallow/internal/shared/wire"
	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
)

// Service owns SSH Key management and provisioner realization.
//
// It is safe for concurrent use. Realization passes within one process are serialized by syncMu
// so the periodic loop, an explicit sync, and a pre-deploy ensure never interleave writes to the
// same sync records; passes in different processes may overlap, which realization tolerates by
// adopting a key another pass already added.
type Service struct {
	keys         sshkeydomain.KeyRepository
	syncs        sshkeydomain.SyncRepository
	material     sshkeydomain.KeyMaterial
	provisioners sshkeydomain.ProvisionerKeys
	now          func() time.Time
	newID        func() string

	syncMu sync.Mutex
	// kick carries at most one pending sync request; RequestSync never blocks.
	kick chan struct{}
}

// NewService wires the SSH Key use cases. now and newID are replaceable in tests; production uses
// UTC wall-clock time and random UUIDs.
func NewService(
	keys sshkeydomain.KeyRepository,
	syncs sshkeydomain.SyncRepository,
	material sshkeydomain.KeyMaterial,
	provisioners sshkeydomain.ProvisionerKeys,
) *Service {
	return &Service{
		keys:         keys,
		syncs:        syncs,
		material:     material,
		provisioners: provisioners,
		now:          func() time.Time { return time.Now().UTC() },
		newID:        uuid.NewString,
		kick:         make(chan struct{}, 1),
	}
}

// KeyItem is the API representation of one SSH Key (contract ssh-keys.md).
type KeyItem struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Purpose      string             `json:"purpose"`
	KeyType      string             `json:"keyType"`
	Fingerprint  string             `json:"fingerprint"`
	PublicKey    string             `json:"publicKey"`
	OwnerUserID  string             `json:"ownerUserId,omitempty"`
	CreatedAt    string             `json:"createdAt"`
	UpdatedAt    string             `json:"updatedAt"`
	ProviderSync []ProviderSyncItem `json:"providerSync"`
}

// ProviderSyncItem is one key's realization status in one provisioner Integration.
type ProviderSyncItem struct {
	IntegrationID string `json:"integrationId"`
	SiteID        string `json:"siteId"`
	State         string `json:"state"`
	SyncedAt      string `json:"syncedAt,omitempty"`
	Error         string `json:"error,omitempty"`
}

// GeneratedAccessKey is the one-time response of GenerateAccessKey: the stored key and the
// private key swallow does not keep.
type GeneratedAccessKey struct {
	Key        KeyItem `json:"key"`
	PrivateKey string  `json:"privateKey"`
}

// List returns the Deployment Key and ownerUserID's Access Keys, each with one sync entry per
// provisioner Integration. A key with no current record for an Integration reads as pending.
func (s *Service) List(ctx context.Context, ownerUserID string) ([]KeyItem, error) {
	keys, err := s.keys.List(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	return s.items(ctx, keys)
}

// Get returns one key visible to ownerUserID — the Deployment Key or one of their own Access Keys —
// with its current sync status. Another User's Access Key is ErrKeyNotFound, indistinguishable from
// an unknown id, so a caller cannot probe other Users' keys.
func (s *Service) Get(ctx context.Context, ownerUserID, id string) (*KeyItem, error) {
	key, err := s.keys.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if key.Purpose == sshkeydomain.PurposeAccess && key.OwnerUserID != ownerUserID {
		return nil, sshkeydomain.ErrKeyNotFound
	}
	return s.item(ctx, key)
}

// ImportAccessKey stores a public key as one of ownerUserID's Access Keys and requests
// realization. It returns ErrInvalidName, ErrInvalidKey, ErrUnsupportedKey, ErrDuplicateKey, or
// ErrDuplicateName for a request that cannot be accepted.
func (s *Service) ImportAccessKey(ctx context.Context, ownerUserID, name, publicKey string) (*KeyItem, error) {
	name, ok := sshkeydomain.NormalizeName(name)
	if !ok {
		return nil, sshkeydomain.ErrInvalidName
	}
	material, err := s.material.ParsePublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	return s.createAccess(ctx, ownerUserID, name, material)
}

// GenerateAccessKey creates an ed25519 key pair, stores only its public key as one of
// ownerUserID's Access Keys, and returns the private key exactly once. swallow keeps no copy, so a
// caller that loses the response cannot recover the private key.
func (s *Service) GenerateAccessKey(ctx context.Context, ownerUserID, name string) (*GeneratedAccessKey, error) {
	name, ok := sshkeydomain.NormalizeName(name)
	if !ok {
		return nil, sshkeydomain.ErrInvalidName
	}
	pair, err := s.material.Generate(name)
	if err != nil {
		return nil, fmt.Errorf("generate access key: %w", err)
	}
	item, err := s.createAccess(ctx, ownerUserID, name, pair.Public)
	if err != nil {
		return nil, err
	}
	return &GeneratedAccessKey{Key: *item, PrivateKey: pair.PrivateKeyPEM}, nil
}

// createAccess stores validated material as ownerUserID's Access Key and requests realization;
// uniqueness violations come back from the repository as domain errors.
func (s *Service) createAccess(ctx context.Context, ownerUserID, name string, material sshkeydomain.PublicKeyMaterial) (*KeyItem, error) {
	now := s.now()
	key := &sshkeydomain.SSHKey{
		ID:          s.newID(),
		Name:        name,
		Purpose:     sshkeydomain.PurposeAccess,
		OwnerUserID: ownerUserID,
		KeyType:     material.KeyType,
		Fingerprint: material.Fingerprint,
		PublicKey:   material.AuthorizedKey,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.keys.CreateAccess(ctx, key); err != nil {
		return nil, err
	}
	s.RequestSync()
	return s.item(ctx, key)
}

// DeleteAccessKey deletes one of ownerUserID's Access Keys and requests realization, which
// removes it from every provisioner where swallow registered it. Another User's key is
// ErrKeyNotFound; the Deployment Key is ErrDeploymentKeyImmutable.
func (s *Service) DeleteAccessKey(ctx context.Context, ownerUserID, id string) error {
	key, err := s.keys.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if key.Purpose == sshkeydomain.PurposeDeployment {
		return sshkeydomain.ErrDeploymentKeyImmutable
	}
	if key.OwnerUserID != ownerUserID {
		return sshkeydomain.ErrKeyNotFound
	}
	if err := s.keys.DeleteAccess(ctx, ownerUserID, id); err != nil {
		return err
	}
	s.RequestSync()
	return nil
}

// ReplaceDeploymentKey replaces the Deployment Key's material with an existing private key and
// requests realization, which retires the previous public key from provisioners. An empty name
// keeps the current name. It returns ErrInvalidKey, ErrUnsupportedKey, ErrPassphraseProtected,
// ErrInvalidName, or ErrDuplicateKey (the key is already an Access Key).
func (s *Service) ReplaceDeploymentKey(ctx context.Context, privateKeyPEM, name string) (*KeyItem, error) {
	current, err := s.keys.FindDeployment(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		name = current.Name
	}
	name, ok := sshkeydomain.NormalizeName(name)
	if !ok {
		return nil, sshkeydomain.ErrInvalidName
	}
	privateKeyPEM = normalizePrivateKey(privateKeyPEM)
	material, err := s.material.ParsePrivateKey(privateKeyPEM, name)
	if err != nil {
		return nil, err
	}
	return s.replaceDeployment(ctx, current, name, material, privateKeyPEM)
}

// RegenerateDeploymentKey replaces the Deployment Key with a fresh ed25519 key pair, keeping its
// name, and requests realization. The new private key is never returned.
func (s *Service) RegenerateDeploymentKey(ctx context.Context) (*KeyItem, error) {
	current, err := s.keys.FindDeployment(ctx)
	if err != nil {
		return nil, err
	}
	pair, err := s.material.Generate(current.Name)
	if err != nil {
		return nil, fmt.Errorf("generate deployment key: %w", err)
	}
	return s.replaceDeployment(ctx, current, current.Name, pair.Public, pair.PrivateKeyPEM)
}

// replaceDeployment writes new Deployment Key material under the existing id. Servers deployed
// earlier keep only the previous public key; that consequence is the caller's to communicate.
func (s *Service) replaceDeployment(
	ctx context.Context,
	current *sshkeydomain.SSHKey,
	name string,
	material sshkeydomain.PublicKeyMaterial,
	privateKeyPEM string,
) (*KeyItem, error) {
	replacement := *current
	replacement.Name = name
	replacement.KeyType = material.KeyType
	replacement.Fingerprint = material.Fingerprint
	replacement.PublicKey = material.AuthorizedKey
	replacement.UpdatedAt = s.now()
	if err := s.keys.ReplaceDeployment(ctx, &replacement, privateKeyPEM); err != nil {
		return nil, err
	}
	s.RequestSync()
	return s.item(ctx, &replacement)
}

// EnsureDeploymentKey generates and stores the Deployment Key when the installation has none. It is
// the installation step (`swallow-api deployment-key ensure`, run by swallowctl install/upgrade and
// the dev/testing seeds), and it must be idempotent because install and every upgrade run it: an
// existing key is left untouched, and losing a creation race to a concurrent run (the store's
// single-Deployment-Key constraint) is success. It returns the Deployment Key's public fingerprint
// and whether this call generated it; no private material is returned.
func (s *Service) EnsureDeploymentKey(ctx context.Context) (fingerprint string, created bool, err error) {
	if existing, err := s.keys.FindDeployment(ctx); err == nil {
		return existing.Fingerprint, false, nil
	} else if !errors.Is(err, sshkeydomain.ErrDeploymentKeyNotFound) {
		return "", false, err
	}
	pair, err := s.material.Generate(sshkeydomain.DefaultDeploymentKeyName)
	if err != nil {
		return "", false, fmt.Errorf("generate deployment key: %w", err)
	}
	now := s.now()
	key := &sshkeydomain.SSHKey{
		ID:          s.newID(),
		Name:        sshkeydomain.DefaultDeploymentKeyName,
		Purpose:     sshkeydomain.PurposeDeployment,
		KeyType:     pair.Public.KeyType,
		Fingerprint: pair.Public.Fingerprint,
		PublicKey:   pair.Public.AuthorizedKey,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.keys.CreateDeployment(ctx, key, pair.PrivateKeyPEM); err != nil {
		if errors.Is(err, sshkeydomain.ErrDeploymentKeyExists) {
			winner, findErr := s.keys.FindDeployment(ctx)
			if findErr != nil {
				return "", false, findErr
			}
			return winner.Fingerprint, false, nil
		}
		return "", false, err
	}
	s.RequestSync()
	return key.Fingerprint, true, nil
}

// DeploymentPrivateKey returns the Deployment Key's private key for automation that is about to
// authenticate to a host. It returns ErrDeploymentKeyNotFound when none exists. Callers must not
// log, persist, or return the value.
func (s *Service) DeploymentPrivateKey(ctx context.Context) (string, error) {
	return s.keys.DeploymentPrivateKey(ctx)
}

// HasDeploymentKey reports whether the installation has a Deployment Key, for callers that only
// need to describe the effective credential without reading secret material.
func (s *Service) HasDeploymentKey(ctx context.Context) (bool, error) {
	_, err := s.keys.FindDeployment(ctx)
	if errors.Is(err, sshkeydomain.ErrDeploymentKeyNotFound) {
		return false, nil
	}
	return err == nil, err
}

// RequestSync asks the process's sync loop to run a realization pass soon. It never blocks: a
// request made while one is already pending is coalesced into it.
func (s *Service) RequestSync() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// SyncRequests is the channel the sync loop waits on for RequestSync. Only one loop per process
// may consume it.
func (s *Service) SyncRequests() <-chan struct{} {
	return s.kick
}

// items projects keys with their sync status against the current provisioner set.
func (s *Service) items(ctx context.Context, keys []*sshkeydomain.SSHKey) ([]KeyItem, error) {
	provisioners, err := s.provisioners.ListProvisioners(ctx)
	if err != nil {
		return nil, err
	}
	records, err := s.syncs.List(ctx)
	if err != nil {
		return nil, err
	}
	index := make(map[string]sshkeydomain.ProviderSync, len(records))
	for _, record := range records {
		index[syncKey(record.KeyID, record.IntegrationID)] = record
	}
	items := make([]KeyItem, 0, len(keys))
	for _, key := range keys {
		items = append(items, keyItem(key, provisioners, index))
	}
	return items, nil
}

func (s *Service) item(ctx context.Context, key *sshkeydomain.SSHKey) (*KeyItem, error) {
	items, err := s.items(ctx, []*sshkeydomain.SSHKey{key})
	if err != nil {
		return nil, err
	}
	return &items[0], nil
}

// keyItem shapes one key. A sync record describes this key only when its fingerprint matches the
// key's current material; a record left from replaced Deployment Key material reads as pending.
func keyItem(key *sshkeydomain.SSHKey, provisioners []sshkeydomain.Provisioner, index map[string]sshkeydomain.ProviderSync) KeyItem {
	syncs := make([]ProviderSyncItem, 0, len(provisioners))
	for _, provisioner := range provisioners {
		entry := ProviderSyncItem{
			IntegrationID: provisioner.IntegrationID,
			SiteID:        provisioner.SiteID,
			State:         string(sshkeydomain.SyncPending),
		}
		if record, ok := index[syncKey(key.ID, provisioner.IntegrationID)]; ok && record.Fingerprint == key.Fingerprint {
			entry.State = string(record.State)
			if record.SyncedAt != nil {
				entry.SyncedAt = wire.Time(*record.SyncedAt)
			}
			if record.State == sshkeydomain.SyncFailed {
				entry.Error = record.Error
			}
		}
		syncs = append(syncs, entry)
	}
	return KeyItem{
		ID:           key.ID,
		Name:         key.Name,
		Purpose:      string(key.Purpose),
		KeyType:      key.KeyType,
		Fingerprint:  key.Fingerprint,
		PublicKey:    key.PublicKey,
		OwnerUserID:  key.OwnerUserID,
		CreatedAt:    wire.Time(key.CreatedAt),
		UpdatedAt:    wire.Time(key.UpdatedAt),
		ProviderSync: syncs,
	}
}

// syncKey indexes sync records by (key, Integration) with a separator no id can contain.
func syncKey(keyID, integrationID string) string {
	return keyID + "\x00" + integrationID
}

// normalizePrivateKey trims surrounding whitespace and restores the single trailing newline
// OpenSSH requires; a key file without it is rejected by ssh as "invalid format".
func normalizePrivateKey(privateKeyPEM string) string {
	return strings.TrimSpace(privateKeyPEM) + "\n"
}
