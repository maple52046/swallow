package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// HTTPProxyController converges each provisioner Integration's HTTP Proxy setting (glossary:
// Controller; decision 071). Its key is the Integration id. It adopts the provisioner's current
// configuration when swallow holds none, writes a spec change at once, reports a configuration
// changed in the provisioner as drift and corrects it at the periodic resync, and removes the
// setting of an Integration that no longer exists. It is the only writer of the setting's status
// after adoption.
type HTTPProxyController struct {
	integrations provisioningdomain.ProvisionerIntegrations
	providers    provisioningdomain.ProviderFactory
	settings     provisioningdomain.HTTPProxyStore
	events       convergence.EventRecorder
	now          func() time.Time
	log          *slog.Logger
}

var _ convergence.Controller = (*HTTPProxyController)(nil)

// NewHTTPProxyController wires the provisioner Integration lister, the provider factory, the
// setting store, and the convergence timeline.
func NewHTTPProxyController(
	integrations provisioningdomain.ProvisionerIntegrations,
	providers provisioningdomain.ProviderFactory,
	settings provisioningdomain.HTTPProxyStore,
	events convergence.EventRecorder,
) *HTTPProxyController {
	return &HTTPProxyController{
		integrations: integrations, providers: providers, settings: settings, events: events,
		now: func() time.Time { return time.Now().UTC() },
		log: slog.Default().With("controller", "http-proxy"),
	}
}

// Name implements convergence.Controller.
func (c *HTTPProxyController) Name() string { return "http-proxy" }

// Collections implements convergence.Controller: the settings, and the Integrations whose
// appearance or change must be converged without waiting for the resync.
func (c *HTTPProxyController) Collections() []string {
	return []string{"provisioner_http_proxies", "integrations"}
}

// KeysForChange implements convergence.Controller; both collections are keyed by Integration id.
func (c *HTTPProxyController) KeysForChange(_ context.Context, change store.Change) ([]string, error) {
	if change.Collection == "provisioner_http_proxies" && change.Operation == store.ChangeDelete {
		return nil, nil
	}
	return []string{change.Key}, nil
}

