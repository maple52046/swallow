package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

const retryTestImage = "custom/ubuntu-24.04-rocm"

// retryProvider is a fake provisioner whose Machine state the fake release and deployment move,
// so a test can drive the whole retry: unusable -> ready -> deployed and SSH-reachable. It adds the
// operator-state actions a broken or rescue Machine needs to return to ready.
type retryProvider struct {
	cancellationProvider
	markFixed int
	// installationLine and installationErr are what the fake installation log read returns;
	// installationSince records the lower bound the executor asked for.
	installationLine  string
	installationErr   error
	installationSince time.Time
	// abortTo, when set, is the state Abort leaves the Machine in, the way MAAS parks an aborted
	// deployment at Allocated.
	abortTo provisioningdomain.MachineStatus
}

func (p *retryProvider) Abort(context.Context, string) (*provisioningdomain.Machine, error) {
	p.aborts++
	if p.abortTo != "" {
		p.machine.Status = p.abortTo
	}
	return p.machine, nil
}

func (p *retryProvider) LatestInstallationFailure(_ context.Context, _ string, since time.Time) (string, error) {
	p.installationSince = since
	return p.installationLine, p.installationErr
}

func (p *retryProvider) Lock(context.Context, string) (*provisioningdomain.Machine, error) {
	return p.machine, nil
}
func (p *retryProvider) Unlock(context.Context, string) (*provisioningdomain.Machine, error) {
	return p.machine, nil
}
func (p *retryProvider) MarkBroken(context.Context, string) (*provisioningdomain.Machine, error) {
	p.machine.Status = provisioningdomain.MachineStatusBroken
	return p.machine, nil
}
func (p *retryProvider) MarkFixed(context.Context, string) (*provisioningdomain.Machine, error) {
	p.markFixed++
	p.machine.Status = provisioningdomain.MachineStatusReady
	return p.machine, nil
}
func (p *retryProvider) EnterRescueMode(context.Context, string) (*provisioningdomain.Machine, error) {
	return p.machine, nil
}
func (p *retryProvider) ExitRescueMode(context.Context, string) (*provisioningdomain.Machine, error) {
	return p.machine, nil
}

type retryProviderFactory struct{ provider *retryProvider }

func (f retryProviderFactory) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

// retryRelease returns the fake Machine to ready, standing in for a provider Release.
type retryRelease struct {
	provider *retryProvider
	calls    int
}

func (r *retryRelease) ExecuteWithOptions(context.Context, provisioningapp.ReleaseServerInput) (*provisioningapp.ProvisioningStateItem, error) {
	r.calls++
	r.provider.machine.Status = provisioningdomain.MachineStatusReady
	return &provisioningapp.ProvisioningStateItem{State: string(provisioningdomain.MachineStatusReady)}, nil
}

// retryDeployments installs the requested image with a reachable address. Like Swallow's real
// preflight it refuses a Machine that is not ready before any provider write, and err forces that
// refusal regardless of state. ephemeral installs the image as a memory deployment, and fails
// leaves the Machine failed the way a provider-side installation failure does.
type retryDeployments struct {
	provider  *retryProvider
	calls     int
	err       error
	ephemeral bool
	fails     bool
}

func (d *retryDeployments) Execute(context.Context, provisioningapp.DeployServersInput) (*provisioningapp.DeployServersResult, error) {
	d.calls++
	if d.err != nil {
		return nil, d.err
	}
	if d.provider.machine.Status != provisioningdomain.MachineStatusReady {
		return nil, fmt.Errorf("%w: server server-id: The Server provisioning state is not ready.", provisioningdomain.ErrDeploymentBatchConflict)
	}
	if d.fails {
		d.provider.machine.Status = provisioningdomain.MachineStatusFailed
		return &provisioningapp.DeployServersResult{Requested: 1}, nil
	}
	d.provider.machine.Status = provisioningdomain.MachineStatusDeployed
	d.provider.machine.DistroSeries = retryTestImage
	d.provider.machine.IPAddresses = []string{"10.0.0.5"}
	d.provider.machine.Ephemeral = d.ephemeral
	return &provisioningapp.DeployServersResult{Requested: 1}, nil
}

