package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

type fakeSource struct {
	sites        []Site
	integrations []Integration
	servers      []Server
	clusters     []Cluster
	operations   []Operation
	alerts       []Alert
	alertErr     error
	sourceErr    error
	siteIDs      []string
}

func (s *fakeSource) ListSites(context.Context) ([]Site, error) {
	if s.sourceErr != nil {
		return nil, s.sourceErr
	}
	return s.sites, nil
}

func (s *fakeSource) ListIntegrations(_ context.Context, siteID string) ([]Integration, error) {
	s.siteIDs = append(s.siteIDs, siteID)
	return s.integrations, nil
}

func (s *fakeSource) ListServers(_ context.Context, siteID string) ([]Server, error) {
	s.siteIDs = append(s.siteIDs, siteID)
	return s.servers, nil
}

func (s *fakeSource) ListClusters(_ context.Context, siteID string) ([]Cluster, error) {
	s.siteIDs = append(s.siteIDs, siteID)
	return s.clusters, nil
}

func (s *fakeSource) ListOperations(_ context.Context, siteID string) ([]Operation, error) {
	s.siteIDs = append(s.siteIDs, siteID)
	return s.operations, nil
}

func (s *fakeSource) ListFiringAlerts(_ context.Context, siteID string) ([]Alert, error) {
	s.siteIDs = append(s.siteIDs, siteID)
	return s.alerts, s.alertErr
}

func TestServiceExecuteBuildsBoundedOverview(t *testing.T) {
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	source := &fakeSource{
		sites: []Site{{ID: "site-a"}, {ID: "site-b"}},
		integrations: []Integration{
			{ID: "integration-a"},
			{ID: "integration-b", LastError: "sync failed"},
		},
		servers: []Server{
			{ProvisioningState: "deployed", ClusterID: "cluster-a", GPUDevices: 8, HealthState: "up"},
			{Absent: true, ProvisioningState: "ready", HealthState: "down"},
			{ProvisioningState: "deployed"},
		},
		clusters: []Cluster{
			{MemberCount: 5, MatchedCount: 3},
			{IntegrationID: "integration-cluster", MemberCount: 2, MatchedCount: 2},
		},
	}
	for i := 0; i < 10; i++ {
		status := "succeeded"
		if i == 0 {
			status = "running"
		}
		if i == 1 || i == 9 {
			status = "failed"
		}
		requestedAt := now.Add(-time.Duration(i) * time.Hour)
		source.operations = append(source.operations, Operation{
			ID:     "operation-" + string(rune('a'+i)),
			Status: status, RequestedAt: requestedAt, UpdatedAt: requestedAt,
		})
	}
	for i := 0; i < 12; i++ {
		severity := "warning"
		if i%2 == 0 {
			severity = "critical"
		}
		startedAt := now.Add(-time.Duration(i) * time.Minute)
		source.alerts = append(source.alerts, Alert{
			Fingerprint: "alert-" + string(rune('a'+i)),
			Severity:    severity, State: "firing", StartsAt: &startedAt,
		})
	}

	result, err := NewService(source, fixedClock{now: now}).Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	if result.Inventory.Sites != 2 || result.Inventory.Servers != 3 ||
		result.Inventory.Absent != 1 || result.Inventory.Deployed != 2 ||
		result.Inventory.Clustered != 1 || result.Inventory.GPUDevices != 8 {
		t.Errorf("Execute() inventory = %+v, want complete platform counts", result.Inventory)
	}
	if result.Inventory.Health != (HealthSummary{Up: 1, Down: 1, Unknown: 1}) {
		t.Errorf("Execute() health = %+v, want up=1 down=1 unknown=1", result.Inventory.Health)
	}
	if result.Integrations.Total != 2 || result.Integrations.Failing != 1 {
		t.Errorf("Execute() integrations = %+v, want total=2 failing=1", result.Integrations)
	}
	if result.Clusters != (ClusterSummary{Total: 2, Unreachable: 1, UnmatchedMembers: 2}) {
		t.Errorf("Execute() clusters = %+v, want total=2 unreachable=1 unmatched=2", result.Clusters)
	}
	if result.Operations.Active != 1 || result.Operations.FailedLast24Hours != 2 ||
		len(result.Operations.Recent) != recentOperationLimit {
		t.Errorf("Execute() operations = %+v, want active=1 failed=2 recent=%d",
			result.Operations, recentOperationLimit)
	}
	if result.Operations.Recent[0].ID != "operation-a" {
		t.Errorf("Execute() newest operation = %q, want operation-a", result.Operations.Recent[0].ID)
	}
	if !result.Monitoring.Available || result.Monitoring.Firing.Critical != 6 ||
		result.Monitoring.Firing.Warning != 6 || len(result.Monitoring.Firing.Items) != firingAlertLimit {
		t.Errorf("Execute() monitoring = %+v, want bounded severity counts", result.Monitoring)
	}
	if result.Monitoring.Firing.Items[0].Severity != "critical" {
		t.Errorf("Execute() first alert severity = %q, want critical",
			result.Monitoring.Firing.Items[0].Severity)
	}
}

func TestServiceExecuteScopesEverySourceAndRejectsUnknownSite(t *testing.T) {
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	source := &fakeSource{sites: []Site{{ID: "site-a"}}}
	service := NewService(source, fixedClock{now: now})

	result, err := service.Execute(context.Background(), "site-a")
	if err != nil {
		t.Fatalf("Execute(site-a) error = %v, want nil", err)
	}
	if result.Inventory.Sites != 1 || result.SiteID != "site-a" {
		t.Errorf("Execute(site-a) scope = %+v inventory=%+v, want one scoped site",
			result.SiteID, result.Inventory)
	}
	for _, got := range source.siteIDs {
		if got != "site-a" {
			t.Errorf("Execute(site-a) forwarded scope %q, want site-a", got)
		}
	}

	_, err = service.Execute(context.Background(), "missing")
	if !errors.Is(err, ErrSiteNotFound) {
		t.Fatalf("Execute(missing) error = %v, want ErrSiteNotFound", err)
	}
}

func TestServiceExecuteKeepsDurableDataWhenMonitoringUnavailable(t *testing.T) {
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	source := &fakeSource{
		sites:    []Site{{ID: "site-a"}},
		servers:  []Server{{ProvisioningState: "deployed"}},
		alertErr: ErrMonitoringUnavailable,
	}

	result, err := NewService(source, fixedClock{now: now}).Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil partial response", err)
	}
	if result.Inventory.Servers != 1 {
		t.Errorf("Execute() servers = %d, want 1", result.Inventory.Servers)
	}
	if result.Monitoring.Available || result.Monitoring.Error == nil ||
		result.Monitoring.Error.Code != "provider_unavailable" {
		t.Errorf("Execute() monitoring = %+v, want provider_unavailable partial state",
			result.Monitoring)
	}
}

func TestServiceExecutePropagatesDurableSourceFailure(t *testing.T) {
	wantErr := errors.New("database unavailable")
	source := &fakeSource{sourceErr: wantErr}

	_, err := NewService(source, fixedClock{}).Execute(context.Background(), "")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Execute() error = %v, want %v", err, wantErr)
	}
}
