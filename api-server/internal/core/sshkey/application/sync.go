package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	sshkeydomain "github.com/maple52046/swallow/internal/core/sshkey/domain"
)

// ReconcileProvisioner realizes every SSH Key in one provisioner Integration: it adds missing keys,
// adopts keys the provisioner already holds, removes keys swallow registered that are no longer
// desired (deleted Access Keys and replaced Deployment Key material), and records each key's
// conditions. An Integration that is no longer an enabled provisioner has its records dropped: a
// deleted Integration can no longer be reached, so its provider keys cannot be removed, and a paused
// one is left alone. It is the SSH Key controller's Reconcile; the returned error means the
// provisioner could not be reached or listed, which the runtime retries.
func (s *Service) ReconcileProvisioner(ctx context.Context, integrationID string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	provisioners, err := s.provisioners.ListProvisioners(ctx)
	if err != nil {
		return fmt.Errorf("list provisioners: %w", err)
	}
	records, err := s.syncs.List(ctx)
	if err != nil {
		return fmt.Errorf("list ssh key syncs: %w", err)
	}
	index := slices.IndexFunc(provisioners, func(p sshkeydomain.Provisioner) bool { return p.IntegrationID == integrationID })
	if index < 0 {
		var errs []error
		for _, record := range records {
			if record.IntegrationID == integrationID {
				errs = append(errs, s.syncs.Delete(ctx, record.KeyID, record.IntegrationID))
			}
		}
		return errors.Join(errs...)
	}
	keys, err := s.keys.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("list ssh keys: %w", err)
	}
	_, err = s.syncProvisioner(ctx, provisioners[index], keys, recordsFor(records, integrationID))
	return err
}

// Targets returns every Integration the SSH Key controller converges: each enabled provisioner,
// and each Integration that still has records, so a removed or paused one is cleaned up.
func (s *Service) Targets(ctx context.Context) ([]string, error) {
	provisioners, err := s.provisioners.ListProvisioners(ctx)
	if err != nil {
		return nil, err
	}
	records, err := s.syncs.List(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, provisioner := range provisioners {
		ids = append(ids, provisioner.IntegrationID)
	}
	for _, record := range records {
		if !slices.Contains(ids, record.IntegrationID) {
			ids = append(ids, record.IntegrationID)
		}
	}
	return ids, nil
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
		realized, ok := convergence.FindCondition(results[key.ID].Status.Conditions, convergence.ConditionRealized)
		switch {
		case ok && realized.Status == convergence.ConditionTrue:
			return nil
		case ok && realized.Reason == convergence.ReasonCapabilityMissing:
			return nil
		default:
			return fmt.Errorf("%w: %s", sshkeydomain.ErrDeploymentKeyNotRegistered, realized.Message)
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
		reason, message := convergence.ReasonCapabilityMissing, "The provisioner cannot hold SSH keys."
		if err != nil {
			reason, message = convergence.ReasonProviderUnreachable, err.Error()
		}
		for _, key := range keys {
			record := prepareRecord(records[key.ID], key, provisioner)
			record.Status = judged(record.Status, key, now, convergence.ConditionFalse, reason, message)
			record.UpdatedAt = now
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
		wasRealized := record.Status.ObservedGeneration == key.Generation &&
			convergence.IsTrue(record.Status.Conditions, convergence.ConditionRealized)
		providerKeyID, added, addErr := realizeKey(ctx, registrar, key, record.ProviderKeyID, remote, remoteByID)
		if addErr != nil {
			record.ProviderKeyID = ""
			record.Status = judged(record.Status, key, now, convergence.ConditionFalse, convergence.ReasonFailed, addErr.Error())
		} else {
			record.ProviderKeyID = providerKeyID
			record.Status = judged(record.Status, key, now, convergence.ConditionTrue, convergence.ReasonConverged, "")
			claimed[providerKeyID] = true
			switch {
			case added && wasRealized:
				s.record(ctx, key, provisioner, convergence.EventDriftCorrected, "The key was missing from the provisioner and was added again.")
			case !wasRealized:
				s.record(ctx, key, provisioner, convergence.EventConverged, "The provisioner holds the key.")
			}
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

// realizeKey makes the provider hold key and returns the provider key id that represents it, and
// whether this call added it: the recorded id when the provider still lists it with the same
// material, else a matching key the provider already holds (adopted), else a newly added key. An
// add refused because another pass added the same key concurrently is resolved by re-listing and
// adopting.
func realizeKey(
	ctx context.Context,
	registrar sshkeydomain.KeyRegistrar,
	key *sshkeydomain.SSHKey,
	recordedID string,
	remote []sshkeydomain.RegisteredKey,
	remoteByID map[string]sshkeydomain.RegisteredKey,
) (string, bool, error) {
	if registered, ok := remoteByID[recordedID]; ok && sshkeydomain.SameKeyMaterial(registered.PublicKey, key.PublicKey) {
		return recordedID, false, nil
	}
	if id, ok := findMaterial(remote, key.PublicKey); ok {
		return id, false, nil
	}
	added, err := registrar.AddKey(ctx, key.PublicKey)
	if err == nil {
		return added.ProviderKeyID, true, nil
	}
	if relisted, listErr := registrar.ListKeys(ctx); listErr == nil {
		if id, ok := findMaterial(relisted, key.PublicKey); ok {
			return id, false, nil
		}
	}
	return "", false, err
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

// judged returns status with the key's realized condition set for its current generation and the
// observation time stamped; a realized key also records the generation it realized.
func judged(status convergence.TargetStatus, key *sshkeydomain.SSHKey, now time.Time, realized convergence.ConditionStatus, reason, message string) convergence.TargetStatus {
	status.Conditions, _ = convergence.SetCondition(status.Conditions, convergence.Condition{
		Type: convergence.ConditionRealized, Status: realized, Reason: reason, Message: message, ObservedGeneration: key.Generation,
	}, now)
	if realized == convergence.ConditionTrue {
		status.ObservedGeneration = key.Generation
	}
	status.ObservedAt = &now
	return status
}

// record appends a timeline event for one key on one provisioner. A failure to record never fails
// the pass, because the event describes a change already made.
func (s *Service) record(ctx context.Context, key *sshkeydomain.SSHKey, provisioner sshkeydomain.Provisioner, eventType convergence.EventType, message string) {
	if s.events == nil {
		return
	}
	_ = s.events.Record(ctx, convergence.Event{
		ResourceKind: "ssh-key", ResourceID: key.ID, Type: eventType, Fields: []string{provisioner.IntegrationID},
		Message: message, Generation: key.Generation, At: s.now(),
	})
}

// persist writes one record. A write failure is not returned: the provider state it describes is
// already true, and the next pass recomputes the record from the provider's list.
func (s *Service) persist(ctx context.Context, record sshkeydomain.ProviderSync) {
	_ = s.syncs.Upsert(ctx, record)
}

// prepareRecord returns the record for key in provisioner, starting from the existing one. When the
// key's material changed since the record was written (the Deployment Key was replaced), the
// previously registered provider key is moved to the retired list so it is removed, not reused,
// and the record's judgement of the old material no longer counts.
func prepareRecord(existing sshkeydomain.ProviderSync, key *sshkeydomain.SSHKey, provisioner sshkeydomain.Provisioner) sshkeydomain.ProviderSync {
	record := existing
	record.KeyID = key.ID
	record.IntegrationID = provisioner.IntegrationID
	record.SiteID = provisioner.SiteID
	record.Status.TargetID = provisioner.IntegrationID
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
