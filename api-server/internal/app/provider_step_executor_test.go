package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
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
		Step: workflowdomain.Task{
			ID: "provision-server", Kind: "provision-os", Attempt: 1,
			Parameters: map[string]any{"request": map[string]any{
				"serverIds": []string{"server-id"},
				"settings":  map[string]any{},
			}},
		},
	})

	if result.Status != workflowdomain.TaskFailed || result.Error == nil {
		t.Fatalf("result = %+v, want failed Step with normalized error", result)
	}
	if result.Error.Code != "intent_snapshot_incomplete" || result.Error.Retryable {
		t.Fatalf("error = %+v, want non-retryable incomplete snapshot", result.Error)
	}
}

func TestDeploymentReadinessFailureWithoutAddressOffersRedeployRecovery(t *testing.T) {
	result := deploymentReadinessFailed(deploymentReadiness{serverName: "lab-control-1", port: 22})

	if result.Status != workflowdomain.TaskFailed || result.Error == nil {
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

	if result.Status != workflowdomain.TaskFailed || result.Error == nil {
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
	// The Step fails before any deployment is submitted (no frozen image), so no OS is replaced
	// and the Server Default User set for the installed one is kept (decision 039).
	server := &serverdomain.Server{ID: "server-id", DefaultUser: "amd"}
	executor := providerStepExecutor{servers: &deploymentProjectionTestRepo{server: server}, protection: stubMutationGuard{}}
	result := executor.Execute(context.Background(), temporalworkflow.StepExecutionInput{
		OperationID: "operation-id",
		Step: workflowdomain.Task{
			ID:      "provision-server",
			Kind:    "provision-os",
			Attempt: 2,
			Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: server.ID}},
			Parameters: map[string]any{"request": map[string]any{
				"serverIds": []string{server.ID},
				"settings":  map[string]any{},
			}},
		},
	})

	if result.Status != workflowdomain.TaskFailed {
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
	if server.DefaultUser != "amd" {
		t.Errorf("DefaultUser after a Step that submitted no deployment = %q, want amd kept", server.DefaultUser)
	}
}

// TestProviderStepClearsDefaultUserOnlyWhenSubmittingDeployment guards decision 039 under
// convergent reruns (decision 061): the account set on the Server belongs to the OS a submitted
// deployment replaces, even when the provider then fails it, but a Step that only observes the
// frozen image already deployed and reachable replaces nothing and keeps it.
func TestProviderStepClearsDefaultUserOnlyWhenSubmittingDeployment(t *testing.T) {
	t.Run("a submitted deployment clears it", func(t *testing.T) {
		executor, server, _, _, deployments := retryHarness(provisioningdomain.MachineStatusReady, "")
		server.DefaultUser = "amd"
		deployments.fails = true

		result := executor.Execute(context.Background(), provisionStep(1, 1))

		if result.Status != workflowdomain.TaskFailed || deployments.calls != 1 {
			t.Fatalf("result = %+v after %d deployments, want a submitted deployment that failed", result, deployments.calls)
		}
		if server.DefaultUser != "" {
			t.Errorf("DefaultUser = %q, want it cleared for the replaced OS", server.DefaultUser)
		}
	})
	t.Run("an already converged deployment keeps it", func(t *testing.T) {
		executor, server, provider, _, deployments := retryHarness(provisioningdomain.MachineStatusDeployed, retryTestImage)
		provider.machine.IPAddresses = []string{"10.0.0.5"}
		server.DefaultUser = "amd"

		result := executor.Execute(context.Background(), provisionStep(2, 1))

		if result.Status != workflowdomain.TaskSucceeded || deployments.calls != 0 {
			t.Fatalf("result = %+v after %d deployments, want success without a deployment", result, deployments.calls)
		}
		if server.DefaultUser != "amd" {
			t.Errorf("DefaultUser = %q, want amd kept", server.DefaultUser)
		}
	})
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
		temporalworkflow.StepExecutionInput{Step: workflowdomain.Task{ID: "provision"}},
		server.ID,
		provisioningapp.DeployServersInput{Settings: provisioningapp.DeploymentSettingsInput{ImageID: &image}},
		true,
	)

	if result.Status != workflowdomain.TaskRequiresAttention || result.Error == nil {
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
	materializeInitialDeployments(context.Background(), repository, "operation-id", []workflowdomain.Task{
		{ID: "provision-server", Kind: "provision-os", Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: server.ID}}},
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
	step := temporalworkflow.StepExecutionInput{Step: workflowdomain.Task{ID: "provision", Attempt: 1}}

	workerContext, stopWorker := context.WithCancelCause(context.Background())
	stopWorker(temporalworkflow.ErrActivityWorkerStopping)
	executor.observeDeploy(workerContext, step, server.ID, input, true)
	if provider.aborts != 0 {
		t.Fatalf("worker stop called provider abort %d times", provider.aborts)
	}

	fencedContext, fence := context.WithCancelCause(context.Background())
	fence(workflowdomain.ErrDispatchFenced)
	executor.observeDeploy(fencedContext, step, server.ID, input, true)
	if provider.aborts != 0 {
		t.Fatalf("a superseded run called provider abort %d times; the run that superseded it resumes the deployment", provider.aborts)
	}

	workflowContext, cancelWorkflow := context.WithCancelCause(context.Background())
	cancelWorkflow(context.Canceled)
	executor.observeDeploy(workflowContext, step, server.ID, input, true)
	if provider.aborts != 1 {
		t.Fatalf("workflow cancellation called provider abort %d times, want 1", provider.aborts)
	}
}

// flakyProvider is the retry harness's provisioner whose machine reads can fail: readErr returns
// the error of the nth read (from 1), or nil to answer; it may also move the machine. events is its
// event stream, newest first as MAAS lists it.
type flakyProvider struct {
	*retryProvider
	mu      sync.Mutex
	reads   int
	readErr func(read int) error
	events  []provisioningdomain.MachineEvent
}

func (p *flakyProvider) GetMachine(ctx context.Context, id string) (*provisioningdomain.Machine, error) {
	p.mu.Lock()
	p.reads++
	read := p.reads
	p.mu.Unlock()
	if p.readErr != nil {
		if err := p.readErr(read); err != nil {
			return nil, err
		}
	}
	return p.retryProvider.GetMachine(ctx, id)
}

func (p *flakyProvider) ListMachineEvents(context.Context, string, int) ([]provisioningdomain.MachineEvent, error) {
	return p.events, nil
}

func (p *flakyProvider) readCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reads
}

