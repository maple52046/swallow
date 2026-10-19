package app

import (
	"context"
	"strings"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// returnCanceledDeploymentStep is the internal step a canceled Workflow issues for the harness
// Server's interrupted provision-os.
func returnCanceledDeploymentStep() temporalworkflow.StepExecutionInput {
	return temporalworkflow.StepExecutionInput{
		OperationID: "operation-id", SiteID: "site-id",
		Step: operationdomain.Task{
			ID: "provision-server-id/" + temporalworkflow.ReturnCanceledDeploymentKind, Kind: temporalworkflow.ReturnCanceledDeploymentKind,
			Attempt: 1, Targets: []operationdomain.ResourceReference{{Kind: "server", ID: "server-id"}},
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
		wantStatus   operationdomain.TaskStatus
		wantAborts   int
		wantReleases int
		wantReason   string
	}{
		{
			name: "deploying is aborted and returned to ready", state: provisioningdomain.MachineStatusDeploying,
			abortTo: provisioningdomain.MachineStatusAllocated, wantStatus: operationdomain.TaskSucceeded, wantAborts: 1, wantReleases: 1,
		},
		{
			name: "allocated after an abort is returned to ready", state: provisioningdomain.MachineStatusAllocated,
			wantStatus: operationdomain.TaskSucceeded, wantReleases: 1,
		},
		{
			name: "a finished deployment is kept", state: provisioningdomain.MachineStatusDeployed,
			wantStatus: operationdomain.TaskSkipped, wantReason: "deployed",
		},
		{
			name: "a failed deployment is kept", state: provisioningdomain.MachineStatusFailed,
			wantStatus: operationdomain.TaskSkipped, wantReason: "failed",
		},
		{
			name: "a ready Server needs nothing", state: provisioningdomain.MachineStatusReady,
			wantStatus: operationdomain.TaskSkipped, wantReason: "already Ready",
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

// TestReturnCanceledDeploymentStopsWhenAbortDoesNotTakeEffect bounds the wait: a provider that
// keeps reporting Deploying after the abort is reported for attention, and nothing is released.
func TestReturnCanceledDeploymentStopsWhenAbortDoesNotTakeEffect(t *testing.T) {
	executor, _, provider, release, _ := retryHarness(provisioningdomain.MachineStatusDeploying, "")
	executor.canceledDeploymentWait = 20 * time.Millisecond

	result := executor.Execute(context.Background(), returnCanceledDeploymentStep())

	if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil ||
		result.Error.Code != "canceled_deployment_still_in_progress" {
		t.Fatalf("result = %+v, want requires_attention canceled_deployment_still_in_progress", result)
	}
	if provider.aborts != 1 || release.calls != 0 {
		t.Errorf("provider calls = %d aborts, %d releases; want one abort and no release", provider.aborts, release.calls)
	}
}
