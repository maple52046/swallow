package application

import (
	"context"
	"errors"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// HTTPProxyItem is a provisioner Integration's Provisioner HTTP Proxy as the API reports it
// (provisioner-http-proxy.md, decisions 056 and 071): the spec swallow owns and the status the
// HTTP Proxy controller reports.
type HTTPProxyItem struct {
	IntegrationID string              `json:"integrationId"`
	Generation    int64               `json:"generation"`
	Spec          HTTPProxyConfigItem `json:"spec"`
	Status        HTTPProxyStatusItem `json:"status"`
}

// HTTPProxyConfigItem is one HTTP Proxy configuration; HTTPProxy is "" unless Mode is external or
// peer, so a spec can be written back unchanged.
type HTTPProxyConfigItem struct {
	Mode      string `json:"mode"`
	HTTPProxy string `json:"httpProxy"`
}

// HTTPProxyStatusItem is the published status. Observed is what the provisioner held when last
// read, null before the first read.
type HTTPProxyStatusItem struct {
	ObservedGeneration int64                       `json:"observedGeneration"`
	Conditions         []convergence.ConditionItem `json:"conditions"`
	Observed           *HTTPProxyConfigItem        `json:"observed"`
	ObservedAt         *string                     `json:"observedAt"`
}

// SetHTTPProxyInput is an operator's replacement Provisioner HTTP Proxy, still unvalidated.
type SetHTTPProxyInput struct {
	Mode      string
	HTTPProxy string
}

// HTTPProxyUseCase reads and writes the Provisioner HTTP Proxy of one provisioner Integration as
// swallow-owned desired state (decision 071).
//
// A write validates the configuration and stores it as the spec; the HTTP Proxy controller in the
// worker writes it to the provisioner and reports it in the status. A read returns the stored
// setting; an Integration with none yet is adopted from a live read first, which never writes to
// the provisioner. The setting belongs to the Integration, not to a Server, so no Server Lock
// applies. Authorization (admin) is enforced by the route.
type HTTPProxyUseCase struct {
	providers provisioningdomain.ProviderFactory
	settings  provisioningdomain.HTTPProxyStore
	now       func() time.Time
}

// NewHTTPProxyUseCase wires the use case to the factory that resolves an Integration's provider and
// the setting store.
func NewHTTPProxyUseCase(providers provisioningdomain.ProviderFactory, settings provisioningdomain.HTTPProxyStore) *HTTPProxyUseCase {
	return &HTTPProxyUseCase{providers: providers, settings: settings, now: func() time.Time { return time.Now().UTC() }}
}

// Get returns the Integration's HTTP Proxy setting. A provisioner without the capability is
// refused as unsupported; the factory's errors (unknown Integration, not a provisioner, no
// credential) and provider errors from an adoption read pass through for the shared mapping.
func (uc *HTTPProxyUseCase) Get(ctx context.Context, integrationID string) (*HTTPProxyItem, error) {
	reader, err := uc.reader(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	setting, err := uc.settings.Get(ctx, integrationID)
	if errors.Is(err, provisioningdomain.ErrSettingNotFound) {
		setting, err = uc.adopt(ctx, integrationID, reader)
	}
	if err != nil {
		return nil, err
	}
	return ToHTTPProxyItem(setting), nil
}

// Set validates input and stores it as the Integration's spec, returning the stored setting. An
// invalid input is ErrInvalidHTTPProxy before the provisioner is resolved; a provisioner without
// the capability is refused as unsupported. The provisioner is not called.
func (uc *HTTPProxyUseCase) Set(ctx context.Context, integrationID string, input SetHTTPProxyInput) (*HTTPProxyItem, error) {
	config, err := provisioningdomain.NormalizeHTTPProxy(provisioningdomain.HTTPProxyConfiguration{
		Mode: provisioningdomain.HTTPProxyMode(input.Mode), URL: input.HTTPProxy,
	})
	if err != nil {
		return nil, err
	}
	if _, err := uc.reader(ctx, integrationID); err != nil {
		return nil, err
	}
	setting, err := uc.settings.PutSpec(ctx, integrationID, config)
	if err != nil {
		return nil, err
	}
	return ToHTTPProxyItem(setting), nil
}

// adopt stores the provisioner's current configuration as the Integration's setting, or returns
// the one another adopter stored first.
func (uc *HTTPProxyUseCase) adopt(ctx context.Context, integrationID string, reader provisioningdomain.HTTPProxyReader) (*provisioningdomain.HTTPProxySetting, error) {
	observed, err := reader.HTTPProxy(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := uc.settings.Adopt(ctx, AdoptedHTTPProxy(integrationID, observed, uc.now())); err != nil {
		return nil, err
	}
	return uc.settings.Get(ctx, integrationID)
}

// reader returns the Integration's HTTP Proxy reader, or the unsupported refusal when its provider
// does not advertise and implement both directions.
func (uc *HTTPProxyUseCase) reader(ctx context.Context, integrationID string) (provisioningdomain.HTTPProxyReader, error) {
	reader, _, err := httpProxyPorts(ctx, uc.providers, integrationID)
	return reader, err
}

func httpProxyPorts(ctx context.Context, providers provisioningdomain.ProviderFactory, integrationID string) (
	provisioningdomain.HTTPProxyReader, provisioningdomain.HTTPProxyWriter, error,
) {
	provider, err := providers.For(ctx, integrationID)
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

// AdoptedHTTPProxy is the setting the first observation adopts: the observed configuration as the
// spec at generation 1, realized by definition.
func AdoptedHTTPProxy(integrationID string, observed provisioningdomain.HTTPProxyConfiguration, now time.Time) *provisioningdomain.HTTPProxySetting {
	return &provisioningdomain.HTTPProxySetting{
		IntegrationID: integrationID, Generation: 1, Spec: observed,
		Status: provisioningdomain.HTTPProxyStatus{
			ObservedGeneration: 1, Conditions: adoptedConditions(now), Observed: &observed, ObservedAt: &now,
		},
	}
}

// adoptedConditions are the conditions of a setting adopted from its observation.
func adoptedConditions(now time.Time) []convergence.Condition {
	var conditions []convergence.Condition
	for _, c := range []convergence.Condition{
		{Type: convergence.ConditionRealized, Status: convergence.ConditionTrue, Reason: convergence.ReasonConverged},
		{Type: convergence.ConditionDrifted, Status: convergence.ConditionFalse, Reason: convergence.ReasonInSync},
		{Type: convergence.ConditionRequiresAttention, Status: convergence.ConditionFalse, Reason: convergence.ReasonAttentionNotNeeded},
	} {
		c.ObservedGeneration = 1
		conditions, _ = convergence.SetCondition(conditions, c, now)
	}
	return conditions
}

// ToHTTPProxyItem shapes a stored setting into the API item.
func ToHTTPProxyItem(setting *provisioningdomain.HTTPProxySetting) *HTTPProxyItem {
	item := &HTTPProxyItem{
		IntegrationID: setting.IntegrationID, Generation: setting.Generation,
		Spec: httpProxyConfigItem(setting.Spec),
		Status: HTTPProxyStatusItem{
			ObservedGeneration: setting.Status.ObservedGeneration,
			Conditions:         convergence.ConditionItems(setting.Status.Conditions),
		},
	}
	if setting.Status.Observed != nil {
		observed := httpProxyConfigItem(*setting.Status.Observed)
		item.Status.Observed = &observed
	}
	if setting.Status.ObservedAt != nil {
		item.Status.ObservedAt = wire.TimePtr(*setting.Status.ObservedAt)
	}
	return item
}

func httpProxyConfigItem(config provisioningdomain.HTTPProxyConfiguration) HTTPProxyConfigItem {
	item := HTTPProxyConfigItem{Mode: string(config.Mode)}
	if config.Mode.NeedsURL() {
		item.HTTPProxy = config.URL
	}
	return item
}
