package app

import (
	"context"
	"errors"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// vmServers holds the Hypervisor and projects the registered Machine's Server after
// projectAfter reads by source (never when negative).
type vmServers struct {
	serverdomain.ServerRepository
	servers      map[string]*serverdomain.Server
	projectAfter int
	reads        int
}

func (r *vmServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	if server, ok := r.servers[id]; ok {
		return server, nil
	}
	return nil, serverdomain.ErrServerNotFound
}

func (r *vmServers) FindBySource(_ context.Context, source serverdomain.Source) (*serverdomain.Server, error) {
	r.reads++
	if r.projectAfter < 0 || r.reads <= r.projectAfter {
		return nil, serverdomain.ErrServerNotFound
	}
	return &serverdomain.Server{ID: "vm-server", Source: source}, nil
}

// vmProvider is a provisioner that registers machines.
type vmProvider struct {
	provisioningdomain.OSProvisioningProvider
	existing      string
	registerErr   error
	powerErr      error
	power         provisioningdomain.PowerState
	registrations []provisioningdomain.MachineRegistration
	rewrites      []provisioningdomain.PowerConfigurationChange
}

func (p *vmProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{Power: true, PowerConfiguration: true, MachineRegistration: true}
}

func (p *vmProvider) FindMachineByMAC(context.Context, []string) (string, error) {
	return p.existing, nil
}

func (p *vmProvider) RegisterMachine(_ context.Context, registration provisioningdomain.MachineRegistration) (string, error) {
	p.registrations = append(p.registrations, registration)
	if p.registerErr != nil {
		return "", p.registerErr
	}
	return "machine-new", nil
}

func (p *vmProvider) PowerConfiguration(context.Context, string) (*provisioningdomain.PowerConfiguration, error) {
	return &provisioningdomain.PowerConfiguration{}, nil
}

func (p *vmProvider) SetPowerConfiguration(_ context.Context, _ string, change provisioningdomain.PowerConfigurationChange) error {
	p.rewrites = append(p.rewrites, change)
	return nil
}

func (p *vmProvider) PowerOn(context.Context, string) (*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *vmProvider) PowerOff(context.Context, string) (*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *vmProvider) QueryPowerState(context.Context, string) (provisioningdomain.PowerState, error) {
	if p.powerErr != nil {
		return provisioningdomain.PowerStateUnknown, p.powerErr
	}
	return p.power, nil
}

// vmIntegrations is a FindByID-only IntegrationRepository.
type vmIntegrations struct {
	sitedomain.IntegrationRepository
	integration *sitedomain.Integration
}

func (r vmIntegrations) FindByID(_ context.Context, id string) (*sitedomain.Integration, error) {
	if r.integration != nil && r.integration.ID == id {
		return r.integration, nil
	}
	return nil, sitedomain.ErrIntegrationNotFound
}

// vmLibvirt is a scripted Hypervisor with one domain.
type vmLibvirt struct {
	machine   *serverdomain.VirtualMachine
	domainErr error
	calls     []string
	keys      []string
}

func (f *vmLibvirt) ListDomains(context.Context, serverdomain.HypervisorLogin) ([]serverdomain.VirtualMachine, error) {
	return []serverdomain.VirtualMachine{*f.machine}, nil
}

func (f *vmLibvirt) Domain(context.Context, serverdomain.HypervisorLogin, string) (*serverdomain.VirtualMachine, error) {
	f.calls = append(f.calls, "domain")
	if f.domainErr != nil {
		return nil, f.domainErr
	}
	copied := *f.machine
	return &copied, nil
}

func (f *vmLibvirt) DestroyDomain(context.Context, serverdomain.HypervisorLogin, string) error {
	f.calls = append(f.calls, "destroy")
	f.machine.State = "shut off"
	return nil
}

func (f *vmLibvirt) AuthorizePublicKey(_ context.Context, _ serverdomain.HypervisorLogin, key string) error {
	f.calls = append(f.calls, "authorize")
	f.keys = append(f.keys, key)
	return nil
}

func (f *vmLibvirt) ReadBootMedia(context.Context, serverdomain.HypervisorLogin, string, string) (serverdomain.BootMediaState, error) {
	return serverdomain.BootMediaState{}, nil
}

func (f *vmLibvirt) ApplyBootMedia(context.Context, serverdomain.HypervisorLogin, string, serverdomain.BootISOFile) (serverdomain.BootMediaState, error) {
	f.calls = append(f.calls, "boot-media")
	return serverdomain.BootMediaState{MediaInserted: true, OverrideReady: true}, nil
}

func (f *vmLibvirt) ClearBootMedia(context.Context, serverdomain.HypervisorLogin, string) error {
	return nil
}

type vmBootMedia struct{ enabled []string }

func (b *vmBootMedia) EnableApplied(_ context.Context, serverID, isoID string) error {
	b.enabled = append(b.enabled, serverID+":"+isoID)
	return nil
}

type vmFixture struct {
	executor  providerStepExecutor
	servers   *vmServers
	provider  *vmProvider
	libvirt   *vmLibvirt
	bootMedia *vmBootMedia
}

func newVMFixture() vmFixture {
	servers := &vmServers{servers: map[string]*serverdomain.Server{"hv-1": {
		ID: "hv-1", Observed: serverdomain.Observed{Hostname: "tainan-ci", Addresses: []string{"10.170.168.22"}},
		Source:       serverdomain.Source{SiteID: "site-1"},
		Provisioning: &serverdomain.ProvisioningStatus{State: "deployed", DeployedImageDefaultUser: "ubuntu"},
	}}}
	provider := &vmProvider{power: provisioningdomain.PowerStateOff}
	libvirt := &vmLibvirt{machine: &serverdomain.VirtualMachine{
		Name: "lab-afde-mi308-1", State: "shut off", Architecture: "x86_64", MACAddresses: []string{"52:54:00:af:de:01"},
	}}
	bootMedia := &vmBootMedia{}
	integration := &sitedomain.Integration{ID: "integration-1", SiteID: "site-1", Kind: sitedomain.IntegrationKindProvisioner,
		Settings: map[string]string{provisioningapp.SettingVirshSSHPublicKey: "ssh-ed25519 AAAA maas-rack"}}
	return vmFixture{
		executor: providerStepExecutor{
			servers: servers, providers: inspectionProviderFactory{provider: provider},
			virtualMachines: &virtualMachineEnroller{
				libvirt: libvirt, isos: plannerISOs{}, bootMedia: bootMedia, integrations: vmIntegrations{integration: integration},
				projectionWait: 50 * time.Millisecond, projectionPoll: time.Millisecond,
			},
		},
		servers: servers, provider: provider, libvirt: libvirt, bootMedia: bootMedia,
	}
}

func vmTask(parameters enrollVirtualMachineParameters) temporalworkflow.StepExecutionInput {
	return temporalworkflow.StepExecutionInput{OperationID: "workflow-id", Step: operationdomain.Task{
		ID: "enroll-virtual-machine-lab-afde-mi308-1", Kind: enrollVirtualMachineTaskKind, Attempt: 1,
		Parameters: parameters.toMap(), Targets: []operationdomain.ResourceReference{{Kind: "server", ID: "hv-1"}},
	}}
}

var vmParameters = enrollVirtualMachineParameters{
	IntegrationID: "integration-1", HypervisorServerID: "hv-1", Domain: "lab-afde-mi308-1", Account: "ubuntu", BootISOID: "iso-a",
}

// The happy path: the shut-off domain gets the provisioner's key and the Boot ISO, is registered
// without commissioning with the virsh Power Configuration, and its projected Server keeps Boot
// Media.
func TestEnrollVirtualMachineRegistersAndKeepsBootMedia(t *testing.T) {
	f := newVMFixture()
	result := f.executor.Execute(context.Background(), vmTask(vmParameters))
	if result.Status != operationdomain.TaskSucceeded {
		t.Fatalf("result = %+v, want success", result)
	}
	if want := []string{"domain", "authorize", "boot-media"}; !equalStringSlices(f.libvirt.calls, want) {
		t.Errorf("libvirt calls = %v, want %v", f.libvirt.calls, want)
	}
	if len(f.libvirt.keys) != 1 || f.libvirt.keys[0] != "ssh-ed25519 AAAA maas-rack" {
		t.Errorf("authorized keys = %v, want the Integration's virsh key", f.libvirt.keys)
	}
	if len(f.provider.registrations) != 1 {
		t.Fatalf("registrations = %+v, want one", f.provider.registrations)
	}
	got := f.provider.registrations[0]
	if got.Hostname != "lab-afde-mi308-1" || got.Architecture != "amd64" || len(got.MACAddresses) != 1 ||
		got.Power.Driver != provisioningdomain.PowerDriverVirsh || got.Power.Address != "qemu+ssh://ubuntu@10.170.168.22/system" || got.Power.PowerID != "lab-afde-mi308-1" {
		t.Errorf("registration = %+v", got)
	}
	if want := []string{"vm-server:iso-a"}; !equalStringSlices(f.bootMedia.enabled, want) {
		t.Errorf("Boot Media enabled = %v, want %v", f.bootMedia.enabled, want)
	}
}

// A Machine that already has the domain's MAC is reused with its power settings replaced.
func TestEnrollVirtualMachineReusesMachine(t *testing.T) {
	f := newVMFixture()
	f.provider.existing = "machine-old"
	if result := f.executor.Execute(context.Background(), vmTask(vmParameters)); result.Status != operationdomain.TaskSucceeded {
		t.Fatalf("result = %+v, want success", result)
	}
	if len(f.provider.registrations) != 0 || len(f.provider.rewrites) != 1 {
		t.Errorf("registrations %d, rewrites %d; want the Machine reused", len(f.provider.registrations), len(f.provider.rewrites))
	}
}

// A running domain waits for the operator unless powerOffRunning lets swallow stop it.
func TestEnrollVirtualMachineRunningDomain(t *testing.T) {
	f := newVMFixture()
	f.libvirt.machine.State = "running"
	result := f.executor.Execute(context.Background(), vmTask(vmParameters))
	if result.Status != operationdomain.TaskRequiresAttention || result.Error.Code != "domain_running" || !result.Error.Retryable {
		t.Fatalf("result = %+v, want domain_running attention", result)
	}
	if len(f.provider.registrations) != 0 {
		t.Error("registered a running domain")
	}

	parameters := vmParameters
	parameters.PowerOffRunning = true
	if result := f.executor.Execute(context.Background(), vmTask(parameters)); result.Status != operationdomain.TaskSucceeded {
		t.Fatalf("result with powerOffRunning = %+v, want success", result)
	}
	if f.libvirt.calls[len(f.libvirt.calls)-3] != "destroy" {
		t.Errorf("libvirt calls = %v, want the domain stopped first", f.libvirt.calls)
	}
}

// Each step's failure has its published code.
func TestEnrollVirtualMachineFailures(t *testing.T) {
	cases := []struct {
		name      string
		arrange   func(f vmFixture)
		status    operationdomain.TaskStatus
		code      string
		retryable bool
	}{
		{"domain missing", func(f vmFixture) {
			f.libvirt.domainErr = &serverdomain.HypervisorError{Err: serverdomain.ErrDomainNotFound, Hypervisor: "tainan-ci", Domain: "x"}
		}, operationdomain.TaskFailed, "domain_not_found", false},
		{"hypervisor unreachable", func(f vmFixture) {
			f.libvirt.domainErr = &serverdomain.HypervisorError{Err: serverdomain.ErrHostUnreachable, Hypervisor: "tainan-ci"}
		}, operationdomain.TaskRequiresAttention, "hypervisor_unreachable", true},
		{"unsupported architecture", func(f vmFixture) { f.libvirt.machine.Architecture = "ppc64le" },
			operationdomain.TaskFailed, "architecture_unsupported", false},
		{"registration refused", func(f vmFixture) {
			f.provider.registerErr = &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorRejected, Detail: "hostname in use"}
		}, operationdomain.TaskRequiresAttention, "machine_registration_refused", true},
		{"provisioner cannot reach the hypervisor", func(f vmFixture) {
			f.provider.powerErr = &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorUnavailable, Detail: "virsh failed"}
		}, operationdomain.TaskRequiresAttention, "provisioner_cannot_reach_hypervisor", true},
		{"power error state", func(f vmFixture) { f.provider.power = provisioningdomain.PowerStateError },
			operationdomain.TaskRequiresAttention, "provisioner_cannot_reach_hypervisor", true},
		{"not projected", func(f vmFixture) { f.servers.projectAfter = -1 },
			operationdomain.TaskRequiresAttention, "server_not_projected", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newVMFixture()
			tc.arrange(f)
			result := f.executor.Execute(context.Background(), vmTask(vmParameters))
			if result.Status != tc.status || result.Error == nil || result.Error.Code != tc.code || result.Error.Retryable != tc.retryable {
				t.Errorf("result = %+v (%+v), want %s %s retryable=%v", result, result.Error, tc.status, tc.code, tc.retryable)
			}
		})
	}
}

