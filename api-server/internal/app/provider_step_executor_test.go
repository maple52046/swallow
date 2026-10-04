package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// TestRescueTransitionClassification pins how the recover Step reads MAAS rescue labels:
// an in-progress entering/exiting transition is waited out, while a settled "failed" rescue
// transition triggers the Mark broken escalation instead of another doomed exit. Both are
// normalized to `rescue`, so the provider label is the only signal (decision 033).
func TestRescueTransitionClassification(t *testing.T) {
	cases := []struct {
		label      string
		inProgress bool
		failed     bool
	}{
		{"Entering rescue mode", true, false},
		{"Exiting rescue mode", true, false},
		{"Rescue mode", false, false},
		{"Failed to exit rescue mode", false, true},
		{"Failed to enter rescue mode", false, true},
		{"Deployed", false, false},
	}
	for _, tc := range cases {
		if got := rescueTransitionInProgress(tc.label); got != tc.inProgress {
			t.Errorf("rescueTransitionInProgress(%q) = %v, want %v", tc.label, got, tc.inProgress)
		}
		if got := rescueTransitionFailed(tc.label); got != tc.failed {
			t.Errorf("rescueTransitionFailed(%q) = %v, want %v", tc.label, got, tc.failed)
		}
	}
}

type cancellationProvider struct {
	aborts     int
	machine    *provisioningdomain.Machine
	powerState provisioningdomain.PowerState
}

func (p *cancellationProvider) Name() string { return "test" }
func (p *cancellationProvider) Probe(context.Context) (provisioningdomain.ProviderInfo, error) {
	return provisioningdomain.ProviderInfo{}, nil
}
func (p *cancellationProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{HardwareValidation: true}
}
func (p *cancellationProvider) ListMachines(context.Context, provisioningdomain.MachineFilter) ([]*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *cancellationProvider) GetMachine(context.Context, string) (*provisioningdomain.Machine, error) {
	if p.machine != nil {
		return p.machine, nil
	}
	return nil, errors.New("observation unavailable")
}
func (p *cancellationProvider) ListOSImages(context.Context) ([]*provisioningdomain.OSImage, error) {
	return nil, nil
}
func (p *cancellationProvider) Deploy(context.Context, provisioningdomain.DeployRequest) (*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *cancellationProvider) Release(context.Context, string) (*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *cancellationProvider) PowerOn(context.Context, string) (*provisioningdomain.Machine, error) {
	return p.machine, nil
}
func (p *cancellationProvider) PowerOff(context.Context, string) (*provisioningdomain.Machine, error) {
	return p.machine, nil
}
func (p *cancellationProvider) QueryPowerState(context.Context, string) (provisioningdomain.PowerState, error) {
	return p.powerState, nil
}
func (p *cancellationProvider) Inspect(context.Context, string) (*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *cancellationProvider) Test(context.Context, string) (*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *cancellationProvider) Abort(context.Context, string) (*provisioningdomain.Machine, error) {
	p.aborts++
	return nil, nil
}
func (p *cancellationProvider) OverrideFailedTesting(context.Context, string) (*provisioningdomain.Machine, error) {
	return nil, nil
}

type cancellationProviderFactory struct{ provider *cancellationProvider }

func (f cancellationProviderFactory) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

type deploymentProjectionTestRepo struct {
	serverdomain.ServerRepository
	server *serverdomain.Server
}

func (r *deploymentProjectionTestRepo) FindByID(context.Context, string) (*serverdomain.Server, error) {
	return r.server, nil
}

func (r *deploymentProjectionTestRepo) Upsert(_ context.Context, server *serverdomain.Server) error {
	r.server = server
	return nil
}

func (r *deploymentProjectionTestRepo) SetDeployment(_ context.Context, _ string, deployment *serverdomain.DeploymentStatus) error {
	r.server.Deployment = deployment
	return nil
}

func (r *deploymentProjectionTestRepo) SetDefaultUser(_ context.Context, _ string, user string) error {
	r.server.DefaultUser = user
	return nil
}

func TestProviderStepRejectsDeploymentWithoutFrozenImage(t *testing.T) {
	result := (providerStepExecutor{}).Execute(context.Background(), temporalworkflow.StepExecutionInput{
		OperationID: "operation-id",
		Step: operationdomain.Task{
			ID: "provision-server", Kind: "provision-os", Attempt: 1,
			Parameters: map[string]any{"request": map[string]any{
				"serverIds": []string{"server-id"},
				"settings":  map[string]any{},
			}},
		},
	})

	if result.Status != operationdomain.TaskFailed || result.Error == nil {
		t.Fatalf("result = %+v, want failed Step with normalized error", result)
	}
	if result.Error.Code != "intent_snapshot_incomplete" || result.Error.Retryable {
		t.Fatalf("error = %+v, want non-retryable incomplete snapshot", result.Error)
	}
}

