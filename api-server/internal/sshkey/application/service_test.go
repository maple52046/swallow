package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
)

// --- fakes ---

type fakeKeyRepo struct {
	mu            sync.Mutex
	keys          map[string]*sshkeydomain.SSHKey
	privateKey    string
	createDeplErr error
	// raceWinner, when set with createDeplErr, is stored as the Deployment Key a concurrent run
	// created first, so the caller can read it back after losing the race.
	raceWinner *sshkeydomain.SSHKey
}

func newFakeKeyRepo() *fakeKeyRepo {
	return &fakeKeyRepo{keys: map[string]*sshkeydomain.SSHKey{}}
}

func (r *fakeKeyRepo) sorted(filter func(*sshkeydomain.SSHKey) bool) []*sshkeydomain.SSHKey {
	var out []*sshkeydomain.SSHKey
	for _, key := range r.keys {
		if filter(key) {
			copied := *key
			out = append(out, &copied)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Purpose == sshkeydomain.PurposeDeployment) != (out[j].Purpose == sshkeydomain.PurposeDeployment) {
			return out[i].Purpose == sshkeydomain.PurposeDeployment
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *fakeKeyRepo) List(_ context.Context, owner string) ([]*sshkeydomain.SSHKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sorted(func(k *sshkeydomain.SSHKey) bool {
		return k.Purpose == sshkeydomain.PurposeDeployment || k.OwnerUserID == owner
	}), nil
}

func (r *fakeKeyRepo) ListAll(context.Context) ([]*sshkeydomain.SSHKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sorted(func(*sshkeydomain.SSHKey) bool { return true }), nil
}

func (r *fakeKeyRepo) FindByID(_ context.Context, id string) (*sshkeydomain.SSHKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.keys[id]
	if !ok {
		return nil, sshkeydomain.ErrKeyNotFound
	}
	copied := *key
	return &copied, nil
}

func (r *fakeKeyRepo) FindDeployment(context.Context) (*sshkeydomain.SSHKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range r.keys {
		if key.Purpose == sshkeydomain.PurposeDeployment {
			copied := *key
			return &copied, nil
		}
	}
	return nil, sshkeydomain.ErrDeploymentKeyNotFound
}

func (r *fakeKeyRepo) checkUnique(key *sshkeydomain.SSHKey) error {
	for _, existing := range r.keys {
		if existing.ID == key.ID {
			continue
		}
		if existing.Fingerprint == key.Fingerprint {
			return sshkeydomain.ErrDuplicateKey
		}
		if key.Purpose == sshkeydomain.PurposeAccess && existing.OwnerUserID == key.OwnerUserID &&
			strings.EqualFold(existing.Name, key.Name) {
			return sshkeydomain.ErrDuplicateName
		}
	}
	return nil
}

func (r *fakeKeyRepo) CreateAccess(_ context.Context, key *sshkeydomain.SSHKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkUnique(key); err != nil {
		return err
	}
	copied := *key
	r.keys[key.ID] = &copied
	return nil
}

func (r *fakeKeyRepo) CreateDeployment(_ context.Context, key *sshkeydomain.SSHKey, privateKeyPEM string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createDeplErr != nil {
		if r.raceWinner != nil {
			r.keys[r.raceWinner.ID] = r.raceWinner
		}
		return r.createDeplErr
	}
	for _, existing := range r.keys {
		if existing.Purpose == sshkeydomain.PurposeDeployment {
			return sshkeydomain.ErrDeploymentKeyExists
		}
	}
	copied := *key
	r.keys[key.ID] = &copied
	r.privateKey = privateKeyPEM
	return nil
}

func (r *fakeKeyRepo) ReplaceDeployment(_ context.Context, key *sshkeydomain.SSHKey, privateKeyPEM string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkUnique(key); err != nil {
		return err
	}
	copied := *key
	r.keys[key.ID] = &copied
	r.privateKey = privateKeyPEM
	return nil
}

func (r *fakeKeyRepo) DeploymentPrivateKey(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.privateKey == "" {
		return "", sshkeydomain.ErrDeploymentKeyNotFound
	}
	return r.privateKey, nil
}

func (r *fakeKeyRepo) DeleteAccess(_ context.Context, owner, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.keys[id]
	if !ok || key.OwnerUserID != owner {
		return sshkeydomain.ErrKeyNotFound
	}
	delete(r.keys, id)
	return nil
}

type fakeSyncRepo struct {
	mu      sync.Mutex
	records map[string]sshkeydomain.ProviderSync
}

func newFakeSyncRepo() *fakeSyncRepo {
	return &fakeSyncRepo{records: map[string]sshkeydomain.ProviderSync{}}
}

func (r *fakeSyncRepo) List(context.Context) ([]sshkeydomain.ProviderSync, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]sshkeydomain.ProviderSync, 0, len(r.records))
	for _, record := range r.records {
		out = append(out, record)
	}
	return out, nil
}

