package app

import (
	"context"
	"strings"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// validationDeploymentHistory bounds how many of a Platform's newest succeeded Workflows are read to
// find its latest deployment; runs and uninstalls are skipped, and a Platform's latest realization
// is among its newest.
const validationDeploymentHistory = 20

// validationDeployments implements validationapp.Deployments over the Workflow store: what an
// after-deployment Validation Schedule follows (validation.md §6).
type validationDeployments struct {
	workflows workflowdomain.WorkflowRepository
}

// notDeployments are the Workflow kinds of a Platform that do not realize its spec.
var notDeployments = map[workflowdomain.WorkflowKind]bool{
	workflowdomain.WorkflowKindRunValidation:       true,
	workflowdomain.WorkflowKindUninstallKubernetes: true,
	workflowdomain.WorkflowKindUninstallSlurm:      true,
}

// PlatformDeployment returns the id of the Platform's newest succeeded Workflow that realized its
// spec (a deployment, a rerun of one, or a change such as the GPU Operator's).
func (d validationDeployments) PlatformDeployment(ctx context.Context, platformID string) (string, error) {
	workflows, _, err := d.workflows.List(ctx, workflowdomain.WorkflowFilter{
		PlatformID: platformID, Status: workflowdomain.WorkflowSucceeded, Limit: validationDeploymentHistory,
	})
	if err != nil {
		return "", err
	}
	for _, workflow := range workflows {
		if !notDeployments[workflow.Kind] {
			return workflow.ID, nil
		}
	}
	return "", nil
}

// ServersDeployment returns each Server's newest succeeded deploy-os Workflow, as
// "<serverId>=<workflowId>" joined in the Servers' order, so a new deployment of any of them changes
// it; "" when none was deployed.
func (d validationDeployments) ServersDeployment(ctx context.Context, serverIDs []string) (string, error) {
	parts := make([]string, 0, len(serverIDs))
	for _, id := range serverIDs {
		workflows, _, err := d.workflows.List(ctx, workflowdomain.WorkflowFilter{
			ServerID: id, Kind: workflowdomain.WorkflowKindDeployOS, Status: workflowdomain.WorkflowSucceeded, Limit: 1,
		})
		if err != nil {
			return "", err
		}
		if len(workflows) > 0 {
			parts = append(parts, id+"="+workflows[0].ID)
		}
	}
	return strings.Join(parts, ","), nil
}