func TestDeploymentReadinessFailureWithoutAddressOffersRedeployRecovery(t *testing.T) {
	result := deploymentReadinessFailed(deploymentReadiness{serverName: "lab-control-1", port: 22})

	if result.Status != operationdomain.TaskFailed || result.Error == nil {
		t.Fatalf("result = %+v, want failed Step", result)
	}
	if result.Error.Code != "deployment_address_unavailable" || !result.Error.Retryable {
		t.Fatalf("error = %+v, want retryable missing-address failure", result.Error)
	}
	if !strings.Contains(result.Error.Message, "release and redeploy") || result.Error.Stage != "ssh_readiness" {
		t.Fatalf("error = %+v, want explicit recovery and readiness stage", result.Error)
	}
}

func TestDeploymentReadinessFailureWithAddressDoesNotOfferRedeploy(t *testing.T) {
	result := deploymentReadinessFailed(deploymentReadiness{
		serverName: "lab-control-2", addresses: []string{"192.168.100.57"}, port: 2222,
	})

	if result.Status != operationdomain.TaskFailed || result.Error == nil {
		t.Fatalf("result = %+v, want failed Step", result)
	}
	if result.Error.Code != "deployment_ssh_unreachable" || !result.Error.Retryable {
		t.Fatalf("error = %+v, want retryable SSH failure", result.Error)
	}
	if strings.Contains(result.Error.Message, "release and redeploy") || !strings.Contains(result.Error.Message, "192.168.100.57") {
		t.Fatalf("message = %q, want observation-only SSH recovery", result.Error.Message)
	}
}

func TestProviderStepProjectsDeploymentFailureOntoServer(t *testing.T) {
	// The value set on the Server belongs to the OS being replaced (decision 045), so a new OS
	// deployment clears it even when the deploy itself then fails.
	server := &serverdomain.Server{ID: "server-id", DefaultUser: "amd"}
	executor := providerStepExecutor{servers: &deploymentProjectionTestRepo{server: server}}
	result := executor.Execute(context.Background(), temporalworkflow.StepExecutionInput{
		OperationID: "operation-id",
		Step: operationdomain.Task{
			ID:      "provision-server",
			Kind:    "provision-os",
			Attempt: 2,
			Targets: []operationdomain.ResourceReference{{Kind: "server", ID: server.ID}},
			Parameters: map[string]any{"request": map[string]any{
				"serverIds": []string{server.ID},
				"settings":  map[string]any{},
			}},
		},
	})

	if result.Status != operationdomain.TaskFailed {
		t.Fatalf("result status = %s, want failed", result.Status)
	}
	if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentFailed {
		t.Fatalf("deployment = %+v, want failed Server projection", server.Deployment)
	}
	if server.Deployment.OperationID != "operation-id" || server.Deployment.StepID != "provision-server" || server.Deployment.Attempt != 2 {
		t.Fatalf("deployment identity = %+v", server.Deployment)
	}
	if server.Deployment.StatusReason == "" || server.Deployment.FinishedAt == nil {
		t.Fatalf("deployment terminal details = %+v", server.Deployment)
	}
	if server.DefaultUser != "" {
		t.Errorf("DefaultUser after a new OS deployment = %q, want it cleared", server.DefaultUser)
	}
}

func TestProviderStepReadinessTimeout(t *testing.T) {
	if got := (providerStepExecutor{}).readinessTimeout(); got != defaultDeploymentReadinessWait {
		t.Fatalf("default readiness timeout = %s", got)
	}
	if got := (providerStepExecutor{readinessWait: time.Second}).readinessTimeout(); got != time.Second {
		t.Fatalf("custom readiness timeout = %s", got)
	}
}