func (r *fakeSyncRepo) Upsert(_ context.Context, record sshkeydomain.ProviderSync) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[syncKey(record.KeyID, record.IntegrationID)] = record
	return nil
}

func (r *fakeSyncRepo) Delete(_ context.Context, keyID, integrationID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.records, syncKey(keyID, integrationID))
	return nil
}

func (r *fakeSyncRepo) get(keyID, integrationID string) (sshkeydomain.ProviderSync, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[syncKey(keyID, integrationID)]
	return record, ok
}

// fakeMaterial produces deterministic, recognizable key material: "ssh-ed25519 KEY<n> <comment>".
type fakeMaterial struct {
	mu   sync.Mutex
	next int
}

func (m *fakeMaterial) Generate(comment string) (sshkeydomain.GeneratedKeyPair, error) {
	m.mu.Lock()
	m.next++
	n := m.next
	m.mu.Unlock()
	return sshkeydomain.GeneratedKeyPair{
		Public: sshkeydomain.PublicKeyMaterial{
			KeyType:       "ssh-ed25519",
			Fingerprint:   fmt.Sprintf("SHA256:gen%d", n),
			AuthorizedKey: fmt.Sprintf("ssh-ed25519 GEN%d %s", n, comment),
		},
		PrivateKeyPEM: fmt.Sprintf("PRIVATE-%d", n),
	}, nil
}

func (m *fakeMaterial) ParsePublicKey(line string) (sshkeydomain.PublicKeyMaterial, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "ssh-ed25519" {
		return sshkeydomain.PublicKeyMaterial{}, sshkeydomain.ErrInvalidKey
	}
	return sshkeydomain.PublicKeyMaterial{
		KeyType: fields[0], Fingerprint: "SHA256:" + fields[1], AuthorizedKey: strings.TrimSpace(line),
	}, nil
}

func (m *fakeMaterial) ParsePrivateKey(privateKeyPEM, comment string) (sshkeydomain.PublicKeyMaterial, error) {
	blob := strings.TrimPrefix(strings.TrimSpace(privateKeyPEM), "PRIVATE-")
	if blob == "" || blob == "encrypted" {
		return sshkeydomain.PublicKeyMaterial{}, sshkeydomain.ErrPassphraseProtected
	}
	return sshkeydomain.PublicKeyMaterial{
		KeyType: "ssh-ed25519", Fingerprint: "SHA256:priv" + blob,
		AuthorizedKey: "ssh-ed25519 PRIV" + blob + " " + comment,
	}, nil
}

// fakeRegistrar is one provisioner's key list.
type fakeRegistrar struct {
	mu      sync.Mutex
	keys    []sshkeydomain.RegisteredKey
	next    int
	addErr  error
	listErr error
	removed []string
}

func (r *fakeRegistrar) ListKeys(context.Context) ([]sshkeydomain.RegisteredKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]sshkeydomain.RegisteredKey(nil), r.keys...), nil
}

