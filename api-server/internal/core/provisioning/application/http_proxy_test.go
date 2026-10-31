package application

import (
	"context"
	"errors"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// proxyProvider is a provisioner holding one Provisioner HTTP Proxy. capable switches the advertised
// capability; writes records every configuration it was given.
type proxyProvider struct {
	provisioningdomain.OSProvisioningProvider
	capable bool
	config  provisioningdomain.HTTPProxyConfiguration
	writes  []provisioningdomain.HTTPProxyConfiguration
	setErr  error
}

func (p *proxyProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{HTTPProxy: p.capable}
}

func (p *proxyProvider) HTTPProxy(context.Context) (provisioningdomain.HTTPProxyConfiguration, error) {
	return p.config, nil
}

func (p *proxyProvider) SetHTTPProxy(_ context.Context, config provisioningdomain.HTTPProxyConfiguration) error {
	if p.setErr != nil {
		return p.setErr
	}
	p.writes = append(p.writes, config)
	p.config = config
	return nil
}

// The read reports the URL only for modes that route through it, so a read can be written back.
func TestHTTPProxyGet(t *testing.T) {
	provider := &proxyProvider{capable: true, config: provisioningdomain.HTTPProxyConfiguration{
		Mode: provisioningdomain.HTTPProxyModeExternal, URL: "http://10.170.168.20:18080",
	}}
	uc := NewHTTPProxyUseCase(fixedProviders{provider: provider})
	item, err := uc.Get(context.Background(), "maas-a")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	want := HTTPProxyItem{IntegrationID: "maas-a", Mode: "external", HTTPProxy: "http://10.170.168.20:18080"}
	if *item != want {
		t.Errorf("Get = %+v, want %+v", *item, want)
	}

	provider.config = provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeBuiltin, URL: "http://leftover:3128"}
	item, err = uc.Get(context.Background(), "maas-a")
	if err != nil || item.Mode != "builtin" || item.HTTPProxy != "" {
		t.Errorf("Get of builtin = %+v, %v; want mode builtin without a URL", item, err)
	}
}

// A provisioner without the capability is refused rather than ignored, for reads and writes alike.
func TestHTTPProxyUnsupported(t *testing.T) {
	uc := NewHTTPProxyUseCase(fixedProviders{provider: &proxyProvider{}})
	var provErr *provisioningdomain.ProviderError
	if _, err := uc.Get(context.Background(), "maas-a"); !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("Get without the capability error = %v, want ProviderErrorRejected", err)
	}
	_, err := uc.Set(context.Background(), "maas-a", SetHTTPProxyInput{Mode: "none"})
	if !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("Set without the capability error = %v, want ProviderErrorRejected", err)
	}
}

func TestHTTPProxySet(t *testing.T) {
	provider := &proxyProvider{capable: true}
	uc := NewHTTPProxyUseCase(fixedProviders{provider: provider})

	item, err := uc.Set(context.Background(), "maas-a", SetHTTPProxyInput{Mode: "external", HTTPProxy: "http://10.170.168.20:18080/"})
	if err != nil {
		t.Fatalf("Set error = %v", err)
	}
	if item.Mode != "external" || item.HTTPProxy != "http://10.170.168.20:18080" {
		t.Errorf("Set = %+v, want the normalized external proxy read back", item)
	}
	if len(provider.writes) != 1 || provider.writes[0].URL != "http://10.170.168.20:18080" {
		t.Errorf("writes = %+v, want one normalized write", provider.writes)
	}

	if _, err := uc.Set(context.Background(), "maas-a", SetHTTPProxyInput{Mode: "peer"}); !errors.Is(err, provisioningdomain.ErrInvalidHTTPProxy) {
		t.Errorf("Set peer without a URL error = %v, want ErrInvalidHTTPProxy", err)
	}
	if len(provider.writes) != 1 {
		t.Errorf("an invalid input reached the provisioner: writes = %+v", provider.writes)
	}

	refusal := &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorRejected, Detail: "Enter a valid URL."}
	provider.setErr = refusal
	if _, err := uc.Set(context.Background(), "maas-a", SetHTTPProxyInput{Mode: "none"}); !errors.Is(err, refusal) {
		t.Errorf("Set refused by the provisioner error = %v, want the provider refusal", err)
	}
}