// The launcher creates one Workflow targeting the Hypervisor with one parallel provisioner Task
// per domain, freezing the effective account.
func TestVirtualMachineEnrollmentLauncher(t *testing.T) {
	f := newVMFixture()
	workflows := &fakeInspectionWorkflows{}
	launcher := virtualMachineEnrollmentLauncher{
		workflows: workflows, integrations: f.executor.virtualMachines.integrations,
		providers: inspectionProviderFactory{provider: f.provider}, servers: f.servers, isos: plannerISOs{},
	}
	request := provisioningapp.VirtualMachineEnrollmentRequest{
		IntegrationID: "integration-1", HypervisorServerID: "hv-1", Domains: []string{"lab-afde-mi308-1", "lab-afde-mi308-2"}, BootISOID: "iso-a",
	}
	accepted, err := launcher.LaunchVirtualMachineEnrollment(context.Background(), request)
	if err != nil {
		t.Fatalf("Launch error = %v", err)
	}
	created := workflows.created
	if accepted.WorkflowID != "workflow-new" || created.Kind != operationdomain.WorkflowKindEnrollVirtualMachines ||
		created.Definition != virtualMachineEnrollmentDefinition || !equalStringSlices(created.TargetServerIDs, []string{"hv-1"}) {
		t.Fatalf("created = %+v", created)
	}
	if len(created.Steps) != 2 {
		t.Fatalf("steps = %+v, want one per domain", created.Steps)
	}
	for _, step := range created.Steps {
		if step.Kind != enrollVirtualMachineTaskKind || step.Executor != operationdomain.RunnerKindProvisioner || len(step.DependsOn) != 0 || step.Parameters["account"] != "ubuntu" {
			t.Errorf("step = %+v, want an independent provisioner Task with the effective account", step)
		}
	}
	if created.Steps[0].ID == created.Steps[1].ID {
		t.Error("Task ids collide")
	}

	refusals := []struct {
		name   string
		change func(r *provisioningapp.VirtualMachineEnrollmentRequest, l *virtualMachineEnrollmentLauncher)
		want   error
	}{
		{"duplicate domains", func(r *provisioningapp.VirtualMachineEnrollmentRequest, _ *virtualMachineEnrollmentLauncher) {
			r.Domains = []string{"a", "a"}
		}, provisioningdomain.ErrInvalidVirtualMachineEnrollment},
		{"another provisioner's Boot ISO", func(r *provisioningapp.VirtualMachineEnrollmentRequest, l *virtualMachineEnrollmentLauncher) {
			r.IntegrationID = "maas-b"
			l.integrations = vmIntegrations{integration: &sitedomain.Integration{ID: "maas-b", SiteID: "site-1", Kind: sitedomain.IntegrationKindProvisioner}}
		}, serverdomain.ErrBootISOWrongIntegration},
		{"hypervisor in another Site", func(_ *provisioningapp.VirtualMachineEnrollmentRequest, _ *virtualMachineEnrollmentLauncher) {
			f.servers.servers["hv-1"].Source.SiteID = "site-2"
		}, provisioningdomain.ErrVirtualMachineEnrollmentConflict},
		{"no deployment key", func(_ *provisioningapp.VirtualMachineEnrollmentRequest, l *virtualMachineEnrollmentLauncher) {
			f.servers.servers["hv-1"].Source.SiteID = "site-1"
			l.keys = noDeploymentKey{}
		}, provisioningdomain.ErrDeploymentKeyMissing},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			r, l := request, launcher
			tc.change(&r, &l)
			if _, err := l.LaunchVirtualMachineEnrollment(context.Background(), r); !errors.Is(err, tc.want) {
				t.Errorf("Launch error = %v, want %v", err, tc.want)
			}
		})
	}
}

type noDeploymentKey struct{}

func (noDeploymentKey) HasDeploymentKey(context.Context) (bool, error) { return false, nil }
