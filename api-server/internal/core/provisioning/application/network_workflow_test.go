package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

type networkWorkflowServerRepo struct {
	mu      sync.Mutex
	servers map[string]*serverdomain.Server
}

func (r *networkWorkflowServerRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	server, ok := r.servers[id]
	if !ok {
		return nil, serverdomain.ErrServerNotFound
	}
	copy := *server
	return &copy, nil
}

func (r *networkWorkflowServerRepo) FindBySource(context.Context, serverdomain.Source) (*serverdomain.Server, error) {
	return nil, serverdomain.ErrServerNotFound
}

func (r *networkWorkflowServerRepo) FindByHardware(context.Context, serverdomain.Hardware) ([]*serverdomain.Server, error) {
	return nil, nil
}

func (r *networkWorkflowServerRepo) List(context.Context, serverdomain.ListFilter) (serverdomain.ListResult, error) {
	return serverdomain.ListResult{}, nil
}

func (r *networkWorkflowServerRepo) Upsert(_ context.Context, server *serverdomain.Server) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *server
	r.servers[server.ID] = &copy
	return nil
}

func (r *networkWorkflowServerRepo) MarkAbsent(context.Context, string, time.Time) ([]string, error) {
	return nil, nil
}

func (r *networkWorkflowServerRepo) SetMembership(context.Context, string, *serverdomain.MembershipStatus) error {
	return nil
}

func (r *networkWorkflowServerRepo) SetGPUs(context.Context, string, []serverdomain.GPU) error {
	return nil
}

func (r *networkWorkflowServerRepo) SetDeployment(_ context.Context, id string, deployment *serverdomain.DeploymentStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	server, ok := r.servers[id]
	if !ok {
		return serverdomain.ErrServerNotFound
	}
	server.Deployment = deployment
	return nil
}

func (r *networkWorkflowServerRepo) SetDefaultUser(_ context.Context, id string, user string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	server, ok := r.servers[id]
	if !ok {
		return serverdomain.ErrServerNotFound
	}
	server.DefaultUser = user
	return nil
}

func (r *networkWorkflowServerRepo) CountByIntegration(context.Context, string) (int, error) {
	return 0, nil
}

func (r *networkWorkflowServerRepo) Delete(context.Context, string) error {
	return nil
}

type networkWorkflowFactory struct {
	provider provisioningdomain.OSProvisioningProvider
}

func (f networkWorkflowFactory) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

type networkWorkflowTemplateRepo struct {
	template *provisioningdomain.DeploymentTemplate
	userData string
}

func (r *networkWorkflowTemplateRepo) Create(context.Context, *provisioningdomain.DeploymentTemplate, string) error {
	return nil
}

func (r *networkWorkflowTemplateRepo) FindByID(_ context.Context, id string) (*provisioningdomain.DeploymentTemplate, error) {
	if r.template == nil || r.template.ID != id {
		return nil, provisioningdomain.ErrDeploymentTemplateNotFound
	}
	copy := *r.template
	return &copy, nil
}

func (r *networkWorkflowTemplateRepo) List(context.Context, provisioningdomain.DeploymentTemplateFilter) ([]*provisioningdomain.DeploymentTemplate, error) {
	return nil, nil
}

func (r *networkWorkflowTemplateRepo) Update(context.Context, *provisioningdomain.DeploymentTemplate) error {
	return nil
}

func (r *networkWorkflowTemplateRepo) Delete(context.Context, string) error { return nil }

func (r *networkWorkflowTemplateRepo) ReplaceUserData(_ context.Context, _ string, userData string) error {
	r.userData = userData
	return nil
}

func (r *networkWorkflowTemplateRepo) ClearUserData(context.Context, string) error {
	r.userData = ""
	return nil
}

func (r *networkWorkflowTemplateRepo) UserData(_ context.Context, id string) (string, error) {
	if r.template == nil || r.template.ID != id {
		return "", provisioningdomain.ErrDeploymentTemplateNotFound
	}
	if r.userData == "" {
		return "", provisioningdomain.ErrDeploymentTemplateUserDataMissing
	}
	return r.userData, nil
}

func (r *networkWorkflowTemplateRepo) CountByIntegration(context.Context, string) (int, error) {
	return 0, nil
}

type networkWorkflowProvider struct {
	mu                sync.Mutex
	networks          map[string]*provisioningdomain.MachineNetwork
	configureRequests []provisioningdomain.NetworkLinkRequest
	deployRequests    []provisioningdomain.DeployRequest
	configureFailures map[string]error
	deployFailures    map[string]error
	configureDelay    time.Duration
	activeConfigures  int
	maxConfigures     int
	releaseResult     *provisioningdomain.Machine
	releaseErr        error
	releaseCalls      int
	unlinked          []string
	locked            bool
	// inspectionReason, when set, flags every machine as needing a new inspection.
	inspectionReason string
	ephemeralDeploy  bool
}

func (p *networkWorkflowProvider) Name() string { return "test" }

func (p *networkWorkflowProvider) Probe(context.Context) (provisioningdomain.ProviderInfo, error) {
	return provisioningdomain.ProviderInfo{Name: "test"}, nil
}

func (p *networkWorkflowProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{NetworkConfiguration: true, EphemeralDeploy: p.ephemeralDeploy}
}

func (p *networkWorkflowProvider) ListMachines(context.Context, provisioningdomain.MachineFilter) ([]*provisioningdomain.Machine, error) {
	return nil, nil
}

func (p *networkWorkflowProvider) GetMachine(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return &provisioningdomain.Machine{
		ID:                       machineID,
		Status:                   provisioningdomain.MachineStatusReady,
		PowerState:               provisioningdomain.PowerStateOff,
		IPAddresses:              []string{"192.0.2.20"},
		Locked:                   p.locked,
		InspectionRequired:       p.inspectionReason != "",
		InspectionRequiredReason: p.inspectionReason,
	}, nil
}