func TestProviderStepDeploymentPowerOnTimeout(t *testing.T) {
	if got := (providerStepExecutor{}).powerOnTimeout(); got != defaultDeploymentPowerOnWait {
		t.Fatalf("default power-on timeout = %s", got)
	}

	server := &serverdomain.Server{
		ID: "server-id",
		Source: serverdomain.Source{
			IntegrationID:     "integration-id",
			ProviderMachineID: "machine-id",
		},
	}
	repository := &deploymentProjectionTestRepo{server: server}
	provider := &cancellationProvider{
		machine: &provisioningdomain.Machine{
			ID: "machine-id", Status: provisioningdomain.MachineStatusDeploying,
		},
		powerState: provisioningdomain.PowerStateOff,
	}
	factory := cancellationProviderFactory{provider: provider}
	executor := providerStepExecutor{
		refresh:     provisioningapp.NewRefreshServerUseCase(repository, factory, nil),
		servers:     repository,
		providers:   factory,
		poll:        time.Millisecond,
		powerOnWait: 2 * time.Millisecond,
	}
	image := "ubuntu/custom-image"
	result := executor.observeDeploy(
		context.Background(),
		temporalworkflow.StepExecutionInput{Step: operationdomain.Task{ID: "provision"}},
		server.ID,
		provisioningapp.DeployServersInput{Settings: provisioningapp.DeploymentSettingsInput{ImageID: &image}},
		true,
	)

	if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil {
		t.Fatalf("result = %+v, want requires-attention Step", result)
	}
	if result.Error.Code != "deployment_power_on_timeout" || result.Error.Stage != "deployment_power_on" {
		t.Fatalf("error = %+v, want explicit provider power-on diagnosis", result.Error)
	}
	if provider.aborts != 0 {
		t.Fatalf("stalled observation called provider abort %d times", provider.aborts)
	}
}

func TestInitialDeploymentProjectionClosesWorkerStartGap(t *testing.T) {
	server := &serverdomain.Server{ID: "server-id"}
	repository := &deploymentProjectionTestRepo{server: server}
	materializeInitialDeployments(context.Background(), repository, "operation-id", []operationdomain.Task{
		{ID: "provision-server", Kind: "provision-os", Targets: []operationdomain.ResourceReference{{Kind: "server", ID: server.ID}}},
		{ID: "install-platform", Kind: "ansible-playbook"},
	})

	if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentDeploying {
		t.Fatalf("deployment = %+v, want immediately visible deploying projection", server.Deployment)
	}
	if server.Deployment.OperationID != "operation-id" || server.Deployment.StepID != "provision-server" || server.Deployment.Attempt != 1 {
		t.Fatalf("deployment identity = %+v", server.Deployment)
	}
}

func TestImageMatchesIncludesRequestedEphemeralMode(t *testing.T) {
	image := "ubuntu/custom-image"
	ephemeral := true
	input := provisioningapp.DeployServersInput{Settings: provisioningapp.DeploymentSettingsInput{ImageID: &image, Ephemeral: &ephemeral}}
	state := &provisioningapp.ProvisioningStateItem{DistroSeries: image}
	if imageMatches(state, input) {
		t.Fatal("disk deployment must not satisfy requested ephemeral deployment")
	}
	state.Ephemeral = true
	if !imageMatches(state, input) {
		t.Fatal("matching image and ephemeral mode should satisfy deployment intent")
	}
}

func TestProviderCancellationAbortsOnlyForWorkflowCancellation(t *testing.T) {
	server := &serverdomain.Server{ID: "server-id", Source: serverdomain.Source{IntegrationID: "integration-id", ProviderMachineID: "machine-id"}}
	repository := &deploymentProjectionTestRepo{server: server}
	provider := &cancellationProvider{}
	factory := cancellationProviderFactory{provider: provider}
	executor := providerStepExecutor{
		servers:   repository,
		providers: factory,
		refresh:   provisioningapp.NewRefreshServerUseCase(repository, factory, nil),
		poll:      time.Hour,
	}
	image := "ubuntu/custom-image"
	input := provisioningapp.DeployServersInput{Settings: provisioningapp.DeploymentSettingsInput{ImageID: &image}}
	step := temporalworkflow.StepExecutionInput{Step: operationdomain.Task{ID: "provision", Attempt: 1}}

	workerContext, stopWorker := context.WithCancelCause(context.Background())
	stopWorker(temporalworkflow.ErrActivityWorkerStopping)
	executor.observeDeploy(workerContext, step, server.ID, input, true)
	if provider.aborts != 0 {
		t.Fatalf("worker stop called provider abort %d times", provider.aborts)
	}

	workflowContext, cancelWorkflow := context.WithCancelCause(context.Background())
	cancelWorkflow(context.Canceled)
	executor.observeDeploy(workflowContext, step, server.ID, input, true)
	if provider.aborts != 1 {
		t.Fatalf("workflow cancellation called provider abort %d times, want 1", provider.aborts)
	}
}
