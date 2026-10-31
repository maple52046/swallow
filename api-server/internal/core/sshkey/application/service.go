// Package application coordinates swallow's SSH Key use cases (decision 039): listing keys with
// their realization in each provisioner, importing and generating Access Keys, replacing or
// regenerating the Deployment Key, creating the Deployment Key at installation, and realizing every
// key in each key-capable provisioner. The realization runs in the worker as the SSH Key controller
// (decision 071); the API only changes keys, which the controller sees through the change feed, and
// asks it to converge at once through the store.
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

	"github.com/maple52046/swallow/internal/core/convergence"
	sshkeydomain "github.com/maple52046/swallow/internal/core/sshkey/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// Service owns SSH Key management and provisioner realization.
//
// It is safe for concurrent use. Realization passes within one process are serialized by syncMu
// so the controller and a pre-deploy ensure never interleave writes to the same sync records;
// passes in different processes may overlap, which realization tolerates by adopting a key another
// pass already added.
type Service struct {
	keys         sshkeydomain.KeyRepository
	syncs        sshkeydomain.SyncRepository
	material     sshkeydomain.KeyMaterial
	provisioners sshkeydomain.ProvisionerKeys
	requests     convergence.Requester
	events       convergence.EventRecorder
	now          func() time.Time
	newID        func() string

	syncMu sync.Mutex
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
	}
}

// AttachRequester lets RequestConvergence reach the SSH Key controller in the worker. Without one,
// RequestConvergence does nothing and keys converge on the change feed and the resync alone.
func (s *Service) AttachRequester(requests convergence.Requester) {
	s.requests = requests
}

// AttachEvents records realization changes (a key realized, or added back after it went missing)
// on the convergence timeline. The worker attaches it; without one, nothing is recorded.
func (s *Service) AttachEvents(events convergence.EventRecorder) {
	s.events = events
}

// ControllerName is the SSH Key controller's name, which a convergence request addresses.
const ControllerName = "ssh-keys"

// KeyItem is the API representation of one SSH Key as a migrated resource (contract ssh-keys.md,
// decision 071): identity, the spec (the key itself), and the status of its realization.
type KeyItem struct {
	ID          string        `json:"id"`
	OwnerUserID string        `json:"ownerUserId,omitempty"`
	Generation  int64         `json:"generation"`
	Spec        KeySpecItem   `json:"spec"`
	Status      KeyStatusItem `json:"status"`
}

// KeySpecItem is what swallow registers: the key's name, purpose, and public key material.
type KeySpecItem struct {
	Name        string `json:"name"`
	Purpose     string `json:"purpose"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publicKey"`
}

// KeyStatusItem is the key's realization: aggregate conditions over every provisioner and one
// entry per provisioner Integration.
type KeyStatusItem struct {
	ObservedGeneration int64                       `json:"observedGeneration"`
	Conditions         []convergence.ConditionItem `json:"conditions"`
	Provisioners       []KeyRealizationItem        `json:"provisioners"`
	CreatedAt          string                      `json:"createdAt"`
	UpdatedAt          string                      `json:"updatedAt"`
}

// KeyRealizationItem is one key's realization in one provisioner Integration; its conditions are
// empty until the controller first judged the key there, and ObservedAt is null until then.
type KeyRealizationItem struct {
	IntegrationID      string                      `json:"integrationId"`
	SiteID             string                      `json:"siteId"`
	ObservedGeneration int64                       `json:"observedGeneration"`
	ObservedAt         *string                     `json:"observedAt"`
	Conditions         []convergence.ConditionItem `json:"conditions"`
}

// GeneratedAccessKey is the one-time response of GenerateAccessKey: the stored key and the
// private key swallow does not keep.
type GeneratedAccessKey struct {
	Key        KeyItem `json:"key"`
	PrivateKey string  `json:"privateKey"`
}

