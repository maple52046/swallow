package app

import (
	"context"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

type lifecycleExecutionRepo struct {
	filter operationdomain.ExecutionListFilter
	items  []*operationdomain.ExecutionOperation
}

func (r *lifecycleExecutionRepo) Create(context.Context, *operationdomain.ExecutionOperation) error {
	return nil
}

func (r *lifecycleExecutionRepo) FindByID(
	context.Context,
	string,
) (*operationdomain.ExecutionOperation, error) {
	return nil, operationdomain.ErrWorkflowNotFound
}

func (r *lifecycleExecutionRepo) List(
	_ context.Context,
	filter operationdomain.ExecutionListFilter,
) (operationdomain.ExecutionListResult, error) {
	r.filter = filter
	return operationdomain.ExecutionListResult{
		Operations: r.items, Total: len(r.items),
	}, nil
}

func (r *lifecycleExecutionRepo) FindActiveByServerIDs(
	context.Context,
	[]string,
) ([]*operationdomain.ExecutionOperation, error) {
	return nil, nil
}

func (r *lifecycleExecutionRepo) Claim(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}

func (r *lifecycleExecutionRepo) UpdateExecution(
	context.Context,
	string,
	string,
	operationdomain.Execution,
) error {
	return nil
}

func (r *lifecycleExecutionRepo) MarkExpiredIndeterminate(
	context.Context,
	time.Time,
) (int64, error) {
	return 0, nil
}

func (r *lifecycleExecutionRepo) SecretVars(context.Context, string) (map[string]any, error) {
	return nil, nil
}

func TestPlatformLifecycleReaderBatchesAndDerivesLatestOperation(t *testing.T) {
	deployedAt := time.Now().UTC().Add(-time.Hour)
	uninstalledAt := deployedAt.Add(time.Minute)
	repo := &lifecycleExecutionRepo{items: []*operationdomain.ExecutionOperation{
		{
			ID: "uninstall-1", PlatformID: "platform-deployed",
			Kind:            operationdomain.WorkflowKindUninstallKubernetes,
			TargetServerIDs: []string{"server-1"},
			Execution:       operationdomain.Execution{Status: operationdomain.StatusFailed},
			RequestedAt:     uninstalledAt,
		},
		{
			ID: "deploy-1", PlatformID: "platform-deployed",
			Kind:            operationdomain.WorkflowKindDeployKubernetes,
			TargetServerIDs: []string{"server-1", "server-2"},
			ExtraVars: map[string]any{
				"swallow_k0s_roles": map[string]any{
					"server-1": "control-plane", "server-2": "worker",
				},
				"swallow_k0s_workload_controller_ids": []any{"server-1"},
			},
			Execution:   operationdomain.Execution{Status: operationdomain.StatusSucceeded},
			RequestedAt: deployedAt,
		},
	}}
	reader := platformLifecycleReader{operations: repo}

	result, err := reader.Read(
		context.Background(),
		[]string{"platform-deployed", "platform-registered"},
	)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The lifecycle reader queries deploy-kubernetes, configure-slurm, uninstall-kubernetes, and
	// uninstall-slurm so Slurm deploy and uninstall drive the same lifecycle projection as k0s.
	if len(repo.filter.PlatformIDs) != 2 || len(repo.filter.Kinds) != 4 {
		t.Fatalf("batch filter = %+v", repo.filter)
	}
	deployed := result["platform-deployed"]
	if deployed.Origin != "deployed" || deployed.State != "uninstall_failed" ||
		deployed.OperationID != "uninstall-1" {
		t.Errorf("deployed lifecycle = %+v", deployed)
	}
	if got := deployed.Deployment.TargetServerIDs; len(got) != 2 || got[1] != "server-2" {
		t.Errorf("deployment target snapshot = %v", got)
	}
	if deployed.Deployment.Intent == nil ||
		deployed.Deployment.Intent.Topology != "multi-node" {
		t.Fatalf("deployment intent = %+v", deployed.Deployment.Intent)
	}
	assignments := deployed.Deployment.Intent.RoleAssignments
	if len(assignments) != 2 || !assignments[0].RunWorkloads || assignments[1].RunWorkloads {
		t.Errorf("deployment role assignments = %+v", assignments)
	}
	registered := result["platform-registered"]
	if registered.Origin != "registered" || registered.State != "registered" {
		t.Errorf("registered lifecycle = %+v", registered)
	}
}

func TestDeploymentIntentProjectsStandaloneWorkloadController(t *testing.T) {
	intent := deploymentIntent(&operationdomain.ExecutionOperation{
		TargetServerIDs: []string{"server-1"},
		ExtraVars: map[string]any{
			"swallow_k0s_roles":                   map[string]any{"server-1": "control-plane"},
			"swallow_k0s_workload_controller_ids": []string{"server-1"},
		},
	})

	if intent == nil || intent.Topology != "standalone" {
		t.Fatalf("standalone deployment intent = %+v", intent)
	}
	if len(intent.RoleAssignments) != 1 || !intent.RoleAssignments[0].RunWorkloads {
		t.Errorf("standalone role assignments = %+v", intent.RoleAssignments)
	}
}
