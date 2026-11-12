package app

import (
	"context"
	"slices"
	"testing"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// deploymentWorkflows lists fixed succeeded Workflows newest first, filtered as the store filters.
type deploymentWorkflows struct {
	workflowdomain.WorkflowRepository
	workflows []*workflowdomain.Workflow
}

func (r deploymentWorkflows) List(_ context.Context, filter workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	var out []*workflowdomain.Workflow
	for _, workflow := range r.workflows {
		if filter.PlatformID != "" && workflow.PlatformID != filter.PlatformID || filter.Kind != "" && workflow.Kind != filter.Kind {
			continue
		}
		if filter.ServerID != "" && !slices.Contains(workflow.TargetServerIDs, filter.ServerID) {
			continue
		}
		out = append(out, workflow)
		if filter.Limit > 0 && len(out) == filter.Limit {
			break
		}
	}
	return out, len(out), nil
}

// A Platform's deployment is its newest succeeded realization, skipping its runs and uninstalls;
// Servers' deployment changes when any of them has a newer deploy-os Workflow.
func TestValidationDeploymentsFollowTheRealizations(t *testing.T) {
	deployments := validationDeployments{workflows: deploymentWorkflows{workflows: []*workflowdomain.Workflow{
		{ID: "run", Kind: workflowdomain.WorkflowKindRunValidation, PlatformID: "p"},
		{ID: "gpu", Kind: workflowdomain.WorkflowKindConfigureGPUOperator, PlatformID: "p"},
		{ID: "deploy", Kind: workflowdomain.WorkflowKindConfigureSlurm, PlatformID: "p"},
		{ID: "os-2", Kind: workflowdomain.WorkflowKindDeployOS, TargetServerIDs: []string{"s2"}},
		{ID: "os-1", Kind: workflowdomain.WorkflowKindDeployOS, TargetServerIDs: []string{"s1", "s2"}},
	}}}
	ctx := context.Background()
	if key, err := deployments.PlatformDeployment(ctx, "p"); err != nil || key != "gpu" {
		t.Errorf("PlatformDeployment() = %q, %v; want the GPU Operator change", key, err)
	}
	if key, _ := deployments.PlatformDeployment(ctx, "other"); key != "" {
		t.Errorf("an undeployed Platform's key = %q", key)
	}
	if key, _ := deployments.ServersDeployment(ctx, []string{"s1", "s2", "s3"}); key != "s1=os-1,s2=os-2" {
		t.Errorf("ServersDeployment() = %q", key)
	}
}