func (r *fakeRegistrar) AddKey(_ context.Context, publicKey string) (sshkeydomain.RegisteredKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.addErr != nil {
		return sshkeydomain.RegisteredKey{}, r.addErr
	}
	r.next++
	key := sshkeydomain.RegisteredKey{ProviderKeyID: fmt.Sprintf("p%d", r.next), PublicKey: publicKey}
	r.keys = append(r.keys, key)
	return key, nil
}

func (r *fakeRegistrar) RemoveKey(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removed = append(r.removed, id)
	for i, key := range r.keys {
		if key.ProviderKeyID == id {
			r.keys = append(r.keys[:i], r.keys[i+1:]...)
			break
		}
	}
	return nil
}

func (r *fakeRegistrar) holds(publicKey string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range r.keys {
		if sshkeydomain.SameKeyMaterial(key.PublicKey, publicKey) {
			return true
		}
	}
	return false
}

type fakeProvisioners struct {
	provisioners []sshkeydomain.Provisioner
	registrars   map[string]*fakeRegistrar
	buildErr     map[string]error
}

func (p *fakeProvisioners) ListProvisioners(context.Context) ([]sshkeydomain.Provisioner, error) {
	return p.provisioners, nil
}

func (p *fakeProvisioners) Registrar(_ context.Context, id string) (sshkeydomain.KeyRegistrar, bool, error) {
	if err := p.buildErr[id]; err != nil {
		return nil, false, err
	}
	registrar, ok := p.registrars[id]
	if !ok {
		return nil, false, nil
	}
	return registrar, true, nil
}

type fixture struct {
	service      *Service
	keys         *fakeKeyRepo
	syncs        *fakeSyncRepo
	provisioners *fakeProvisioners
	maas         *fakeRegistrar
}

// newFixture builds a service with one key-capable provisioner "maas-1" (site "site-1").
func newFixture(t *testing.T) *fixture {
	t.Helper()
	maas := &fakeRegistrar{}
	provisioners := &fakeProvisioners{
		provisioners: []sshkeydomain.Provisioner{{IntegrationID: "maas-1", SiteID: "site-1"}},
		registrars:   map[string]*fakeRegistrar{"maas-1": maas},
		buildErr:     map[string]error{},
	}
	f := &fixture{keys: newFakeKeyRepo(), syncs: newFakeSyncRepo(), provisioners: provisioners, maas: maas}
	f.service = NewService(f.keys, f.syncs, &fakeMaterial{}, provisioners)
	ids := 0
	f.service.newID = func() string { ids++; return fmt.Sprintf("key-%d", ids) }
	f.service.now = func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) }
	return f
}

func (f *fixture) deployment(t *testing.T) *sshkeydomain.SSHKey {
	t.Helper()
	if _, _, err := f.service.EnsureDeploymentKey(context.Background()); err != nil {
		t.Fatalf("EnsureDeploymentKey: %v", err)
	}
	key, err := f.keys.FindDeployment(context.Background())
	if err != nil {
		t.Fatalf("FindDeployment: %v", err)
	}
	return key
}

func drainKick(s *Service) bool {
	select {
	case <-s.SyncRequests():
		return true
	default:
		return false
	}
}

// --- bootstrap ---

func TestEnsureDeploymentKey_GeneratesOnceAndIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	fingerprint, created, err := f.service.EnsureDeploymentKey(ctx)
	if err != nil || !created {
		t.Fatalf("first EnsureDeploymentKey() = %v, %v; want true, nil", created, err)
	}
	first, _ := f.keys.FindDeployment(ctx)
	if fingerprint != first.Fingerprint {
		t.Errorf("EnsureDeploymentKey() fingerprint = %q, want the stored key's %q", fingerprint, first.Fingerprint)
	}
	if first.Name != sshkeydomain.DefaultDeploymentKeyName || first.OwnerUserID != "" {
		t.Errorf("deployment key = %+v, want default name and no owner", first)
	}
	if !drainKick(f.service) {
		t.Errorf("generating the deployment key must request a sync")
	}

	fingerprint, created, err = f.service.EnsureDeploymentKey(ctx)
	if err != nil || created || fingerprint != first.Fingerprint {
		t.Fatalf("second EnsureDeploymentKey() = %q, %v, %v; want the existing fingerprint, false, nil", fingerprint, created, err)
	}
	second, _ := f.keys.FindDeployment(ctx)
	if second.Fingerprint != first.Fingerprint {
		t.Errorf("EnsureDeploymentKey replaced an existing key: %s -> %s", first.Fingerprint, second.Fingerprint)
	}
}

