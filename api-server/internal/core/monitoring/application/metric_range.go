package application

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// Bounds of a usage-history request (server-metrics.md, "Usage History"). Each one exists to
// keep a chart request from turning into an unbounded scan of the metrics store.
const (
	// maxRangeScopeServers caps how many Servers one grouping may resolve to. The server_id
	// matcher is a regex alternation, so its cost grows with the scope.
	maxRangeScopeServers = 1000
	// maxPerServerScope caps perServer mode, whose response grows with every Server.
	maxPerServerScope = 50
	// minRangeSpan and maxRangeSpan bound an explicit start/end range.
	minRangeSpan = 5 * time.Minute
	maxRangeSpan = 7 * 24 * time.Hour
	// targetRangePoints is the most points a series aims for; the step is derived from it.
	targetRangePoints = 180
	// stepGranularity is the multiple every step is rounded up to, and also the minimum step:
	// finer than the 30 s scrape interval would only repeat samples.
	stepGranularity = 30 * time.Second
)

// rangeWindows are the named windows a client may ask for, ending at the request time.
var rangeWindows = map[string]time.Duration{
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
}

// defaultRangeWindow applies when a request names neither a window nor a start/end range.
const defaultRangeWindow = "1h"

// RangeMode selects how a scope's Servers are returned.
type RangeMode string

const (
	// RangeModeAggregate combines the scope into one series per metric, using the metric's
	// scope aggregate. It is the default because a grouping is usually asked about as a whole.
	RangeModeAggregate RangeMode = "aggregate"
	// RangeModePerServer returns each Server's own series; limited to maxPerServerScope.
	RangeModePerServer RangeMode = "perServer"
)

// ErrInvalidMetricRange marks a usage-history request the contract refuses: a bad scope
// combination, window, time range, mode, or a scope too large. The delivery layer answers it
// with validation_error and the wrapped message.
var ErrInvalidMetricRange = errors.New("invalid metric range request")

// MetricRangeInput is a parsed usage-history request. The delivery layer has already split
// lists and parsed timestamps; this use case owns every semantic rule.
type MetricRangeInput struct {
	// ScopeKind and ScopeID name a grouping; ScopeKind is ScopeServers for an explicit list in
	// ServerIDs, in which case ScopeID is empty.
	ScopeKind monitoringdomain.ScopeKind
	ScopeID   string
	ServerIDs []string
	// Metrics are the requested names; empty means every name.
	Metrics []string
	// Window is a named window; Start/End an explicit range. Both empty means the default
	// window. Supplying both is refused.
	Window string
	Start  *time.Time
	End    *time.Time
	// Mode is empty for the default (aggregate).
	Mode string
}

// MetricRangeScope reports what a request's scope resolved to.
type MetricRangeScope struct {
	Kind        string  `json:"kind"`
	ID          *string `json:"id"`
	ServerCount int     `json:"serverCount"`
}

// MetricRangePoint is one wire point; Timestamp is ISO 8601 UTC per the API conventions.
type MetricRangePoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

// MetricRangeSeries is one metric's series for the whole scope (ServerID null) or one Server.
type MetricRangeSeries struct {
	Metric   string             `json:"metric"`
	ServerID *string            `json:"serverId"`
	Points   []MetricRangePoint `json:"points"`
}

// MetricRangeResult is the usage-history response body without the Grafana link, which the
// delivery layer attaches. Series holds only series with at least one point: a metric with no
// data has no series, so absence reads as "no data" rather than a flat zero line.
type MetricRangeResult struct {
	Scope       MetricRangeScope    `json:"scope"`
	Mode        string              `json:"mode"`
	Start       string              `json:"start"`
	End         string              `json:"end"`
	StepSeconds int                 `json:"stepSeconds"`
	Series      []MetricRangeSeries `json:"series"`
}

// MetricRangeService answers usage-history reads of the named metrics over a scope.
//
// It resolves a grouping to Servers through ServerScopeResolver at request time (so history is
// that of current members), bounds the range and resolution, and evaluates the same named
// expressions as the current-value read — wrapped in the metric's scope aggregate in aggregate
// mode. It stores nothing. Errors: ErrUnknownMetric, ErrInvalidMetricRange,
// monitoringdomain.ErrScopeNotFound, monitoringdomain.ErrInvalidServerID,
// monitoringdomain.ErrNoMetricsIntegration, and *monitoringdomain.QueryError from the store.
type MetricRangeService struct {
	factory monitoringdomain.MonitoringFactory
	scopes  monitoringdomain.ServerScopeResolver
	now     func() time.Time
}

