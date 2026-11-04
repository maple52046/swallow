package app

import (
	"context"
	"errors"
	"testing"

	monitoringapp "github.com/maple52046/swallow/internal/core/monitoring/application"
	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	overviewapp "github.com/maple52046/swallow/internal/core/overview/application"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

type overviewServerRepository struct {
	serverdomain.ServerRepository
	result serverdomain.ListResult
}

func (r overviewServerRepository) List(_ context.Context, _ serverdomain.ListFilter) (serverdomain.ListResult, error) {
	return r.result, nil
}

// overviewHealth answers a fixed health state per Server ID, or err for every call.
type overviewHealth struct {
	states map[string]string
	err    error
}

func (h overviewHealth) ResolveHealth(_ context.Context, servers []*serverdomain.Server) (map[string]serverdomain.HealthStatus, error) {
	if h.err != nil {
		return nil, h.err
	}
	health := map[string]serverdomain.HealthStatus{}
	for _, server := range servers {
		health[server.ID] = serverdomain.HealthStatus{State: h.states[server.ID]}
	}
	return health, nil
}

// Overview capacity must not count a BMC graphics controller as workload acceleration.
func TestReaderListServersCountsOnlyComputeGPUs(t *testing.T) {
	repository := overviewServerRepository{result: serverdomain.ListResult{Servers: []*serverdomain.Server{
		{Observed: serverdomain.Observed{GPUs: []serverdomain.GPU{
			{Vendor: "AMD", Model: "MI300X", Count: 8, Kind: serverdomain.GPUKindCompute},
			{Vendor: "ASPEED", Model: "Graphics Family", Count: 1, Kind: serverdomain.GPUKindDisplay},
		}}},
	}}}
	reader := &OverviewReader{servers: repository, health: overviewHealth{}}

	servers, err := reader.ListServers(context.Background(), "site-a")
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("ListServers length = %d, want 1", len(servers))
	}
	if servers[0].GPUDevices != 8 {
		t.Errorf("GPUDevices = %d, want 8 compute GPUs", servers[0].GPUDevices)
	}
}

// Every Server carries the state the shared resolver classified, absent ones included, and a
// failure to classify fails the overview rather than counting Servers nobody classified.
func TestReaderListServersCarriesClassifiedHealth(t *testing.T) {
	repository := overviewServerRepository{result: serverdomain.ListResult{Servers: []*serverdomain.Server{
		{ID: "up"}, {ID: "gone", Absent: true},
	}}}
	reader := &OverviewReader{servers: repository, health: overviewHealth{states: map[string]string{
		"up": serverdomain.HealthUp, "gone": serverdomain.HealthNotApplicable,
	}}}

	servers, err := reader.ListServers(context.Background(), "")
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(servers) != 2 || servers[0].HealthState != serverdomain.HealthUp || servers[1].HealthState != serverdomain.HealthNotApplicable {
		t.Errorf("ListServers() = %+v, want up and not_applicable", servers)
	}

	failing := &OverviewReader{servers: repository, health: overviewHealth{err: errors.New("store down")}}
	if _, err := failing.ListServers(context.Background(), ""); err == nil {
		t.Error("ListServers() error = nil, want the classification failure")
	}
}

// overviewAlertFactory answers every alert-source lookup with one fixed error.
type overviewAlertFactory struct {
	err error
}

func (f overviewAlertFactory) Querier(context.Context, string) (monitoringdomain.MetricsQuerier, error) {
	return nil, f.err
}

func (f overviewAlertFactory) Alerts(context.Context, string) (monitoringdomain.AlertSource, error) {
	return nil, f.err
}

// A missing metrics Integration or Alertmanager address is "not configured", while a failing
// configured Alertmanager is "unavailable"; anything else stays a fatal error.
func TestReaderListFiringAlertsSeparatesNotConfiguredFromUnavailable(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{"no metrics integration", monitoringdomain.ErrNoMetricsIntegration, overviewapp.ErrMonitoringNotConfigured},
		{"no alertmanager address", monitoringdomain.ErrNoAlertSource, overviewapp.ErrMonitoringNotConfigured},
		{"alertmanager unreachable", &monitoringdomain.QueryError{Kind: monitoringdomain.QueryErrorUnavailable}, overviewapp.ErrMonitoringUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader := &OverviewReader{alerts: monitoringapp.NewAlertService(overviewAlertFactory{err: tc.err}, knownSites{})}

			_, err := reader.ListFiringAlerts(context.Background(), "site-a")
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("ListFiringAlerts() error = %v, want %v", err, tc.wantErr)
			}
		})
	}

	reader := &OverviewReader{alerts: monitoringapp.NewAlertService(overviewAlertFactory{err: errors.New("boom")}, knownSites{})}
	_, err := reader.ListFiringAlerts(context.Background(), "site-a")
	if errors.Is(err, overviewapp.ErrMonitoringNotConfigured) || errors.Is(err, overviewapp.ErrMonitoringUnavailable) {
		t.Errorf("ListFiringAlerts() error = %v, want a fatal error outside both partial states", err)
	}
}

// knownSites reports every Site as existing; the overview resolves its Site before reading alerts.
type knownSites struct{}

func (knownSites) SiteExists(context.Context, string) (bool, error) { return true, nil }
