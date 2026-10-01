package application

import (
	"context"
	"errors"
	"fmt"
	"slices"

	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
)

// Sync realizes every SSH Key in every provisioner Integration: it adds missing keys, adopts keys a
// provisioner already holds, removes keys swallow registered that are no longer desired (deleted
// Access Keys and replaced Deployment Key material), and records each key's state. It also drops
// sync records of Integrations that no longer exist.
//
// A failure in one provisioner does not stop the others; the returned error joins every
// provisioner-level failure for logging, while per-key failures are recorded as SyncFailed. Sync is
// safe to call repeatedly and concurrently with other passes in this process.
func (s *Service) Sync(ctx context.Context) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	provisioners, err := s.provisioners.ListProvisioners(ctx)
	if err != nil {
		return fmt.Errorf("list provisioners: %w", err)
	}
	keys, err := s.keys.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("list ssh keys: %w", err)
	}
	records, err := s.syncs.List(ctx)
	if err != nil {
		return fmt.Errorf("list ssh key syncs: %w", err)
	}

	known := make(map[string]bool, len(provisioners))
	var errs []error
	for _, provisioner := range provisioners {
		known[provisioner.IntegrationID] = true
		if _, err := s.syncProvisioner(ctx, provisioner, keys, recordsFor(records, provisioner.IntegrationID)); err != nil {
			errs = append(errs, fmt.Errorf("integration %s: %w", provisioner.IntegrationID, err))
		}
	}
	// An Integration that was deleted can no longer be reached, so its provider keys cannot be
	// removed; its records only describe a target that is gone.
	for _, record := range records {
		if known[record.IntegrationID] {
			continue
		}
		if err := s.syncs.Delete(ctx, record.KeyID, record.IntegrationID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// EnsureRegistered realizes every SSH Key in one provisioner Integration and reports whether the
// Deployment Key is usable for a deployment that provisioner is about to start. It returns nil when
// the Deployment Key is synced, when the provisioner cannot hold keys (nothing swallow can do; the
// Server must authorize the key another way), or when the Integration is not a provisioner. It
// returns ErrDeploymentKeyNotFound when the installation has no Deployment Key (deployment callers
// check that first; this covers a key removed in between), and otherwise
// ErrDeploymentKeyNotRegistered with the provider's reason, which callers treat as retryable
// because the provisioner may recover.
func (s *Service) EnsureRegistered(ctx context.Context, integrationID string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	provisioners, err := s.provisioners.ListProvisioners(ctx)
	if err != nil {
		return fmt.Errorf("list provisioners: %w", err)
	}
	index := slices.IndexFunc(provisioners, func(p sshkeydomain.Provisioner) bool {
		return p.IntegrationID == integrationID
	})
	if index < 0 {
		return nil
	}
	keys, err := s.keys.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("list ssh keys: %w", err)
	}
	records, err := s.syncs.List(ctx)
	if err != nil {
		return fmt.Errorf("list ssh key syncs: %w", err)
	}
	results, _ := s.syncProvisioner(ctx, provisioners[index], keys, recordsFor(records, integrationID))

	for _, key := range keys {
		if key.Purpose != sshkeydomain.PurposeDeployment {
			continue
		}
		result := results[key.ID]
		switch result.State {
		case sshkeydomain.SyncSynced, sshkeydomain.SyncUnsupported:
			return nil
		default:
			return fmt.Errorf("%w: %s", sshkeydomain.ErrDeploymentKeyNotRegistered, result.Error)
		}
	}
	return sshkeydomain.ErrDeploymentKeyNotFound
}

// syncProvisioner runs one realization pass against one provisioner and persists the resulting
// records. records holds this Integration's existing records keyed by key id, including orphans.
// It returns every desired key's resulting record, and an error only when the provisioner could
// not be reached or listed at all (per-key failures are recorded, not returned).
//
// Removal is conservative: swallow removes only provider key ids it recorded as registered or
// adopted, only when no desired key claims the same provider key in this pass, and only when the
// provider still lists it. Operator-added provider keys are never candidates.
func (s *Service) syncProvisioner(
	ctx context.Context,
	provisioner sshkeydomain.Provisioner,
	keys []*sshkeydomain.SSHKey,
	records map[string]sshkeydomain.ProviderSync,
) (map[string]sshkeydomain.ProviderSync, error) {
	now := s.now()
	results := make(map[string]sshkeydomain.ProviderSync, len(keys))

	registrar, capable, err := s.provisioners.Registrar(ctx, provisioner.IntegrationID)
	var remote []sshkeydomain.RegisteredKey
	if err == nil && capable {
		remote, err = registrar.ListKeys(ctx)
	}
	if err != nil || !capable {
		state, reason := sshkeydomain.SyncUnsupported, ""
		if err != nil {
			state, reason = sshkeydomain.SyncFailed, err.Error()
		}
		for _, key := range keys {
			record := prepareRecord(records[key.ID], key, provisioner)
			record.State, record.Error, record.UpdatedAt = state, reason, now
			results[key.ID] = record
			s.persist(ctx, record)
		}
		return results, err
	}

	remoteByID := make(map[string]sshkeydomain.RegisteredKey, len(remote))
	for _, key := range remote {
		remoteByID[key.ProviderKeyID] = key
	}
	claimed := make(map[string]bool, len(keys))
	for _, key := range keys {
		record := prepareRecord(records[key.ID], key, provisioner)
		record.UpdatedAt = now
		providerKeyID, addErr := realizeKey(ctx, registrar, key, record.ProviderKeyID, remote, remoteByID)
		if addErr != nil {
			record.ProviderKeyID = ""
			record.State, record.Error = sshkeydomain.SyncFailed, addErr.Error()
		} else {
			record.ProviderKeyID = providerKeyID
			record.State, record.Error, record.SyncedAt = sshkeydomain.SyncSynced, "", &now
			claimed[providerKeyID] = true
		}
		results[key.ID] = record
	}

	for keyID, record := range results {
		record.RetiredProviderKeyIDs = s.removeRetired(ctx, registrar, record.RetiredProviderKeyIDs, claimed, remoteByID)
		results[keyID] = record
		s.persist(ctx, record)
	}
	for keyID, record := range records {
		if _, desired := results[keyID]; desired {
			continue
		}
		// An orphan: its key was deleted. Both its current and retired provider keys go.
		pending := record.RetiredProviderKeyIDs
		if record.ProviderKeyID != "" {
			pending = append(slices.Clone(pending), record.ProviderKeyID)
		}
		remaining := s.removeRetired(ctx, registrar, pending, claimed, remoteByID)
		if len(remaining) == 0 {
			_ = s.syncs.Delete(ctx, record.KeyID, record.IntegrationID)
			continue
		}
		record.ProviderKeyID = ""
		record.RetiredProviderKeyIDs = remaining
		record.UpdatedAt = now
		s.persist(ctx, record)
	}
	return results, nil
}

// realizeKey makes the provider hold key and returns the provider key id that represents it: the
// recorded id when the provider still lists it with the same material, else a matching key the
// provider already holds (adopted), else a newly added key. An add refused because another pass
// added the same key concurrently is resolved by re-listing and adopting.
func realizeKey(
	ctx context.Context,
	registrar sshkeydomain.KeyRegistrar,
	key *sshkeydomain.SSHKey,
	recordedID string,
	remote []sshkeydomain.RegisteredKey,
	remoteByID map[string]sshkeydomain.RegisteredKey,
) (string, error) {
	if registered, ok := remoteByID[recordedID]; ok && sshkeydomain.SameKeyMaterial(registered.PublicKey, key.PublicKey) {
		return recordedID, nil
	}
	if id, ok := findMaterial(remote, key.PublicKey); ok {
		return id, nil
	}
	added, err := registrar.AddKey(ctx, key.PublicKey)
	if err == nil {
		return added.ProviderKeyID, nil
	}
	if relisted, listErr := registrar.ListKeys(ctx); listErr == nil {
		if id, ok := findMaterial(relisted, key.PublicKey); ok {
			return id, nil
		}
	}
	return "", err
}

// removeRetired removes each provider key id that is still present and unclaimed, returning the ids
// whose removal failed so a later pass retries them. Ids the provider no longer lists, and ids a
// desired key claims in this pass, need no removal and are dropped.
func (s *Service) removeRetired(
	ctx context.Context,
	registrar sshkeydomain.KeyRegistrar,
	ids []string,
	claimed map[string]bool,
	remoteByID map[string]sshkeydomain.RegisteredKey,
) []string {
	var remaining []string
	for _, id := range ids {
		if claimed[id] {
			continue
		}
		if _, present := remoteByID[id]; !present {
			continue
		}
		if err := registrar.RemoveKey(ctx, id); err != nil {
			remaining = append(remaining, id)
		}
	}
	return remaining
}

// persist writes one record. A write failure is not returned: the provider state it describes is
// already true, and the next pass recomputes the record from the provider's list.
func (s *Service) persist(ctx context.Context, record sshkeydomain.ProviderSync) {
	_ = s.syncs.Upsert(ctx, record)
}

// prepareRecord returns the record for key in provisioner, starting from the existing one. When the
// key's material changed since the record was written (the Deployment Key was replaced), the
// previously registered provider key is moved to the retired list so it is removed, not reused.
func prepareRecord(existing sshkeydomain.ProviderSync, key *sshkeydomain.SSHKey, provisioner sshkeydomain.Provisioner) sshkeydomain.ProviderSync {
	record := existing
	record.KeyID = key.ID
	record.IntegrationID = provisioner.IntegrationID
	record.SiteID = provisioner.SiteID
	if record.ProviderKeyID != "" && record.Fingerprint != key.Fingerprint {
		record.RetiredProviderKeyIDs = append(slices.Clone(record.RetiredProviderKeyIDs), record.ProviderKeyID)
		record.ProviderKeyID = ""
	}
	record.Fingerprint = key.Fingerprint
	return record
}

// findMaterial returns the provider id of the key carrying publicKey's material, ignoring comments.
func findMaterial(keys []sshkeydomain.RegisteredKey, publicKey string) (string, bool) {
	for _, key := range keys {
		if sshkeydomain.SameKeyMaterial(key.PublicKey, publicKey) {
			return key.ProviderKeyID, true
		}
	}
	return "", false
}

// recordsFor selects one Integration's records, keyed by key id.
func recordsFor(records []sshkeydomain.ProviderSync, integrationID string) map[string]sshkeydomain.ProviderSync {
	out := make(map[string]sshkeydomain.ProviderSync)
	for _, record := range records {
		if record.IntegrationID == integrationID {
			out[record.KeyID] = record
		}
	}
	return out
}
