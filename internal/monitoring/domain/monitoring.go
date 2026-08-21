// Package domain defines the monitoring ports.
//
// gdcm stores no metrics and no alerts. It queries the metrics store and reads alerts
// from Alertmanager, and its only contribution is correlation: turning a label set back
// into the server, site, and cluster it belongs to.
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
	LabelCluster  = "cluster"
	// LabelKubernetesNode is what in-cluster metrics carry instead of a server_id,
	// because gdcm does not own a cluster's scrape configuration.
	LabelKubernetesNode = "node"
)

// Sample is one instant-query result.
type Sample struct {
	Labels    map[string]string
	Value     float64
	Timestamp time.Time
}

// ServerID returns the server this sample belongs to, or "" when it carries no
// server_id — which is the case for metrics produced inside a cluster.
func (s Sample) ServerID() string {
	return s.Labels[LabelServerID]
}

// MetricsQuerier reads from the central metrics store.
//
// Deliberately narrow: gdcm exposes named, parameterised queries rather than a PromQL
// passthrough, so the set of expressions that can reach the store is bounded by this
// codebase rather than by a client.
type MetricsQuerier interface {
	Name() string
	// Query evaluates a PromQL expression at the current instant.
	Query(ctx context.Context, expr string) ([]Sample, error)
}

// AlertState is Alertmanager's view of an alert.
type AlertState string

const (
	AlertStateFiring AlertState = "firing"
	// AlertStateSuppressed means a silence or an inhibition rule is hiding it. This
	// is what "acknowledged" looks like: it is a fact in Alertmanager, not a field
	// gdcm sets.
	AlertStateSuppressed AlertState = "suppressed"
	AlertStateUnknown    AlertState = "unknown"
)

// Alert is an alert as Alertmanager reports it, plus gdcm's correlation.
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

	// Correlation, resolved by gdcm from the label set. Empty when the alert's labels
	// do not identify one.
	ServerID  string
	SiteID    string
	ClusterID string
}

// SilenceRequest suppresses an alert. Silencing is how an alert is acknowledged: the
// state lives in Alertmanager, so that gdcm and the alerting pipeline cannot disagree
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
	// while a deployment has one central store.
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
