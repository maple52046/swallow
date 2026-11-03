// Package domain defines the monitoring ports.
//
// swallow stores no metrics and no alerts. It queries the metrics store and reads alerts
// from Alertmanager, and its only contribution is correlation: turning a label set back
// into the server, site, and platform it belongs to.
//
// See docs/decisions/003-metrics-label-contract.md.
package domain

import (
	"context"
	"errors"
	"time"
)

// Canonical label names from the metrics label contract. These are the only join keys
// between a metric and a server.
const (
	LabelServerID = "server_id"
	LabelSite     = "site"
	LabelPlatform = "platform_id"
	// LabelPlatformLegacy is a read alias of LabelPlatform.
	LabelPlatformLegacy = "platform"
	// LabelExporter names the exporter a series was scraped from.
	LabelExporter = "exporter"
	// HealthExporter is the basic tier's exporter (decision 003): a Server's health is whether the
	// Site Prometheus can scrape it, and no other exporter counts.
	HealthExporter = "node-exporter"
)

// PlatformTokenStore keeps the hash of each Platform's monitoring token, the credential its Grafana
// reads the Site Prometheus with through swallow's gateway (decision 003). The token itself is
// never stored; a Platform has at most one, and saving replaces it.
type PlatformTokenStore interface {
	SaveToken(ctx context.Context, platformID string, hash []byte, issuedAt time.Time) error
	// TokenHash returns ErrPlatformTokenNotFound when the Platform has no token.
	TokenHash(ctx context.Context, platformID string) ([]byte, error)
}

// PlatformMetricsUpstream finds where a Platform's metrics are read: the label proxy in front of
// its Site's Prometheus, which confines every query to the Platform's own series. It returns
// ErrScopeNotFound when the Platform does not exist and ErrNoMetricsIntegration when its Site has
// no Prometheus or no label proxy.
type PlatformMetricsUpstream interface {
	LabelProxyURL(ctx context.Context, platformID string) (string, error)
}

// ServerSiteResolver tells which Site each Server belongs to, so a read is sent to that Site's
// Prometheus. Implementations omit an id they do not know.
type ServerSiteResolver interface {
	SitesOf(ctx context.Context, serverIDs []string) (map[string]string, error)
}

// Sample is one instant-query result.
type Sample struct {
	Labels    map[string]string
	Value     float64
	Timestamp time.Time
}

// ServerID returns the server this sample belongs to, or "" when it carries no
// server_id — which is the case for metrics produced inside a platform.
func (s Sample) ServerID() string {
	return s.Labels[LabelServerID]
}

// Point is one value of a time series at one evaluation timestamp.
type Point struct {
	Timestamp time.Time
	Value     float64
}

// Series is one range-query result: a label set and its points in ascending time order.
//
// Points are only the timestamps the store had a value for. A missing timestamp is a gap —
// the host was down or not yet scraped — and must never be filled in as zero by a caller.
type Series struct {
	Labels map[string]string
	Points []Point
}

// ServerID returns the server this series belongs to, or "" for an aggregate across servers
// or a series produced inside a platform.
func (s Series) ServerID() string {
	return s.Labels[LabelServerID]
}

// TimeRange is the evaluation window of a range query. Step is the spacing between
// evaluated timestamps; Start and End are inclusive bounds the store aligns points to.
type TimeRange struct {
	Start time.Time
	End   time.Time
	Step  time.Duration
}

// MetricsQuerier evaluates swallow's named metrics against one Site's metrics store.
//
// Deliberately narrow: swallow exposes named, parameterised reads rather than a query
// passthrough, so what can reach the store is bounded by the adapter that implements this port,
// never by a client. The core names the metric, the Servers, and whether to combine them; the
// expression that computes a metric, and the exporter series it reads, are the adapter's.
//
// Per-Server results carry LabelServerID so the caller can attribute them; a Server the store has
// no value for has no result. Implementations drop non-finite values (NaN, ±Inf) instead of
// returning them: a division by zero in the store means "no meaningful value", and those values
// cannot cross JSON anyway. An empty ServerSet is answered with no results and no store request.
// Store failures are reported as *QueryError so the delivery layer can tell an unreachable store
// from a rejected expression; a metric the adapter cannot compute is an ordinary error, because
// NamedMetrics and the adapter are kept in step by the adapter's tests.
type MetricsQuerier interface {
	Name() string
	// Current evaluates metric for each Server at the current instant.
	Current(ctx context.Context, metric Metric, servers ServerSet) ([]Sample, error)
	// History evaluates metric at every step of window: one series per Server, or, when combined
	// is true, at most one series without a server_id that combines the Servers with
	// metric.Aggregate. The caller bounds the window and step; the querier forwards them as given.
	History(ctx context.Context, metric Metric, servers ServerSet, window TimeRange, combined bool) ([]Series, error)
	// ExporterUp reports, per Server, whether the store's latest scrape of the named exporter on it
	// succeeded: a value above zero is up, zero is down. A Server with no target for that exporter
	// has no sample, which is unknown rather than down.
	ExporterUp(ctx context.Context, exporter string, servers ServerSet) ([]Sample, error)
	// ExporterUpWithin reports, per Server, whether any scrape of the named exporter on it in the
	// window ending now succeeded: a value above zero means at least one did, zero means every
	// scrape in the window failed. A Server the store did not scrape in the window has no sample,
	// which is unknown. window must be at least one second.
	ExporterUpWithin(ctx context.Context, exporter string, servers ServerSet, window time.Duration) ([]Sample, error)
}