// flakyHarness is the retry harness over a flaky provisioner whose machine is in state with the
// harness image and a reachable address.
func flakyHarness(state provisioningdomain.MachineStatus, readErr func(int) error) (providerStepExecutor, *serverdomain.Server, *flakyProvider) {
	executor, server, provider, _, _ := retryHarness(state, retryTestImage)
	provider.machine.IPAddresses = []string{"10.0.0.5"}
	flaky := &flakyProvider{retryProvider: provider, readErr: readErr}
	repository := executor.servers.(*deploymentProjectionTestRepo)
	factory := eventProviderFactory{provider: flaky}
	executor.providers = factory
	executor.refresh = provisioningapp.NewRefreshServerUseCase(repository, factory, nil)
	return executor, server, flaky
}

func unreachableProvisioner() error {
	return &provisioningdomain.ProviderError{
		Kind: provisioningdomain.ProviderErrorUnavailable, Detail: "MAAS did not answer.",
		Endpoint: "http://maas.lab:5240/MAAS/api/2.0/machines/machine-id/", Operation: "read machine",
		Err: errors.New("dial tcp 10.0.0.2:5240: connect: connection refused"),
	}
}

// observeLoops runs each provisioner observation loop of the harness Server once with step.
var observeLoops = map[string]func(context.Context, providerStepExecutor, *flakyProvider, temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult{
	"deploy": func(ctx context.Context, e providerStepExecutor, _ *flakyProvider, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
		image := retryTestImage
		return e.observeDeploy(ctx, step, "server-id", provisioningapp.DeployServersInput{Settings: provisioningapp.DeploymentSettingsInput{ImageID: &image}}, true)
	},
	"release": func(ctx context.Context, e providerStepExecutor, _ *flakyProvider, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
		return e.observeRelease(ctx, step, "server-id", false, true)
	},
	"recover": func(ctx context.Context, e providerStepExecutor, _ *flakyProvider, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
		return e.recoverToReady(ctx, step, "server-id", provisioningapp.RecoverServerInput{})
	},
	"inspect": func(ctx context.Context, e providerStepExecutor, p *flakyProvider, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
		outcome := e.observeInspection(ctx, step, "server-id", p, "machine-id", time.Now())
		if outcome.kind == inspectionReady {
			return providerSucceeded()
		}
		return outcome.result
	},
}

// Every provisioner observation tolerates failed reads for a bounded time and then asks for
// attention with the last read error, instead of ignoring them until its own deadline; a credential
// the provisioner rejects asks for attention at once; reads that recover resume the observation.
func TestProviderObservationsBoundFailedReads(t *testing.T) {
	for name, observe := range observeLoops {
		t.Run(name+"/unreachable for the whole window", func(t *testing.T) {
			executor, _, provider := flakyHarness(provisioningdomain.MachineStatusReady, func(int) error { return unreachableProvisioner() })
			executor.providerReadErrorWait = 20 * time.Millisecond
			waits := &recordedWaits{}
			step := provisionStep(1, 1)
			step.Waiting = waits.publisher()

			result := observe(context.Background(), executor, provider, step)

			if result.Status != workflowdomain.TaskRequiresAttention || result.Error == nil || result.Error.Code != "provider_unavailable" || !result.Error.Retryable {
				t.Fatalf("result = %+v (error %+v), want retryable provider_unavailable attention", result, result.Error)
			}
			d := result.Error.Diagnostic
			if d == nil || d.FailureClass != workflowdomain.FailureTransient || d.Attempts < 2 || d.Operation != "read machine" ||
				d.Endpoint == "" || d.Target == nil || d.Target.ID != "server-id" || !strings.Contains(d.Cause, "connection refused") {
				t.Errorf("diagnostic = %+v, want transient with the read's endpoint, operation, cause, and the Server", d)
			}
			if !strings.Contains(result.Error.Message, "MAAS did not answer.") || len(waits.observations()) == 0 ||
				!strings.Contains(waits.observations()[0], "cannot read server-id") {
				t.Errorf("message %q, published %q; want the provisioner's detail while waiting and at the end", result.Error.Message, waits.observations())
			}
		})
		t.Run(name+"/an unclassified failure is unknown", func(t *testing.T) {
			executor, _, provider := flakyHarness(provisioningdomain.MachineStatusReady, func(int) error { return errors.New("store timeout") })
			executor.providerReadErrorWait = 20 * time.Millisecond

			result := observe(context.Background(), executor, provider, provisionStep(1, 1))

			if result.Error == nil || result.Error.Code != "provider_unavailable" || result.Error.Diagnostic == nil ||
				result.Error.Diagnostic.FailureClass != workflowdomain.FailureUnknown {
				t.Fatalf("result = %+v (error %+v), want provider_unavailable classified unknown", result, result.Error)
			}
		})
		t.Run(name+"/a rejected credential stops at once", func(t *testing.T) {
			executor, _, provider := flakyHarness(provisioningdomain.MachineStatusReady, func(int) error {
				return &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorAuth, Detail: "MAAS rejected the API key.", Operation: "read machine"}
			})

			result := observe(context.Background(), executor, provider, provisionStep(1, 1))

			if result.Status != workflowdomain.TaskRequiresAttention || result.Error == nil || result.Error.Code != "provider_auth" || !result.Error.Retryable {
				t.Fatalf("result = %+v (error %+v), want retryable provider_auth attention", result, result.Error)
			}
			if d := result.Error.Diagnostic; d == nil || d.FailureClass != workflowdomain.FailurePermanent || d.Remediation == "" {
				t.Errorf("diagnostic = %+v, want permanent with a next step", d)
			}
			if provider.readCount() != 1 || provider.aborts != 0 {
				t.Errorf("reads = %d, aborts = %d; want one read and no abort", provider.readCount(), provider.aborts)
			}
		})
		t.Run(name+"/reads that recover resume the observation", func(t *testing.T) {
			executor, _, provider := flakyHarness(provisioningdomain.MachineStatusReady, func(read int) error {
				if read <= 3 {
					return unreachableProvisioner()
				}
				return nil
			})
			executor.providerReadErrorWait = time.Hour
			if name == "deploy" {
				provider.machine.Status = provisioningdomain.MachineStatusDeployed
			}

			result := observe(context.Background(), executor, provider, provisionStep(1, 1))

			if result.Status != workflowdomain.TaskSucceeded {
				t.Fatalf("result = %+v (error %+v), want succeeded once reads recovered", result, result.Error)
			}
		})
	}
	t.Run("a resumed attempt reads again instead of submitting a second deployment", func(t *testing.T) {
		executor, _, provider := flakyHarness(provisioningdomain.MachineStatusDeployed, func(read int) error {
			if read == 1 {
				return unreachableProvisioner()
			}
			return nil
		})
		executor.providerReadErrorWait = time.Hour
		deployments := executor.deployments.(*retryDeployments)

		result := executor.Execute(context.Background(), provisionStep(1, 2))

		if deployments.calls != 0 {
			t.Fatalf("the resumed attempt submitted the deployment %d times after a failed read", deployments.calls)
		}
		if result.Status != workflowdomain.TaskSucceeded || provider.readCount() < 2 {
			t.Fatalf("result = %+v (error %+v) after %d reads, want succeeded once a read showed the deployment", result, result.Error, provider.readCount())
		}
	})
	t.Run("a machine the provisioner no longer has stops at once", func(t *testing.T) {
		executor, _, provider := flakyHarness(provisioningdomain.MachineStatusReady, func(int) error { return provisioningdomain.ErrMachineNotFound })
		result := observeLoops["release"](context.Background(), executor, provider, provisionStep(1, 1))
		if result.Error == nil || result.Error.Code != "provider_observation_failed" || provider.readCount() != 1 {
			t.Errorf("result = %+v after %d reads, want provider_observation_failed after one", result.Error, provider.readCount())
		}
	})
	t.Run("an Integration without a credential is a rejected credential", func(t *testing.T) {
		executor, _, provider := flakyHarness(provisioningdomain.MachineStatusReady, func(int) error { return sitedomain.ErrCredentialNotSet })
		result := observeLoops["recover"](context.Background(), executor, provider, provisionStep(1, 1))
		if result.Error == nil || result.Error.Code != "provider_auth" {
			t.Errorf("result error = %+v, want provider_auth", result.Error)
		}
	})
}

