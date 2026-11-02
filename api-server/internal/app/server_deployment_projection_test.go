package app

import (
	"context"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

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
	err := observer.projectDeployment(context.Background(), "operation-id", workflowdomain.Task{
		ID: "provision-server", Kind: "provision-os", Attempt: 1,
		Status:  workflowdomain.TaskFailed,
		Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: server.ID}},
		Error: &workflowdomain.NormalizedError{
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

func TestServerDeploymentClearedCompletionClearsResultAfterSuccessfulRelease(t *testing.T) {
	server := &serverdomain.Server{
		ID:         "server-id",
		Deployment: &serverdomain.DeploymentStatus{State: serverdomain.DeploymentSucceeded},
	}
	assignments := newFakeAssignmentRepo()
	for _, assignment := range []*softwaredomain.Assignment{
		{ServerID: server.ID, Kind: softwaredomain.KindDockerCE, State: softwaredomain.StateInstalled, LastWorkflowID: "install-1"},
		{ServerID: server.ID, Kind: softwaredomain.KindNFS, State: softwaredomain.StateFailed},
		{ServerID: "other", Kind: softwaredomain.KindDockerCE, State: softwaredomain.StateInstalled},
	} {
		_ = assignments.Upsert(context.Background(), assignment)
	}
	cleared := workflowProjections(&deploymentProjectionTestRepo{server: server}, assignments, nil, nil, nil)[completionServerDeploymentCleared]
	release := workflowdomain.Task{
		ID: "release-server", Kind: "release-os", Status: workflowdomain.TaskRunning,
		Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: server.ID}},
	}
	transition := workflowapp.TaskTransition{Workflow: &workflowdomain.Workflow{ID: "release-operation"}, Task: release}
	if err := cleared.Project(context.Background(), transition); err != nil || server.Deployment == nil {
		t.Fatalf("a running release cleared the deployment (%v): %+v", err, server.Deployment)
	}
	transition.Task.Status = workflowdomain.TaskSucceeded
	if err := cleared.Project(context.Background(), transition); err != nil {
		t.Fatal(err)
	}
	if server.Deployment != nil {
		t.Fatalf("deployment = %+v, want cleared after release", server.Deployment)
	}
	// The software went with the OS: the Server's live assignments are absent, keeping their last
	// Workflow, and another Server's are untouched.
	for _, kind := range []softwaredomain.Kind{softwaredomain.KindDockerCE, softwaredomain.KindNFS} {
		got, _ := assignments.FindByServerAndKind(context.Background(), server.ID, kind)
		if got.State != softwaredomain.StateAbsent {
			t.Errorf("%s assignment = %s, want absent after release", kind, got.State)
		}
	}
	if got, _ := assignments.FindByServerAndKind(context.Background(), server.ID, softwaredomain.KindDockerCE); got.LastWorkflowID != "install-1" {
		t.Errorf("docker-ce last Workflow = %q, want it kept", got.LastWorkflowID)
	}
	if got, _ := assignments.FindByServerAndKind(context.Background(), "other", softwaredomain.KindDockerCE); got.State != softwaredomain.StateInstalled {
		t.Errorf("another Server's assignment = %s, want installed", got.State)
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
	err := observer.projectDeployment(context.Background(), "operation-id", workflowdomain.Task{
		ID: "provision-server-id", Kind: "provision-os", Attempt: 1,
		Status:  workflowdomain.TaskCanceled,
		Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: server.ID}},
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
	err := observer.projectDeployment(context.Background(), "operation-id", workflowdomain.Task{
		ID: "provision-server-id", Kind: "provision-os", Attempt: 1,
		Status:  workflowdomain.TaskCanceled,
		Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: server.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentCanceled {
		t.Fatalf("deployment = %+v, want canceled for an in-flight deploy", server.Deployment)
	}
}

// TestServerDeploymentObserverIgnoresStaleCancelOfLaterOutcome reproduces vm-gpu-01: its
// provision-os succeeded at attempt 6 after Job retries, and canceling the platform Workflow
// re-emitted the Task as canceled from the parent's start snapshot at attempt 1. The older
// attempt's cancel must not regress the recorded outcome.
func TestServerDeploymentObserverIgnoresStaleCancelOfLaterOutcome(t *testing.T) {
	for _, state := range []serverdomain.DeploymentState{serverdomain.DeploymentSucceeded, serverdomain.DeploymentFailed} {
		t.Run(string(state), func(t *testing.T) {
			server := &serverdomain.Server{
				ID: "server-id",
				Deployment: &serverdomain.DeploymentStatus{
					State: state, OperationID: "operation-id", StepID: "provision-server-id", Attempt: 6,
				},
			}
			observer := serverDeploymentStepObserver{servers: &deploymentProjectionTestRepo{server: server}}
			err := observer.projectDeployment(context.Background(), "operation-id", workflowdomain.Task{
				ID: "provision-server-id", Kind: "provision-os", Attempt: 1,
				Status:  workflowdomain.TaskCanceled,
				Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: server.ID}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if server.Deployment.State != state || server.Deployment.Attempt != 6 {
				t.Fatalf("deployment = %+v, want %s at attempt 6 kept", server.Deployment, state)
			}
		})
	}
}

// TestServerDeploymentObserverCancelAppliesToNewerAttempt verifies the guard covers only attempts
// no newer than the recorded outcome: a cancel for a later attempt of the same Step still applies,
// so a re-run that is canceled is not masked by an older succeeded attempt.
func TestServerDeploymentObserverCancelAppliesToNewerAttempt(t *testing.T) {
	server := &serverdomain.Server{
		ID: "server-id",
		Deployment: &serverdomain.DeploymentStatus{
			State: serverdomain.DeploymentSucceeded, OperationID: "operation-id",
			StepID: "provision-server-id", Attempt: 1,
		},
	}
	observer := serverDeploymentStepObserver{servers: &deploymentProjectionTestRepo{server: server}}
	err := observer.projectDeployment(context.Background(), "operation-id", workflowdomain.Task{
		ID: "provision-server-id", Kind: "provision-os", Attempt: 2,
		Status:  workflowdomain.TaskCanceled,
		Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: server.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentCanceled || server.Deployment.Attempt != 2 {
		t.Fatalf("deployment = %+v, want canceled for the newer attempt", server.Deployment)
	}
}
