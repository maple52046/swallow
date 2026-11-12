package application

import (
	"context"
	"errors"
	"testing"

	"github.com/maple52046/swallow/internal/core/store/storetest"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// A Task may act only on what its Workflow owns: a target Server, or a declared resource. A Task
// aimed anywhere else would bypass the busy check and the resource ownership, so the plan is
// refused before anything is written.
func TestCreateRefusesTaskTargetsOutsideTheWorkflowResources(t *testing.T) {
	server := func(id string) workflowdomain.ResourceReference {
		return workflowdomain.ResourceReference{Kind: "server", ID: id}
	}
	platform := workflowdomain.ResourceReference{Kind: "platform", ID: "p-1"}
	tests := []struct {
		name      string
		servers   []string
		resources []workflowdomain.ResourceReference
		targets   []workflowdomain.ResourceReference
		wantErr   bool
	}{
		{name: "target Server", servers: []string{"s-1"}, targets: []workflowdomain.ResourceReference{server("s-1")}},
		{name: "declared Platform", servers: []string{"s-1"}, resources: []workflowdomain.ResourceReference{platform},
			targets: []workflowdomain.ResourceReference{platform, server("s-1")}},
		{name: "no targets at all", targets: nil},
		{name: "Server the Workflow does not target", servers: []string{"s-1"},
			targets: []workflowdomain.ResourceReference{server("s-2")}, wantErr: true},
		{name: "undeclared Workflow", targets: []workflowdomain.ResourceReference{server("s-1")}, wantErr: true},
		{name: "undeclared Platform", servers: []string{"s-1"}, targets: []workflowdomain.ResourceReference{platform}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supersededWorkflowRepo{}
			service := NewWorkflowService(&storetest.Transactor{}, idleWorkflowRepo{repo}, workflowdomain.PlanCatalog{}, newMemoryOwnership())
			_, err := service.Create(context.Background(), CreateWorkflowInput{
				Kind: workflowdomain.WorkflowKindCustom, Definition: "test", DefinitionVersion: 1, SiteID: "site-1",
				TargetServerIDs: tc.servers, TargetResources: tc.resources,
				Steps: []workflowdomain.Task{{ID: "run", Kind: "ansible-playbook", Name: "Run", Targets: tc.targets}},
			})
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidOperation) {
					t.Fatalf("Create() error = %v, want ErrInvalidOperation", err)
				}
				if repo.created != nil {
					t.Error("Create() stored the refused Workflow")
				}
				return
			}
			if err != nil {
				t.Fatalf("Create() error = %v, want nil", err)
			}
		})
	}
}

// idleWorkflowRepo reports no active work on any target and records the create in the wrapped repo.
type idleWorkflowRepo struct {
	*supersededWorkflowRepo
}

func (idleWorkflowRepo) List(context.Context, workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	return nil, 0, nil
}