func TestEnsureDeploymentKey_LostRaceIsSuccess(t *testing.T) {
	f := newFixture(t)
	f.keys.createDeplErr = sshkeydomain.ErrDeploymentKeyExists
	f.keys.raceWinner = &sshkeydomain.SSHKey{ID: "winner", Purpose: sshkeydomain.PurposeDeployment, Fingerprint: "SHA256:winner"}

	fingerprint, created, err := f.service.EnsureDeploymentKey(context.Background())
	if err != nil || created || fingerprint != "SHA256:winner" {
		t.Fatalf("EnsureDeploymentKey() = %q, %v, %v; want the winner's fingerprint, false, nil", fingerprint, created, err)
	}
}

// --- access keys ---

func TestImportAccessKey_ValidatesAndRequestsSync(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.service.ImportAccessKey(ctx, "admin", "  ", "ssh-ed25519 AAAA"); !errors.Is(err, sshkeydomain.ErrInvalidName) {
		t.Errorf("blank name error = %v, want ErrInvalidName", err)
	}
	if _, err := f.service.ImportAccessKey(ctx, "admin", "laptop", "ssh-dss AAAA"); !errors.Is(err, sshkeydomain.ErrInvalidKey) {
		t.Errorf("bad key error = %v, want ErrInvalidKey", err)
	}

	item, err := f.service.ImportAccessKey(ctx, "admin", " laptop ", "ssh-ed25519 AAAA alice@laptop")
	if err != nil {
		t.Fatalf("ImportAccessKey: %v", err)
	}
	if item.Name != "laptop" || item.Purpose != "access" || item.OwnerUserID != "admin" {
		t.Errorf("ImportAccessKey() = %+v, want trimmed name, access purpose, admin owner", item)
	}
	if len(item.ProviderSync) != 1 || item.ProviderSync[0].State != "pending" {
		t.Errorf("new key providerSync = %+v, want one pending entry", item.ProviderSync)
	}
	if !drainKick(f.service) {
		t.Errorf("importing a key must request a sync")
	}

	if _, err := f.service.ImportAccessKey(ctx, "admin", "LAPTOP", "ssh-ed25519 BBBB"); !errors.Is(err, sshkeydomain.ErrDuplicateName) {
		t.Errorf("duplicate name error = %v, want ErrDuplicateName", err)
	}
	if _, err := f.service.ImportAccessKey(ctx, "admin", "other", "ssh-ed25519 AAAA copy"); !errors.Is(err, sshkeydomain.ErrDuplicateKey) {
		t.Errorf("duplicate material error = %v, want ErrDuplicateKey", err)
	}
}

func TestGenerateAccessKey_ReturnsPrivateKeyOnceAndStoresOnlyPublic(t *testing.T) {
	f := newFixture(t)
	generated, err := f.service.GenerateAccessKey(context.Background(), "admin", "jumpbox")
	if err != nil {
		t.Fatalf("GenerateAccessKey: %v", err)
	}
	if generated.PrivateKey == "" {
		t.Fatalf("GenerateAccessKey returned no private key")
	}
	if generated.Key.Purpose != "access" {
		t.Errorf("generated key purpose = %q, want access", generated.Key.Purpose)
	}
	if f.keys.privateKey != "" {
		t.Errorf("an Access Key's private key must never reach the repository")
	}
}

