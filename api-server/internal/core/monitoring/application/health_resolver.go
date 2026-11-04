// Package application turns metric samples back into statements about servers.
package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// HealthTargets reports which Servers the exporter target list gives the Site metrics store a
// target of an exporter for (discovery-prometheus.md, Exporter Targets). TargetedServers returns the
// IDs of those among servers, taking the Servers as given; it must apply exactly the rule the target
// list applies, so health is read from a sample only where swallow told the store to scrape. Only a
// failed read of swallow's records is an error.
type HealthTargets interface {
	TargetedServers(ctx context.Context, exporter string, servers []*serverdomain.Server) (map[string]bool, error)
}

// HealthCoverage reports which Servers swallow's Node Exporter policy covers: the Servers swallow
// keeps the health exporter installed on (software-deployment.md, Node Exporter). CoveredServers
// returns the IDs of those among servers; only a failed read of swallow's records is an error.
type HealthCoverage interface {
	CoveredServers(ctx context.Context, servers []*serverdomain.Server) (map[string]bool, error)
}

// HealthResolver classifies the health axis (servers-list.md, Health). It implements the server
// context's HealthResolver port, which is why the server context needs no knowledge of the metrics
// store or of the monitoring policy; the composition root supplies the target list and the
// coverage policy from the contexts that own them.
//
// The classification is swallow's monitoring policy, in order: a Server the target list gives a
// health exporter target is up or down by its Site metrics store's latest up sample, and unknown
// without one; a Server the Node Exporter policy covers but that has no target is unknown; any
// other Server is not_applicable. It is safe for concurrent use.
type HealthResolver struct {
	factory  monitoringdomain.MonitoringFactory
	targets  HealthTargets
	coverage HealthCoverage
}

// NewHealthResolver builds the classification over each Site's metrics store, the exporter target
// list, and the Node Exporter policy. Every dependency is required.
func NewHealthResolver(factory monitoringdomain.MonitoringFactory, targets HealthTargets, coverage HealthCoverage) *HealthResolver {
	return &HealthResolver{factory: factory, targets: targets, coverage: coverage}
}

// ResolveHealth returns the classified health of every Server in servers, keyed by Server ID.
//
// The metrics store is read best-effort: a Site with no metrics Integration, a querier that cannot
// be built, or a read that fails leaves that Site's targets unknown rather than failing the caller
// or making a Server look down. Only the target list and the coverage policy, which read swallow's
// own records, return an error. The store is read once per Site that has targets, never per Server.
func (r *HealthResolver) ResolveHealth(ctx context.Context, servers []*serverdomain.Server) (map[string]serverdomain.HealthStatus, error) {
	if len(servers) == 0 {
		return nil, nil
	}
	targeted, err := r.targets.TargetedServers(ctx, monitoringdomain.HealthExporter, servers)
	if err != nil {
		return nil, fmt.Errorf("list the health exporter's targets: %w", err)
	}
	var scraped, others []*serverdomain.Server
	for _, server := range servers {
		if targeted[server.ID] {
			scraped = append(scraped, server)
		} else {
			others = append(others, server)
		}
	}
	covered, err := r.coverage.CoveredServers(ctx, others)
	if err != nil {
		return nil, fmt.Errorf("apply the node exporter policy: %w", err)
	}
	observed := r.latestUp(ctx, scraped)

	health := make(map[string]serverdomain.HealthStatus, len(servers))
	for _, server := range servers {
		switch {
		case targeted[server.ID]:
			status, ok := observed[server.ID]
			if !ok {
				status = serverdomain.HealthStatus{State: serverdomain.HealthUnknown}
			}
			health[server.ID] = status
		case covered[server.ID]:
			health[server.ID] = serverdomain.HealthStatus{State: serverdomain.HealthUnknown}
		default:
			health[server.ID] = serverdomain.HealthStatus{State: serverdomain.HealthNotApplicable}
		}
	}
	return health, nil
}

// latestUp reads the health exporter's latest up sample of each target from its own Site's metrics
// store and returns up or down by Server ID; a Server with no sample is left out. A Site that
// cannot be read is skipped, so its targets stay unknown: the failure is logged at debug level
// only, because health is read on every Server list and a store outage would otherwise log on each
// request. An identifier outside the safe character set is never put in a query; its Site is
// skipped the same way.
func (r *HealthResolver) latestUp(ctx context.Context, targets []*serverdomain.Server) map[string]serverdomain.HealthStatus {
	observed := map[string]serverdomain.HealthStatus{}
	for _, group := range groupServersBySite(targets) {
		// A Server without a Site has no metrics store to ask.
		if group.siteID == "" {
			continue
		}
		samples, err := r.siteUp(ctx, group)
		if err != nil {
			slog.DebugContext(ctx, "server health: cannot read the site's metrics store; its targets are unknown",
				"siteId", group.siteID, "error", err)
			continue
		}
		members := make(map[string]bool, len(group.serverIDs))
		for _, id := range group.serverIDs {
			members[id] = true
		}
		for _, sample := range samples {
			serverID := sample.ServerID()
			if !members[serverID] {
				continue
			}
			// A second series of one target cannot make an answered Server down: any successful
			// scrape is up.
			if previous, seen := observed[serverID]; seen && previous.State == serverdomain.HealthUp {
				continue
			}
			state := serverdomain.HealthDown
			if sample.Value > 0 {
				state = serverdomain.HealthUp
			}
			observedAt := sample.Timestamp
			observed[serverID] = serverdomain.HealthStatus{State: state, ObservedAt: &observedAt}
		}
	}
	return observed
}

// siteUp reads one Site's up samples of the health exporter for the group. A Site without a metrics
// Integration answers nothing with no error.
func (r *HealthResolver) siteUp(ctx context.Context, group siteGroup) ([]monitoringdomain.Sample, error) {
	querier, err := r.factory.Querier(ctx, group.siteID)
	if errors.Is(err, monitoringdomain.ErrNoMetricsIntegration) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	set, err := monitoringdomain.NewServerSet(group.serverIDs)
	if err != nil {
		return nil, err
	}
	return querier.ExporterUp(ctx, monitoringdomain.HealthExporter, set)
}

// groupServersBySite splits servers by their Site, keeping the caller's order within a group and
// ordering groups by first appearance.
func groupServersBySite(servers []*serverdomain.Server) []siteGroup {
	index := map[string]int{}
	var groups []siteGroup
	for _, server := range servers {
		siteID := server.Source.SiteID
		at, seen := index[siteID]
		if !seen {
			at = len(groups)
			index[siteID] = at
			groups = append(groups, siteGroup{siteID: siteID})
		}
		groups[at].serverIDs = append(groups[at].serverIDs, server.ID)
	}
	return groups
}
