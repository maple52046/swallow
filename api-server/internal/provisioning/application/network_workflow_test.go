package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
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

func (r *networkWorkflowServerRepo) MarkAbsent(context.Context, string, time.Time) (int, error) {
	return 0, nil
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
	unlinked          []string
	locked            bool
}

func (p *networkWorkflowProvider) Name() string { return "test" }

func (p *networkWorkflowProvider) Probe(context.Context) (provisioningdomain.ProviderInfo, error) {
	return provisioningdomain.ProviderInfo{Name: "test"}, nil
}

func (p *networkWorkflowProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{NetworkConfiguration: true}
}

func (p *networkWorkflowProvider) ListMachines(context.Context, provisioningdomain.MachineFilter) ([]*provisioningdomain.Machine, error) {
	return nil, nil
}

func (p *networkWorkflowProvider) GetMachine(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return &provisioningdomain.Machine{
		ID:          machineID,
		Status:      provisioningdomain.MachineStatusReady,
		PowerState:  provisioningdomain.PowerStateOff,
		IPAddresses: []string{"192.0.2.20"},
		Locked:      p.locked,
	}, nil
}

func (p *networkWorkflowProvider) ListOSImages(context.Context) ([]*provisioningdomain.OSImage, error) {
	return []*provisioningdomain.OSImage{{
		ID:           "ubuntu/noble",
		Name:         "Ubuntu 24.04",
		OSSystem:     "ubuntu",
		Release:      "noble",
		Architecture: "amd64",
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

func (r *networkWorkflowTaskRepo) ClaimNext(
	context.Context,
	string,
	time.Time,
	time.Duration,
) (*provisioningdomain.ProvisioningTask, error) {
	return nil, provisioningdomain.ErrNoProvisioningTask
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
	r.current.NextRunAt = now
	r.current.UpdatedAt = now
	return nil
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
		ID: "subnet-a", Name: "primary", CIDR: "192.0.2.0/24", Managed: true,
	}}
	links := []provisioningdomain.NetworkLink{{
		ID: "link-" + machineID, State: provisioningdomain.NetworkStateProviderManaged,
		ProviderMode: "AUTO", SubnetID: "subnet-a", CIDR: "192.0.2.0/24",
	}}
	if ambiguous {
		subnets = append(subnets, provisioningdomain.NetworkSubnet{
			ID: "subnet-b", Name: "secondary", CIDR: "198.51.100.0/24", Managed: true,
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

func TestSuggestNetworkUsesStaticBindingOrDHCPFallback(t *testing.T) {
	tests := []struct {
		name    string
		network *provisioningdomain.MachineNetwork
		want    NetworkSuggestion
	}{
		{
			name:    "provider managed defaults to DHCP",
			network: workflowNetwork("machine-a", false),
			want: NetworkSuggestion{
				Mode:        provisioningdomain.DeploymentNetworkDHCP,
				InterfaceID: "nic-machine-a",
				SubnetID:    "subnet-a",
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
	return NewDeployServersUseCase(repo, nil, networkWorkflowFactory{provider: provider}), provider
}

func TestDeployServersDefaultsToDHCPAndReportsFailureStage(t *testing.T) {
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
		if req.Mode != provisioningdomain.NetworkLinkDHCP || req.SubnetID != "subnet-a" {
			t.Fatalf("expected explicit DHCP on subnet-a, got %#v", req)
		}
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
	uc := NewDeployServersUseCase(base.servers, templates, base.providers)

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
	if tasks.created.Phase != provisioningdomain.ProvisioningTaskWaitingForRelease ||
		tasks.created.NextRunAt.Before(started.Add(releaseDispatchRecoveryDelay-time.Second)) {
		t.Fatalf("fresh task could race provider dispatch: %#v", tasks.created)
	}
	if tasks.current.Phase != provisioningdomain.ProvisioningTaskWaitingForReady ||
		tasks.current.NextRunAt.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("accepted release did not schedule cleanup immediately: %#v", tasks.current)
	}
	if len(tasks.current.Snapshot) != 1 || tasks.current.Snapshot[0].IPAddress != "192.0.2.30" {
		t.Fatalf("unexpected static snapshot: %#v", tasks.current.Snapshot)
	}
}

func TestProvisioningTaskWorkerResumesExactStaticCleanup(t *testing.T) {
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
	worker := NewProvisioningTaskWorker(
		tasks, repo, networkWorkflowFactory{provider: provider}, time.Second, time.Minute)

	if err := worker.process(context.Background(), cloneProvisioningTask(task)); err != nil {
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
	service := NewProvisioningTaskService(tasks, repo)

	if provisioningTaskItem(task).Retryable {
		t.Fatal("an unaccepted Release must not advertise cleanup retry")
	}
	_, err := service.Retry(context.Background(), task.ID)
	if !errors.Is(err, provisioningdomain.ErrProvisioningTaskConflict) {
		t.Fatalf("expected retry conflict, got %v", err)
	}
	if tasks.current.Status != provisioningdomain.ProvisioningTaskFailed {
		t.Fatalf("rejected retry changed task state: %#v", tasks.current)
	}
}

type workflowMutationGuard struct{ err error }

func (g workflowMutationGuard) RequireUnlocked(context.Context, []string) error { return g.err }

func TestProvisioningTaskWorkerFailsBeforeCleanupWhenExternallyLocked(t *testing.T) {
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
	worker := NewProvisioningTaskWorker(
		tasks, repo, networkWorkflowFactory{provider: provider}, time.Second, time.Minute)

	if err := worker.process(context.Background(), cloneProvisioningTask(task)); err != nil {
		t.Fatalf("process locked cleanup: %v", err)
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
	service := NewProvisioningTaskService(tasks, repo, workflowMutationGuard{err: guardErr})

	_, err := service.Retry(context.Background(), task.ID)
	if !errors.Is(err, serverdomain.ErrServerLocked) {
		t.Fatalf("Retry() error = %v, want ErrServerLocked", err)
	}
	if tasks.current.Status != provisioningdomain.ProvisioningTaskFailed {
		t.Fatalf("locked retry changed task state: %#v", tasks.current)
	}
}
