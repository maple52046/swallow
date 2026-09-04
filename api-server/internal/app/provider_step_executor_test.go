package app

import (
	"context"
	"strings"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

type deploymentProjectionTestRepo struct {
	serverdomain.ServerRepository
	server *serverdomain.Server
}

func (r *deploymentProjectionTestRepo) FindByID(context.Context, string) (*serverdomain.Server, error) {
	return r.server, nil
}

func (r *deploymentProjectionTestRepo) SetDeployment(_ context.Context, _ string, deployment *serverdomain.DeploymentStatus) error {
	r.server.Deployment = deployment
	return nil
}

func TestProviderStepRejectsDeploymentWithoutFrozenImage(t *testing.T) {
	result := (providerStepExecutor{}).Execute(context.Background(), temporalworkflow.StepExecutionInput{
		OperationID: "operation-id",
		Step: operationdomain.OperationStep{
			ID: "provision-server", Kind: "provision-os", Attempt: 1,
			Parameters: map[string]any{"request": map[string]any{
				"serverIds": []string{"server-id"},
				"settings":  map[string]any{},
			}},
		},
	})

	if result.Status != operationdomain.StepFailed || result.Error == nil {
		t.Fatalf("result = %+v, want failed Step with normalized error", result)
	}
	if result.Error.Code != "intent_snapshot_incomplete" || result.Error.Retryable {
		t.Fatalf("error = %+v, want non-retryable incomplete snapshot", result.Error)
	}
}

func TestDeploymentReadinessFailureWithoutAddressOffersRedeployRecovery(t *testing.T) {
	result := deploymentReadinessFailed(deploymentReadiness{serverName: "lab-control-1", port: 22})

	if result.Status != operationdomain.StepFailed || result.Error == nil {
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

	if result.Status != operationdomain.StepFailed || result.Error == nil {
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
	server := &serverdomain.Server{ID: "server-id"}
	executor := providerStepExecutor{servers: &deploymentProjectionTestRepo{server: server}}
	result := executor.Execute(context.Background(), temporalworkflow.StepExecutionInput{
		OperationID: "operation-id",
		Step: operationdomain.OperationStep{
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

	if result.Status != operationdomain.StepFailed {
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
}

func TestProviderStepReadinessTimeout(t *testing.T) {
	if got := (providerStepExecutor{}).readinessTimeout(); got != defaultDeploymentReadinessWait {
		t.Fatalf("default readiness timeout = %s", got)
	}
	if got := (providerStepExecutor{readinessWait: time.Second}).readinessTimeout(); got != time.Second {
		t.Fatalf("custom readiness timeout = %s", got)
	}
}

func TestInitialDeploymentProjectionClosesWorkerStartGap(t *testing.T) {
	server := &serverdomain.Server{ID: "server-id"}
	repository := &deploymentProjectionTestRepo{server: server}
	materializeInitialDeployments(context.Background(), repository, "operation-id", []operationdomain.OperationStep{
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
