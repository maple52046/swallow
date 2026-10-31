package app

import (
	"context"
	"errors"
	"fmt"

	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// activeServerWorkReader lets provisioning reject Lock without depending on operation
// repository types. It reports only identifiers needed for an actionable conflict.
type activeServerWorkReader struct {
	orchestrations workflowdomain.WorkflowRepository
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
	return work, nil
}

// releaseCleanupRetrier retries a failed release network cleanup by retrying the release-os Task
// of the Workflow that owns it, which advances the cleanup again under that Workflow's lease.
type releaseCleanupRetrier struct {
	orchestrations workflowdomain.WorkflowRepository
	workflows      *workflowapp.WorkflowService
}

// RetryReleaseCleanup finds the Workflow's release-os Task for serverID and requests its retry; a
// Task that cannot be retried safely is a provisioning task conflict.
func (r releaseCleanupRetrier) RetryReleaseCleanup(ctx context.Context, workflowID, serverID string) error {
	workflow, err := r.orchestrations.FindByID(ctx, workflowID)
	if errors.Is(err, workflowdomain.ErrWorkflowNotFound) {
		return fmt.Errorf("%w: the release Workflow %s no longer exists", provisioningdomain.ErrProvisioningTaskConflict, workflowID)
	}
	if err != nil {
		return err
	}
	for _, task := range workflow.Steps {
		if task.Kind != "release-os" || !targetsServer(task, serverID) {
			continue
		}
		err := r.workflows.RetryStep(ctx, workflowID, task.ID)
		if errors.Is(err, workflowdomain.ErrTaskRetryUnsafe) {
			return fmt.Errorf("%w: release Task %s of Workflow %s cannot be retried in its current state",
				provisioningdomain.ErrProvisioningTaskConflict, task.ID, workflowID)
		}
		return err
	}
	return fmt.Errorf("%w: Workflow %s has no release Task for the Server", provisioningdomain.ErrProvisioningTaskConflict, workflowID)
}

// targetsServer reports whether task targets the Server serverID.
func targetsServer(task workflowdomain.Task, serverID string) bool {
	for _, target := range task.Targets {
		if target.Kind == "server" && target.ID == serverID {
			return true
		}
	}
	return false
}