// A deployment publishes what it waits for on every observation, and a run a later dispatch
// superseded stops at its next publication without projecting an outcome onto the Server.
func TestObserveDeployPublishesItsWait(t *testing.T) {
	t.Run("each observation is published", func(t *testing.T) {
		var provider *flakyProvider
		executor, _, provider := flakyHarness(provisioningdomain.MachineStatusDeploying, func(read int) error {
			if read == 3 {
				provider.machine.Status = provisioningdomain.MachineStatusDeployed
			}
			return nil
		})
		waits := &recordedWaits{}
		step := provisionStep(1, 1)
		step.Waiting = waits.publisher()

		result := observeLoops["deploy"](context.Background(), executor, provider, step)

		published := waits.observations()
		if result.Status != workflowdomain.TaskSucceeded || len(published) != 2 {
			t.Fatalf("result = %+v, published %q; want success after two published observations", result, published)
		}
		for _, observation := range published {
			if !strings.Contains(observation, "server-id as deploying") {
				t.Errorf("observation = %q, want the Server and its provisioner state", observation)
			}
		}
		if waits.seen[1].Attempts != 2 {
			t.Errorf("second observation attempts = %d, want 2", waits.seen[1].Attempts)
		}
	})
	t.Run("a superseded run stops without a projection", func(t *testing.T) {
		executor, server, provider := flakyHarness(provisioningdomain.MachineStatusDeploying, nil)
		waits := &recordedWaits{err: workflowdomain.ErrDispatchFenced}
		step := provisionStep(1, 1)
		step.Resumed, step.Waiting = true, waits.publisher()

		result := executor.Execute(context.Background(), step)

		if !isSuperseded(result) || provider.aborts != 0 {
			t.Fatalf("result = %+v after %d aborts, want a superseded run that aborted nothing", result, provider.aborts)
		}
		if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentDeploying {
			t.Errorf("Server deployment = %+v, want it still deploying", server.Deployment)
		}
	})
}