// retryHarness builds a provider Step executor over one Server whose provider Machine is in state.
func retryHarness(state provisioningdomain.MachineStatus, distroSeries string) (providerStepExecutor, *serverdomain.Server, *retryProvider, *retryRelease, *retryDeployments) {
	server := &serverdomain.Server{ID: "server-id", Source: serverdomain.Source{IntegrationID: "integration-id", ProviderMachineID: "machine-id"}}
	repository := &deploymentProjectionTestRepo{server: server}
	provider := &retryProvider{cancellationProvider: cancellationProvider{machine: &provisioningdomain.Machine{
		ID: "machine-id", Status: state, DistroSeries: distroSeries,
	}}}
	factory := retryProviderFactory{provider: provider}
	release := &retryRelease{provider: provider}
	deployments := &retryDeployments{provider: provider}
	executor := providerStepExecutor{
		deployments: deployments,
		release:     release,
		refresh:     provisioningapp.NewRefreshServerUseCase(repository, factory, nil),
		servers:     repository,
		providers:   factory,
		poll:        time.Millisecond,
		sshProbe:    func(context.Context, string, int) error { return nil },
	}
	return executor, server, provider, release, deployments
}

// provisionStep is a provision-os Step for the harness Server at the given operator attempt and
// Temporal activity attempt.
func provisionStep(attempt, activityAttempt int) temporalworkflow.StepExecutionInput {
	image := retryTestImage
	return temporalworkflow.StepExecutionInput{
		OperationID: "operation-id", SiteID: "site-id", ActivityAttempt: activityAttempt,
		Step: workflowdomain.Task{
			ID: "provision-server-id", Kind: "provision-os", Attempt: attempt,
			Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: "server-id"}},
			Parameters: map[string]any{"request": provisioningapp.DeployServersInput{
				ServerIDs: []string{"server-id"},
				Settings:  provisioningapp.DeploymentSettingsInput{ImageID: &image},
			}},
		},
	}
}

// TestRetriedDeploymentRecoversUnusableServerBeforeRedeploying reproduces lab-afde-mi308-3: a
// retried provision-os whose Machine the provider left failed was refused by the ready-only
// preflight on every retry and parked again forever. An explicit retry now returns the Machine to
// ready with the Recover plan for its state and then redeploys the frozen intent.
func TestRetriedDeploymentRecoversUnusableServerBeforeRedeploying(t *testing.T) {
	tests := []struct {
		state         provisioningdomain.MachineStatus
		wantReleases  int
		wantMarkFixed int
	}{
		{state: provisioningdomain.MachineStatusFailed, wantReleases: 1},
		{state: provisioningdomain.MachineStatusAllocated, wantReleases: 1},
		{state: provisioningdomain.MachineStatusBroken, wantMarkFixed: 1},
	}
	for _, tc := range tests {
		t.Run(string(tc.state), func(t *testing.T) {
			executor, server, provider, release, deployments := retryHarness(tc.state, "")

			result := executor.Execute(context.Background(), provisionStep(2, 1))

			if result.Status != workflowdomain.TaskSucceeded {
				t.Fatalf("retried deployment = %+v, want succeeded after recovery and redeploy", result)
			}
			if release.calls != tc.wantReleases || provider.markFixed != tc.wantMarkFixed {
				t.Errorf("recovery primitives = %d releases, %d mark fixed; want %d, %d",
					release.calls, provider.markFixed, tc.wantReleases, tc.wantMarkFixed)
			}
			if deployments.calls != 1 {
				t.Errorf("deployments submitted = %d, want 1 after the Server returned to ready", deployments.calls)
			}
			if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentSucceeded {
				t.Errorf("Server deployment = %+v, want succeeded", server.Deployment)
			}
		})
	}
}

