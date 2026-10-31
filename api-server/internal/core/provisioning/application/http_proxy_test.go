package application

import (
	"context"
	"errors"
	"testing"

	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	"github.com/maple52046/swallow/internal/core/provisioning/domain/settingstest"
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

func externalProxy(url string) provisioningdomain.HTTPProxyConfiguration {
	return provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeExternal, URL: url}
}

// The first read adopts the provisioner's configuration as the spec without writing to it; later
// reads return the stored setting.
func TestHTTPProxyGetAdoptsTheFirstObservation(t *testing.T) {
	provider := &proxyProvider{capable: true, config: externalProxy("http://10.170.168.20:18080")}
	settings := settingstest.NewHTTPProxyStore()
	uc := NewHTTPProxyUseCase(fixedProviders{provider: provider}, settings)

	item, err := uc.Get(context.Background(), "maas-a")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if item.Generation != 1 || item.Spec != (HTTPProxyConfigItem{Mode: "external", HTTPProxy: "http://10.170.168.20:18080"}) {
		t.Errorf("Get = %+v, want the observed proxy adopted at generation 1", item)
	}
	if item.Status.Observed == nil || item.Status.ObservedGeneration != 1 || len(provider.writes) != 0 {
		t.Errorf("status = %+v, writes %v; want realized by adoption without a write", item.Status, provider.writes)
	}

	provider.config = provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeBuiltin}
	item, err = uc.Get(context.Background(), "maas-a")
	if err != nil || item.Spec.Mode != "external" {
		t.Errorf("second Get = %+v, %v; want the stored spec, not a new adoption", item, err)
	}
}

// A provisioner without the capability is refused rather than ignored, for reads and writes alike.
func TestHTTPProxyUnsupported(t *testing.T) {
	uc := NewHTTPProxyUseCase(fixedProviders{provider: &proxyProvider{}}, settingstest.NewHTTPProxyStore())
	var provErr *provisioningdomain.ProviderError
	if _, err := uc.Get(context.Background(), "maas-a"); !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("Get without the capability error = %v, want ProviderErrorRejected", err)
	}
	_, err := uc.Set(context.Background(), "maas-a", SetHTTPProxyInput{Mode: "none"})
	if !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("Set without the capability error = %v, want ProviderErrorRejected", err)
	}
}

// A write stores the normalized spec and a new generation; the provisioner is not called.
func TestHTTPProxySetStoresTheSpec(t *testing.T) {
	provider := &proxyProvider{capable: true}
	uc := NewHTTPProxyUseCase(fixedProviders{provider: provider}, settingstest.NewHTTPProxyStore())

	item, err := uc.Set(context.Background(), "maas-a", SetHTTPProxyInput{Mode: "external", HTTPProxy: "http://10.170.168.20:18080/"})
	if err != nil {
		t.Fatalf("Set error = %v", err)
	}
	if item.Generation != 1 || item.Spec.HTTPProxy != "http://10.170.168.20:18080" || item.Status.Observed != nil {
		t.Errorf("Set = %+v, want the normalized spec at generation 1, not yet observed", item)
	}
	if len(provider.writes) != 0 {
		t.Errorf("writes = %+v, want none: the controller writes the provisioner", provider.writes)
	}
	if _, err := uc.Set(context.Background(), "maas-a", SetHTTPProxyInput{Mode: "peer"}); !errors.Is(err, provisioningdomain.ErrInvalidHTTPProxy) {
		t.Errorf("Set peer without a URL error = %v, want ErrInvalidHTTPProxy", err)
	}
}

type proxyControllerFixture struct {
	provider   *proxyProvider
	settings   *settingstest.HTTPProxyStore
	controller *HTTPProxyController
}

func newProxyControllerFixture(config provisioningdomain.HTTPProxyConfiguration) *proxyControllerFixture {
	f := &proxyControllerFixture{provider: &proxyProvider{capable: true, config: config}, settings: settingstest.NewHTTPProxyStore()}
	f.controller = NewHTTPProxyController(fixedIntegrations{"maas-a"}, fixedProviders{provider: f.provider}, f.settings, nil)
	return f
}

func (f *proxyControllerFixture) reconcile(t *testing.T, trigger convergence.Trigger) error {
	t.Helper()
	return f.controller.Reconcile(context.Background(), convergence.Request{Key: "maas-a", Trigger: trigger})
}

func (f *proxyControllerFixture) setting(t *testing.T) *provisioningdomain.HTTPProxySetting {
	t.Helper()
	setting, err := f.settings.Get(context.Background(), "maas-a")
	if err != nil {
		t.Fatalf("setting: %v", err)
	}
	return setting
}

// The controller adopts, converges a spec change at once, and corrects drift only at resync.
func TestHTTPProxyController(t *testing.T) {
	f := newProxyControllerFixture(externalProxy("http://old:3128"))
	if err := f.reconcile(t, convergence.TriggerResync); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if got := f.setting(t); got.Generation != 1 || got.Spec != externalProxy("http://old:3128") || len(f.provider.writes) != 0 {
		t.Fatalf("adopted = %+v, writes %v", got, f.provider.writes)
	}

	if _, err := f.settings.PutSpec(context.Background(), "maas-a", externalProxy("http://new:3128")); err != nil {
		t.Fatal(err)
	}
	if err := f.reconcile(t, convergence.TriggerChange); err != nil {
		t.Fatalf("converge: %v", err)
	}
	got := f.setting(t)
	if len(f.provider.writes) != 1 || got.Status.ObservedGeneration != 2 || !convergence.IsTrue(got.Status.Conditions, convergence.ConditionRealized) {
		t.Fatalf("after a spec change: %+v, writes %v", got.Status, f.provider.writes)
	}

	f.provider.config = provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeNone}
	if err := f.reconcile(t, convergence.TriggerChange); err != nil {
		t.Fatalf("drift on change: %v", err)
	}
	if got := f.setting(t); !convergence.IsTrue(got.Status.Conditions, convergence.ConditionDrifted) || len(f.provider.writes) != 1 {
		t.Fatalf("drift on a change trigger: %+v, writes %v", got.Status, f.provider.writes)
	}
	if err := f.reconcile(t, convergence.TriggerResync); err != nil {
		t.Fatalf("drift at resync: %v", err)
	}
	if got := f.setting(t); convergence.IsTrue(got.Status.Conditions, convergence.ConditionDrifted) || f.provider.config != externalProxy("http://new:3128") {
		t.Fatalf("after resync: %+v, provider %+v", got.Status, f.provider.config)
	}

	writes := f.settings.StatusWrites
	if err := f.reconcile(t, convergence.TriggerChange); err != nil {
		t.Fatal(err)
	}
	if f.settings.StatusWrites != writes {
		t.Error("a steady setting was rewritten on a change trigger")
	}
}

// fixedIntegrations lists a fixed set of provisioner Integrations.
type fixedIntegrations []string

func (f fixedIntegrations) ProvisionerIDs(context.Context) ([]string, error) { return f, nil }