// NewMetricRangeService builds the usage-history read. scopes resolves Pools, Zones, and
// Platforms; it is never consulted for an explicit Server list.
func NewMetricRangeService(
	factory monitoringdomain.MonitoringFactory,
	scopes monitoringdomain.ServerScopeResolver,
) *MetricRangeService {
	return &MetricRangeService{factory: factory, scopes: scopes, now: time.Now}
}

// WithClock replaces the clock used to end a named window. It exists for tests, which need a
// fixed "now" to assert the evaluated range; production keeps time.Now.
func (s *MetricRangeService) WithClock(now func() time.Time) *MetricRangeService {
	s.now = now
	return s
}

// Query validates the request, resolves its scope, and evaluates every requested metric over
// the bounded range. Metrics, mode, and range are validated before the scope is resolved, and
// the scope's size before the store is queried, so a refused request never costs a store query.
func (s *MetricRangeService) Query(ctx context.Context, input MetricRangeInput) (*MetricRangeResult, error) {
	queries, err := resolveMetrics(input.Metrics)
	if err != nil {
		return nil, err
	}
	mode, err := parseRangeMode(input.Mode)
	if err != nil {
		return nil, err
	}
	window, err := s.timeRange(input)
	if err != nil {
		return nil, err
	}

	serverIDs, err := s.resolveScope(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(serverIDs) > maxRangeScopeServers {
		return nil, fmt.Errorf("%w: the scope resolved to %d Servers; at most %d may be charted at once, so choose a narrower scope",
			ErrInvalidMetricRange, len(serverIDs), maxRangeScopeServers)
	}
	if mode == RangeModePerServer && len(serverIDs) > maxPerServerScope {
		return nil, fmt.Errorf("%w: perServer mode supports at most %d Servers and the scope has %d; use aggregate mode",
			ErrInvalidMetricRange, maxPerServerScope, len(serverIDs))
	}

	result := &MetricRangeResult{
		Scope: MetricRangeScope{
			Kind:        string(input.ScopeKind),
			ID:          wire.String(input.ScopeID),
			ServerCount: len(serverIDs),
		},
		Mode:        string(mode),
		Start:       wire.Time(window.Start),
		End:         wire.Time(window.End),
		StepSeconds: int(window.Step / time.Second),
		Series:      []MetricRangeSeries{},
	}
	// An empty grouping is a fact about the grouping, not about the store: answer it without
	// requiring a metrics Integration or spending a query.
	if len(serverIDs) == 0 {
		return result, nil
	}

	pattern, err := labelPattern(serverIDs)
	if err != nil {
		return nil, err
	}
	querier, err := s.factory.Querier(ctx, "")
	if err != nil {
		return nil, err
	}

	for _, query := range queries {
		expr := expandQuery(query.expr, pattern)
		if mode == RangeModeAggregate {
			expr = query.aggregate + " (" + expr + ")"
		}
		series, err := querier.QueryRange(ctx, expr, window)
		if err != nil {
			return nil, err
		}
		result.Series = append(result.Series, shapeSeries(query.name, mode, serverIDs, series)...)
	}
	return result, nil
}

// parseRangeMode defaults an empty mode to aggregate and refuses anything else unknown.
func parseRangeMode(raw string) (RangeMode, error) {
	switch RangeMode(raw) {
	case "", RangeModeAggregate:
		return RangeModeAggregate, nil
	case RangeModePerServer:
		return RangeModePerServer, nil
	default:
		return "", fmt.Errorf("%w: mode must be %q or %q", ErrInvalidMetricRange, RangeModeAggregate, RangeModePerServer)
	}
}

// timeRange turns a named window or an explicit start/end into the bounded evaluation range.
// The end of a named window is the request time truncated to the second, which is the
// precision the wire timestamps carry.
func (s *MetricRangeService) timeRange(input MetricRangeInput) (monitoringdomain.TimeRange, error) {
	explicit := input.Start != nil || input.End != nil
	if explicit && input.Window != "" {
		return monitoringdomain.TimeRange{}, fmt.Errorf("%w: send either window or start/end, not both", ErrInvalidMetricRange)
	}

	var start, end time.Time
	if explicit {
		if input.Start == nil || input.End == nil {
			return monitoringdomain.TimeRange{}, fmt.Errorf("%w: start and end must be sent together", ErrInvalidMetricRange)
		}
		start, end = input.Start.UTC(), input.End.UTC()
		span := end.Sub(start)
		if span <= 0 {
			return monitoringdomain.TimeRange{}, fmt.Errorf("%w: start must be before end", ErrInvalidMetricRange)
		}
		if span < minRangeSpan || span > maxRangeSpan {
			return monitoringdomain.TimeRange{}, fmt.Errorf("%w: the range must span between 5 minutes and 7 days", ErrInvalidMetricRange)
		}
	} else {
		name := input.Window
		if name == "" {
			name = defaultRangeWindow
		}
		span, ok := rangeWindows[name]
		if !ok {
			return monitoringdomain.TimeRange{}, fmt.Errorf("%w: window must be one of 1h, 6h, 24h, 7d", ErrInvalidMetricRange)
		}
		end = s.now().UTC().Truncate(time.Second)
		start = end.Add(-span)
	}
	return monitoringdomain.TimeRange{Start: start, End: end, Step: rangeStep(end.Sub(start))}, nil
}

// rangeStep is the contract's resolution rule: at most about targetRangePoints points, rounded
// up to a whole multiple of stepGranularity, never finer than stepGranularity.
func rangeStep(span time.Duration) time.Duration {
	units := math.Ceil(span.Seconds() / targetRangePoints / stepGranularity.Seconds())
	if units < 1 {
		units = 1
	}
	return time.Duration(units) * stepGranularity
}

// resolveScope returns the Servers a request covers: the explicit list as given, or the
// grouping's current members from the resolver.
func (s *MetricRangeService) resolveScope(ctx context.Context, input MetricRangeInput) ([]string, error) {
	switch input.ScopeKind {
	case monitoringdomain.ScopeServers:
		if len(input.ServerIDs) == 0 {
			return nil, fmt.Errorf("%w: serverIds is empty", ErrInvalidMetricRange)
		}
		return input.ServerIDs, nil
	case monitoringdomain.ScopePool, monitoringdomain.ScopeZone, monitoringdomain.ScopePlatform:
		if input.ScopeID == "" {
			return nil, fmt.Errorf("%w: the %s id is empty", ErrInvalidMetricRange, input.ScopeKind)
		}
		return s.scopes.ResolveScope(ctx, input.ScopeKind, input.ScopeID)
	default:
		return nil, fmt.Errorf("%w: exactly one of serverIds, poolId, zoneId, or platformId is required", ErrInvalidMetricRange)
	}
}

// shapeSeries maps store series onto wire series for one metric. In aggregate mode the store
// answers at most one unlabelled series; in perServer mode each series is attributed by its
// server_id and ordered like the scope, and a series without one is dropped because it cannot
// be attributed. Empty series are dropped so absence keeps meaning "no data".
func shapeSeries(
	metric string,
	mode RangeMode,
	scope []string,
	series []monitoringdomain.Series,
) []MetricRangeSeries {
	if mode == RangeModeAggregate {
		shaped := make([]MetricRangeSeries, 0, 1)
		for _, item := range series {
			if len(item.Points) == 0 {
				continue
			}
			shaped = append(shaped, MetricRangeSeries{Metric: metric, Points: toWirePoints(item.Points)})
		}
		return shaped
	}

	byServer := make(map[string]monitoringdomain.Series, len(series))
	for _, item := range series {
		if serverID := item.ServerID(); serverID != "" && len(item.Points) > 0 {
			byServer[serverID] = item
		}
	}
	shaped := make([]MetricRangeSeries, 0, len(byServer))
	for _, serverID := range scope {
		item, ok := byServer[serverID]
		if !ok {
			continue
		}
		shaped = append(shaped, MetricRangeSeries{
			Metric:   metric,
			ServerID: wire.String(serverID),
			Points:   toWirePoints(item.Points),
		})
	}
	return shaped
}

// toWirePoints formats store points as contract points.
func toWirePoints(points []monitoringdomain.Point) []MetricRangePoint {
	wirePoints := make([]MetricRangePoint, len(points))
	for i, point := range points {
		wirePoints[i] = MetricRangePoint{Timestamp: wire.Time(point.Timestamp), Value: point.Value}
	}
	return wirePoints
}

// GrafanaLink returns the optional Grafana link attached to the usage-history response; the
// charts a client draws from the series never depend on it.
func (s *MetricRangeService) GrafanaLink(ctx context.Context) *string {
	return grafanaLink(ctx, s.factory)
}
