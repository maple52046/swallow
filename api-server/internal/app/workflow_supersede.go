package app

import (
	"context"
	"fmt"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/shared/pagination"
)

// supersedeBlockingWorkflows is the backend half of cancelActiveWorkflows (decision 061), shared by
// Release, Recover, and Platform uninstall: it asks each unfinished Workflow holding a target to
// cancel and returns their IDs so Create does not refuse the new Operation as busy. The new
// Operation waits for those Workflows' resource leases, so it acts only after they end, including
// after a canceled deployment returned its Server to Ready (decision 060). A refused cancel is
// accepted only when the Workflow finished meanwhile; one still running fails the request as a
// conflict naming it, because the new Operation would otherwise wait behind it with no end. Call it
// once every other validation passed, right before Create, because a cancel cannot be undone.
func supersedeBlockingWorkflows(ctx context.Context, workflows *operationapp.WorkflowService, targetIDs []string) ([]string, error) {
	seen := map[string]bool{}
	var superseded []string
	for _, serverID := range targetIDs {
		active, _, err := workflows.List(ctx, operationapp.ListOperationsInput{
			ServerID: serverID, Active: true, Page: pagination.Page{Page: 1, PageSize: 100},
		})
		if err != nil {
			return nil, err
		}
		for _, operation := range active {
			if seen[operation.ID] {
				continue
			}
			seen[operation.ID] = true
			if err := workflows.Cancel(ctx, operation.ID); err != nil {
				current, findErr := workflows.Get(ctx, operation.ID)
				if findErr != nil || !operationdomain.WorkflowStatus(current.Status).Terminal() {
					return nil, fmt.Errorf("%w: %s could not be canceled: %v", operationdomain.ErrTargetsBusy, operation.Intent, err)
				}
				continue
			}
			superseded = append(superseded, operation.ID)
		}
	}
	return superseded, nil
}
