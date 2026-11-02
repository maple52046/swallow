package app

import (
	"context"
	"testing"
	"time"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// lifecycleWorkflowRepo returns fixed Workflows and records the filter it was listed with.
type lifecycleWorkflowRepo struct {
	workflowdomain.WorkflowRepository
	filter workflowdomain.WorkflowFilter
	items  []*workflowdomain.Workflow
}

func (r *lifecycleWorkflowRepo) List(_ context.Context, filter workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	r.filter = filter
	return r.items, len(r.items), nil
}

func TestPlatformLifecycleReaderBatchesAndDerivesLatestOperation(t *testing.T) {
	deployedAt := time.Now().UTC().Add(-time.Hour)
	uninstalledAt := deployedAt.Add(time.Minute)
	repo := &lifecycleWorkflowRepo{items: []*workflowdomain.Workflow{
		{
			ID: "uninstall-1", PlatformID: "platform-deployed",
			Kind:            workflowdomain.WorkflowKindUninstallKubernetes,
			TargetServerIDs: []string{"server-1"},
			Status:          workflowdomain.WorkflowFailed,
			RequestedAt:     uninstalledAt,
		},
		{
			ID: "deploy-1", PlatformID: "platform-deployed",
			Kind:            workflowdomain.WorkflowKindDeployKubernetes,
			TargetServerIDs: []string{"server-1", "server-2"},
			Intent: map[string]any{"extraVars": map[string]any{
				"swallow_k0s_roles": map[string]any{
					"server-1": "control-plane", "server-2": "worker",
				},
				"swallow_k0s_workload_controller_ids": []any{"server-1"},
			}},
			Status:      workflowdomain.WorkflowSucceeded,
			RequestedAt: deployedAt,
		},
		{
			ID: "verify-1", PlatformID: "platform-deployed",
			Kind:        workflowdomain.WorkflowKindDeployK8sExporters,
			Status:      workflowdomain.WorkflowRunning,
			RequestedAt: uninstalledAt.Add(time.Minute),
		},
	}}
	reader := platformLifecycleReader{orchestrations: repo}

	result, err := reader.Read(
		context.Background(),
		[]string{"platform-deployed", "platform-registered"},
	)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(repo.filter.PlatformIDs) != 2 {
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
	intent := deploymentIntentKubernetes(&workflowdomain.Workflow{
		TargetServerIDs: []string{"server-1"},
		Intent: map[string]any{"extraVars": map[string]any{
			"swallow_k0s_roles":                   map[string]any{"server-1": "control-plane"},
			"swallow_k0s_workload_controller_ids": []string{"server-1"},
		}},
	})

	if intent == nil || intent.Topology != "standalone" {
		t.Fatalf("standalone deployment intent = %+v", intent)
	}
	if len(intent.RoleAssignments) != 1 || !intent.RoleAssignments[0].RunWorkloads {
		t.Errorf("standalone role assignments = %+v", intent.RoleAssignments)
	}
}

// A Slurm login host runs no cluster daemon, so it is not a role assignment; it must still be
// projected in LoginServerIDs so the read model can show the cluster access point.
func TestDeploymentIntentSlurmProjectsLoginNodes(t *testing.T) {
	intent := deploymentIntentSlurm(&workflowdomain.Workflow{
		TargetServerIDs: []string{"c1", "w1", "login1"},
		Intent: map[string]any{
			"extraVars": map[string]any{
				"swallow_slurm_controller_ids": []string{"c1"},
				"swallow_slurm_compute_ids":    []string{"w1"},
				"swallow_slurm_login_ids":      []string{"login1"},
			},
		},
	})
	if intent == nil {
		t.Fatal("expected a Slurm deployment intent")
	}
	if len(intent.LoginServerIDs) != 1 || intent.LoginServerIDs[0] != "login1" {
		t.Errorf("login server ids = %v, want [login1]", intent.LoginServerIDs)
	}
	for _, assignment := range intent.RoleAssignments {
		if assignment.ServerID == "login1" {
			t.Errorf("a login-only node must not be a role assignment: %+v", assignment)
		}
	}
}
