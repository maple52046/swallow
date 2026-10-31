package app

import (
	"context"

	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// serverWorkflowHolds implements provisioningapp.ServerWorkflowHolds over Workflow resource
// ownership: a Server is held while an unfinished Workflow owns `server:<id>` (decision 072), so
// the Server controller never changes a machine a Workflow is deploying, releasing, or inspecting.
type serverWorkflowHolds struct {
	leases workflowdomain.OwnershipRepository
}

var _ provisioningapp.ServerWorkflowHolds = serverWorkflowHolds{}

func (h serverWorkflowHolds) HeldByWorkflow(ctx context.Context, serverID string) (bool, error) {
	return h.leases.IsHeld(ctx, "server:"+serverID)
}