// TestDeploymentRetryLeavesOtherStatesToTheirOwnRules covers the states the recover-then-redeploy
// path must not touch: a first attempt resumed by an activity retry reports the provider failure
// instead of recovering on its own, a matching deployed image is already done, and a different
// deployed image is never replaced by a retry.
func TestDeploymentRetryLeavesOtherStatesToTheirOwnRules(t *testing.T) {
	tests := []struct {
		name          string
		state         provisioningdomain.MachineStatus
		distroSeries  string
		step          temporalworkflow.StepExecutionInput
		wantStatus    workflowdomain.TaskStatus
		wantCode      string
		wantRetryable bool
	}{
		{
			name: "activity retry of the first attempt reports the failed deployment", state: provisioningdomain.MachineStatusFailed,
			step: provisionStep(1, 2), wantStatus: workflowdomain.TaskFailed, wantCode: "deployment_failed", wantRetryable: true,
		},
		{
			name: "retry of a deployment already installed with the image succeeds", state: provisioningdomain.MachineStatusDeployed,
			distroSeries: retryTestImage, step: provisionStep(2, 1), wantStatus: workflowdomain.TaskSucceeded,
		},
		{
			name: "retry never replaces a different deployed image", state: provisioningdomain.MachineStatusDeployed,
			distroSeries: "ubuntu/noble", step: provisionStep(2, 1), wantStatus: workflowdomain.TaskFailed, wantCode: "deployment_image_mismatch",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			executor, _, provider, release, deployments := retryHarness(tc.state, tc.distroSeries)
			if tc.state == provisioningdomain.MachineStatusDeployed {
				provider.machine.IPAddresses = []string{"10.0.0.5"}
			}

			result := executor.Execute(context.Background(), tc.step)

			if result.Status != tc.wantStatus {
				t.Fatalf("result = %+v, want status %q", result, tc.wantStatus)
			}
			if tc.wantCode != "" && (result.Error == nil || result.Error.Code != tc.wantCode || result.Error.Retryable != tc.wantRetryable) {
				t.Errorf("result error = %+v, want code %q retryable %v", result.Error, tc.wantCode, tc.wantRetryable)
			}
			if release.calls != 0 || provider.markFixed != 0 || deployments.calls != 0 {
				t.Errorf("provider writes = %d releases, %d mark fixed, %d deployments; want none",
					release.calls, provider.markFixed, deployments.calls)
			}
		})
	}
}

// TestDeploymentRetryRefusesLayoutThatNeedsInspection reproduces the second lab-afde-mi308-3
// failure: each retry released the Machine and redeployed onto a storage layout that cannot boot
// UEFI, failing in curtin the same way. A disk retry now ends before Recover with a non-retryable
// inspection_required, so the Operation frees the Server for Inspect; a memory retry never
// partitions the disk and still recovers and redeploys.
func TestDeploymentRetryRefusesLayoutThatNeedsInspection(t *testing.T) {
	const reason = "MAAS boots this Server with UEFI, but its storage layout has no EFI system partition."

	t.Run("disk retry", func(t *testing.T) {
		executor, _, provider, release, deployments := retryHarness(provisioningdomain.MachineStatusFailed, "")
		provider.machine.InspectionRequired = true
		provider.machine.InspectionRequiredReason = reason

		result := executor.Execute(context.Background(), provisionStep(2, 1))

		if result.Status != workflowdomain.TaskFailed || result.Error == nil {
			t.Fatalf("result = %+v, want a failed Step", result)
		}
		if result.Error.Code != "inspection_required" || result.Error.Retryable ||
			result.Error.Stage != "deployment_preflight" || result.Error.Message != reason {
			t.Errorf("result error = %+v, want non-retryable inspection_required at deployment_preflight with the reason", result.Error)
		}
		if release.calls != 0 || provider.markFixed != 0 || deployments.calls != 0 {
			t.Errorf("provider writes = %d releases, %d mark fixed, %d deployments; want none",
				release.calls, provider.markFixed, deployments.calls)
		}
	})

	t.Run("memory retry", func(t *testing.T) {
		executor, _, provider, release, deployments := retryHarness(provisioningdomain.MachineStatusFailed, "")
		provider.machine.InspectionRequired = true
		provider.machine.InspectionRequiredReason = reason
		step := provisionStep(2, 1)
		request := step.Step.Parameters["request"].(provisioningapp.DeployServersInput)
		ram := true
		request.Settings.Ephemeral = &ram
		step.Step.Parameters["request"] = request
		deployments.ephemeral = true

		result := executor.Execute(context.Background(), step)

		if result.Status != workflowdomain.TaskSucceeded {
			t.Fatalf("result = %+v, want succeeded after recovery and redeploy", result)
		}
		if release.calls != 1 || deployments.calls != 1 {
			t.Errorf("releases = %d, deployments = %d; want 1 and 1", release.calls, deployments.calls)
		}
	})
}

