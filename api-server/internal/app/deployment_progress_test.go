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

// eventProvider is a deploying Machine whose provider also keeps an event stream, the shape the
// stage-stall detection reads. readErr simulates a temporarily unreadable stream.
type eventProvider struct {
	*cancellationProvider
	events  []provisioningdomain.MachineEvent
	readErr error
}

func (p *eventProvider) ListMachineEvents(context.Context, string, int) ([]provisioningdomain.MachineEvent, error) {
	return p.events, p.readErr
}

type eventProviderFactory struct {
	provider provisioningdomain.OSProvisioningProvider
}

func (f eventProviderFactory) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

// progressFixture wires observeDeploy against a powered-on deploying Machine whose current
// deployment attempt started at attemptStart.
type progressFixture struct {
	executor   providerStepExecutor
	repository *deploymentProjectionTestRepo
	step       temporalworkflow.StepExecutionInput
	input      provisioningapp.DeployServersInput
	serverID   string
}

func newProgressFixture(t *testing.T, provider provisioningdomain.OSProvisioningProvider, attemptStart time.Time, stallWait time.Duration) progressFixture {
	t.Helper()
	step := temporalworkflow.StepExecutionInput{
		OperationID: "operation-id",
		Step:        operationdomain.Task{ID: "provision-server", Kind: "provision-os", Attempt: 1},
	}
	server := &serverdomain.Server{
		ID:     "server-id",
		Source: serverdomain.Source{IntegrationID: "integration-id", ProviderMachineID: "machine-id"},
		Deployment: &serverdomain.DeploymentStatus{
			State: serverdomain.DeploymentDeploying, OperationID: step.OperationID,
			StepID: step.Step.ID, Attempt: step.Step.Attempt, StartedAt: attemptStart,
		},
	}
	repository := &deploymentProjectionTestRepo{server: server}
	factory := eventProviderFactory{provider: provider}
	image := "ubuntu/noble"
	return progressFixture{
		executor: providerStepExecutor{
			refresh:        provisioningapp.NewRefreshServerUseCase(repository, factory, nil),
			servers:        repository,
			providers:      factory,
			poll:           time.Millisecond,
			powerOnWait:    time.Hour,
			stageStallWait: stallWait,
		},
		repository: repository,
		step:       step,
		input:      provisioningapp.DeployServersInput{Settings: provisioningapp.DeploymentSettingsInput{ImageID: &image}},
		serverID:   server.ID,
	}
}

// observeBriefly runs observeDeploy until it returns or a worker-stop cancellation ends it, so a
// test can prove "still observing" without waiting for the two-hour timeout. Worker-stop
// cancellation never aborts the provider deployment.
func (f progressFixture) observeBriefly(t *testing.T) temporalworkflow.StepExecutionResult {
	t.Helper()
	ctx, stop := context.WithCancelCause(context.Background())
	timer := time.AfterFunc(30*time.Millisecond, func() { stop(temporalworkflow.ErrActivityWorkerStopping) })
	defer timer.Stop()
	return f.executor.observeDeploy(ctx, f.step, f.serverID, f.input, true)
}

func deployingMachine() *cancellationProvider {
	return &cancellationProvider{
		machine:    &provisioningdomain.Machine{ID: "machine-id", Status: provisioningdomain.MachineStatusDeploying, PowerState: provisioningdomain.PowerStateOn},
		powerState: provisioningdomain.PowerStateOn,
	}
}

func machineEvent(eventType string, at time.Time) provisioningdomain.MachineEvent {
	return provisioningdomain.MachineEvent{Type: eventType, OccurredAt: at.UTC().Format(time.RFC3339Nano)}
}

