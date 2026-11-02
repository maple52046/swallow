package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/store"
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

func (r *autoInspectIntegrations) FindByID(_ context.Context, id string) (*sitedomain.Integration, error) {
	for _, integration := range r.integrations {
		if integration.ID == id {
			return integration, nil
		}
	}
	return nil, sitedomain.ErrIntegrationNotFound
}

// autoInspectServers answers List per Integration and records the filter it was given.
type autoInspectServers struct {
	serverdomain.ServerRepository
	byIntegration map[string][]*serverdomain.Server
	filters       []serverdomain.ListFilter
}

func (r *autoInspectServers) List(_ context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	r.filters = append(r.filters, filter)
	var servers []*serverdomain.Server
	for _, server := range r.byIntegration[filter.IntegrationID] {
		if filter.ProvisioningState == "" || (server.Provisioning != nil && server.Provisioning.State == filter.ProvisioningState) {
			servers = append(servers, server)
		}
	}
	return serverdomain.ListResult{Servers: servers, Total: len(servers)}, nil
}

func (r *autoInspectServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	for _, servers := range r.byIntegration {
		for _, server := range servers {
			if server.ID == id {
				return server, nil
			}
		}
	}
	return nil, serverdomain.ErrServerNotFound
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

// The controller inspects only a fresh, present, unlocked new Server of an enabled provisioner
// whose autoInspect is on, once, and leaves a busy one for a later pass.
func TestAutoInspectControllerStartsFreshNewServersOnce(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Hour)
	locked := newServer("locked", "new", fresh)
	locked.Provisioning.Locked = true
	absent := newServer("absent", "new", fresh)
	absent.Absent = true
	integrations := &autoInspectIntegrations{integrations: []*sitedomain.Integration{
		{ID: "maas-on", Kind: sitedomain.IntegrationKindProvisioner, Enabled: true},
		{ID: "maas-off", Kind: sitedomain.IntegrationKindProvisioner, Enabled: true, Settings: map[string]string{SettingAutoInspect: "false"}},
		{ID: "maas-paused", Kind: sitedomain.IntegrationKindProvisioner},
	}}
	byIntegration := map[string][]*serverdomain.Server{
		"maas-on": {
			newServer("fresh", "new", fresh),
			newServer("old", "new", now.Add(-AutoInspectWindow-time.Minute)),
			newServer("inspected", "new", fresh),
			newServer("busy", "new", fresh),
			newServer("ready", "ready", fresh),
			locked, absent,
		},
		"maas-off":    {newServer("ignored", "new", fresh)},
		"maas-paused": {newServer("paused", "new", fresh)},
	}
	for integrationID, servers := range byIntegration {
		for _, server := range servers {
			server.Source.IntegrationID = integrationID
		}
	}
	servers := &autoInspectServers{byIntegration: byIntegration}
	inspections := &recordingInspections{inspected: map[string]bool{"inspected": true}, busy: map[string]bool{"busy": true}}
	controller := NewAutoInspectController(integrations, servers, inspections)
	controller.now = func() time.Time { return now }

	keys, err := controller.Keys(context.Background())
	if err != nil {
		t.Fatalf("Keys error = %v", err)
	}
	if !integrations.filter.EnabledOnly || integrations.filter.Kind != sitedomain.IntegrationKindProvisioner {
		t.Errorf("integration filter = %+v, want enabled provisioners", integrations.filter)
	}
	for _, id := range []string{"fresh", "old", "inspected", "busy", "locked", "absent", "ignored", "paused", "ready", "missing"} {
		if err := controller.Reconcile(context.Background(), convergence.Request{Key: id}); err != nil {
			t.Errorf("Reconcile(%s) error = %v", id, err)
		}
	}
	if len(inspections.launched) != 1 || inspections.launched[0].ServerID != "fresh" {
		t.Fatalf("launched %+v; want only the fresh new Server", inspections.launched)
	}
	if got := inspections.launched[0]; got.Origin != provisioningdomain.InspectionOriginAutomatic || got.RequestedBy != "system" {
		t.Errorf("launch = %+v, want an automatic request by system", got)
	}
	if strings.Contains(strings.Join(keys, ","), "ignored") || strings.Contains(strings.Join(keys, ","), "ready") {
		t.Errorf("Keys() = %v, want only the new Servers of maas-on", keys)
	}
}

// A changed Integration re-examines every candidate; a deleted Server needs nothing.
func TestAutoInspectControllerKeysForChange(t *testing.T) {
	integrations := &autoInspectIntegrations{integrations: []*sitedomain.Integration{
		{ID: "maas-on", Kind: sitedomain.IntegrationKindProvisioner, Enabled: true},
	}}
	servers := &autoInspectServers{byIntegration: map[string][]*serverdomain.Server{"maas-on": {newServer("fresh", "new", time.Now())}}}
	controller := NewAutoInspectController(integrations, servers, &recordingInspections{})

	keys, err := controller.KeysForChange(context.Background(), store.Change{Collection: "integrations", Key: "maas-on", Operation: store.ChangeUpdate})
	if err != nil || len(keys) != 1 || keys[0] != "fresh" {
		t.Errorf("KeysForChange(integration) = %v, %v; want [fresh]", keys, err)
	}
	keys, _ = controller.KeysForChange(context.Background(), store.Change{Collection: "servers", Key: "s1", Operation: store.ChangeDelete})
	if len(keys) != 0 {
		t.Errorf("KeysForChange(deleted server) = %v, want none", keys)
	}
	keys, _ = controller.KeysForChange(context.Background(), store.Change{Collection: "servers", Key: "s1", Operation: store.ChangeUpdate})
	if len(keys) != 1 || keys[0] != "s1" {
		t.Errorf("KeysForChange(server) = %v, want [s1]", keys)
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
