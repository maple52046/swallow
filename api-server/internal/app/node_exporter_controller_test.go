package app

import (
	"context"
	"errors"
	"testing"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
)

// scrapeQuerier answers the windowed up read with samples (or err) and records the read.
type scrapeQuerier struct {
	monitoringdomain.MetricsQuerier
	samples  []monitoringdomain.Sample
	err      error
	exporter string
	window   time.Duration
}

func (q *scrapeQuerier) ExporterUpWithin(_ context.Context, exporter string, _ monitoringdomain.ServerSet, window time.Duration) ([]monitoringdomain.Sample, error) {
	q.exporter, q.window = exporter, window
	return q.samples, q.err
}

// scrapeFactory hands out one querier for site-1 and reports every other Site as having no
// metrics Integration.
type scrapeFactory struct {
	querier *scrapeQuerier
	sites   []string
}

func (f *scrapeFactory) Querier(_ context.Context, siteID string) (monitoringdomain.MetricsQuerier, error) {
	f.sites = append(f.sites, siteID)
	if siteID != "site-1" {
		return nil, monitoringdomain.ErrNoMetricsIntegration
	}
	return f.querier, nil
}

func (f *scrapeFactory) Alerts(context.Context, string) (monitoringdomain.AlertSource, error) {
	return nil, monitoringdomain.ErrNoAlertSource
}

// ensureStubs satisfies the controller's store ports; every method panics, for tests that only
// build the controller.
type ensureStubs struct {
	softwareapp.EnsureAssignments
	softwareapp.SiteAutomation
	softwareapp.WorkflowLister
	softwareapp.WorkflowCanceler
}

func upSample(serverID string, value float64) monitoringdomain.Sample {
	return monitoringdomain.Sample{Labels: map[string]string{monitoringdomain.LabelServerID: serverID}, Value: value}
}

// The Server's Site Prometheus decides the observation: any successful scrape in the window is up,
// only failed ones are down, and no answer, no Prometheus, or no Site is unknown.
func TestExporterScrapesObservation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		siteID  string
		samples []monitoringdomain.Sample
		err     error
		want    softwareapp.ScrapeObservation
		wantErr bool
	}{
		{name: "up", siteID: "site-1", samples: []monitoringdomain.Sample{upSample("srv-1", 1)}, want: softwareapp.ScrapeUp},
		{name: "down", siteID: "site-1", samples: []monitoringdomain.Sample{upSample("srv-1", 0)}, want: softwareapp.ScrapeDown},
		{name: "no sample", siteID: "site-1", want: softwareapp.ScrapeUnknown},
		{name: "another Server's sample", siteID: "site-1", samples: []monitoringdomain.Sample{upSample("srv-2", 0)}, want: softwareapp.ScrapeUnknown},
		{name: "no Prometheus at the Site", siteID: "site-2", want: softwareapp.ScrapeUnknown},
		{name: "no Site", siteID: "", want: softwareapp.ScrapeUnknown},
		{name: "query fails", siteID: "site-1", err: errors.New("unreachable"), want: softwareapp.ScrapeUnknown, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			querier := &scrapeQuerier{samples: tc.samples, err: tc.err}
			factory := &scrapeFactory{querier: querier}
			got, err := exporterScrapes{factory: factory}.Scrapes(context.Background(), tc.siteID, "node-exporter", "srv-1", 10*time.Minute)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("Scrapes() = %v, %v, want %v (error %v)", got, err, tc.want, tc.wantErr)
			}
			if tc.siteID == "" && len(factory.sites) != 0 {
				t.Errorf("Querier asked for %v, want no lookup without a Site", factory.sites)
			}
			if tc.siteID == "site-1" && (querier.exporter != "node-exporter" || querier.window != 10*time.Minute) {
				t.Errorf("read %q over %s, want node-exporter over 10m", querier.exporter, querier.window)
			}
		})
	}
}

// The worker's controller is the software ensure controller of the Node Exporter module's kind,
// whose installs carry its own requester.
func TestNewNodeExporterController(t *testing.T) {
	modules := testModules(t)
	if _, err := newNodeExporterController(nodeExporterControllerDeps{catalog: modules.software}); err == nil {
		t.Error("newNodeExporterController(no software service) error = nil, want an error")
	}
	// Building it does no I/O, so stubs that panic when called are enough.
	stubs := ensureStubs{}
	controller, err := newNodeExporterController(nodeExporterControllerDeps{
		catalog: modules.software, software: &softwareapp.SoftwareService{},
		servers: &fakeServerRepo{}, assignments: stubs, automation: stubs,
		workflows: stubs, canceler: stubs, metrics: &scrapeFactory{},
	})
	if err != nil {
		t.Fatalf("newNodeExporterController: %v", err)
	}
	if controller.Name() != "node-exporter" {
		t.Errorf("Name() = %q, want node-exporter", controller.Name())
	}
	if got := softwareapp.EnsureRequester("node-exporter"); got != "swallow:node-exporter-controller" {
		t.Errorf("EnsureRequester = %q, want swallow:node-exporter-controller", got)
	}
}