// TestDeploymentFailureNamesTheProvisionerCause guards the diagnosis gap behind the repeated
// lab-afde-mi308-3 retries: the Step said only that deployment failed, while MAAS knew why. The
// message now carries the provider's error description and this attempt's installation error
// line, falling back step by step; the code and retryability do not change.
func TestDeploymentFailureNamesTheProvisionerCause(t *testing.T) {
	const (
		description = "Installation failed (refer to the installation log for more information)."
		line        = "An error occured handling 'nvme0n1-part2': RuntimeError - Cannot find previous partition on disk /dev/nvme0n1"
	)
	tests := []struct {
		name        string
		description string
		line        string
		readErr     error
		want        string
	}{
		{
			name: "description and installation error", description: description, line: line,
			want: "The provisioner reported that OS deployment failed: Installation failed (refer to the installation log for more information). Installation error: " + line,
		},
		{
			name: "unreadable installation log", description: description, readErr: fmt.Errorf("MAAS unavailable"),
			want: "The provisioner reported that OS deployment failed: Installation failed (refer to the installation log for more information).",
		},
		{
			name: "nothing to explain",
			want: "The provisioner reported that OS deployment failed.",
		},
	}
	startedAt := time.Date(2026, 10, 19, 5, 22, 0, 0, time.UTC)
	for _, tc := range tests {
		for _, path := range []struct {
			name string
			step temporalworkflow.StepExecutionInput
			prep func(*retryProvider, *retryDeployments)
		}{
			{name: "observed", step: provisionStep(1, 1), prep: func(_ *retryProvider, d *retryDeployments) { d.fails = true }},
			{name: "activity retry", step: provisionStep(1, 2), prep: func(p *retryProvider, _ *retryDeployments) {
				p.machine.Status = provisioningdomain.MachineStatusFailed
			}},
		} {
			t.Run(tc.name+"/"+path.name, func(t *testing.T) {
				executor, _, provider, _, deployments := retryHarness(provisioningdomain.MachineStatusReady, "")
				provider.machine.ErrorDescription = tc.description
				provider.installationLine, provider.installationErr = tc.line, tc.readErr
				path.prep(provider, deployments)
				step := path.step
				step.Step.StartedAt = &startedAt

				result := executor.Execute(context.Background(), step)

				if result.Status != workflowdomain.TaskFailed || result.Error == nil {
					t.Fatalf("result = %+v, want a failed Step", result)
				}
				if result.Error.Code != "deployment_failed" || !result.Error.Retryable || result.Error.Stage != "deployment" {
					t.Errorf("result error = %+v, want retryable deployment_failed at deployment", result.Error)
				}
				if result.Error.Message != tc.want {
					t.Errorf("message = %q\nwant      %q", result.Error.Message, tc.want)
				}
				if !provider.installationSince.Equal(startedAt) {
					t.Errorf("installation read since = %v, want the attempt start %v", provider.installationSince, startedAt)
				}
			})
		}
	}
}

// TestPreflightRejectionIsAKnownOutcome verifies a deployment Swallow refused before writing to
// the provider ends the Step with a non-retryable deployment_preflight_rejected, instead of the
// retryable provider_outcome_unknown that invited the operator to repeat a refusal forever.
func TestPreflightRejectionIsAKnownOutcome(t *testing.T) {
	executor, _, _, _, deployments := retryHarness(provisioningdomain.MachineStatusReady, "")
	deployments.err = fmt.Errorf("%w: server server-id: The Server provisioning state is not ready.", provisioningdomain.ErrDeploymentBatchConflict)

	result := executor.Execute(context.Background(), provisionStep(1, 1))

	if result.Status != workflowdomain.TaskFailed || result.Error == nil {
		t.Fatalf("result = %+v, want a failed Step", result)
	}
	if result.Error.Code != "deployment_preflight_rejected" || result.Error.Retryable || result.Error.Stage != "deployment_preflight" {
		t.Errorf("result error = %+v, want non-retryable deployment_preflight_rejected at deployment_preflight", result.Error)
	}
}