// DF-82: a failed deployment and a failed release carry the provisioner's explanation and the
// events it recorded about the machine during the attempt, oldest first.
func TestProviderFailuresCarryTheProvisionerEvidence(t *testing.T) {
	startedAt := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	events := []provisioningdomain.MachineEvent{
		{Type: "Failed", Description: "Marking node failed: Installation failed", OccurredAt: startedAt.Add(9 * time.Minute).Format(time.RFC3339)},
		{Type: "Installing OS", Description: "curtin started", OccurredAt: startedAt.Add(4 * time.Minute).Format(time.RFC3339)},
		{Type: "Deployed", Description: "an earlier deployment", OccurredAt: startedAt.Add(-time.Hour).Format(time.RFC3339)},
	}
	assertEvidence := func(t *testing.T, d *workflowdomain.TaskDiagnostic, cause string) {
		t.Helper()
		if d == nil || d.FailureClass != workflowdomain.FailurePermanent || !strings.Contains(d.Cause, cause) || d.Remediation == "" ||
			d.Target == nil || d.Target.ID != "server-id" {
			t.Fatalf("diagnostic = %+v, want permanent with cause %q, a next step, and the Server", d, cause)
		}
		if len(d.ProviderEvents) != 2 || d.ProviderEvents[0].Type != "Installing OS" || d.ProviderEvents[1].Type != "Failed" ||
			d.ProviderEvents[1].At == nil || d.ProviderEvents[1].Description == "" {
			t.Errorf("provider events = %+v, want this attempt's two events, oldest first", d.ProviderEvents)
		}
	}

	t.Run("deployment", func(t *testing.T) {
		executor, _, provider := flakyHarness(provisioningdomain.MachineStatusFailed, nil)
		provider.events = events
		provider.machine.ErrorDescription = "Installation failed (refer to the installation log for more information)."
		provider.installationLine = "Cannot find previous partition on disk /dev/nvme0n1"
		step := provisionStep(1, 2)
		step.Step.StartedAt = &startedAt

		result := executor.Execute(context.Background(), step)

		if result.Error == nil || result.Error.Code != "deployment_failed" || !result.Error.Retryable {
			t.Fatalf("result = %+v (error %+v), want retryable deployment_failed", result, result.Error)
		}
		assertEvidence(t, result.Error.Diagnostic, "installation error: Cannot find previous partition")
		if !strings.Contains(result.Error.Diagnostic.Cause, "provisioner error description: Installation failed") {
			t.Errorf("cause = %q, want the provisioner's error description", result.Error.Diagnostic.Cause)
		}
	})
	t.Run("release", func(t *testing.T) {
		executor, _, provider := flakyHarness(provisioningdomain.MachineStatusFailed, nil)
		provider.events = events
		provider.machine.ErrorDescription = "Failed to erase disks."
		step := provisionStep(1, 1)
		step.Step.StartedAt = &startedAt

		result := observeLoops["release"](context.Background(), executor, provider, step)

		if result.Error == nil || result.Error.Code != "release_failed" || !result.Error.Retryable ||
			result.Error.Message != "The provisioner reported that release failed: Failed to erase disks." {
			t.Fatalf("result error = %+v, want release_failed naming the provisioner's error", result.Error)
		}
		assertEvidence(t, result.Error.Diagnostic, "Failed to erase disks")
	})
}

