package app

import (
	"context"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// activeServerWorkReader lets provisioning reject Lock without depending on operation
// repository types. It reports only identifiers needed for an actionable conflict.
type activeServerWorkReader struct {
	operations     operationdomain.ExecutionRepository
	orchestrations operationdomain.OrchestrationRepository
	tasks          provisioningdomain.ProvisioningTaskRepository
}

func (r activeServerWorkReader) ActiveWork(
	ctx context.Context,
	serverID string,
) (provisioningapp.ActiveServerWork, error) {
	work := provisioningapp.ActiveServerWork{}
	operations, err := r.operations.FindActiveByServerIDs(ctx, []string{serverID})
	if err != nil {
		return work, err
	}
	for _, operation := range operations {
		work.OperationIDs = append(work.OperationIDs, operation.ID)
	}
	if r.orchestrations != nil {
		v3, _, listErr := r.orchestrations.List(ctx, operationdomain.OrchestrationFilter{ServerID: serverID, ActiveOnly: true})
		if listErr != nil {
			return work, listErr
		}
		for _, operation := range v3 {
			work.OperationIDs = append(work.OperationIDs, operation.ID)
		}
	}
	tasks, err := r.tasks.ListByServer(ctx, serverID)
	if err != nil {
		return work, err
	}
	for _, task := range tasks {
		if task.Status == provisioningdomain.ProvisioningTaskPending ||
			task.Status == provisioningdomain.ProvisioningTaskRunning {
			work.TaskIDs = append(work.TaskIDs, task.ID)
		}
	}
	return work, nil
}