func (p *networkWorkflowProvider) ListOSImages(context.Context) ([]*provisioningdomain.OSImage, error) {
	return []*provisioningdomain.OSImage{{
		ID:           "ubuntu/noble",
		Name:         "Ubuntu 24.04",
		OSSystem:     "ubuntu",
		Release:      "noble",
		Architecture: "amd64",
		Complete:     true,
	}}, nil
}

func (p *networkWorkflowProvider) Deploy(_ context.Context, req provisioningdomain.DeployRequest) (*provisioningdomain.Machine, error) {
	p.mu.Lock()
	p.deployRequests = append(p.deployRequests, req)
	err := p.deployFailures[req.MachineID]
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &provisioningdomain.Machine{
		ID:             req.MachineID,
		Status:         provisioningdomain.MachineStatusDeploying,
		ProviderStatus: "Deploying",
		PowerState:     provisioningdomain.PowerStateOff,
		OSSystem:       req.OSSystem,
		DistroSeries:   req.DistroSeries,
	}, nil
}

func (p *networkWorkflowProvider) Release(context.Context, string) (*provisioningdomain.Machine, error) {
	p.releaseCalls++
	if p.releaseErr != nil {
		return nil, p.releaseErr
	}
	if p.releaseResult == nil {
		return nil, errors.New("not used")
	}
	return p.releaseResult, nil
}

func (p *networkWorkflowProvider) ListNetworkSubnets(context.Context) ([]provisioningdomain.NetworkSubnet, error) {
	return nil, nil
}

func (p *networkWorkflowProvider) InspectNetwork(_ context.Context, machineID string) (*provisioningdomain.MachineNetwork, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	network, ok := p.networks[machineID]
	if !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}
	return network, nil
}

func (p *networkWorkflowProvider) ConfigureNetworkLink(_ context.Context, machineID string, req provisioningdomain.NetworkLinkRequest) (*provisioningdomain.MachineNetwork, error) {
	p.mu.Lock()
	p.activeConfigures++
	if p.activeConfigures > p.maxConfigures {
		p.maxConfigures = p.activeConfigures
	}
	p.configureRequests = append(p.configureRequests, req)
	err := p.configureFailures[machineID]
	delay := p.configureDelay
	p.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}

	p.mu.Lock()
	p.activeConfigures--
	network := p.networks[machineID]
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return network, nil
}

func (p *networkWorkflowProvider) UnlinkNetwork(
	_ context.Context,
	machineID, interfaceID, linkID string,
) (*provisioningdomain.MachineNetwork, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unlinked = append(p.unlinked, machineID+"/"+interfaceID+"/"+linkID)
	network, ok := p.networks[machineID]
	if !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}
	for interfaceIndex := range network.Interfaces {
		iface := &network.Interfaces[interfaceIndex]
		if iface.ID != interfaceID {
			continue
		}
		links := iface.Links[:0]
		for _, link := range iface.Links {
			if link.ID != linkID {
				links = append(links, link)
			}
		}
		iface.Links = links
	}
	return network, nil
}

type networkWorkflowTaskRepo struct {
	created *provisioningdomain.ProvisioningTask
	current *provisioningdomain.ProvisioningTask
}

func cloneProvisioningTask(task *provisioningdomain.ProvisioningTask) *provisioningdomain.ProvisioningTask {
	if task == nil {
		return nil
	}
	copy := *task
	copy.Snapshot = append([]provisioningdomain.StaticNetworkLinkSnapshot(nil), task.Snapshot...)
	return &copy
}

func (r *networkWorkflowTaskRepo) Create(_ context.Context, task *provisioningdomain.ProvisioningTask) error {
	r.created = cloneProvisioningTask(task)
	r.current = cloneProvisioningTask(task)
	return nil
}

func (r *networkWorkflowTaskRepo) FindByID(_ context.Context, id string) (*provisioningdomain.ProvisioningTask, error) {
	if r.current == nil || r.current.ID != id {
		return nil, provisioningdomain.ErrProvisioningTaskNotFound
	}
	return cloneProvisioningTask(r.current), nil
}

func (r *networkWorkflowTaskRepo) ListByServer(_ context.Context, serverID string) ([]*provisioningdomain.ProvisioningTask, error) {
	if r.current == nil || r.current.ServerID != serverID {
		return nil, nil
	}
	return []*provisioningdomain.ProvisioningTask{cloneProvisioningTask(r.current)}, nil
}

func (r *networkWorkflowTaskRepo) Save(_ context.Context, task *provisioningdomain.ProvisioningTask) error {
	r.current = cloneProvisioningTask(task)
	return nil
}

func (r *networkWorkflowTaskRepo) Retry(_ context.Context, id string, now time.Time) error {
	if r.current == nil || r.current.ID != id {
		return provisioningdomain.ErrProvisioningTaskNotFound
	}
	if r.current.Status != provisioningdomain.ProvisioningTaskFailed {
		return provisioningdomain.ErrProvisioningTaskConflict
	}
	r.current.Status = provisioningdomain.ProvisioningTaskPending
	r.current.Error = ""
	r.current.Attempt++
	r.current.UpdatedAt = now
	return nil
}

// recordingCleanupRetrier records which release Workflow and Server a cleanup retry went to.
type recordingCleanupRetrier struct {
	workflowID, serverID string
	err                  error
}

func (r *recordingCleanupRetrier) RetryReleaseCleanup(_ context.Context, workflowID, serverID string) error {
	r.workflowID, r.serverID = workflowID, serverID
	return r.err
}

func workflowServer(id string) *serverdomain.Server {
	return &serverdomain.Server{
		ID: id,
		Source: serverdomain.Source{
			SiteID:            "site-a",
			IntegrationID:     "provider-a",
			ProviderMachineID: "machine-" + id,
		},
		Provisioning: &serverdomain.ProvisioningStatus{
			State:         string(provisioningdomain.MachineStatusReady),
			IntegrationID: "provider-a",
		},
	}
}

