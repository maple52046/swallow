package app

import (
	"context"
	"fmt"
	"slices"

	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/modules/platform/slurm"
)

// installedExporters is the discovery context's ExporterInventory, over two sources.
//
// Managed Software: a kind whose catalog entry declares an exporter runs it on every Server whose
// assignment of that kind is installed, or uninstalling (the package is still there until the
// uninstall finishes).
//
// Platform features: a Slurm Platform whose Omnistat is ready runs the Omnistat exporter on every
// compute member of its applied spec (the spec the last successful Workflow realized; the spec
// itself for a Platform with none applied yet).
//
// A pending or failed install is not reported, nor is an Omnistat that is installing again: the
// exporter may not be running, and a target that cannot answer only produces failing series.
type installedExporters struct {
	catalog     *softwaredomain.Catalog
	assignments softwaredomain.AssignmentRepository
	platforms   exporterPlatforms
}

// exporterPlatforms lists every Platform with its feature records and spec, in every Site.
type exporterPlatforms interface {
	List(ctx context.Context, siteID string) ([]*platformdomain.Platform, error)
}

// InstalledExporters lists the declared exporters of every installed assignment and ready
// Platform feature.
func (e installedExporters) InstalledExporters(ctx context.Context) ([]discoveryapp.InstalledExporter, error) {
	var exporters []discoveryapp.InstalledExporter
	for _, entry := range e.catalog.Entries() {
		if entry.Exporter == nil {
			continue
		}
		assignments, err := e.assignments.List(ctx, softwaredomain.AssignmentFilter{Kind: entry.Kind})
		if err != nil {
			return nil, fmt.Errorf("list the %s assignments: %w", entry.Kind, err)
		}
		for _, assignment := range assignments {
			if assignment.State != softwaredomain.StateInstalled && assignment.State != softwaredomain.StateUninstalling {
				continue
			}
			exporters = append(exporters, discoveryapp.InstalledExporter{
				ServerID: assignment.ServerID, Name: entry.Exporter.Name, Port: entry.Exporter.Port,
			})
		}
	}
	platforms, err := e.platforms.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list the Platforms: %w", err)
	}
	for _, platform := range platforms {
		for _, serverID := range omnistatExporterServers(platform) {
			exporters = append(exporters, discoveryapp.InstalledExporter{
				ServerID: serverID, Name: platformdomain.OmnistatScrapeJob, Port: platformdomain.OmnistatExporterPort,
			})
		}
	}
	return exporters, nil
}

// omnistatExporterServers returns the compute members of a Slurm Platform whose Omnistat is ready,
// which configure-omnistat installed the exporter on; none for any other Platform.
func omnistatExporterServers(platform *platformdomain.Platform) []string {
	if platform.Type != platformdomain.PlatformTypeSlurm || platform.Omnistat == nil ||
		platform.Omnistat.State != platformdomain.OmnistatReady {
		return nil
	}
	spec := platform.Status.Applied
	if spec == nil {
		spec = platform.Spec
	}
	if spec == nil {
		return nil
	}
	var serverIDs []string
	for _, member := range spec.Members {
		if slices.Contains(member.Roles, slurm.RoleCompute) {
			serverIDs = append(serverIDs, member.ServerID)
		}
	}
	return serverIDs
}
