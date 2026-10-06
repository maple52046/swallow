package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// autoInspectIntegrations lists fixed provisioner Integrations.
type autoInspectIntegrations struct {
	sitedomain.IntegrationRepository
	integrations []*sitedomain.Integration
	filter       sitedomain.IntegrationFilter
}

func (r *autoInspectIntegrations) List(_ context.Context, filter sitedomain.IntegrationFilter) ([]*sitedomain.Integration, error) {
	r.filter = filter
	return r.integrations, nil
}

// autoInspectServers answers List per Integration and records the filter it was given.
type autoInspectServers struct {
	serverdomain.ServerRepository
	byIntegration map[string][]*serverdomain.Server
	filters       []serverdomain.ListFilter
}

func (r *autoInspectServers) List(_ context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	r.filters = append(r.filters, filter)
	servers := r.byIntegration[filter.IntegrationID]
	return serverdomain.ListResult{Servers: servers, Total: len(servers)}, nil
}

// recordingInspections records launches; inspected Servers already had a Workflow and busy
// Servers are refused.
type recordingInspections struct {
	inspected map[string]bool
	busy      map[string]bool
	launched  []InspectionRequest
}

func (r *recordingInspections) LaunchInspection(_ context.Context, request InspectionRequest) (*InspectionAccepted, error) {
	if r.busy[request.ServerID] {
		return nil, errors.New("busy")
	}
	r.launched = append(r.launched, request)
	return &InspectionAccepted{WorkflowID: "workflow-" + request.ServerID}, nil
}

func (r *recordingInspections) HasInspection(_ context.Context, serverID string) (bool, error) {
	return r.inspected[serverID], nil
}

func newServer(id, state string, createdAt time.Time) *serverdomain.Server {
	return &serverdomain.Server{ID: id, CreatedAt: createdAt, Provisioning: &serverdomain.ProvisioningStatus{State: state}}
}

func TestAutoInspectUseCaseStartsFreshNewServersOnce(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Hour)
	locked := newServer("locked", "new", fresh)
	locked.Provisioning.Locked = true
	absent := newServer("absent", "new", fresh)
	absent.Absent = true
	integrations := &autoInspectIntegrations{integrations: []*sitedomain.Integration{
		{ID: "maas-on"},
		{ID: "maas-off", Settings: map[string]string{SettingAutoInspect: "false"}},
	}}
	servers := &autoInspectServers{byIntegration: map[string][]*serverdomain.Server{
		"maas-on": {
			newServer("fresh", "new", fresh),
			newServer("old", "new", now.Add(-AutoInspectWindow-time.Minute)),
			newServer("inspected", "new", fresh),
			newServer("busy", "new", fresh),
			newServer("ready", "ready", fresh),
			locked, absent,
		},
		"maas-off": {newServer("ignored", "new", fresh)},
	}}
	inspections := &recordingInspections{inspected: map[string]bool{"inspected": true}, busy: map[string]bool{"busy": true}}
	uc := NewAutoInspectUseCase(integrations, servers, inspections)
	uc.now = func() time.Time { return now }

	started, err := uc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if started != 1 || len(inspections.launched) != 1 || inspections.launched[0].ServerID != "fresh" {
		t.Fatalf("started %d, launched %+v; want only the fresh new Server", started, inspections.launched)
	}
	if got := inspections.launched[0]; got.Origin != provisioningdomain.InspectionOriginAutomatic || got.RequestedBy != "system" {
		t.Errorf("launch = %+v, want an automatic request by system", got)
	}
	if !integrations.filter.EnabledOnly || integrations.filter.Kind != sitedomain.IntegrationKindProvisioner {
		t.Errorf("integration filter = %+v, want enabled provisioners", integrations.filter)
	}
	if len(servers.filters) != 1 || servers.filters[0].ProvisioningState != "new" || servers.filters[0].IntegrationID != "maas-on" {
		t.Errorf("server filters = %+v, want one new-state listing of maas-on", servers.filters)
	}
}

func TestAutoInspectEnabled(t *testing.T) {
	for settings, want := range map[string]bool{"": true, "true": true, "false": false, "no": true} {
		integration := &sitedomain.Integration{Settings: map[string]string{}}
		if settings != "" {
			integration.Settings[SettingAutoInspect] = settings
		}
		if got := AutoInspectEnabled(integration); got != want {
			t.Errorf("AutoInspectEnabled(%q) = %v, want %v", settings, got, want)
		}
	}
}

// enrollmentProvider is a provisioner with existing-host enrollment.
type enrollmentProvider struct {
	provisioningdomain.OSProvisioningProvider
}

func (enrollmentProvider) Name() string { return "maas" }

func (enrollmentProvider) ExistingHostEnrollment(context.Context) (*provisioningdomain.ExistingHostEnrollment, error) {
	return &provisioningdomain.ExistingHostEnrollment{Endpoint: "http://10.0.0.5:5240/MAAS", Token: "a:b:c'd"}, nil
}

// plainProvider has no existing-host enrollment.
type plainProvider struct {
	provisioningdomain.OSProvisioningProvider
}

type fixedProviders struct {
	provider provisioningdomain.OSProvisioningProvider
}

func (f fixedProviders) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

func TestHostEnrollmentBundle(t *testing.T) {
	uc := NewHostEnrollmentUseCase(fixedProviders{provider: enrollmentProvider{}})
	bundle, err := uc.Bundle(context.Background(), "maas-a", "http://10.0.0.5/")
	if err != nil {
		t.Fatalf("Bundle error = %v", err)
	}
	if bundle.ProviderKind != "maas" || bundle.Endpoint != "http://10.0.0.5:5240/MAAS" || bundle.Token != "a:b:c'd" {
		t.Errorf("bundle = %+v", bundle)
	}
	wantCommand := `curl -fsSL 'http://10.0.0.5/downloads/swallow-enroll.sh' | sudo sh -s -- --provisioner=maas --endpoint 'http://10.0.0.5:5240/MAAS' --token 'a:b:c'\''d'`
	if bundle.Command != wantCommand {
		t.Errorf("Command = %s, want %s", bundle.Command, wantCommand)
	}

	for _, bad := range []string{"", "10.0.0.5", "ftp://10.0.0.5", "http://10.0.0.5/swallow", "http://admin:x@10.0.0.5", "http://10.0.0.5/?a=b"} {
		if _, err := uc.Bundle(context.Background(), "maas-a", bad); !errors.Is(err, provisioningdomain.ErrInvalidEnrollmentRequest) {
			t.Errorf("Bundle(swallowUrl %q) error = %v, want ErrInvalidEnrollmentRequest", bad, err)
		}
	}

	_, err = NewHostEnrollmentUseCase(fixedProviders{provider: plainProvider{}}).Bundle(context.Background(), "other", "http://10.0.0.5")
	var providerErr *provisioningdomain.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("Bundle without the capability error = %v, want a rejection", err)
	}
}

// The script downloads the CLI from the address it was fetched from and hands every argument to
// `swallow servers enroll`; it carries no credential of its own.
func TestHostEnrollmentScript(t *testing.T) {
	script := HostEnrollmentScript("http://10.0.0.5:8080/")
	for _, want := range []string{
		"swallow_url='http://10.0.0.5:8080'",
		`"$swallow_url/downloads/swallow"`,
		`servers enroll "$@"`,
		`rm -rf "$workdir"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	if strings.Contains(script, "@SWALLOW_URL@") {
		t.Error("script has an unfilled placeholder")
	}
}
