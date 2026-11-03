package domain

import "fmt"

// Named metric names (server-metrics.md, "Metric Names"). They are API values: a client asks for
// them by name, so a name never changes meaning or unit once published.
const (
	MetricCPUUsagePercent               = "cpuUsagePercent"
	MetricMemoryUsedPercent             = "memoryUsedPercent"
	MetricDiskUsedPercent               = "diskUsedPercent"
	MetricNetworkReceiveBytesPerSecond  = "networkReceiveBytesPerSecond"
	MetricNetworkTransmitBytesPerSecond = "networkTransmitBytesPerSecond"
)

// ScopeAggregate is how a metric combines several Servers into one series when a scope is charted
// as a whole (server-metrics.md, "Scope aggregate"). It belongs to the metric, not to the request,
// so a client cannot choose an aggregate that misrepresents what the metric measures. At each
// timestamp only the Servers that had a value take part; a Server without data is not a zero.
type ScopeAggregate string

const (
	// ScopeAverage combines utilisation and fill levels.
	ScopeAverage ScopeAggregate = "average"
	// ScopeSum combines quantities that add up across hosts, such as throughput.
	ScopeSum ScopeAggregate = "sum"
	// ScopeMaximum keeps the highest Server's value, for quantities where the worst host is the
	// answer, such as a temperature.
	ScopeMaximum ScopeAggregate = "maximum"
)

// Metric is one named metric swallow evaluates: the name a client asks for and how a scope of
// Servers combines into one series. How a metrics store computes it is the adapter's knowledge;
// the core never holds a store's query language or an exporter's series names.
type Metric struct {
	Name      string
	Aggregate ScopeAggregate
}

// NamedMetrics returns the complete set of metrics a client may ask for, in user scan order (the
// order the names endpoint reports). It is basic monitoring only: every metric comes from the
// basic tier's exporter and applies to every Server. The slice is a fresh copy on every call.
//
// A fixed set rather than a query passthrough (decision 003): an arbitrary expression through an
// authenticated API makes swallow responsible for query cost it cannot predict.
func NamedMetrics() []Metric {
	return []Metric{
		{Name: MetricCPUUsagePercent, Aggregate: ScopeAverage},
		{Name: MetricMemoryUsedPercent, Aggregate: ScopeAverage},
		{Name: MetricDiskUsedPercent, Aggregate: ScopeAverage},
		{Name: MetricNetworkReceiveBytesPerSecond, Aggregate: ScopeSum},
		{Name: MetricNetworkTransmitBytesPerSecond, Aggregate: ScopeSum},
	}
}

// ServerSet is the Servers one metrics read is about, in the caller's order, each id already
// checked to be safe inside a query's label matcher.
//
// Ids are validated rather than escaped: swallow issues them as UUIDs, so a character outside
// letters, digits, '-', and '_' means the caller passed something that did not come from swallow,
// and interpolating it into a query would be the wrong response. The ids are reachable only
// through NewServerSet, so an adapter may place them in a matcher as they are. The zero value is
// an empty set, which matches no Server.
type ServerSet struct {
	ids []string
}

// NewServerSet checks every id and keeps the caller's order. An empty or unsafe id is
// ErrInvalidServerID, which the delivery layer answers as validation_error.
func NewServerSet(serverIDs []string) (ServerSet, error) {
	for _, id := range serverIDs {
		if id == "" {
			return ServerSet{}, fmt.Errorf("%w: empty", ErrInvalidServerID)
		}
		for _, r := range id {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			default:
				return ServerSet{}, fmt.Errorf("%w: %q", ErrInvalidServerID, id)
			}
		}
	}
	return ServerSet{ids: append([]string(nil), serverIDs...)}, nil
}

// IDs returns a copy of the checked ids in the caller's order.
func (s ServerSet) IDs() []string {
	return append([]string(nil), s.ids...)
}
