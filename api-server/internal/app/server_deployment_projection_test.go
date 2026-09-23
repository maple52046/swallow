package app

import (
	"context"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

type legacyProjectionOperationRepo struct {
	operationdomain.WorkflowRepository
	operations []*operationdomain.Workflow
}

func (r *legacyProjectionOperationRepo) List(
	context.Context,
	operationdomain.WorkflowFilter,
) ([]*operationdomain.Workflow, int, error) {
	return r.operations, len(r.operations), nil
}

func TestServerDeploymentObserverKeepsProviderFactSeparateFromVerifiedFailure(t *testing.T) {
	now := time.Now().UTC()
	server := &serverdomain.Server{
		ID: "server-id",
		Provisioning: &serverdomain.ProvisioningStatus{
			State: "deployed", ProviderState: "Deployed",
		},
	}
	observer := serverDeploymentStepObserver{
		servers: &deploymentProjectionTestRepo{server: server},
	}
	err := observer.ObserveStep(context.Background(), "operation-id", operationdomain.Task{
		ID: "provision-server", Kind: "provision-os", Attempt: 1,
		Status:  operationdomain.TaskFailed,
		Targets: []operationdomain.ResourceReference{{Kind: "server", ID: server.ID}},
		Error: &operationdomain.NormalizedError{
			Code: "deployment_ssh_unreachable", Message: "SSH could not be reached.",
			Retryable: true, Stage: "ssh_readiness",
		},
		StartedAt: &now, FinishedAt: &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentFailed {
		t.Fatalf("deployment = %+v, want failed", server.Deployment)
	}
	if server.Deployment.Code != "deployment_ssh_unreachable" {
		t.Fatalf("deployment code = %q, want the stable code carried onto the axis", server.Deployment.Code)
	}
	if server.Deployment.Stage != "ssh_readiness" || server.Deployment.StatusReason != "SSH could not be reached." {
		t.Fatalf("deployment diagnostics = %+v", server.Deployment)
	}
	if server.Provisioning.State != "deployed" {
		t.Fatalf("provider lifecycle was rewritten to %q", server.Provisioning.State)
	}
}

func TestServerDeploymentObserverClearsResultAfterSuccessfulRelease(t *testing.T) {
	server := &serverdomain.Server{
		ID:         "server-id",
		Deployment: &serverdomain.DeploymentStatus{State: serverdomain.DeploymentSucceeded},
	}
	observer := serverDeploymentStepObserver{
		servers: &deploymentProjectionTestRepo{server: server},
	}
	err := observer.ObserveStep(context.Background(), "release-operation", operationdomain.Task{
		ID: "release-server", Kind: "release-os", Status: operationdomain.TaskSucceeded,
		Targets: []operationdomain.ResourceReference{{Kind: "server", ID: server.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Deployment != nil {
		t.Fatalf("deployment = %+v, want cleared after release", server.Deployment)
	}
}

func TestLegacyPlatformReadinessFailureProjectsPerServerDeploymentFailure(t *testing.T) {
	now := time.Now().UTC()
	server := &serverdomain.Server{
		ID:       "server-id",
		Observed: serverdomain.Observed{Hostname: "lab-control-1"},
		Provisioning: &serverdomain.ProvisioningStatus{
			State: "deployed", ProviderState: "Deployed",
		},
	}
	serverRepo := &deploymentProjectionTestRepo{server: server}
	operations := &legacyProjectionOperationRepo{operations: []*operationdomain.Workflow{{
		ID: "operation-id", Kind: operationdomain.WorkflowKindDeployKubernetes,
		SiteID: "site-id", RequestedAt: now,
		Steps: []operationdomain.Task{
			{
				ID: "provision-server-id", Kind: "provision-os", Attempt: 1,
				Status:    operationdomain.TaskSucceeded,
				Targets:   []operationdomain.ResourceReference{{Kind: "server", ID: server.ID}},
				StartedAt: &now, FinishedAt: &now,
			},
			{
				ID: "wait-for-ssh", Kind: "wait-for-ssh", Attempt: 1,
				Status:  operationdomain.TaskFailed,
				Targets: []operationdomain.ResourceReference{{Kind: "server", ID: server.ID}},
				Error: &operationdomain.NormalizedError{
					Code: "ssh_readiness_timeout", Stage: "ssh_readiness",
				},
				FinishedAt: &now,
			},
		},
	}}}

	err := reconcileLegacyDeploymentProjections(context.Background(), operations, providerStepExecutor{
		servers: serverRepo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentFailed {
		t.Fatalf("deployment = %+v, want failed", server.Deployment)
	}
	if server.Deployment.Stage != "ssh_readiness" {
		t.Fatalf("stage = %q, want ssh_readiness", server.Deployment.Stage)
	}
	if server.Deployment.StatusReason == "" {
		t.Fatal("status reason is empty, want per-Server missing-address diagnosis")
	}
	if server.Provisioning.State != "deployed" {
		t.Fatalf("provider state = %q, want installed OS fact preserved", server.Provisioning.State)
	}
}

// TestServerDeploymentObserverKeepsSucceededDeploymentOnWorkflowCancel verifies the core
// invariant: a completed OS deployment (provision-os) is not un-done by cancelling the
// enclosing Workflow. Cancelling a platform deploy re-emits the already-succeeded provision-os
// as canceled from the parent's stale step snapshot; the per-Server deployment axis (its own OS
// deployment result) must keep succeeded, since the cancellation belongs to the Workflow and
// Platform lifecycle, not this axis.
func TestServerDeploymentObserverKeepsSucceededDeploymentOnWorkflowCancel(t *testing.T) {
	started := time.Now().Add(-time.Hour).UTC()
	server := &serverdomain.Server{
		ID: "server-id",
		Deployment: &serverdomain.DeploymentStatus{
			State: serverdomain.DeploymentSucceeded, OperationID: "operation-id",
			StepID: "provision-server-id", Attempt: 1, StartedAt: started,
		},
	}
	observer := serverDeploymentStepObserver{servers: &deploymentProjectionTestRepo{server: server}}
	err := observer.ObserveStep(context.Background(), "operation-id", operationdomain.Task{
		ID: "provision-server-id", Kind: "provision-os", Attempt: 1,
		Status:  operationdomain.TaskCanceled,
		Targets: []operationdomain.ResourceReference{{Kind: "server", ID: server.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentSucceeded {
		t.Fatalf("deployment = %+v, want succeeded preserved after workflow cancel", server.Deployment)
	}
}

// TestServerDeploymentObserverCancelsInFlightDeploy verifies a genuinely in-flight deploy still
// cancels: the guard only protects a terminal outcome, not a deploying/verifying one.
func TestServerDeploymentObserverCancelsInFlightDeploy(t *testing.T) {
	server := &serverdomain.Server{
		ID: "server-id",
		Deployment: &serverdomain.DeploymentStatus{
			State: serverdomain.DeploymentDeploying, OperationID: "operation-id",
			StepID: "provision-server-id", Attempt: 1,
		},
	}
	observer := serverDeploymentStepObserver{servers: &deploymentProjectionTestRepo{server: server}}
	err := observer.ObserveStep(context.Background(), "operation-id", operationdomain.Task{
		ID: "provision-server-id", Kind: "provision-os", Attempt: 1,
		Status:  operationdomain.TaskCanceled,
		Targets: []operationdomain.ResourceReference{{Kind: "server", ID: server.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentCanceled {
		t.Fatalf("deployment = %+v, want canceled for an in-flight deploy", server.Deployment)
	}
}

// TestServerDeploymentObserverCancelAppliesToNewerAttempt verifies the guard is scoped to the
// same attempt: a cancel for a later attempt of the same Step still applies, so a re-run that is
// canceled is not masked by an older succeeded attempt.
func TestServerDeploymentObserverCancelAppliesToNewerAttempt(t *testing.T) {
	server := &serverdomain.Server{
		ID: "server-id",
		Deployment: &serverdomain.DeploymentStatus{
			State: serverdomain.DeploymentSucceeded, OperationID: "operation-id",
			StepID: "provision-server-id", Attempt: 1,
		},
	}
	observer := serverDeploymentStepObserver{servers: &deploymentProjectionTestRepo{server: server}}
	err := observer.ObserveStep(context.Background(), "operation-id", operationdomain.Task{
		ID: "provision-server-id", Kind: "provision-os", Attempt: 2,
		Status:  operationdomain.TaskCanceled,
		Targets: []operationdomain.ResourceReference{{Kind: "server", ID: server.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentCanceled || server.Deployment.Attempt != 2 {
		t.Fatalf("deployment = %+v, want canceled for the newer attempt", server.Deployment)
	}
}