func workflowNetwork(machineID string, ambiguous bool) *provisioningdomain.MachineNetwork {
	subnets := []provisioningdomain.NetworkSubnet{{
		ID: "subnet-a", Name: "primary", CIDR: "192.0.2.0/24", Managed: true, ProvisionerDHCP: true,
	}}
	links := []provisioningdomain.NetworkLink{{
		ID: "link-" + machineID, State: provisioningdomain.NetworkStateProviderManaged,
		ProviderMode: "AUTO", SubnetID: "subnet-a", CIDR: "192.0.2.0/24",
	}}
	if ambiguous {
		subnets = append(subnets, provisioningdomain.NetworkSubnet{
			ID: "subnet-b", Name: "secondary", CIDR: "198.51.100.0/24", Managed: true, ProvisionerDHCP: true,
		})
		links = nil
	}
	return &provisioningdomain.MachineNetwork{
		MachineID: machineID,
		Interfaces: []provisioningdomain.NetworkInterface{{
			ID: "nic-" + machineID, Name: "eno1", Boot: true,
			Links: links, AvailableSubnets: subnets,
		}},
	}
}

// siteDHCPNetwork is one boot NIC on a subnet whose DHCP the provisioner does not serve, as on
// a Boot ISO network; linked says whether the NIC already has a link there. The suggestion must
// stay Automatic on it (decision 058): the conflict with the site's DHCP pool is the client's
// warning, and DHCP is the user's choice.
func siteDHCPNetwork(linked bool) *provisioningdomain.MachineNetwork {
	iface := provisioningdomain.NetworkInterface{
		ID: "nic-site", Name: "enp1s0", Boot: true,
		AvailableSubnets: []provisioningdomain.NetworkSubnet{{
			ID: "subnet-site", Name: "site", CIDR: "10.140.168.0/23", Managed: true,
		}},
	}
	if linked {
		iface.Links = []provisioningdomain.NetworkLink{{
			ID: "link-site", State: provisioningdomain.NetworkStateProviderManaged,
			ProviderMode: "AUTO", SubnetID: "subnet-site", CIDR: "10.140.168.0/23",
		}}
	}
	return &provisioningdomain.MachineNetwork{MachineID: "machine-site", Interfaces: []provisioningdomain.NetworkInterface{iface}}
}

