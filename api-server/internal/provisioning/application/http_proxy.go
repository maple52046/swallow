package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// HTTPProxyItem is a provisioner Integration's Provisioner HTTP Proxy as the API reports it
// (provisioner-http-proxy.md, decision 056). HTTPProxy is "" unless Mode is external or peer, so an
// item can be written back unchanged.
type HTTPProxyItem struct {
	IntegrationID string `json:"integrationId"`
	Mode          string `json:"mode"`
	HTTPProxy     string `json:"httpProxy"`
}

// SetHTTPProxyInput is an operator's replacement Provisioner HTTP Proxy, still unvalidated.
type SetHTTPProxyInput struct {
	Mode      string
	HTTPProxy string
}

// HTTPProxyUseCase reads and writes the provisioner-owned Provisioner HTTP Proxy of one provisioner
// Integration (decision 056).
//
// Both directions are live: the provisioner is the source of truth and swallow stores nothing, so a
// read always reflects the provisioner and a write is read back before it is reported. The setting
// belongs to the Integration, not to a Server, so no Server Lock applies. Writes are not serialized:
// two concurrent writes may interleave their provider calls, and the read-back of each reports what
// the provisioner ended up holding. Authorization (admin) is enforced by the route.
type HTTPProxyUseCase struct {
	providers provisioningdomain.ProviderFactory
}

// NewHTTPProxyUseCase wires the use case to the factory that resolves an Integration's provider.
func NewHTTPProxyUseCase(providers provisioningdomain.ProviderFactory) *HTTPProxyUseCase {
	return &HTTPProxyUseCase{providers: providers}
}

// Get returns the Integration's Provisioner HTTP Proxy read live from its provisioner. A provisioner
// without the capability is refused as unsupported; the factory's errors (unknown Integration, not a
// provisioner, no credential) and provider errors pass through for the shared mapping.
func (uc *HTTPProxyUseCase) Get(ctx context.Context, integrationID string) (*HTTPProxyItem, error) {
	reader, _, err := uc.ports(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	return readHTTPProxy(ctx, integrationID, reader)
}

// Set validates input, writes it through to the provisioner, and returns the configuration read
// back. An invalid input is ErrInvalidHTTPProxy before the provisioner is resolved or called; a
// refusal by the provisioner is its ProviderError.
func (uc *HTTPProxyUseCase) Set(ctx context.Context, integrationID string, input SetHTTPProxyInput) (*HTTPProxyItem, error) {
	config, err := provisioningdomain.NormalizeHTTPProxy(provisioningdomain.HTTPProxyConfiguration{
		Mode: provisioningdomain.HTTPProxyMode(input.Mode), URL: input.HTTPProxy,
	})
	if err != nil {
		return nil, err
	}
	reader, writer, err := uc.ports(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if err := writer.SetHTTPProxy(ctx, config); err != nil {
		return nil, err
	}
	return readHTTPProxy(ctx, integrationID, reader)
}

// ports returns the Integration's HTTP Proxy ports, or the unsupported refusal when its provider
// does not advertise and implement both.
func (uc *HTTPProxyUseCase) ports(ctx context.Context, integrationID string) (
	provisioningdomain.HTTPProxyReader, provisioningdomain.HTTPProxyWriter, error,
) {
	provider, err := uc.providers.For(ctx, integrationID)
	if err != nil {
		return nil, nil, err
	}
	reader, canRead := provider.(provisioningdomain.HTTPProxyReader)
	writer, canWrite := provider.(provisioningdomain.HTTPProxyWriter)
	if !provider.Capabilities().HTTPProxy || !canRead || !canWrite {
		return nil, nil, unsupported("an HTTP proxy for deployed machines")
	}
	return reader, writer, nil
}

// readHTTPProxy shapes the live configuration into the API item.
func readHTTPProxy(ctx context.Context, integrationID string, reader provisioningdomain.HTTPProxyReader) (*HTTPProxyItem, error) {
	config, err := reader.HTTPProxy(ctx)
	if err != nil {
		return nil, err
	}
	item := &HTTPProxyItem{IntegrationID: integrationID, Mode: string(config.Mode)}
	if config.Mode.NeedsURL() {
		item.HTTPProxy = config.URL
	}
	return item, nil
}
