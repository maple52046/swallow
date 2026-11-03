package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	nodeexporter "github.com/maple52046/swallow/modules/software/node-exporter"
)

// nodeExporterControllerDeps are what the worker already has that the Node Exporter controller
// reads and acts through.
type nodeExporterControllerDeps struct {
	catalog     *softwaredomain.Catalog
	software    *softwareapp.SoftwareService
	servers     softwareapp.EnsureServers
	assignments softwareapp.EnsureAssignments
	automation  softwareapp.SiteAutomation
	workflows   softwareapp.WorkflowLister
	canceler    softwareapp.WorkflowCanceler
	metrics     monitoringdomain.MonitoringFactory
}

// newNodeExporterController composes the worker's Node Exporter controller (software-deployment.md,
// Node Exporter): the software context's ensure controller for the node-exporter kind, installing
// through the same SoftwareService the API's installs and the Platform operations use, and observing
// the exporter the kind's module declares through each Site's Prometheus. Building it does no I/O;
// whether the worker runs it is config.APIConfig.NodeExporterEnsure.
func newNodeExporterController(deps nodeExporterControllerDeps) (*softwareapp.EnsureController, error) {
	software, ok := deps.catalog.Lookup(nodeexporter.Kind)
	if !ok {
		return nil, fmt.Errorf("compose the Node Exporter controller: the build has no %s module", nodeexporter.Kind)
	}
	if deps.software == nil || deps.metrics == nil {
		return nil, errors.New("compose the Node Exporter controller: the software service and the metrics factory are required")
	}
	return softwareapp.NewEnsureController(softwareapp.EnsureControllerDeps{
		Entry: software.Entry(), Installer: deps.software, Servers: deps.servers, Assignments: deps.assignments,
		Automation: deps.automation, Workflows: deps.workflows, Canceler: deps.canceler,
		Scrapes: exporterScrapes{factory: deps.metrics},
	})
}

// exporterScrapes implements softwareapp.ExporterScrapes over the monitoring context's per-Site
// queriers: the Server's Site Prometheus (its metrics Integration) answers whether any scrape of
// the exporter's target on the Server succeeded in the window. It reads through swallow's own
// querier, never the Platform label proxy, so it sees every Server's series.
type exporterScrapes struct {
	factory monitoringdomain.MonitoringFactory
}

// Scrapes maps the querier's answer onto the observation: a sample above zero is up, zero is down,
// no sample for the Server is unknown. A Site without a metrics Integration, or a Server without a
// Site, is unknown with no error; a failed query is an error.
func (s exporterScrapes) Scrapes(ctx context.Context, siteID, exporter, serverID string, window time.Duration) (softwareapp.ScrapeObservation, error) {
	// An empty siteID would select any Site's Prometheus, which does not scrape this Server.
	if siteID == "" {
		return softwareapp.ScrapeUnknown, nil
	}
	querier, err := s.factory.Querier(ctx, siteID)
	if errors.Is(err, monitoringdomain.ErrNoMetricsIntegration) {
		return softwareapp.ScrapeUnknown, nil
	}
	if err != nil {
		return softwareapp.ScrapeUnknown, err
	}
	servers, err := monitoringdomain.NewServerSet([]string{serverID})
	if err != nil {
		return softwareapp.ScrapeUnknown, err
	}
	samples, err := querier.ExporterUpWithin(ctx, exporter, servers, window)
	if err != nil {
		return softwareapp.ScrapeUnknown, err
	}
	for _, sample := range samples {
		if sample.ServerID() != serverID {
			continue
		}
		if sample.Value > 0 {
			return softwareapp.ScrapeUp, nil
		}
		return softwareapp.ScrapeDown, nil
	}
	return softwareapp.ScrapeUnknown, nil
}
