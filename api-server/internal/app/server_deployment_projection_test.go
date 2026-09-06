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