// ScopeKind names what a metrics scope groups Servers by.
type ScopeKind string

const (
	// ScopeServers is an explicit list of Servers chosen by the caller.
	ScopeServers ScopeKind = "servers"
	// ScopePool is the Servers currently placed in one Pool.
	ScopePool ScopeKind = "pool"
	// ScopeZone is the Servers currently placed in one Zone.
	ScopeZone ScopeKind = "zone"
	// ScopePlatform is the Servers whose membership axis names one Platform.
	ScopePlatform ScopeKind = "platform"
)

// ServerScopeResolver turns a grouping into the Servers it contains right now.
//
// It is the monitoring context's only view of Pools, Zones, and Platforms: grouping is owned
// by other contexts, and metrics carry only server_id (decision 003 forbids grouping labels on
// series), so a grouping is joined to series at query time through this port. Implementations
// must:
//
//   - return the ids of non-absent Servers only, in a stable order;
//   - return ErrScopeNotFound when the grouping id does not exist, so a typo is a 404 rather
//     than an empty chart;
//   - never be asked for ScopeServers, which needs no resolution.
type ServerScopeResolver interface {
	ResolveScope(ctx context.Context, kind ScopeKind, id string) ([]string, error)
}

// AlertState is Alertmanager's view of an alert.
type AlertState string

const (
	AlertStateFiring AlertState = "firing"
	// AlertStateSuppressed means a silence or an inhibition rule is hiding it. This
	// is what "acknowledged" looks like: it is a fact in Alertmanager, not a field
	// swallow sets.
	AlertStateSuppressed AlertState = "suppressed"
	AlertStateUnknown    AlertState = "unknown"
)

// Alert is an alert as Alertmanager reports it, plus swallow's correlation.
type Alert struct {
	// Fingerprint is Alertmanager's identifier. Opaque.
	Fingerprint string
	Name        string
	Severity    string
	State       AlertState
	Summary     string
	Description string
	Labels      map[string]string
	StartsAt    time.Time

	// Correlation, resolved by swallow from the label set. Empty when the alert's labels
	// do not identify one.
	ServerID   string
	SiteID     string
	PlatformID string
}

// SilenceRequest suppresses an alert. Silencing is how an alert is acknowledged: the
// state lives in Alertmanager, so that swallow and the alerting pipeline cannot disagree
// about whether something was dealt with.
type SilenceRequest struct {
	// Matchers select what to silence, as exact label equality.
	Matchers  map[string]string
	Duration  time.Duration
	CreatedBy string
	Comment   string
}

// AlertSource reads and suppresses alerts.
type AlertSource interface {
	ListAlerts(ctx context.Context) ([]*Alert, error)
	Silence(ctx context.Context, req SilenceRequest) (silenceID string, err error)
}

// MonitoringFactory resolves a registered integration into monitoring clients.
type MonitoringFactory interface {
	// Querier returns a querier of the Site's Prometheus (its metrics Integration), or
	// ErrNoMetricsIntegration when the Site has none. An empty siteID matches any Site's.
	Querier(ctx context.Context, siteID string) (MetricsQuerier, error)
	// Alerts returns an alert source for the site, or ErrNoAlertSource.
	Alerts(ctx context.Context, siteID string) (AlertSource, error)
}

var (
	// ErrPlatformTokenNotFound means the Platform has no monitoring token.
	ErrPlatformTokenNotFound = errors.New("platform monitoring token not found")
	// ErrNoMetricsIntegration means no metrics store is registered, so health and
	// metrics are simply unknown rather than failed.
	ErrNoMetricsIntegration = errors.New("no metrics integration registered")
	// ErrNoAlertSource means the metrics integration has no Alertmanager address
	// configured.
	ErrNoAlertSource = errors.New("no alert source configured")
	// ErrInvalidServerID means a server identifier contained characters that cannot
	// safely be interpolated into a query.
	ErrInvalidServerID = errors.New("invalid server identifier")
	// ErrScopeNotFound means the Pool, Zone, or Platform a metrics scope names does not
	// exist. It is a client error (404), not an empty scope.
	ErrScopeNotFound = errors.New("metrics scope not found")
)

// QueryErrorKind classifies a metrics failure for the delivery layer.
type QueryErrorKind string

const (
	QueryErrorUnavailable QueryErrorKind = "unavailable"
	QueryErrorAuth        QueryErrorKind = "auth"
	QueryErrorRejected    QueryErrorKind = "rejected"
)

// QueryError is a failure reported by, or while reaching, the monitoring stack.
type QueryError struct {
	Kind   QueryErrorKind
	Detail string
	Err    error
}

func (e *QueryError) Error() string {
	if e.Detail != "" {
		return string(e.Kind) + ": " + e.Detail
	}
	return string(e.Kind)
}

func (e *QueryError) Unwrap() error { return e.Err }