// A deployment stage the provisioner stops advancing asks for attention with the events it recorded,
// the stage's window as the budget, and a class that says whether waiting could still help.
func TestStalledDeploymentsCarryTheProvisionerEvidence(t *testing.T) {
	t.Run("an install stage stall", func(t *testing.T) {
		now := time.Now()
		provider := &eventProvider{cancellationProvider: deployingMachine(), events: []provisioningdomain.MachineEvent{
			machineEvent("Configuring OS", now.Add(-40*time.Minute)),
			machineEvent("Installing OS", now.Add(-41*time.Minute)),
		}}
		fixture := newProgressFixture(t, provider, now.Add(-time.Hour), 30*time.Minute)

		result := fixture.observeBriefly(t)

		if result.Error == nil || result.Error.Code != "deployment_provider_stage_stall" {
			t.Fatalf("result = %+v, want deployment_provider_stage_stall", result)
		}
		d := result.Error.Diagnostic
		if d == nil || d.FailureClass != workflowdomain.FailureTransient || d.BudgetSeconds != int64((30*time.Minute)/time.Second) ||
			len(d.ProviderEvents) != 2 || d.ProviderEvents[0].Type != "Installing OS" || d.Remediation == "" {
			t.Errorf("diagnostic = %+v, want transient with the stall window and the events, oldest first", d)
		}
	})
	t.Run("a Server that stays powered off", func(t *testing.T) {
		executor, _, provider := flakyHarness(provisioningdomain.MachineStatusDeploying, nil)
		provider.powerState = provisioningdomain.PowerStateOff
		executor.powerOnWait = 2 * time.Millisecond

		result := observeLoops["deploy"](context.Background(), executor, provider, provisionStep(1, 1))

		if result.Error == nil || result.Error.Code != "deployment_power_on_timeout" || result.Error.Diagnostic == nil ||
			result.Error.Diagnostic.FailureClass != workflowdomain.FailurePermanent {
			t.Fatalf("result error = %+v, want a permanent deployment_power_on_timeout", result.Error)
		}
	})
}