func TestDeleteAccessKey_RefusesDeploymentKeyAndOtherOwners(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deployment := f.deployment(t)
	item, err := f.service.ImportAccessKey(ctx, "admin", "laptop", "ssh-ed25519 AAAA")
	if err != nil {
		t.Fatalf("ImportAccessKey: %v", err)
	}

	if err := f.service.DeleteAccessKey(ctx, "admin", deployment.ID); !errors.Is(err, sshkeydomain.ErrDeploymentKeyImmutable) {
		t.Errorf("delete deployment key error = %v, want ErrDeploymentKeyImmutable", err)
	}
	if err := f.service.DeleteAccessKey(ctx, "someone-else", item.ID); !errors.Is(err, sshkeydomain.ErrKeyNotFound) {
		t.Errorf("delete another user's key error = %v, want ErrKeyNotFound", err)
	}
	if err := f.service.DeleteAccessKey(ctx, "admin", item.ID); err != nil {
		t.Errorf("delete own key: %v", err)
	}
}

// --- deployment key replacement ---

func TestReplaceDeploymentKey_KeepsIDAndRejectsEncryptedKeys(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	original := f.deployment(t)

	if _, err := f.service.ReplaceDeploymentKey(ctx, "PRIVATE-encrypted", ""); !errors.Is(err, sshkeydomain.ErrPassphraseProtected) {
		t.Errorf("encrypted key error = %v, want ErrPassphraseProtected", err)
	}

	item, err := f.service.ReplaceDeploymentKey(ctx, "  PRIVATE-ops  ", "")
	if err != nil {
		t.Fatalf("ReplaceDeploymentKey: %v", err)
	}
	if item.ID != original.ID || item.Name != original.Name {
		t.Errorf("replacement = %+v, want id %s and name %s kept", item, original.ID, original.Name)
	}
	if item.Fingerprint == original.Fingerprint {
		t.Errorf("replacement kept the old fingerprint")
	}
	if got := f.keys.privateKey; got != "PRIVATE-ops\n" {
		t.Errorf("stored private key = %q, want trimmed with one trailing newline", got)
	}
}

// --- realization ---

func TestSync_AddsAdoptsAndNeverRemovesOperatorKeys(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deployment := f.deployment(t)
	// The operator already added a personal key in MAAS, and happens to have added the same
	// material swallow later imports as an Access Key.
	f.maas.keys = []sshkeydomain.RegisteredKey{
		{ProviderKeyID: "op-1", PublicKey: "ssh-rsa OPERATOR bob"},
		{ProviderKeyID: "op-2", PublicKey: "ssh-ed25519 SHARED renamed-in-maas"},
	}
	access, err := f.service.ImportAccessKey(ctx, "admin", "shared", "ssh-ed25519 SHARED alice")
	if err != nil {
		t.Fatalf("ImportAccessKey: %v", err)
	}

	if err := f.service.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !f.maas.holds(deployment.PublicKey) {
		t.Errorf("Sync did not register the deployment key")
	}
	record, _ := f.syncs.get(access.ID, "maas-1")
	if record.ProviderKeyID != "op-2" || record.State != sshkeydomain.SyncSynced {
		t.Errorf("access key record = %+v, want adopted op-2 and synced", record)
	}

	// Deleting the adopted Access Key removes the provider key swallow adopted, but the
	// operator's unrelated key stays.
	if err := f.service.DeleteAccessKey(ctx, "admin", access.ID); err != nil {
		t.Fatalf("DeleteAccessKey: %v", err)
	}
	if err := f.service.Sync(ctx); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if f.maas.holds("ssh-ed25519 SHARED") {
		t.Errorf("Sync kept a deleted Access Key in the provisioner")
	}
	if !f.maas.holds("ssh-rsa OPERATOR") {
		t.Errorf("Sync removed a key the operator added outside swallow")
	}
	if _, ok := f.syncs.get(access.ID, "maas-1"); ok {
		t.Errorf("the orphan record should be dropped once its provider key is removed")
	}
}

