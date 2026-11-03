package application

import (
	"context"
	"errors"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
)

// siteGroup is the Servers of one Site, in the caller's order.
type siteGroup struct {
	siteID    string
	serverIDs []string
}

// siteQuery is a group whose Site has a Prometheus to ask.
type siteQuery struct {
	siteGroup
	querier monitoringdomain.MetricsQuerier
}

// groupBySite splits serverIDs by the Site each belongs to, keeping the caller's order within a
// group and ordering groups by first appearance. A Server the resolver does not know is left out:
// without a Site there is no Prometheus to ask.
func groupBySite(ctx context.Context, sites monitoringdomain.ServerSiteResolver, serverIDs []string) ([]siteGroup, error) {
	siteOf, err := sites.SitesOf(ctx, serverIDs)
	if err != nil {
		return nil, err
	}
	index := map[string]int{}
	var groups []siteGroup
	for _, serverID := range serverIDs {
		siteID, ok := siteOf[serverID]
		if !ok {
			continue
		}
		at, seen := index[siteID]
		if !seen {
			at = len(groups)
			index[siteID] = at
			groups = append(groups, siteGroup{siteID: siteID})
		}
		groups[at].serverIDs = append(groups[at].serverIDs, serverID)
	}
	return groups, nil
}

// siteQueriers resolves each group's Prometheus, skipping a Site that has none. It returns
// ErrNoMetricsIntegration only when no group has one, so a partly monitored fleet still answers for
// the monitored part.
func siteQueriers(ctx context.Context, factory monitoringdomain.MonitoringFactory, groups []siteGroup) ([]siteQuery, error) {
	queries := make([]siteQuery, 0, len(groups))
	for _, group := range groups {
		querier, err := factory.Querier(ctx, group.siteID)
		if errors.Is(err, monitoringdomain.ErrNoMetricsIntegration) {
			continue
		}
		if err != nil {
			return nil, err
		}
		queries = append(queries, siteQuery{siteGroup: group, querier: querier})
	}
	if len(queries) == 0 {
		return nil, monitoringdomain.ErrNoMetricsIntegration
	}
	return queries, nil
}
