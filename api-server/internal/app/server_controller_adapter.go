package app

import (
	"context"

	workflowstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/workflow"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
)

// serverWorkflowHolds implements provisioningapp.ServerWorkflowHolds over the Workflow resource
// leases: a Server is held while any Workflow holds its `server:<id>` lease, so the Server
// controller never changes a machine that a Workflow is deploying, releasing, or inspecting.
type serverWorkflowHolds struct {
	leases *workflowstore.MongoResourceLeaseRepo
}

var _ provisioningapp.ServerWorkflowHolds = serverWorkflowHolds{}

func (h serverWorkflowHolds) HeldByWorkflow(ctx context.Context, serverID string) (bool, error) {
	return h.leases.IsHeld(ctx, "server:"+serverID)
}