func TestSync_ReplacedDeploymentKeyRetiresOldMaterial(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	original := f.deployment(t)
	if err := f.service.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	replaced, err := f.service.RegenerateDeploymentKey(ctx)
	if err != nil {
		t.Fatalf("RegenerateDeploymentKey: %v", err)
	}
	if replaced.ProviderSync[0].State != "pending" {
		t.Errorf("new material must read pending before a sync, got %q", replaced.ProviderSync[0].State)
	}
	if err := f.service.Sync(ctx); err != nil {
		t.Fatalf("Sync after regenerate: %v", err)
	}
	if f.maas.holds(original.PublicKey) {
		t.Errorf("the previous deployment public key was not removed from the provisioner")
	}
	if !f.maas.holds(replaced.PublicKey) {
		t.Errorf("the new deployment public key was not registered")
	}
	items, _ := f.service.List(ctx, "admin")
	if items[0].ProviderSync[0].State != "synced" {
		t.Errorf("deployment key state = %q, want synced", items[0].ProviderSync[0].State)
	}
}

func TestSync_RecordsUnsupportedAndFailedProvisioners(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deployment := f.deployment(t)
	f.provisioners.provisioners = append(f.provisioners.provisioners,
		sshkeydomain.Provisioner{IntegrationID: "plain-1", SiteID: "site-2"},
		sshkeydomain.Provisioner{IntegrationID: "broken-1", SiteID: "site-3"})
	f.provisioners.buildErr["broken-1"] = errors.New("This site's provisioner has no credential configured.")

	if err := f.service.Sync(ctx); err == nil {
		t.Errorf("Sync() error = nil, want the broken provisioner reported")
	}
	if record, _ := f.syncs.get(deployment.ID, "plain-1"); record.State != sshkeydomain.SyncUnsupported {
		t.Errorf("non-capable provisioner state = %q, want unsupported", record.State)
	}
	record, _ := f.syncs.get(deployment.ID, "broken-1")
	if record.State != sshkeydomain.SyncFailed || record.Error == "" {
		t.Errorf("unbuildable provisioner record = %+v, want failed with a reason", record)
	}
	if record, _ := f.syncs.get(deployment.ID, "maas-1"); record.State != sshkeydomain.SyncSynced {
		t.Errorf("one failing provisioner must not stop the others; maas-1 state = %q", record.State)
	}
}

func TestEnsureRegistered_GatesOnDeploymentKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.deployment(t)

	f.maas.addErr = errors.New("MAAS refused the key")
	if err := f.service.EnsureRegistered(ctx, "maas-1"); !errors.Is(err, sshkeydomain.ErrDeploymentKeyNotRegistered) {
		t.Errorf("EnsureRegistered() error = %v, want ErrDeploymentKeyNotRegistered", err)
	}

	f.maas.addErr = nil
	if err := f.service.EnsureRegistered(ctx, "maas-1"); err != nil {
		t.Errorf("EnsureRegistered() after recovery = %v, want nil", err)
	}

	f.provisioners.provisioners = append(f.provisioners.provisioners, sshkeydomain.Provisioner{IntegrationID: "plain-1"})
	if err := f.service.EnsureRegistered(ctx, "plain-1"); err != nil {
		t.Errorf("EnsureRegistered() on a non-capable provisioner = %v, want nil", err)
	}
	if err := f.service.EnsureRegistered(ctx, "not-a-provisioner"); err != nil {
		t.Errorf("EnsureRegistered() on an unknown integration = %v, want nil", err)
	}
}

func TestEnsureRegistered_FailsWithoutDeploymentKey(t *testing.T) {
	f := newFixture(t)
	if err := f.service.EnsureRegistered(context.Background(), "maas-1"); !errors.Is(err, sshkeydomain.ErrDeploymentKeyNotFound) {
		t.Errorf("EnsureRegistered() without a Deployment Key = %v, want ErrDeploymentKeyNotFound", err)
	}
}

func TestRequestSync_NeverBlocks(t *testing.T) {
	f := newFixture(t)
	for range 3 {
		f.service.RequestSync()
	}
	if !drainKick(f.service) {
		t.Fatalf("RequestSync did not queue a request")
	}
	if drainKick(f.service) {
		t.Errorf("repeated RequestSync calls must coalesce into one pending request")
	}
}
