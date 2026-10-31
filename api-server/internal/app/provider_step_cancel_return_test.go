package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// returnCanceledDeploymentStep is the internal step a canceled Workflow issues for the harness
// Server's interrupted provision-os.
func returnCanceledDeploymentStep() temporalworkflow.StepExecutionInput {
	return temporalworkflow.StepExecutionInput{
		OperationID: "operation-id", SiteID: "site-id",
		Step: workflowdomain.Task{
			ID: "provision-server-id/" + temporalworkflow.ReturnCanceledDeploymentKind, Kind: temporalworkflow.ReturnCanceledDeploymentKind,
			Attempt: 1, Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: "server-id"}},
		},
	}
}

// TestReturnCanceledDeployment covers decision 060: a cancel returns only what the canceled
// deployment left in flight to Ready. A Deploying Machine is aborted and, once MAAS parks it at
// Allocated, released; a Machine that holds a deployment outcome (deployed, failed) is left as it
// is, as is one already Ready. The step never writes the Server's deployment result.
func TestReturnCanceledDeployment(t *testing.T) {
	tests := []struct {
		name         string
		state        provisioningdomain.MachineStatus
		abortTo      provisioningdomain.MachineStatus
		wantStatus   workflowdomain.TaskStatus
		wantAborts   int
		wantReleases int
		wantReason   string
	}{
		{
			name: "deploying is aborted and returned to ready", state: provisioningdomain.MachineStatusDeploying,
			abortTo: provisioningdomain.MachineStatusAllocated, wantStatus: workflowdomain.TaskSucceeded, wantAborts: 1, wantReleases: 1,
		},
		{
			name: "allocated after an abort is returned to ready", state: provisioningdomain.MachineStatusAllocated,
			wantStatus: workflowdomain.TaskSucceeded, wantReleases: 1,
		},
		{
			name: "a finished deployment is kept", state: provisioningdomain.MachineStatusDeployed,
			wantStatus: workflowdomain.TaskSkipped, wantReason: "deployed",
		},
		{
			name: "a failed deployment is kept", state: provisioningdomain.MachineStatusFailed,
			wantStatus: workflowdomain.TaskSkipped, wantReason: "failed",
		},
		{
			name: "a ready Server needs nothing", state: provisioningdomain.MachineStatusReady,
			wantStatus: workflowdomain.TaskSkipped, wantReason: "already Ready",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			executor, server, provider, release, deployments := retryHarness(tc.state, "")
			provider.abortTo = tc.abortTo

			result := executor.Execute(context.Background(), returnCanceledDeploymentStep())

			if result.Status != tc.wantStatus {
				t.Fatalf("result = %+v, want status %q", result, tc.wantStatus)
			}
			if provider.aborts != tc.wantAborts || release.calls != tc.wantReleases || deployments.calls != 0 {
				t.Errorf("provider calls = %d aborts, %d releases, %d deployments; want %d, %d, 0",
					provider.aborts, release.calls, deployments.calls, tc.wantAborts, tc.wantReleases)
			}
			if tc.wantReason != "" && !strings.Contains(result.WaitingReason, tc.wantReason) {
				t.Errorf("reason = %q, want it to mention %q", result.WaitingReason, tc.wantReason)
			}
			if server.Deployment != nil {
				t.Errorf("Server deployment = %+v, want the step to leave it to reconcile", server.Deployment)
			}
		})
	}
}

// releaseStep is a first-attempt release-os Step for the harness Server.
func releaseStep(unbindStaticIPs bool) temporalworkflow.StepExecutionInput {
	return temporalworkflow.StepExecutionInput{
		OperationID: "operation-id", SiteID: "site-id", ActivityAttempt: 1,
		Step: workflowdomain.Task{
			ID: "release-server-id", Kind: "release-os", Attempt: 1,
			Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: "server-id"}},
			Parameters: map[string]any{"request": provisioningapp.ReleaseServerInput{
				ServerID: "server-id", UnbindStaticIPs: unbindStaticIPs,
			}},
		},
	}
}

// TestReleaseObservesBeforeActingOnFirstAttempt covers decision 061: a Release that superseded a
// Workflow starts only after it ends, and a canceled deployment may already have returned the
// Server to Ready. The first attempt therefore observes first: Ready meets the goal without a provider
// Release, and a requested static IP cleanup that never ran is reported instead of hidden.
func TestReleaseObservesBeforeActingOnFirstAttempt(t *testing.T) {
	tests := []struct {
		name         string
		state        provisioningdomain.MachineStatus
		unbind       bool
		wantStatus   workflowdomain.TaskStatus
		wantCode     string
		wantReleases int
	}{
		{name: "already ready", state: provisioningdomain.MachineStatusReady, wantStatus: workflowdomain.TaskSucceeded},
		{
			name: "already ready with static IP cleanup requested", state: provisioningdomain.MachineStatusReady, unbind: true,
			wantStatus: workflowdomain.TaskFailed, wantCode: "release_cleanup_not_applied",
		},
		{name: "deployed is released", state: provisioningdomain.MachineStatusDeployed, wantStatus: workflowdomain.TaskSucceeded, wantReleases: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			executor, _, _, release, _ := retryHarness(tc.state, "")

			result := executor.Execute(context.Background(), releaseStep(tc.unbind))

			if result.Status != tc.wantStatus {
				t.Fatalf("result = %+v, want status %q", result, tc.wantStatus)
			}
			if tc.wantCode != "" && (result.Error == nil || result.Error.Code != tc.wantCode || result.Error.Retryable) {
				t.Errorf("result error = %+v, want non-retryable %s", result.Error, tc.wantCode)
			}
			if release.calls != tc.wantReleases {
				t.Errorf("provider releases = %d, want %d", release.calls, tc.wantReleases)
			}
		})
	}
}

// TestReturnCanceledDeploymentStopsWhenAbortDoesNotTakeEffect bounds the wait: a provider that
// keeps reporting Deploying after the abort is reported for attention, and nothing is released.
func TestReturnCanceledDeploymentStopsWhenAbortDoesNotTakeEffect(t *testing.T) {
	executor, _, provider, release, _ := retryHarness(provisioningdomain.MachineStatusDeploying, "")
	executor.canceledDeploymentWait = 20 * time.Millisecond

	result := executor.Execute(context.Background(), returnCanceledDeploymentStep())

	if result.Status != workflowdomain.TaskRequiresAttention || result.Error == nil ||
		result.Error.Code != "canceled_deployment_still_in_progress" {
		t.Fatalf("result = %+v, want requires_attention canceled_deployment_still_in_progress", result)
	}
	if provider.aborts != 1 || release.calls != 0 {
		t.Errorf("provider calls = %d aborts, %d releases; want one abort and no release", provider.aborts, release.calls)
	}
}