// Keys implements convergence.Controller with every enabled provisioner Integration and every
// stored setting, so a setting whose Integration is gone is removed.
func (c *HTTPProxyController) Keys(ctx context.Context) ([]string, error) {
	keys, err := c.integrations.ProvisionerIDs(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := c.settings.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, setting := range settings {
		if !slices.Contains(keys, setting.IntegrationID) {
			keys = append(keys, setting.IntegrationID)
		}
	}
	return keys, nil
}

// Reconcile implements convergence.Controller.
func (c *HTTPProxyController) Reconcile(ctx context.Context, request convergence.Request) error {
	integrationID := request.Key
	reader, writer, err := httpProxyPorts(ctx, c.providers, integrationID)
	switch {
	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		return c.settings.Delete(ctx, integrationID)
	case notAProvisionerSetting(err):
		return nil
	case err != nil:
		return err
	}
	setting, err := c.settings.Get(ctx, integrationID)
	if err != nil && !errors.Is(err, provisioningdomain.ErrSettingNotFound) {
		return err
	}
	now := c.now()
	observed, err := reader.HTTPProxy(ctx)
	if err != nil {
		if setting != nil {
			c.writeUnreachable(ctx, setting, now)
		}
		return err
	}
	if setting == nil {
		adopted, err := c.settings.Adopt(ctx, AdoptedHTTPProxy(integrationID, observed, now))
		if adopted {
			c.record(ctx, integrationID, 1, convergence.EventAdopted, "The provisioner's HTTP proxy became the spec.")
		}
		return err
	}

	result := convergence.ConvergeSingleTarget(ctx, convergence.SingleTarget{
		Now: now, Trigger: request.Trigger, Generation: setting.Generation,
		ObservedGeneration: setting.Status.ObservedGeneration, Conditions: setting.Status.Conditions,
		Drift: httpProxyDrift(setting.Spec, observed),
		Realize: func(ctx context.Context) (string, error) {
			if err := writer.SetHTTPProxy(ctx, setting.Spec); err != nil {
				return "", err
			}
			after, err := reader.HTTPProxy(ctx)
			if err != nil {
				return "", err
			}
			observed = after
			return httpProxyDrift(setting.Spec, after), nil
		},
		Rejected: providerRejection,
	})
	for _, event := range result.Events {
		c.record(ctx, integrationID, setting.Generation, event.Type, event.Message)
	}
	before := setting.Status.Observed
	if result.Changed || request.Trigger == convergence.TriggerResync || before == nil || *before != observed {
		status := provisioningdomain.HTTPProxyStatus{
			ObservedGeneration: result.ObservedGeneration, Conditions: result.Conditions,
			Observed: &observed, ObservedAt: &now,
		}
		if err := c.settings.SetStatus(ctx, integrationID, status); err != nil && !errors.Is(err, provisioningdomain.ErrSettingNotFound) {
			return err
		}
	}
	return result.Err
}

func (c *HTTPProxyController) writeUnreachable(ctx context.Context, setting *provisioningdomain.HTTPProxySetting, now time.Time) {
	conditions, changed := convergence.SetCondition(setting.Status.Conditions, convergence.Condition{
		Type: convergence.ConditionRealized, Status: convergence.ConditionFalse, Reason: convergence.ReasonProviderUnreachable,
		Message: "The provisioner could not be reached; retrying.", ObservedGeneration: setting.Generation,
	}, now)
	if !changed {
		return
	}
	status := setting.Status
	status.Conditions = conditions
	if err := c.settings.SetStatus(ctx, setting.IntegrationID, status); err != nil {
		c.log.Warn("http proxy status not written", "integration", setting.IntegrationID, "error", err)
	}
}

func (c *HTTPProxyController) record(ctx context.Context, integrationID string, generation int64, eventType convergence.EventType, message string) {
	recordSettingEvent(ctx, c.events, c.log, "http-proxy", integrationID, generation, eventType, message)
}

// httpProxyDrift describes how the provisioner's configuration departs from the spec.
func httpProxyDrift(spec, observed provisioningdomain.HTTPProxyConfiguration) string {
	if spec == observed {
		return ""
	}
	describe := func(config provisioningdomain.HTTPProxyConfiguration) string {
		if config.Mode.NeedsURL() {
			return fmt.Sprintf("%s %s", config.Mode, config.URL)
		}
		return string(config.Mode)
	}
	return fmt.Sprintf("The provisioner's HTTP proxy is %s; the spec is %s.", describe(observed), describe(spec))
}

// SubnetDNSController converges each provisioner subnet's DNS servers (glossary: Controller;
// decision 071). Keys are `subnets:<integrationId>`, which adopts subnets swallow holds no setting
// for and removes the settings of subnets the provisioner no longer has, and
// `subnet-dns:<integrationId>/<subnetId>` for one setting. A spec change is written at once; a
// change made in the provisioner is drift, corrected at the periodic resync.
type SubnetDNSController struct {
	integrations provisioningdomain.ProvisionerIntegrations
	providers    provisioningdomain.ProviderFactory
	settings     provisioningdomain.SubnetDNSStore
	events       convergence.EventRecorder
	now          func() time.Time
	log          *slog.Logger
}

var _ convergence.Controller = (*SubnetDNSController)(nil)

// NewSubnetDNSController wires the provisioner Integration lister, the provider factory, the
// setting store, and the convergence timeline.
func NewSubnetDNSController(
	integrations provisioningdomain.ProvisionerIntegrations,
	providers provisioningdomain.ProviderFactory,
	settings provisioningdomain.SubnetDNSStore,
	events convergence.EventRecorder,
) *SubnetDNSController {
	return &SubnetDNSController{
		integrations: integrations, providers: providers, settings: settings, events: events,
		now: func() time.Time { return time.Now().UTC() },
		log: slog.Default().With("controller", "subnet-dns"),
	}
}

// Name implements convergence.Controller.
func (c *SubnetDNSController) Name() string { return "subnet-dns" }

// Collections implements convergence.Controller.
func (c *SubnetDNSController) Collections() []string {
	return []string{"provisioner_subnet_dns", "integrations"}
}

// KeysForChange implements convergence.Controller. A setting's document id is
// "<integrationId>/<subnetId>"; an Integration change re-reads its subnets.
func (c *SubnetDNSController) KeysForChange(_ context.Context, change store.Change) ([]string, error) {
	if change.Collection == "integrations" {
		return []string{"subnets:" + change.Key}, nil
	}
	if change.Operation == store.ChangeDelete {
		return nil, nil
	}
	return []string{"subnet-dns:" + change.Key}, nil
}

// Keys implements convergence.Controller: a subnet pass per enabled provisioner Integration and
// per Integration that still has settings, then every stored setting.
func (c *SubnetDNSController) Keys(ctx context.Context) ([]string, error) {
	integrations, err := c.integrations.ProvisionerIDs(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := c.settings.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, setting := range settings {
		if !slices.Contains(integrations, setting.IntegrationID) {
			integrations = append(integrations, setting.IntegrationID)
		}
	}
	keys := make([]string, 0, len(integrations)+len(settings))
	for _, id := range integrations {
		keys = append(keys, "subnets:"+id)
	}
	for _, setting := range settings {
		keys = append(keys, "subnet-dns:"+setting.IntegrationID+"/"+setting.SubnetID)
	}
	return keys, nil
}

// Reconcile implements convergence.Controller.
func (c *SubnetDNSController) Reconcile(ctx context.Context, request convergence.Request) error {
	kind, rest, _ := strings.Cut(request.Key, ":")
	switch kind {
	case "subnets":
		return c.reconcileIntegration(ctx, rest)
	case "subnet-dns":
		integrationID, subnetID, ok := strings.Cut(rest, "/")
		if !ok {
			return fmt.Errorf("subnet-dns controller: malformed key %q", request.Key)
		}
		return c.reconcileSubnet(ctx, integrationID, subnetID, request.Trigger)
	}
	return fmt.Errorf("subnet-dns controller: unknown key %q", request.Key)
}

// reconcileIntegration adopts the subnets swallow holds no setting for and removes the settings of
// subnets (or an Integration) the provisioner no longer has.
func (c *SubnetDNSController) reconcileIntegration(ctx context.Context, integrationID string) error {
	stored, err := c.settings.ListByIntegration(ctx, integrationID)
	if err != nil {
		return err
	}
	ports, err := subnetDNSPortsFor(ctx, c.providers, integrationID)
	switch {
	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		for _, setting := range stored {
			if err := c.settings.Delete(ctx, integrationID, setting.SubnetID); err != nil {
				return err
			}
		}
		return nil
	case notAProvisionerSetting(err):
		return nil
	case err != nil:
		return err
	}
	subnets, err := ports.lister.ListNetworkSubnets(ctx)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	now := c.now()
	for _, subnet := range subnets {
		present[subnet.ID] = true
		adopted, err := c.settings.Adopt(ctx, AdoptedSubnetDNS(integrationID, subnet, now))
		if err != nil {
			return err
		}
		if adopted {
			c.record(ctx, integrationID, subnet.ID, 1, convergence.EventAdopted, "The subnet's DNS servers became the spec.")
		}
	}
	for _, setting := range stored {
		if !present[setting.SubnetID] {
			if err := c.settings.Delete(ctx, integrationID, setting.SubnetID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *SubnetDNSController) reconcileSubnet(ctx context.Context, integrationID, subnetID string, trigger convergence.Trigger) error {
	setting, err := c.settings.Get(ctx, integrationID, subnetID)
	if errors.Is(err, provisioningdomain.ErrSettingNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	ports, err := subnetDNSPortsFor(ctx, c.providers, integrationID)
	if errors.Is(err, sitedomain.ErrIntegrationNotFound) || notAProvisionerSetting(err) {
		return c.settings.Delete(ctx, integrationID, subnetID)
	}
	if err != nil {
		return err
	}
	now := c.now()
	subnet, err := findSubnet(ctx, ports.lister, subnetID)
	if errors.Is(err, provisioningdomain.ErrSubnetNotFound) {
		return c.settings.Delete(ctx, integrationID, subnetID)
	}
	if err != nil {
		return err
	}
	observed := subnet.DNSServers
	result := convergence.ConvergeSingleTarget(ctx, convergence.SingleTarget{
		Now: now, Trigger: trigger, Generation: setting.Generation,
		ObservedGeneration: setting.Status.ObservedGeneration, Conditions: setting.Status.Conditions,
		Drift: subnetDNSDrift(setting.Servers, observed),
		Realize: func(ctx context.Context) (string, error) {
			if err := ports.writer.SetSubnetDNSServers(ctx, subnetID, setting.Servers); err != nil {
				return "", err
			}
			after, err := findSubnet(ctx, ports.lister, subnetID)
			if err != nil {
				return "", err
			}
			observed = after.DNSServers
			return subnetDNSDrift(setting.Servers, observed), nil
		},
		Rejected: providerRejection,
	})
	for _, event := range result.Events {
		c.record(ctx, integrationID, subnetID, setting.Generation, event.Type, event.Message)
	}
	if result.Changed || trigger == convergence.TriggerResync || !slices.Equal(setting.Status.Observed, observed) {
		status := provisioningdomain.SubnetDNSStatus{
			ObservedGeneration: result.ObservedGeneration, Conditions: result.Conditions,
			Observed: append([]string(nil), observed...), ObservedAt: &now,
		}
		if err := c.settings.SetStatus(ctx, integrationID, subnetID, status); err != nil && !errors.Is(err, provisioningdomain.ErrSettingNotFound) {
			return err
		}
	}
	return result.Err
}

func (c *SubnetDNSController) record(ctx context.Context, integrationID, subnetID string, generation int64, eventType convergence.EventType, message string) {
	recordSettingEvent(ctx, c.events, c.log, "subnet-dns", integrationID+"/"+subnetID, generation, eventType, message)
}

// subnetDNSDrift describes how the subnet's DNS servers depart from the spec; order matters,
// because resolvers are tried in order.
func subnetDNSDrift(spec, observed []string) string {
	if slices.Equal(spec, observed) || (len(spec) == 0 && len(observed) == 0) {
		return ""
	}
	show := func(servers []string) string {
		if len(servers) == 0 {
			return "none"
		}
		return strings.Join(servers, ", ")
	}
	return fmt.Sprintf("The provisioner's DNS servers are %s; the spec is %s.", show(observed), show(spec))
}

// providerRejection classifies a provider error for the single-target rules: a rejection is
// permanent until the spec changes.
func providerRejection(err error) (string, bool) {
	var providerErr *provisioningdomain.ProviderError
	if errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorRejected {
		return providerErr.Detail, true
	}
	return "", false
}

// notAProvisionerSetting reports an Integration whose provider can hold no such setting: not a
// provisioner, no adapter, or a provisioner without the capability. Such an Integration has nothing
// to converge.
func notAProvisionerSetting(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, provisioningdomain.ErrIntegrationNotProvisioner) || errors.Is(err, provisioningdomain.ErrProviderKindUnsupported) {
		return true
	}
	detail, rejected := providerRejection(err)
	return rejected && strings.HasPrefix(detail, "This provisioner does not support")
}

func recordSettingEvent(ctx context.Context, events convergence.EventRecorder, log *slog.Logger, kind, id string, generation int64, eventType convergence.EventType, message string) {
	if events == nil {
		return
	}
	err := events.Record(ctx, convergence.Event{
		ResourceKind: kind, ResourceID: id, Type: eventType, Message: message, Generation: generation, At: time.Now().UTC(),
	})
	if err != nil {
		log.Warn("convergence event not recorded", "kind", kind, "id", id, "type", eventType, "error", err)
	}
}
