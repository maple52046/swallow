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

// ExporterFilter narrows an ExporterInventory listing; the zero value lists every exporter of
// every Server. Name keeps only the exporter of that name, and ServerID only that Server's. The
// filter only narrows: an exporter it keeps is reported exactly as an unfiltered listing would.
type ExporterFilter struct {
	Name     string
	ServerID string
}

// ExporterInventory lists the exporters swallow knows are installed, in any Site. An
// implementation reports an exporter only while its installation is recorded as done (an
// installed Software Assignment, or a Platform feature that is ready), so a Server without an
// exporter is never a scrape target and its health is never down (servers-list.md, Health). It
// may use the filter to read less, but must not report anything the filter excludes.
type ExporterInventory interface {
	InstalledExporters(ctx context.Context, filter ExporterFilter) ([]InstalledExporter, error)
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

// TargetAddress is the address the Site metrics store scrapes a Server's installed exporters at, or
// "" when the Server is never a target: it is absent, or it has no known address, since a target
// with no address only produces a failing series. It is the one rule the exporter target list and
// TargetedServers share.
func TargetAddress(server *serverdomain.Server) string {
	if server == nil || server.Absent {
		return ""
	}
	return server.PrimaryAddress()
}

// Targets returns one http_sd target group per installed exporter of the Site's present Servers,
// ordered by Server and exporter; an empty siteID means every Site. A Server without a primary
// address is skipped (TargetAddress).
func (d *ExporterDiscovery) Targets(ctx context.Context, siteID string) ([]ScrapeTarget, error) {
	installed, err := d.exporters.InstalledExporters(ctx, ExporterFilter{})
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
		address := TargetAddress(server)
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

// TargetedServers reports which of servers Targets lists a target of the named exporter for, as the
// set of their IDs: the exporter is installed on the Server and TargetAddress has an address for it.
// It reads the inventory once for the whole batch (narrowed to the one Server when servers has one)
// and takes the Servers as given rather than reading them again. Only an inventory read failure is
// an error.
func (d *ExporterDiscovery) TargetedServers(ctx context.Context, exporter string, servers []*serverdomain.Server) (map[string]bool, error) {
	if len(servers) == 0 {
		return map[string]bool{}, nil
	}
	filter := ExporterFilter{Name: exporter}
	if len(servers) == 1 {
		filter.ServerID = servers[0].ID
	}
	installed, err := d.exporters.InstalledExporters(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list the installed %s exporters: %w", exporter, err)
	}
	runs := make(map[string]bool, len(installed))
	for _, item := range installed {
		if item.Name == exporter {
			runs[item.ServerID] = true
		}
	}
	targeted := map[string]bool{}
	for _, server := range servers {
		if runs[server.ID] && TargetAddress(server) != "" {
			targeted[server.ID] = true
		}
	}
	return targeted, nil
}
