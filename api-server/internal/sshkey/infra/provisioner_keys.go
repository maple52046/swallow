package infra

import (
	"context"
	"errors"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
)

// ProvisionerKeys implements sshkeydomain.ProvisionerKeys over the site integration repository
// and the provisioning ProviderFactory, so SSH key realization shares the single provider
// transport and credential path the rest of swallow uses. Capability is decided exactly as ADR 031
// requires: the provider must both advertise SSHKeyRegistration and implement SSHKeyRegistrar.
type ProvisionerKeys struct {
	integrations sitedomain.IntegrationRepository
	providers    provisioningdomain.ProviderFactory
}

// NewProvisionerKeys wires the adapter.
func NewProvisionerKeys(integrations sitedomain.IntegrationRepository, providers provisioningdomain.ProviderFactory) *ProvisionerKeys {
	return &ProvisionerKeys{integrations: integrations, providers: providers}
}

// ListProvisioners returns every enabled provisioner Integration across all Sites.
func (p *ProvisionerKeys) ListProvisioners(ctx context.Context) ([]sshkeydomain.Provisioner, error) {
	integrations, err := p.integrations.List(ctx, sitedomain.IntegrationFilter{
		Kind:        sitedomain.IntegrationKindProvisioner,
		EnabledOnly: true,
	})
	if err != nil {
		return nil, err
	}
	provisioners := make([]sshkeydomain.Provisioner, 0, len(integrations))
	for _, integration := range integrations {
		provisioners = append(provisioners, sshkeydomain.Provisioner{
			IntegrationID: integration.ID,
			SiteID:        integration.SiteID,
		})
	}
	return provisioners, nil
}

// Registrar builds the provider for integrationID and returns its key registrar when the provider
// is key-capable. A provider that cannot be built (missing credential, unknown product) is an
// error with an operator-safe message; a built provider without the capability is (nil, false, nil).
func (p *ProvisionerKeys) Registrar(ctx context.Context, integrationID string) (sshkeydomain.KeyRegistrar, bool, error) {
	provider, err := p.providers.For(ctx, integrationID)
	if err != nil {
		return nil, false, operatorError(err)
	}
	registrar, ok := provider.(provisioningdomain.SSHKeyRegistrar)
	if !ok || !provider.Capabilities().SSHKeyRegistration {
		return nil, false, nil
	}
	return keyRegistrar{registrar: registrar}, true, nil
}

// keyRegistrar adapts one provider's SSHKeyRegistrar to the sshkey domain vocabulary.
type keyRegistrar struct {
	registrar provisioningdomain.SSHKeyRegistrar
}

// ListKeys, AddKey, and RemoveKey translate between provider and sshkey vocabulary and reduce
// provider errors to operator-safe messages; RemoveKey inherits the provider's "missing is
// satisfied" rule.
func (k keyRegistrar) ListKeys(ctx context.Context) ([]sshkeydomain.RegisteredKey, error) {
	keys, err := k.registrar.ListSSHKeys(ctx)
	if err != nil {
		return nil, operatorError(err)
	}
	registered := make([]sshkeydomain.RegisteredKey, 0, len(keys))
	for _, key := range keys {
		registered = append(registered, sshkeydomain.RegisteredKey{ProviderKeyID: key.ID, PublicKey: key.PublicKey})
	}
	return registered, nil
}

func (k keyRegistrar) AddKey(ctx context.Context, publicKey string) (sshkeydomain.RegisteredKey, error) {
	key, err := k.registrar.AddSSHKey(ctx, publicKey)
	if err != nil {
		return sshkeydomain.RegisteredKey{}, operatorError(err)
	}
	return sshkeydomain.RegisteredKey{ProviderKeyID: key.ID, PublicKey: key.PublicKey}, nil
}

func (k keyRegistrar) RemoveKey(ctx context.Context, providerKeyID string) error {
	return operatorError(k.registrar.RemoveSSHKey(ctx, providerKeyID))
}

// operatorError reduces a provider failure to the message sync records store and the API shows.
// A ProviderError's Detail is written to be client-safe; any other error (a missing credential, an
// unknown integration) already has a safe message. The underlying transport error, which may name
// internal addresses, is dropped.
func operatorError(err error) error {
	var providerErr *provisioningdomain.ProviderError
	if errors.As(err, &providerErr) && providerErr.Detail != "" {
		return errors.New(providerErr.Detail)
	}
	return err
}
