// Package application turns metric samples back into statements about servers.
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	monitoringdomain "github.com/maple52046/swallow/internal/monitoring/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// HealthResolver fills in the health axis from the metrics store.
//
// It implements the server context's HealthResolver port, which is why the server
// context needs no knowledge of Prometheus.
type HealthResolver struct {
	factory monitoringdomain.MonitoringFactory
}

func NewHealthResolver(factory monitoringdomain.MonitoringFactory) *HealthResolver {
	return &HealthResolver{factory: factory}
}

// ResolveHealth returns health by server ID.
//
// Servers with no answer are omitted rather than reported down: a server that is not
// being scraped is unknown, and reporting it as down would be a claim nobody made.
// A missing or unreachable metrics store is likewise not an error — it leaves every
// health axis unknown.
func (r *HealthResolver) ResolveHealth(
	ctx context.Context,
	serverIDs []string,
) (map[string]*serverdomain.HealthStatus, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}

	querier, err := r.factory.Querier(ctx, "")
	if errors.Is(err, monitoringdomain.ErrNoMetricsIntegration) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	pattern, err := labelPattern(serverIDs)
	if err != nil {
		return nil, err
	}

	// max() over the matcher because a server has several scrape jobs — node_exporter
	// and possibly dcgm-exporter — and being reachable on any of them means it is up.
	expr := fmt.Sprintf(`max by (%s) (up{%s=~"%s"})`,
		monitoringdomain.LabelServerID, monitoringdomain.LabelServerID, pattern)

	samples, err := querier.Query(ctx, expr)
	if err != nil {
		return nil, err
	}

	health := make(map[string]*serverdomain.HealthStatus, len(samples))
	for _, sample := range samples {
		serverID := sample.ServerID()
		if serverID == "" {
			continue
		}

		state := serverdomain.HealthDown
		if sample.Value > 0 {
			state = serverdomain.HealthUp
		}
		health[serverID] = &serverdomain.HealthStatus{
			State:      state,
			ObservedAt: sample.Timestamp,
		}
	}
	return health, nil
}

// labelPattern builds a PromQL regex alternation from server IDs.
//
// Identifiers are validated rather than escaped: they are swallow-issued UUIDs, so
// anything outside this character set means the caller is passing something that did
// not come from swallow, and interpolating it into a query would be the wrong response.
func labelPattern(serverIDs []string) (string, error) {
	for _, id := range serverIDs {
		if id == "" {
			return "", fmt.Errorf("%w: empty", monitoringdomain.ErrInvalidServerID)
		}
		for _, r := range id {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			default:
				return "", fmt.Errorf("%w: %q", monitoringdomain.ErrInvalidServerID, id)
			}
		}
	}
	return strings.Join(serverIDs, "|"), nil
}