// A Release's static IP cleanup that stops advancing asks for attention after its window instead of
// holding the release Task indefinitely.
func TestReleaseCleanupIsBounded(t *testing.T) {
	provider := &cleanupProvider{cancellationProvider: cancellationProvider{machine: &provisioningdomain.Machine{
		ID: "machine-a", Status: provisioningdomain.MachineStatusReleasing,
	}}}
	servers := &deploymentProjectionTestRepo{server: &serverdomain.Server{
		ID: "server-a", Source: serverdomain.Source{IntegrationID: "provider-a", ProviderMachineID: "machine-a"},
	}}
	tasks := &cleanupTaskRepo{task: &provisioningdomain.ProvisioningTask{
		ID: "task-a", Kind: provisioningdomain.ProvisioningTaskReleaseNetworkCleanup,
		ServerID: "server-a", IntegrationID: "provider-a", ProviderMachineID: "machine-a",
		Status: provisioningdomain.ProvisioningTaskPending, Phase: provisioningdomain.ProvisioningTaskWaitingForReady, RequestID: "workflow-a",
	}}
	factory := cleanupProviderFactory{provider: provider}
	executor := providerStepExecutor{
		servers: servers, providers: factory, tasks: tasks, poll: time.Millisecond, releaseCleanupWait: 20 * time.Millisecond,
		cleanup: provisioningapp.NewReleaseNetworkCleanup(tasks, servers, factory),
	}

	result := executor.observeReleaseCleanup(context.Background(), "server-a", "workflow-a", true, false)

	if result.Status != workflowdomain.TaskRequiresAttention || result.Error == nil || result.Error.Code != "network_cleanup_timeout" {
		t.Fatalf("result = %+v (error %+v), want network_cleanup_timeout attention", result, result.Error)
	}
	d := result.Error.Diagnostic
	if d == nil || d.Attempts < 2 || !strings.Contains(d.Cause, string(provisioningdomain.ProvisioningTaskWaitingForReady)) || d.Target == nil {
		t.Errorf("diagnostic = %+v, want the wait's attempts, the cleanup's last phase, and the Server", d)
	}
}

// A failed provisioner call carries a diagnostic classified by the provisioner's answer, with the
// endpoint and operation the adapter recorded.
func TestNormalizeProviderErrorExplainsTheFailure(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantClass workflowdomain.FailureClass
	}{
		{name: "rejected credential", wantCode: "provider_auth", wantClass: workflowdomain.FailurePermanent,
			err: &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorAuth, Detail: "denied", Endpoint: "http://maas.lab/MAAS", Operation: "deploy machine"}},
		{name: "refused request", wantCode: "provider_rejected", wantClass: workflowdomain.FailurePermanent,
			err: &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorRejected, Detail: "not ready", Endpoint: "http://maas.lab/MAAS", Operation: "deploy machine"}},
		{name: "unreachable provisioner", wantCode: "provider_unavailable", wantClass: workflowdomain.FailureTransient,
			err: &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorUnavailable, Detail: "down", Endpoint: "http://maas.lab/MAAS", Operation: "deploy machine"}},
		{name: "swallow's own refusal", wantCode: "deployment_preflight_rejected", wantClass: workflowdomain.FailurePermanent,
			err: provisioningdomain.ErrDeploymentBatchConflict},
		{name: "unknown outcome", wantCode: "provider_outcome_unknown", wantClass: workflowdomain.FailureUnknown,
			err: errors.New("connection reset by peer")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeProviderError(tc.err, "deployment")

			if result.Error == nil || result.Error.Code != tc.wantCode {
				t.Fatalf("normalizeProviderError(%v) error = %+v, want code %s", tc.err, result.Error, tc.wantCode)
			}
			d := result.Error.Diagnostic
			if d == nil || d.FailureClass != tc.wantClass || d.Cause == "" {
				t.Fatalf("diagnostic = %+v, want class %s with the cause", d, tc.wantClass)
			}
			var providerErr *provisioningdomain.ProviderError
			if errors.As(tc.err, &providerErr) && (d.Endpoint != providerErr.Endpoint || d.Operation != providerErr.Operation) {
				t.Errorf("diagnostic endpoint %q operation %q, want the adapter's", d.Endpoint, d.Operation)
			}
		})
	}
	executed := withServerTarget(normalizeProviderError(errors.New("boom"), "release"),
		workflowdomain.Task{Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: "server-id"}}})
	if target := executed.Error.Diagnostic.Target; target == nil || target.ID != "server-id" {
		t.Errorf("target = %+v, want the Task's Server", target)
	}
}