// List returns the Deployment Key and ownerUserID's Access Keys, each with one realization per
// provisioner Integration. A key with no current record for an Integration has no conditions there.
func (s *Service) List(ctx context.Context, ownerUserID string) ([]KeyItem, error) {
	keys, err := s.keys.List(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	return s.items(ctx, keys)
}

// Get returns one key visible to ownerUserID — the Deployment Key or one of their own Access Keys —
// with its realization. Another User's Access Key is ErrKeyNotFound, indistinguishable from
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

// ImportAccessKey stores a public key as one of ownerUserID's Access Keys; the controller then
// realizes it. It returns ErrInvalidName, ErrInvalidKey, ErrUnsupportedKey, ErrDuplicateKey, or
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

// createAccess stores validated material as ownerUserID's Access Key at generation 1; uniqueness
// violations come back from the repository as domain errors.
func (s *Service) createAccess(ctx context.Context, ownerUserID, name string, material sshkeydomain.PublicKeyMaterial) (*KeyItem, error) {
	now := s.now()
	key := &sshkeydomain.SSHKey{
		ID:          s.newID(),
		Generation:  1,
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
	return s.item(ctx, key)
}

// DeleteAccessKey deletes one of ownerUserID's Access Keys; the controller then removes it from
// every provisioner where swallow registered it. Another User's key is
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
	return s.keys.DeleteAccess(ctx, ownerUserID, id)
}

// ReplaceDeploymentKey replaces the Deployment Key's material with an existing private key, a new
// generation the controller realizes by retiring the previous public key from provisioners. An empty name
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
// name, as a new generation. The new private key is never returned.
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
		Generation:  1,
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

// RequestConvergence asks the SSH Key controller to realize every key in every provisioner now,
// rather than at its next resync, by writing a request to the store.
func (s *Service) RequestConvergence(ctx context.Context) error {
	if s.requests == nil {
		return nil
	}
	return s.requests.Request(ctx, ControllerName)
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

// keyItem shapes one key. A record describes this key only when its fingerprint matches the key's
// current material; a record left from replaced Deployment Key material has no conditions to show.
// The aggregate conditions are derived here from the per-provisioner ones: realized only when every
// provisioner realized the current generation (and for an installation with no provisioner, which
// has nothing to realize), otherwise carrying the first unrealized provisioner's reason.
func keyItem(key *sshkeydomain.SSHKey, provisioners []sshkeydomain.Provisioner, index map[string]sshkeydomain.ProviderSync) KeyItem {
	realizations := make([]KeyRealizationItem, 0, len(provisioners))
	realized, reason, message := true, convergence.ReasonConverged, ""
	observed := key.Generation
	transition := key.UpdatedAt
	for _, provisioner := range provisioners {
		entry := KeyRealizationItem{IntegrationID: provisioner.IntegrationID, SiteID: provisioner.SiteID, Conditions: []convergence.ConditionItem{}}
		var status convergence.TargetStatus
		if record, ok := index[syncKey(key.ID, provisioner.IntegrationID)]; ok && record.Fingerprint == key.Fingerprint {
			status = record.Status
			entry.ObservedGeneration = status.ObservedGeneration
			entry.Conditions = convergence.ConditionItems(status.Conditions)
			if status.ObservedAt != nil {
				entry.ObservedAt = wire.TimePtr(*status.ObservedAt)
			}
		}
		condition, ok := convergence.FindCondition(status.Conditions, convergence.ConditionRealized)
		if ok && condition.LastTransitionTime.After(transition) {
			transition = condition.LastTransitionTime
		}
		if status.ObservedGeneration < observed {
			observed = status.ObservedGeneration
		}
		if realized && (!ok || condition.Status != convergence.ConditionTrue || status.ObservedGeneration < key.Generation) {
			realized, reason, message = false, convergence.ReasonConverging, ""
			if ok && condition.Status == convergence.ConditionFalse {
				reason, message = condition.Reason, condition.Message
			}
		}
		realizations = append(realizations, entry)
	}
	aggregate := convergence.Condition{
		Type: convergence.ConditionRealized, Status: convergence.ConditionFalse, Reason: reason, Message: message,
		LastTransitionTime: transition, ObservedGeneration: key.Generation,
	}
	if realized {
		aggregate.Status = convergence.ConditionTrue
		if len(provisioners) == 0 {
			aggregate.Message = "No provisioner holds SSH keys."
		}
	}
	return KeyItem{
		ID:          key.ID,
		OwnerUserID: key.OwnerUserID,
		Generation:  key.Generation,
		Spec: KeySpecItem{
			Name: key.Name, Purpose: string(key.Purpose), KeyType: key.KeyType,
			Fingerprint: key.Fingerprint, PublicKey: key.PublicKey,
		},
		Status: KeyStatusItem{
			ObservedGeneration: observed,
			Conditions:         convergence.ConditionItems([]convergence.Condition{aggregate}),
			Provisioners:       realizations,
			CreatedAt:          wire.Time(key.CreatedAt),
			UpdatedAt:          wire.Time(key.UpdatedAt),
		},
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