// TestObserveDeployReportsStalledProviderStage reproduces the lab-compute-3 incident: MAAS keeps the
// Machine powered on and Deploying while curtin waits on an unreachable mirror, so its newest event
// stays "Configuring OS". The Step must ask for attention with the stuck stage instead of waiting out
// the two-hour observation, and must not abort the provider deployment.
func TestObserveDeployReportsStalledProviderStage(t *testing.T) {
	now := time.Now()
	machine := deployingMachine()
	provider := &eventProvider{cancellationProvider: machine, events: []provisioningdomain.MachineEvent{
		machineEvent("Configuring OS", now.Add(-40*time.Minute)),
		machineEvent("Installing OS", now.Add(-41*time.Minute)),
		machineEvent("Deploying", now.Add(-58*time.Minute)),
	}}
	fixture := newProgressFixture(t, provider, now.Add(-time.Hour), 30*time.Minute)

	result := fixture.observeBriefly(t)

	if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil {
		t.Fatalf("observeDeploy() = %+v, want requires-attention Step", result)
	}
	if result.Error.Code != "deployment_provider_stage_stall" || result.Error.Stage != "configuring_os" || !result.Error.Retryable {
		t.Errorf("error = %+v, want retryable deployment_provider_stage_stall at configuring_os", result.Error)
	}
	if !strings.Contains(result.Error.Message, `"Configuring OS"`) {
		t.Errorf("message = %q, want the stuck provider stage named", result.Error.Message)
	}
	if machine.aborts != 0 {
		t.Errorf("stage stall aborted the provider deployment %d times, want 0", machine.aborts)
	}
	deployment := fixture.repository.server.Deployment
	if deployment.State != serverdomain.DeploymentDeploying || deployment.Stage != "configuring_os" || deployment.Code != "" {
		t.Errorf("deployment projection = %+v, want deploying at configuring_os with no failure code", deployment)
	}
	if !strings.Contains(deployment.StatusReason, "Configuring OS") {
		t.Errorf("deployment reason = %q, want the provider stage surfaced", deployment.StatusReason)
	}
}

// TestObserveDeployProjectsAdvancingStageWithoutStall proves progress is measured from the newest
// event: an ancient earlier stage does not stall a deployment whose provider just advanced.
func TestObserveDeployProjectsAdvancingStageWithoutStall(t *testing.T) {
	now := time.Now()
	provider := &eventProvider{cancellationProvider: deployingMachine(), events: []provisioningdomain.MachineEvent{
		machineEvent("Configuring OS", now.Add(-50*time.Minute)),
		machineEvent("Rebooting", now.Add(-time.Minute)),
	}}
	fixture := newProgressFixture(t, provider, now.Add(-time.Hour), 30*time.Minute)

	result := fixture.observeBriefly(t)

	if result.Status != operationdomain.TaskCanceled {
		t.Fatalf("observeDeploy() = %+v, want observation still running until the worker stop", result)
	}
	deployment := fixture.repository.server.Deployment
	if deployment.Stage != "rebooting" || !strings.Contains(deployment.StatusReason, "Rebooting") || deployment.Code != "" {
		t.Errorf("deployment projection = %+v, want the newest stage surfaced as a non-failure reason", deployment)
	}
}

// TestObserveDeployIgnoresEventsFromEarlierLifecycle keeps a previous deploy or release of the same
// Machine from counting as this attempt's progress: with no event since the attempt started, nothing
// is projected and the stall is measured from the attempt start.
func TestObserveDeployIgnoresEventsFromEarlierLifecycle(t *testing.T) {
	now := time.Now()
	staleEvents := []provisioningdomain.MachineEvent{machineEvent("Deployed", now.Add(-3*time.Hour))}

	t.Run("within grace keeps observing", func(t *testing.T) {
		provider := &eventProvider{cancellationProvider: deployingMachine(), events: staleEvents}
		fixture := newProgressFixture(t, provider, now.Add(-5*time.Minute), 30*time.Minute)

		result := fixture.observeBriefly(t)

		if result.Status != operationdomain.TaskCanceled {
			t.Fatalf("observeDeploy() = %+v, want observation still running", result)
		}
		if reason := fixture.repository.server.Deployment.StatusReason; reason != "" {
			t.Errorf("deployment reason = %q, want no stale provider event surfaced", reason)
		}
	})

	t.Run("past grace reports no progress", func(t *testing.T) {
		provider := &eventProvider{cancellationProvider: deployingMachine(), events: staleEvents}
		fixture := newProgressFixture(t, provider, now.Add(-5*time.Minute), time.Minute)

		result := fixture.observeBriefly(t)

		if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil {
			t.Fatalf("observeDeploy() = %+v, want requires-attention Step", result)
		}
		if result.Error.Code != "deployment_provider_stage_stall" || result.Error.Stage != "deployment_progress" {
			t.Errorf("error = %+v, want stall with no recorded stage", result.Error)
		}
		if strings.Contains(result.Error.Message, "Deployed") {
			t.Errorf("message = %q, must not cite an event from an earlier lifecycle", result.Error.Message)
		}
	})
}

