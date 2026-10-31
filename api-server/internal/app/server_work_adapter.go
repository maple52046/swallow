package app

import (
	"context"

	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// activeServerWorkReader lets provisioning reject Lock without depending on operation
// repository types. It reports only identifiers needed for an actionable conflict.
type activeServerWorkReader struct {
	orchestrations workflowdomain.WorkflowRepository
	tasks          provisioningdomain.ProvisioningTaskRepository
}

func (r activeServerWorkReader) ActiveWork(
	ctx context.Context,
	serverID string,
) (provisioningapp.ActiveServerWork, error) {
	work := provisioningapp.ActiveServerWork{}
	workflows, _, err := r.orchestrations.List(ctx, workflowdomain.WorkflowFilter{ServerID: serverID, ActiveOnly: true})
	if err != nil {
		return work, err
	}
	for _, workflow := range workflows {
		work.OperationIDs = append(work.OperationIDs, workflow.ID)
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
