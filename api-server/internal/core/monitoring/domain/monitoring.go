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
	// LabelPlatformLegacy and LabelClusterLegacy are one-release read aliases.
	LabelPlatformLegacy = "platform"
	LabelClusterLegacy  = "cluster"
	// LabelKubernetesNode is what in-platform metrics carry instead of a server_id,
	// because swallow does not own a platform's scrape configuration.
	LabelKubernetesNode = "node"
)

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

// MetricsQuerier reads from the central metrics store.
//
// Deliberately narrow: swallow exposes named, parameterised queries rather than a PromQL
// passthrough, so the set of expressions that can reach the store is bounded by this
// codebase rather than by a client.
//
// Implementations drop non-finite values (NaN, ±Inf) instead of returning them: a division by
// zero in the store means "no meaningful value", and those values cannot cross JSON anyway.
// Failures are reported as *QueryError so the delivery layer can tell an unreachable store
// from a rejected expression.
type MetricsQuerier interface {
	Name() string
	// Query evaluates a PromQL expression at the current instant.
	Query(ctx context.Context, expr string) ([]Sample, error)
	// QueryRange evaluates a PromQL expression at every step of the range. The caller is
	// responsible for bounding the range and step; the querier forwards them as given.
	QueryRange(ctx context.Context, expr string, window TimeRange) ([]Series, error)
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
	// Querier returns a metrics querier for the site, or ErrNoMetricsIntegration.
	// An empty siteID means any site's metrics integration will do, which is correct
	// while an installation has one central store.
	Querier(ctx context.Context, siteID string) (MetricsQuerier, error)
	// Alerts returns an alert source for the site, or ErrNoAlertSource.
	Alerts(ctx context.Context, siteID string) (AlertSource, error)
}

var (
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
