package app

import (
	"context"
	"fmt"

	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

// installedExporters is the discovery context's ExporterInventory. A Managed Software kind whose
// catalog entry declares an exporter runs it on every Server whose assignment of that kind is
// installed, or uninstalling (the package is still there until the uninstall finishes). A pending
// or failed assignment is not reported: the exporter may not be running, and a target that cannot
// answer would make the Server's health down instead of unknown.
type installedExporters struct {
	catalog     *softwaredomain.Catalog
	assignments softwaredomain.AssignmentRepository
}

// InstalledExporters lists the declared exporters of every installed assignment.
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
	return exporters, nil
}
