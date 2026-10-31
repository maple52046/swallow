package app

import (
	"context"
	"testing"
	"time"

	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// cleanupProvider is a Ready Machine whose one static link a release recorded.
type cleanupProvider struct {
	cancellationProvider
	network  *provisioningdomain.MachineNetwork
	unlinked []string
}

func (p *cleanupProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{NetworkConfiguration: true}
}
func (p *cleanupProvider) ListNetworkSubnets(context.Context) ([]provisioningdomain.NetworkSubnet, error) {
	return nil, nil
}
func (p *cleanupProvider) InspectNetwork(context.Context, string) (*provisioningdomain.MachineNetwork, error) {
	return p.network, nil
}
func (p *cleanupProvider) ConfigureNetworkLink(context.Context, string, provisioningdomain.NetworkLinkRequest) (*provisioningdomain.MachineNetwork, error) {
	return p.network, nil
}
func (p *cleanupProvider) UnlinkNetwork(_ context.Context, _, interfaceID, linkID string) (*provisioningdomain.MachineNetwork, error) {
	p.unlinked = append(p.unlinked, interfaceID+"/"+linkID)
	p.network.Interfaces[0].Links = nil
	return p.network, nil
}

type cleanupProviderFactory struct{ provider *cleanupProvider }

func (f cleanupProviderFactory) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

// cleanupTaskRepo holds one cleanup task.
type cleanupTaskRepo struct {
	task *provisioningdomain.ProvisioningTask
}

func (r *cleanupTaskRepo) Create(_ context.Context, task *provisioningdomain.ProvisioningTask) error {
	r.task = task
	return nil
}
func (r *cleanupTaskRepo) FindByID(context.Context, string) (*provisioningdomain.ProvisioningTask, error) {
	copy := *r.task
	return &copy, nil
}
func (r *cleanupTaskRepo) ListByServer(context.Context, string) ([]*provisioningdomain.ProvisioningTask, error) {
	copy := *r.task
	return []*provisioningdomain.ProvisioningTask{&copy}, nil
}
func (r *cleanupTaskRepo) Save(_ context.Context, task *provisioningdomain.ProvisioningTask) error {
	copy := *task
	r.task = &copy
	return nil
}
func (r *cleanupTaskRepo) Retry(_ context.Context, _ string, now time.Time) error {
	r.task.Status, r.task.Error, r.task.UpdatedAt = provisioningdomain.ProvisioningTaskPending, "", now
	r.task.Attempt++
	return nil
}

// The release Task finishes the static IP cleanup itself once the Server is Ready, under its own
// lease, instead of waiting for a background queue to pick it up.
func TestReleaseTaskRunsItsNetworkCleanup(t *testing.T) {
	provider := &cleanupProvider{
		cancellationProvider: cancellationProvider{machine: &provisioningdomain.Machine{
			ID: "machine-a", Status: provisioningdomain.MachineStatusReady, ProviderStatus: "Ready",
		}},
		network: &provisioningdomain.MachineNetwork{Interfaces: []provisioningdomain.NetworkInterface{{
			ID: "nic-a", Links: []provisioningdomain.NetworkLink{{
				ID: "link-a", State: provisioningdomain.NetworkStateStatic, SubnetID: "subnet-a", IPAddress: "192.0.2.30",
			}},
		}}},
	}
	servers := &deploymentProjectionTestRepo{server: &serverdomain.Server{
		ID: "server-a", Source: serverdomain.Source{IntegrationID: "provider-a", ProviderMachineID: "machine-a"},
		Provisioning: &serverdomain.ProvisioningStatus{State: "ready"},
	}}
	tasks := &cleanupTaskRepo{task: &provisioningdomain.ProvisioningTask{
		ID: "task-a", Kind: provisioningdomain.ProvisioningTaskReleaseNetworkCleanup,
		ServerID: "server-a", IntegrationID: "provider-a", ProviderMachineID: "machine-a",
		Status: provisioningdomain.ProvisioningTaskPending, Phase: provisioningdomain.ProvisioningTaskWaitingForReady,
		Attempt: 1, RequestID: "workflow-a",
		Snapshot: []provisioningdomain.StaticNetworkLinkSnapshot{{
			InterfaceID: "nic-a", LinkID: "link-a", SubnetID: "subnet-a", IPAddress: "192.0.2.30",
		}},
	}}
	factory := cleanupProviderFactory{provider: provider}
	executor := providerStepExecutor{
		servers: servers, providers: factory, tasks: tasks, poll: time.Millisecond,
		cleanup: provisioningapp.NewReleaseNetworkCleanup(tasks, servers, factory),
	}

	result := executor.observeReleaseCleanup(context.Background(), "server-a", "workflow-a", true, false)

	if result.Status != workflowdomain.TaskSucceeded {
		t.Fatalf("release cleanup result = %+v, want succeeded", result)
	}
	if len(provider.unlinked) != 1 || provider.unlinked[0] != "nic-a/link-a" {
		t.Errorf("unlinked = %v, want the recorded static link", provider.unlinked)
	}
	if tasks.task.Status != provisioningdomain.ProvisioningTaskSucceeded || tasks.task.Phase != provisioningdomain.ProvisioningTaskComplete {
		t.Errorf("cleanup task = %+v, want succeeded and complete", tasks.task)
	}
}
