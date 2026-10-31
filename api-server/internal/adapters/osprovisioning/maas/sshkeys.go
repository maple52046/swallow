package maas

import (
	"context"
	"net/url"
	"strconv"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// This file implements provisioningdomain.SSHKeyRegistrar against MAAS account SSH keys
// (decision 039). MAAS keeps SSH public keys per MAAS user and, on every deployment that user
// starts, has cloud-init write them into the deployed image's default user. swallow deploys with
// the integration's API key, so registering a key here makes every later swallow deployment
// through this MAAS authorize it. The keys belong to the MAAS user that owns the API key: an
// operator who shares that MAAS account sees swallow's keys in the MAAS UI, and keys the operator
// added there are listed too (swallow never removes a key it did not register or adopt).

// sshKeysPath is the MAAS collection of SSH keys for the authenticated user.
const sshKeysPath = "/account/prefs/sshkeys/"

// sshKeyPath is the MAAS endpoint for one SSH key, addressed by its numeric id.
func sshKeyPath(id string) string {
	return sshKeysPath + url.PathEscape(id) + "/"
}

// sshKeyJSON is the subset of a MAAS SSH key swallow reads. MAAS reports the id as a number; the
// adapter carries it as an opaque string so the domain never depends on its type.
type sshKeyJSON struct {
	ID  int    `json:"id"`
	Key string `json:"key"`
}

func (k sshKeyJSON) toDomain() provisioningdomain.ProviderSSHKey {
	return provisioningdomain.ProviderSSHKey{ID: strconv.Itoa(k.ID), PublicKey: k.Key}
}

// ListSSHKeys returns every SSH key MAAS holds for the API-key owner, including keys MAAS imported
// from Launchpad or GitHub and keys an operator pasted into the MAAS UI.
func (p *Provider) ListSSHKeys(ctx context.Context) ([]provisioningdomain.ProviderSSHKey, error) {
	var out []sshKeyJSON
	if err := p.client.get(ctx, sshKeysPath, nil, &out); err != nil {
		return nil, translateError(err, "")
	}
	keys := make([]provisioningdomain.ProviderSSHKey, 0, len(out))
	for _, key := range out {
		keys = append(keys, key.toDomain())
	}
	return keys, nil
}

// AddSSHKey registers one authorized_keys line. MAAS validates the key and refuses a malformed key
// or one the account already holds with a 400, which translateError maps to ProviderErrorRejected;
// callers that must be idempotent list first and adopt a matching key instead of re-adding it.
func (p *Provider) AddSSHKey(ctx context.Context, publicKey string) (provisioningdomain.ProviderSSHKey, error) {
	var out sshKeyJSON
	if err := p.client.postMultipart(ctx, sshKeysPath, map[string]string{"key": publicKey}, &out); err != nil {
		return provisioningdomain.ProviderSSHKey{}, translateError(err, "")
	}
	return out.toDomain(), nil
}

// RemoveSSHKey deletes one MAAS SSH key. A 404 means the key is already gone (an operator deleted
// it in MAAS, or an earlier removal succeeded without being recorded), which is the requested end
// state, so it is not an error.
func (p *Provider) RemoveSSHKey(ctx context.Context, keyID string) error {
	err := p.client.delete(ctx, sshKeyPath(keyID))
	if err == nil || isNotFound(err) {
		return nil
	}
	return translateError(err, "")
}
