package application

import (
	"context"
	"fmt"
	"sort"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// Labels every exporter target carries (decision 003). platform_id is present only when the Server
// has a Platform Claim.
const (
	LabelServerID   = "server_id"
	LabelSite       = "site"
	LabelPlatformID = "platform_id"
	LabelExporter   = "exporter"
)

// InstalledExporter is one metrics exporter swallow knows runs on a Server: Name is the
// exporter's declared name and Port where it listens on the Server's primary address.
type InstalledExporter struct {
	ServerID string
	Name     string
	Port     int
}

// ExporterInventory lists the exporters swallow knows are installed, in any Site. An
// implementation reports an exporter only while its installation is recorded as done (an
// installed Software Assignment, or a Platform feature that is ready), so a Server without an
// exporter is never a scrape target and its health stays unknown rather than down.
type ExporterInventory interface {
	InstalledExporters(ctx context.Context) ([]InstalledExporter, error)
}

// ExporterDiscovery serves the Site metrics store its scrape targets: every installed exporter of the
// Site's present Servers, labelled with the contract so each series joins back to its Server and
// Platform.
type ExporterDiscovery struct {
	servers   serverdomain.ServerRepository
	exporters ExporterInventory
}

// NewExporterDiscovery builds the exporter target list over the Server projection and the
// exporters swallow knows are installed.
func NewExporterDiscovery(servers serverdomain.ServerRepository, exporters ExporterInventory) *ExporterDiscovery {
	return &ExporterDiscovery{servers: servers, exporters: exporters}
}

// Targets returns one http_sd target group per installed exporter of the Site's present Servers,
// ordered by Server and exporter; an empty siteID means every Site. A Server without a primary
// address is skipped, since a target with no address only produces a failing series.
func (d *ExporterDiscovery) Targets(ctx context.Context, siteID string) ([]ScrapeTarget, error) {
	installed, err := d.exporters.InstalledExporters(ctx)
	if err != nil {
		return nil, err
	}
	result, err := d.servers.List(ctx, serverdomain.ListFilter{SiteID: siteID, Limit: 0})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*serverdomain.Server, len(result.Servers))
	for _, server := range result.Servers {
		byID[server.ID] = server
	}

	sort.Slice(installed, func(i, j int) bool {
		if installed[i].ServerID != installed[j].ServerID {
			return installed[i].ServerID < installed[j].ServerID
		}
		return installed[i].Name < installed[j].Name
	})
	targets := make([]ScrapeTarget, 0, len(installed))
	for _, exporter := range installed {
		server := byID[exporter.ServerID]
		if server == nil || server.Absent {
			continue
		}
		address := server.PrimaryAddress()
		if address == "" {
			continue
		}
		labels := map[string]string{
			LabelServerID: server.ID,
			LabelSite:     server.Source.SiteID,
			LabelExporter: exporter.Name,
		}
		if server.PlatformClaim != nil && server.PlatformClaim.PlatformID != "" {
			labels[LabelPlatformID] = server.PlatformClaim.PlatformID
		}
		targets = append(targets, ScrapeTarget{
			Targets: []string{fmt.Sprintf("%s:%d", address, exporter.Port)},
			Labels:  labels,
		})
	}
	return targets, nil
}