// TestObserveDeployWithoutProgressDataKeepsObserving proves missing data is never read as a stall:
// a provider without an event stream, or a failed event read, leaves the power-on diagnosis and the
// two-hour observation authoritative.
func TestObserveDeployWithoutProgressDataKeepsObserving(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name     string
		provider provisioningdomain.OSProvisioningProvider
	}{
		{name: "provider without event stream", provider: deployingMachine()},
		{name: "event read failure", provider: &eventProvider{cancellationProvider: deployingMachine(), readErr: errors.New("events unavailable")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newProgressFixture(t, tc.provider, now.Add(-time.Hour), time.Nanosecond)

			result := fixture.observeBriefly(t)

			if result.Status != operationdomain.TaskCanceled {
				t.Fatalf("observeDeploy() = %+v, want observation still running", result)
			}
		})
	}
}

func TestProviderStepStageStallTimeout(t *testing.T) {
	if got := (providerStepExecutor{}).stageStallTimeout(); got != defaultDeploymentStageStallWait {
		t.Errorf("default stage-stall timeout = %s, want %s", got, defaultDeploymentStageStallWait)
	}
	if defaultDeploymentStageStallWait <= defaultDeploymentPowerOnWait {
		t.Errorf("stage-stall window %s must exceed the power-on window %s so power-off keeps its own diagnosis",
			defaultDeploymentStageStallWait, defaultDeploymentPowerOnWait)
	}
	if got := (providerStepExecutor{stageStallWait: time.Second}).stageStallTimeout(); got != time.Second {
		t.Errorf("custom stage-stall timeout = %s, want 1s", got)
	}
}

func TestProviderEventStage(t *testing.T) {
	cases := map[string]string{
		"Configuring OS":      "configuring_os",
		"Performing PXE boot": "performing_pxe_boot",
		"  Loading ephemeral": "loading_ephemeral",
		"Image Deployed!":     "image_deployed",
		"---":                 "deployment_progress",
	}
	for eventType, want := range cases {
		if got := providerEventStage(eventType); got != want {
			t.Errorf("providerEventStage(%q) = %q, want %q", eventType, got, want)
		}
	}
}

func TestLatestProviderEventSkipsUnparseableAndOlderEvents(t *testing.T) {
	start := time.Date(2026, 10, 2, 16, 30, 0, 0, time.UTC)
	events := []provisioningdomain.MachineEvent{
		{Type: "Garbled", OccurredAt: "Fri, 02 Oct. 2026 99:99:99"},
		machineEvent("Released", start.Add(-time.Hour)),
		machineEvent("Installing OS", start.Add(17*time.Minute)),
		machineEvent("Configuring OS", start.Add(18*time.Minute)),
		{Type: "", OccurredAt: start.Add(time.Hour).Format(time.RFC3339)},
	}

	latest, ok := latestProviderEvent(events, start)

	if !ok || latest.eventType != "Configuring OS" || !latest.at.Equal(start.Add(18*time.Minute)) {
		t.Errorf("latestProviderEvent() = %+v, %v; want Configuring OS at %s", latest, ok, start.Add(18*time.Minute))
	}
	if _, ok := latestProviderEvent(events[:2], start); ok {
		t.Error("latestProviderEvent() found an event, want none when every event predates the attempt or is unparseable")
	}
}
