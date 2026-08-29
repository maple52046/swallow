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
	return nil, operationdomain.ErrOperationNotFound
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

func TestClusterLifecycleReaderBatchesAndDerivesLatestOperation(t *testing.T) {
	deployedAt := time.Now().UTC().Add(-time.Hour)
	uninstalledAt := deployedAt.Add(time.Minute)
	repo := &lifecycleExecutionRepo{items: []*operationdomain.ExecutionOperation{
		{
			ID: "uninstall-1", ClusterID: "cluster-deployed",
			Kind:            operationdomain.OperationKindUninstallKubernetes,
			TargetServerIDs: []string{"server-1"},
			Execution:       operationdomain.Execution{Status: operationdomain.StatusFailed},
			RequestedAt:     uninstalledAt,
		},
		{
			ID: "deploy-1", ClusterID: "cluster-deployed",
			Kind:            operationdomain.OperationKindDeployKubernetes,
			TargetServerIDs: []string{"server-1", "server-2"},
			Execution:       operationdomain.Execution{Status: operationdomain.StatusSucceeded},
			RequestedAt:     deployedAt,
		},
	}}
	reader := clusterLifecycleReader{operations: repo}

	result, err := reader.Read(
		context.Background(),
		[]string{"cluster-deployed", "cluster-registered"},
	)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(repo.filter.ClusterIDs) != 2 || len(repo.filter.Kinds) != 2 {
		t.Fatalf("batch filter = %+v", repo.filter)
	}
	deployed := result["cluster-deployed"]
	if deployed.Origin != "deployed" || deployed.State != "uninstall_failed" ||
		deployed.OperationID != "uninstall-1" {
		t.Errorf("deployed lifecycle = %+v", deployed)
	}
	if got := deployed.Deployment.TargetServerIDs; len(got) != 2 || got[1] != "server-2" {
		t.Errorf("deployment target snapshot = %v", got)
	}
	registered := result["cluster-registered"]
	if registered.Origin != "registered" || registered.State != "registered" {
		t.Errorf("registered lifecycle = %+v", registered)
	}
}