func TestSuggestNetworkUsesStaticBindingOrAutomaticFallback(t *testing.T) {
	tests := []struct {
		name    string
		network *provisioningdomain.MachineNetwork
		want    NetworkSuggestion
	}{
		{
			name:    "provider managed defaults to automatic",
			network: workflowNetwork("machine-a", false),
			want: NetworkSuggestion{
				Mode:        provisioningdomain.DeploymentNetworkAutomatic,
				InterfaceID: "nic-machine-a",
				SubnetID:    "subnet-a",
			},
		},
		{
			name:    "linked subnet without provisioner DHCP stays automatic",
			network: siteDHCPNetwork(true),
			want: NetworkSuggestion{
				Mode:        provisioningdomain.DeploymentNetworkAutomatic,
				InterfaceID: "nic-site",
				SubnetID:    "subnet-site",
			},
		},
		{
			name:    "sole managed subnet without provisioner DHCP stays automatic",
			network: siteDHCPNetwork(false),
			want: NetworkSuggestion{
				Mode:        provisioningdomain.DeploymentNetworkAutomatic,
				InterfaceID: "nic-site",
				SubnetID:    "subnet-site",
			},
		},
		{
			name:    "no subnet choice stays automatic",
			network: workflowNetwork("machine-c", true),
			want: NetworkSuggestion{
				Mode:        provisioningdomain.DeploymentNetworkAutomatic,
				InterfaceID: "nic-machine-c",
			},
		},
		{
			name: "explicit static binding is preserved",
			network: &provisioningdomain.MachineNetwork{
				MachineID: "machine-b",
				Interfaces: []provisioningdomain.NetworkInterface{{
					ID: "nic-machine-b", Name: "eno1", Boot: true,
					Links: []provisioningdomain.NetworkLink{{
						ID: "link-machine-b", State: provisioningdomain.NetworkStateStatic,
						ProviderMode: "STATIC", SubnetID: "subnet-a",
						IPAddress: "192.0.2.42", DefaultGateway: true,
					}},
				}},
			},
			want: NetworkSuggestion{
				Mode:           provisioningdomain.DeploymentNetworkStatic,
				InterfaceID:    "nic-machine-b",
				SubnetID:       "subnet-a",
				IPAddress:      "192.0.2.42",
				DefaultGateway: true,
			},
		},
		{
			name: "explicit static binding on a subnet without provisioner DHCP stays static",
			network: func() *provisioningdomain.MachineNetwork {
				network := siteDHCPNetwork(false)
				network.Interfaces[0].Links = []provisioningdomain.NetworkLink{{
					ID: "link-site", State: provisioningdomain.NetworkStateStatic,
					ProviderMode: "STATIC", SubnetID: "subnet-site", IPAddress: "10.140.168.40",
				}}
				return network
			}(),
			want: NetworkSuggestion{
				Mode:        provisioningdomain.DeploymentNetworkStatic,
				InterfaceID: "nic-site",
				SubnetID:    "subnet-site",
				IPAddress:   "10.140.168.40",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := suggestNetwork(test.network); got != test.want {
				t.Fatalf("suggestNetwork() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func setupNetworkWorkflow(count int) (*DeployServersUseCase, *networkWorkflowProvider) {
	repo := &networkWorkflowServerRepo{servers: make(map[string]*serverdomain.Server)}
	provider := &networkWorkflowProvider{
		networks:          make(map[string]*provisioningdomain.MachineNetwork),
		configureFailures: make(map[string]error),
		deployFailures:    make(map[string]error),
	}
	for index := 0; index < count; index++ {
		id := string(rune('a' + index))
		server := workflowServer(id)
		repo.servers[id] = server
		provider.networks[server.Source.ProviderMachineID] = workflowNetwork(server.Source.ProviderMachineID, false)
	}
	return NewDeployServersUseCase(repo, nil, networkWorkflowFactory{provider: provider}, nil, &stubDeploymentKeys{}), provider
}

func TestDeployServersDefaultsToAutomaticAndReportsFailureStage(t *testing.T) {
	uc, provider := setupNetworkWorkflow(2)
	provider.deployFailures["machine-b"] = &provisioningdomain.ProviderError{
		Kind: provisioningdomain.ProviderErrorRejected, Detail: "reservation changed",
	}
	imageID := "ubuntu/noble"

	result, err := uc.Execute(context.Background(), DeployServersInput{
		ServerIDs: []string{"a", "b"},
		Settings:  DeploymentSettingsInput{ImageID: &imageID},
	})
	if err != nil {
		t.Fatalf("deploy batch: %v", err)
	}
	if result.Requested != 2 || len(result.Accepted) != 1 || len(result.Failed) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Failed[0].Stage != "deployment" || result.Failed[0].Message != "reservation changed" {
		t.Fatalf("unexpected failure: %#v", result.Failed[0])
	}
	for _, req := range provider.configureRequests {
		if req.Mode != provisioningdomain.NetworkLinkAuto || req.SubnetID != "subnet-a" {
			t.Fatalf("expected provider auto-assign on subnet-a, got %#v", req)
		}
	}
}

// DHCP intent (decision 058) replaces the NIC's existing link with a DHCP link on the same subnet,
// with no address and no gateway, instead of collapsing into provider auto-assign.
func TestDeployServersWithDHCPConfiguresDHCPLink(t *testing.T) {
	uc, provider := setupNetworkWorkflow(1)
	imageID := "ubuntu/noble"

	result, err := uc.Execute(context.Background(), DeployServersInput{
		ServerIDs: []string{"a"},
		Settings:  DeploymentSettingsInput{ImageID: &imageID},
		Network:   &DeploymentNetworkInput{Mode: "DHCP"},
	})
	if err != nil {
		t.Fatalf("deploy batch: %v", err)
	}
	if len(result.Accepted) != 1 || len(provider.configureRequests) != 1 {
		t.Fatalf("accepted = %d, configure requests = %d, want 1 and 1", len(result.Accepted), len(provider.configureRequests))
	}
	req := provider.configureRequests[0]
	if req.Mode != provisioningdomain.NetworkLinkDHCP || req.SubnetID != "subnet-a" || req.LinkID != "link-machine-a" ||
		req.IPAddress != "" || req.DefaultGateway {
		t.Errorf("configure request = %#v, want a DHCP link replacing link-machine-a on subnet-a", req)
	}
}

// stubDeploymentKeys records the Integration it was asked to register keys in and returns err;
// missing makes HasDeploymentKey report that the installation has no Deployment Key.
type stubDeploymentKeys struct {
	integrations []string
	err          error
	missing      bool
}

func (s *stubDeploymentKeys) HasDeploymentKey(context.Context) (bool, error) {
	return !s.missing, nil
}

func (s *stubDeploymentKeys) EnsureRegistered(_ context.Context, integrationID string) error {
	s.integrations = append(s.integrations, integrationID)
	return s.err
}

// TestDeployServersRequiresDeploymentKey guards decision 039: without a Deployment Key both the
// durable acceptance and a direct batch are refused before any provider read or write.
func TestDeployServersRequiresDeploymentKey(t *testing.T) {
	imageID := "ubuntu/noble"
	input := DeployServersInput{ServerIDs: []string{"a"}, Settings: DeploymentSettingsInput{ImageID: &imageID}}
	uc, provider := setupNetworkWorkflow(1)
	keys := &stubDeploymentKeys{missing: true}
	uc.keys = keys

	if _, _, err := uc.ResolveOperationInput(context.Background(), input); !errors.Is(err, provisioningdomain.ErrDeploymentKeyMissing) {
		t.Errorf("ResolveOperationInput() error = %v, want ErrDeploymentKeyMissing", err)
	}
	if _, err := uc.Execute(context.Background(), input); !errors.Is(err, provisioningdomain.ErrDeploymentKeyMissing) {
		t.Errorf("Execute() error = %v, want ErrDeploymentKeyMissing", err)
	}
	if len(keys.integrations) != 0 || len(provider.configureRequests) != 0 || len(provider.deployRequests) != 0 {
		t.Errorf("a missing Deployment Key must stop the deployment before registration or any provider write")
	}
}

// TestDeployServersEnsuresSSHKeysBeforeAnyWrite guards decision 039: a batch must make the
// provisioner hold the Deployment Key before its first provider write, and must not deploy at all
// when that fails, because the deployed Server would not authorize swallow.
func TestDeployServersEnsuresSSHKeysBeforeAnyWrite(t *testing.T) {
	imageID := "ubuntu/noble"
	input := DeployServersInput{ServerIDs: []string{"a"}, Settings: DeploymentSettingsInput{ImageID: &imageID}}

	uc, provider := setupNetworkWorkflow(1)
	keys := &stubDeploymentKeys{err: errors.New("MAAS refused the key")}
	uc.keys = keys
	if _, err := uc.Execute(context.Background(), input); !errors.Is(err, provisioningdomain.ErrSSHKeyRegistration) {
		t.Fatalf("Execute() error = %v, want ErrSSHKeyRegistration", err)
	}
	if len(provider.configureRequests) != 0 || len(provider.deployRequests) != 0 {
		t.Fatalf("a failed key registration must stop the batch before any provider write")
	}

	keys.err = nil
	if _, err := uc.Execute(context.Background(), input); err != nil {
		t.Fatalf("Execute() after registration recovered: %v", err)
	}
	if len(keys.integrations) != 2 || keys.integrations[1] != "provider-a" {
		t.Errorf("EnsureRegistered calls = %v, want the targets' integration provider-a", keys.integrations)
	}
	if len(provider.deployRequests) != 1 {
		t.Errorf("deploy requests = %d, want 1 once the key is registered", len(provider.deployRequests))
	}
}

func TestResolveOperationInputFreezesTemplateIntent(t *testing.T) {
	base, provider := setupNetworkWorkflow(1)
	templates := &networkWorkflowTemplateRepo{
		template: &provisioningdomain.DeploymentTemplate{
			ID: "template-a", IntegrationID: "provider-a", ImageID: "ubuntu/noble",
			NetworkMode: provisioningdomain.DeploymentNetworkDHCP, HasUserData: true,
		},
		userData: "#cloud-config\nhostname: frozen",
	}
	uc := NewDeployServersUseCase(base.servers, templates, base.providers, nil, &stubDeploymentKeys{})

	frozen, secret, err := uc.ResolveOperationInput(context.Background(), DeployServersInput{
		ServerIDs: []string{"a"}, TemplateID: "template-a",
		UserData: DeploymentUserDataInput{Mode: "inherit"},
	})
	if err != nil {
		t.Fatalf("resolve operation input: %v", err)
	}
	if frozen.TemplateID != "" || frozen.Settings.ImageID == nil || *frozen.Settings.ImageID != "ubuntu/noble" {
		t.Fatalf("template dependency was not frozen: %#v", frozen)
	}
	if frozen.UserData.Mode != "replace" || frozen.UserData.Value != "" || secret != templates.userData {
		t.Fatalf("secret was not separated from durable intent: input=%#v secret=%q", frozen.UserData, secret)
	}
	if frozen.Network == nil || frozen.Network.Mode != "dhcp" || len(frozen.Network.Assignments) != 1 ||
		frozen.Network.Assignments[0].InterfaceID != "nic-machine-a" ||
		frozen.Network.Assignments[0].SubnetID != "subnet-a" {
		t.Fatalf("network intent was not frozen: %#v", frozen.Network)
	}

	templates.template.ImageID = "ubuntu/jammy"
	templates.userData = "changed"
	if *frozen.Settings.ImageID != "ubuntu/noble" || secret != "#cloud-config\nhostname: frozen" {
		t.Fatal("resolved Operation intent changed with its source template")
	}
	if len(provider.configureRequests) != 0 || len(provider.deployRequests) != 0 {
		t.Fatal("resolving durable intent performed provider writes")
	}
}

// TestResolveOperationInputKeepsTemplateModeWhenNetworkOmitsIt guards a request whose network
// intent has no mode, as a Platform's provision_os sends when it only adds per-target assignments:
// the mode stays the Template's, or Automatic without one, and the frozen intent records that
// resolved mode. An explicit mode still replaces the Template's.
func TestResolveOperationInputKeepsTemplateModeWhenNetworkOmitsIt(t *testing.T) {
	tests := []struct {
		name         string
		templateMode provisioningdomain.DeploymentNetworkMode // empty deploys without a Template
		network      DeploymentNetworkInput
		wantMode     string
		wantIP       string
	}{
		{
			name:         "static template keeps static and takes the request's static IP",
			templateMode: provisioningdomain.DeploymentNetworkStatic,
			network: DeploymentNetworkInput{
				Assignments: []DeploymentNetworkAssignmentInput{{ServerID: "a", IPAddress: "192.0.2.40"}},
			},
			wantMode: "static",
			wantIP:   "192.0.2.40",
		},
		{
			name:         "blank mode keeps a dhcp template",
			templateMode: provisioningdomain.DeploymentNetworkDHCP,
			network:      DeploymentNetworkInput{Mode: "  "},
			wantMode:     "dhcp",
		},
		{
			name:         "explicit mode replaces the template's",
			templateMode: provisioningdomain.DeploymentNetworkDHCP,
			network:      DeploymentNetworkInput{Mode: "Automatic"},
			wantMode:     "automatic",
		},
		{
			name:     "no template resolves to automatic",
			network:  DeploymentNetworkInput{SubnetID: "subnet-a"},
			wantMode: "automatic",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base, _ := setupNetworkWorkflow(1)
			imageID := "ubuntu/noble"
			input := DeployServersInput{ServerIDs: []string{"a"}, Network: &tc.network}
			templates := &networkWorkflowTemplateRepo{}
			if tc.templateMode != "" {
				templates.template = &provisioningdomain.DeploymentTemplate{
					ID: "template-a", IntegrationID: "provider-a", ImageID: imageID, NetworkMode: tc.templateMode,
				}
				input.TemplateID = "template-a"
			} else {
				input.Settings.ImageID = &imageID
			}
			uc := NewDeployServersUseCase(base.servers, templates, base.providers, nil, &stubDeploymentKeys{})

			frozen, _, err := uc.ResolveOperationInput(context.Background(), input)
			if err != nil {
				t.Fatalf("ResolveOperationInput(network %+v) error = %v", tc.network, err)
			}
			if frozen.Network == nil || len(frozen.Network.Assignments) != 1 {
				t.Fatalf("ResolveOperationInput(network %+v) network = %#v, want one frozen assignment", tc.network, frozen.Network)
			}
			if got := frozen.Network.Mode; got != tc.wantMode {
				t.Errorf("ResolveOperationInput(network %+v) mode = %q, want %q", tc.network, got, tc.wantMode)
			}
			if got := frozen.Network.Assignments[0].IPAddress; got != tc.wantIP {
				t.Errorf("ResolveOperationInput(network %+v) ipAddress = %q, want %q", tc.network, got, tc.wantIP)
			}
		})
	}
}

// TestResolveOperationInputPreservesVerificationRun guards the verify-os-image workflow: the
// VerificationRun bypass must survive into the frozen durable input, or the executor's re-resolve
// of the proving deploy re-applies the unverified-custom gate and blocks the very run that would
// establish the verification.
func TestResolveOperationInputPreservesVerificationRun(t *testing.T) {
	base, _ := setupNetworkWorkflow(1)
	uc := NewDeployServersUseCase(base.servers, nil, base.providers, nil, &stubDeploymentKeys{})
	imageID := "ubuntu/noble"

	frozen, _, err := uc.ResolveOperationInput(context.Background(), DeployServersInput{
		ServerIDs:       []string{"a"},
		Settings:        DeploymentSettingsInput{ImageID: &imageID},
		VerificationRun: true,
	})
	if err != nil {
		t.Fatalf("resolve operation input: %v", err)
	}
	if !frozen.VerificationRun {
		t.Error("VerificationRun must be frozen into the durable input so the executor's re-resolve bypasses the unverified-custom gate")
	}
}

func TestDeployServersReportsNetworkFailureWithoutRollingBackOtherTargets(t *testing.T) {
	uc, provider := setupNetworkWorkflow(2)
	provider.configureFailures["machine-b"] = &provisioningdomain.ProviderError{
		Kind: provisioningdomain.ProviderErrorRejected, Detail: "address is no longer available",
	}
	imageID := "ubuntu/noble"

	result, err := uc.Execute(context.Background(), DeployServersInput{
		ServerIDs: []string{"a", "b"},
		Settings:  DeploymentSettingsInput{ImageID: &imageID},
	})
	if err != nil {
		t.Fatalf("deploy batch: %v", err)
	}
	if len(result.Accepted) != 1 || len(result.Failed) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Failed[0].ServerID != "b" ||
		result.Failed[0].Stage != "network_configuration" ||
		result.Failed[0].Message != "address is no longer available" {
		t.Fatalf("unexpected network failure: %#v", result.Failed[0])
	}
	if len(provider.deployRequests) != 1 || provider.deployRequests[0].MachineID != "machine-a" {
		t.Fatalf("deployment requests = %#v, want only machine-a", provider.deployRequests)
	}
}

func TestDeployServersRejectsDuplicateStaticIPsBeforeAnyWrite(t *testing.T) {
	uc, provider := setupNetworkWorkflow(2)
	imageID := "ubuntu/noble"

	_, err := uc.Execute(context.Background(), DeployServersInput{
		ServerIDs: []string{"a", "b"},
		Settings:  DeploymentSettingsInput{ImageID: &imageID},
		Network: &DeploymentNetworkInput{
			Mode: "static",
			Assignments: []DeploymentNetworkAssignmentInput{
				{ServerID: "a", IPAddress: "192.0.2.40"},
				{ServerID: "b", IPAddress: "192.0.2.40"},
			},
		},
	})
	if !errors.Is(err, provisioningdomain.ErrInvalidDeploymentBatch) {
		t.Fatalf("expected invalid batch, got %v", err)
	}
	if len(provider.configureRequests) != 0 || len(provider.deployRequests) != 0 {
		t.Fatalf("invalid Static batch performed writes: configure=%d deploy=%d",
			len(provider.configureRequests), len(provider.deployRequests))
	}
}

func TestDeployServersRejectsMoreThanOneHundredTargets(t *testing.T) {
	uc, provider := setupNetworkWorkflow(0)
	imageID := "ubuntu/noble"
	serverIDs := make([]string, maxDeploymentTargets+1)
	_, err := uc.Execute(context.Background(), DeployServersInput{
		ServerIDs: serverIDs,
		Settings:  DeploymentSettingsInput{ImageID: &imageID},
	})
	if !errors.Is(err, provisioningdomain.ErrInvalidDeploymentBatch) {
		t.Fatalf("expected target limit validation, got %v", err)
	}
	if len(provider.configureRequests) != 0 || len(provider.deployRequests) != 0 {
		t.Fatal("oversized batch performed provider writes")
	}
}

func TestDeployServersCompletesNetworkPreflightBeforeAnyWrite(t *testing.T) {
	uc, provider := setupNetworkWorkflow(2)
	provider.networks["machine-b"] = workflowNetwork("machine-b", true)
	imageID := "ubuntu/noble"

	_, err := uc.Execute(context.Background(), DeployServersInput{
		ServerIDs: []string{"a", "b"},
		Settings:  DeploymentSettingsInput{ImageID: &imageID},
	})
	if !errors.Is(err, provisioningdomain.ErrDeploymentBatchConflict) {
		t.Fatalf("expected batch conflict, got %v", err)
	}
	if len(provider.configureRequests) != 0 || len(provider.deployRequests) != 0 {
		t.Fatalf("preflight performed writes: configure=%d deploy=%d", len(provider.configureRequests), len(provider.deployRequests))
	}
}

func TestDeployServersLimitsNetworkDispatchToFourWorkers(t *testing.T) {
	uc, provider := setupNetworkWorkflow(12)
	provider.configureDelay = 15 * time.Millisecond
	imageID := "ubuntu/noble"
	serverIDs := make([]string, 12)
	for index := range serverIDs {
		serverIDs[index] = string(rune('a' + index))
	}

	result, err := uc.Execute(context.Background(), DeployServersInput{
		ServerIDs: serverIDs,
		Settings:  DeploymentSettingsInput{ImageID: &imageID},
	})
	if err != nil {
		t.Fatalf("deploy batch: %v", err)
	}
	if len(result.Accepted) != len(serverIDs) {
		t.Fatalf("accepted %d of %d", len(result.Accepted), len(serverIDs))
	}
	if provider.maxConfigures < 2 || provider.maxConfigures > deploymentWorkers {
		t.Fatalf("network configure concurrency = %d, want 2..%d", provider.maxConfigures, deploymentWorkers)
	}
}

func TestStaticSnapshotMatchRequiresExactUnchangedBinding(t *testing.T) {
	network := workflowNetwork("machine-a", false)
	network.Interfaces[0].Links[0].State = provisioningdomain.NetworkStateStatic
	network.Interfaces[0].Links[0].IPAddress = "192.0.2.30"
	snapshot := staticLinkSnapshot(network)
	if len(snapshot) != 1 || !staticSnapshotStillMatches(network, snapshot[0]) {
		t.Fatalf("expected exact static snapshot to match: %#v", snapshot)
	}
	network.Interfaces[0].Links[0].IPAddress = "192.0.2.31"
	if staticSnapshotStillMatches(network, snapshot[0]) {
		t.Fatal("changed address must be preserved instead of matching cleanup snapshot")
	}
}

func TestDeployServersRejectsDefaultGatewayForDHCPBeforeAnyWrite(t *testing.T) {
	uc, provider := setupNetworkWorkflow(1)
	imageID := "ubuntu/noble"

	_, err := uc.Execute(context.Background(), DeployServersInput{
		ServerIDs: []string{"a"},
		Settings:  DeploymentSettingsInput{ImageID: &imageID},
		Network: &DeploymentNetworkInput{
			Mode:           "dhcp",
			DefaultGateway: true,
		},
	})
	if !errors.Is(err, provisioningdomain.ErrInvalidDeploymentBatch) {
		t.Fatalf("expected invalid DHCP gateway intent, got %v", err)
	}
	if len(provider.configureRequests) != 0 || len(provider.deployRequests) != 0 {
		t.Fatal("invalid DHCP gateway intent performed provider writes")
	}
}

func TestReleaseSchedulesCleanupOnlyAfterProviderAcceptance(t *testing.T) {
	repo := &networkWorkflowServerRepo{servers: map[string]*serverdomain.Server{
		"a": workflowServer("a"),
	}}
	network := workflowNetwork("machine-a", false)
	network.Interfaces[0].Links[0].State = provisioningdomain.NetworkStateStatic
	network.Interfaces[0].Links[0].ProviderMode = "STATIC"
	network.Interfaces[0].Links[0].IPAddress = "192.0.2.30"
	provider := &networkWorkflowProvider{
		networks:          map[string]*provisioningdomain.MachineNetwork{"machine-a": network},
		configureFailures: map[string]error{},
		deployFailures:    map[string]error{},
		releaseResult: &provisioningdomain.Machine{
			ID: "machine-a", Status: provisioningdomain.MachineStatusReady,
			ProviderStatus: "Ready", IPAddresses: []string{"192.0.2.30"},
		},
	}
	tasks := &networkWorkflowTaskRepo{}
	uc := NewReleaseServerUseCase(repo, networkWorkflowFactory{provider: provider}, tasks)
	started := time.Now().UTC()

	result, err := uc.ExecuteWithOptions(context.Background(), ReleaseServerInput{
		ServerID: "a", UnbindStaticIPs: true, RequestID: "request-a",
	})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if tasks.created == nil || tasks.current == nil || result.TaskID == "" {
		t.Fatalf("cleanup task was not persisted and returned: %#v", result)
	}
	if tasks.created.Phase != provisioningdomain.ProvisioningTaskWaitingForRelease || tasks.created.Attempt != 1 ||
		tasks.created.CreatedAt.Before(started) {
		t.Fatalf("fresh task = %#v, want waiting for release on its first attempt", tasks.created)
	}
	if tasks.current.Phase != provisioningdomain.ProvisioningTaskWaitingForReady {
		t.Fatalf("accepted release did not move cleanup to waiting for Ready: %#v", tasks.current)
	}
	if len(tasks.current.Snapshot) != 1 || tasks.current.Snapshot[0].IPAddress != "192.0.2.30" {
		t.Fatalf("unexpected static snapshot: %#v", tasks.current.Snapshot)
	}
}

func TestReleaseNetworkCleanupResumesExactStaticCleanup(t *testing.T) {
	server := workflowServer("a")
	repo := &networkWorkflowServerRepo{servers: map[string]*serverdomain.Server{"a": server}}
	network := workflowNetwork("machine-a", false)
	network.Interfaces[0].Links[0].State = provisioningdomain.NetworkStateStatic
	network.Interfaces[0].Links[0].ProviderMode = "STATIC"
	network.Interfaces[0].Links[0].IPAddress = "192.0.2.30"
	provider := &networkWorkflowProvider{
		networks:          map[string]*provisioningdomain.MachineNetwork{"machine-a": network},
		configureFailures: map[string]error{},
		deployFailures:    map[string]error{},
	}
	task := &provisioningdomain.ProvisioningTask{
		ID: "task-a", Kind: provisioningdomain.ProvisioningTaskReleaseNetworkCleanup,
		ServerID: "a", IntegrationID: "provider-a", ProviderMachineID: "machine-a",
		Status: provisioningdomain.ProvisioningTaskRunning,
		Phase:  provisioningdomain.ProvisioningTaskCleaningNetwork,
		Snapshot: []provisioningdomain.StaticNetworkLinkSnapshot{{
			InterfaceID: "nic-machine-a", LinkID: "link-machine-a",
			SubnetID: "subnet-a", IPAddress: "192.0.2.30",
		}},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	tasks := &networkWorkflowTaskRepo{current: cloneProvisioningTask(task)}
	cleanup := NewReleaseNetworkCleanup(tasks, repo, networkWorkflowFactory{provider: provider})

	if err := cleanup.Advance(context.Background(), cloneProvisioningTask(task)); err != nil {
		t.Fatalf("resume cleanup: %v", err)
	}
	if tasks.current.Status != provisioningdomain.ProvisioningTaskSucceeded ||
		tasks.current.Phase != provisioningdomain.ProvisioningTaskComplete {
		t.Fatalf("cleanup did not complete: %#v", tasks.current)
	}
	if len(provider.unlinked) != 1 || provider.unlinked[0] != "machine-a/nic-machine-a/link-machine-a" {
		t.Fatalf("unexpected unlink calls: %#v", provider.unlinked)
	}
}

func TestProvisioningTaskRetryRejectsUnacceptedRelease(t *testing.T) {
	repo := &networkWorkflowServerRepo{servers: map[string]*serverdomain.Server{
		"a": workflowServer("a"),
	}}
	task := &provisioningdomain.ProvisioningTask{
		ID:       "task-a",
		Kind:     provisioningdomain.ProvisioningTaskReleaseNetworkCleanup,
		ServerID: "a",
		Status:   provisioningdomain.ProvisioningTaskFailed,
		Phase:    provisioningdomain.ProvisioningTaskWaitingForRelease,
		Error:    "Release was not accepted.",
	}
	tasks := &networkWorkflowTaskRepo{current: cloneProvisioningTask(task)}
	retrier := &recordingCleanupRetrier{}
	service := NewProvisioningTaskService(tasks, repo, retrier, workflowMutationGuard{})

	if provisioningTaskItem(task).Retryable {
		t.Fatal("an unaccepted Release must not advertise cleanup retry")
	}
	_, err := service.Retry(context.Background(), task.ID)
	if !errors.Is(err, provisioningdomain.ErrProvisioningTaskConflict) {
		t.Fatalf("expected retry conflict, got %v", err)
	}
	if tasks.current.Status != provisioningdomain.ProvisioningTaskFailed || retrier.workflowID != "" {
		t.Fatalf("rejected retry changed task state or reached the Workflow: %#v", tasks.current)
	}
}

// A cleanup retry does not run the cleanup itself: it retries the release Task of the Workflow that
// owns it, so the cleanup runs again under that Workflow's lease.
func TestProvisioningTaskRetryRetriesTheOwningReleaseTask(t *testing.T) {
	repo := &networkWorkflowServerRepo{servers: map[string]*serverdomain.Server{
		"a": workflowServer("a"),
	}}
	task := &provisioningdomain.ProvisioningTask{
		ID: "task-a", Kind: provisioningdomain.ProvisioningTaskReleaseNetworkCleanup,
		ServerID: "a", Status: provisioningdomain.ProvisioningTaskFailed,
		Phase: provisioningdomain.ProvisioningTaskCleaningNetwork, RequestID: "workflow-a",
	}
	tasks := &networkWorkflowTaskRepo{current: cloneProvisioningTask(task)}
	retrier := &recordingCleanupRetrier{}
	service := NewProvisioningTaskService(tasks, repo, retrier, workflowMutationGuard{})

	if !provisioningTaskItem(task).Retryable {
		t.Fatal("a failed cleanup of an accepted Release must advertise retry")
	}
	if _, err := service.Retry(context.Background(), task.ID); err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if retrier.workflowID != "workflow-a" || retrier.serverID != "a" {
		t.Errorf("retry went to Workflow %q Server %q, want workflow-a and a", retrier.workflowID, retrier.serverID)
	}
	if tasks.current.Status != provisioningdomain.ProvisioningTaskFailed {
		t.Errorf("Retry changed the task itself: %#v", tasks.current)
	}
}

type workflowMutationGuard struct{ err error }

func (g workflowMutationGuard) RequireUnlocked(context.Context, []string) error { return g.err }

func TestReleaseNetworkCleanupFailsBeforeCleanupWhenExternallyLocked(t *testing.T) {
	server := workflowServer("a")
	repo := &networkWorkflowServerRepo{servers: map[string]*serverdomain.Server{"a": server}}
	network := workflowNetwork("machine-a", false)
	network.Interfaces[0].Links[0].State = provisioningdomain.NetworkStateStatic
	network.Interfaces[0].Links[0].ProviderMode = "STATIC"
	network.Interfaces[0].Links[0].IPAddress = "192.0.2.30"
	provider := &networkWorkflowProvider{
		networks:          map[string]*provisioningdomain.MachineNetwork{"machine-a": network},
		configureFailures: map[string]error{}, deployFailures: map[string]error{}, locked: true,
	}
	task := &provisioningdomain.ProvisioningTask{
		ID: "task-a", Kind: provisioningdomain.ProvisioningTaskReleaseNetworkCleanup,
		ServerID: "a", IntegrationID: "provider-a", ProviderMachineID: "machine-a",
		Status: provisioningdomain.ProvisioningTaskRunning,
		Phase:  provisioningdomain.ProvisioningTaskCleaningNetwork,
		Snapshot: []provisioningdomain.StaticNetworkLinkSnapshot{{
			InterfaceID: "nic-machine-a", LinkID: "link-machine-a",
			SubnetID: "subnet-a", IPAddress: "192.0.2.30",
		}},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	tasks := &networkWorkflowTaskRepo{current: cloneProvisioningTask(task)}
	cleanup := NewReleaseNetworkCleanup(tasks, repo, networkWorkflowFactory{provider: provider})

	if err := cleanup.Advance(context.Background(), cloneProvisioningTask(task)); err != nil {
		t.Fatalf("advance locked cleanup: %v", err)
	}
	if len(provider.unlinked) != 0 {
		t.Fatalf("locked cleanup changed network links: %v", provider.unlinked)
	}
	if tasks.current.Status != provisioningdomain.ProvisioningTaskFailed ||
		!strings.Contains(tasks.current.Error, "Unlock") {
		t.Fatalf("locked cleanup task = %#v, want actionable retryable failure", tasks.current)
	}
	if !repo.servers["a"].Provisioning.Locked {
		t.Fatal("external lock was not projected onto the Server")
	}
}

func TestProvisioningTaskRetryUsesLiveLockGuard(t *testing.T) {
	repo := &networkWorkflowServerRepo{servers: map[string]*serverdomain.Server{
		"a": workflowServer("a"),
	}}
	task := &provisioningdomain.ProvisioningTask{
		ID: "task-a", Kind: provisioningdomain.ProvisioningTaskReleaseNetworkCleanup,
		ServerID: "a", Status: provisioningdomain.ProvisioningTaskFailed,
		Phase: provisioningdomain.ProvisioningTaskCleaningNetwork,
	}
	tasks := &networkWorkflowTaskRepo{current: cloneProvisioningTask(task)}
	guardErr := &serverdomain.ServerLockedError{Name: "a"}
	service := NewProvisioningTaskService(tasks, repo, &recordingCleanupRetrier{}, workflowMutationGuard{err: guardErr})

	_, err := service.Retry(context.Background(), task.ID)
	if !errors.Is(err, serverdomain.ErrServerLocked) {
		t.Fatalf("Retry() error = %v, want ErrServerLocked", err)
	}
	if tasks.current.Status != provisioningdomain.ProvisioningTaskFailed {
		t.Fatalf("locked retry changed task state: %#v", tasks.current)
	}
}
