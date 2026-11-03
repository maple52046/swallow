// Package application turns metric samples back into statements about servers.
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// HealthResolver fills in the health axis from the metrics store.
//
// It implements the server context's HealthResolver port, which is why the server
// context needs no knowledge of Prometheus.
type HealthResolver struct {
	factory monitoringdomain.MonitoringFactory
	sites   monitoringdomain.ServerSiteResolver
}

// NewHealthResolver reads each Server's health from its Site's Prometheus.
func NewHealthResolver(factory monitoringdomain.MonitoringFactory, sites monitoringdomain.ServerSiteResolver) *HealthResolver {
	return &HealthResolver{factory: factory, sites: sites}
}

// ResolveHealth returns health by server ID.
//
// Health is the Site Prometheus's `up` for the Server's Node Exporter only. Servers with no
// answer are omitted rather than reported down: a server that is not being scraped (no Node
// Exporter swallow knows of, or a Site without a Prometheus) is unknown, and reporting it as down
// would be a claim nobody made. A missing metrics store is likewise not an error.
func (r *HealthResolver) ResolveHealth(
	ctx context.Context,
	serverIDs []string,
) (map[string]*serverdomain.HealthStatus, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}

	if _, err := labelPattern(serverIDs); err != nil {
		return nil, err
	}
	groups, err := groupBySite(ctx, r.sites, serverIDs)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, nil
	}
	queries, err := siteQueriers(ctx, r.factory, groups)
	if errors.Is(err, monitoringdomain.ErrNoMetricsIntegration) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	health := make(map[string]*serverdomain.HealthStatus, len(serverIDs))
	for _, query := range queries {
		pattern, err := labelPattern(query.serverIDs)
		if err != nil {
			return nil, err
		}
		expr := fmt.Sprintf(`max by (%s) (up{%s=%q, %s=~"%s"})`,
			monitoringdomain.LabelServerID, monitoringdomain.LabelExporter, monitoringdomain.HealthExporter,
			monitoringdomain.LabelServerID, pattern)
		samples, err := query.querier.Query(ctx, expr)
		if err != nil {
			return nil, err
		}
		for _, sample := range samples {
			serverID := sample.ServerID()
			if serverID == "" {
				continue
			}
			state := serverdomain.HealthDown
			if sample.Value > 0 {
				state = serverdomain.HealthUp
			}
			health[serverID] = &serverdomain.HealthStatus{State: state, ObservedAt: sample.Timestamp}
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
