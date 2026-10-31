package application

import (
	"context"
	"errors"
	"testing"

	"github.com/maple52046/swallow/internal/core/store/storetest"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// busyWorkflowRepo reports every target as already inside an unfinished Operation.
// Only List is exercised: Create returns at the active-work guard before any other
// repository method is reached, so the embedded nil interface is never dereferenced.
type busyWorkflowRepo struct {
	workflowdomain.WorkflowRepository
}

func (busyWorkflowRepo) List(
	context.Context, workflowdomain.WorkflowFilter,
) ([]*workflowdomain.Workflow, int, error) {
	return nil, 1, nil
}

// TestCreateClassifiesActiveDurableWorkAsBusyConflict guards the acceptance-time reason
// operators actually read. A target already inside an unfinished Operation must surface
// as ErrTargetsBusy (a 409 conflict everywhere) and must not be classified as a generic
// ErrInvalidOperation, which the provisioning delivery mapper would otherwise leak as an
// opaque Internal error.
func TestCreateClassifiesActiveDurableWorkAsBusyConflict(t *testing.T) {
	service := NewWorkflowService(&storetest.Transactor{}, busyWorkflowRepo{}, workflowdomain.PlanCatalog{})

	_, err := service.Create(context.Background(), CreateWorkflowInput{
		Kind:              workflowdomain.WorkflowKindReleaseOS,
		Definition:        "os-release",
		DefinitionVersion: 1,
		SiteID:            "site-1",
		TargetServerIDs:   []string{"server-1"},
		Steps: []workflowdomain.Task{{
			ID: "release-server-1", Kind: "release-os", Name: "Release server-1",
		}},
	})

	if err == nil {
		t.Fatal("expected a target with active durable work to be rejected")
	}
	if !errors.Is(err, workflowdomain.ErrTargetsBusy) {
		t.Fatalf("want ErrTargetsBusy, got %v", err)
	}
	if errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("active durable work must not be classified as ErrInvalidOperation: %v", err)
	}
}

// supersededWorkflowRepo reports two unfinished Operations on the target and records the create.
type supersededWorkflowRepo struct {
	workflowdomain.WorkflowRepository
	created *workflowdomain.Workflow
}

func (r *supersededWorkflowRepo) List(context.Context, workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	return []*workflowdomain.Workflow{{ID: "op-canceling"}, {ID: "op-other"}}, 2, nil
}

func (r *supersededWorkflowRepo) Create(_ context.Context, operation *workflowdomain.Workflow) error {
	r.created = operation
	return nil
}

func (r *supersededWorkflowRepo) AppendEvent(context.Context, workflowdomain.TimelineEvent) error {
	return nil
}

// TestCreateIgnoresOnlySupersededOperations covers decision 061: Operations whose cancellation the
// caller just requested no longer make the target busy, while any other unfinished Operation still
// does, so a Release or Recover can be accepted behind a cancel without racing unrelated work.
func TestCreateIgnoresOnlySupersededOperations(t *testing.T) {
	input := func(superseded ...string) CreateWorkflowInput {
		return CreateWorkflowInput{
			Kind: workflowdomain.WorkflowKindReleaseOS, Definition: "os-release", DefinitionVersion: 1,
			SiteID: "site-1", TargetServerIDs: []string{"server-1"}, SupersededOperationIDs: superseded,
			Steps: []workflowdomain.Task{{ID: "release-server-1", Kind: "release-os", Name: "Release server-1"}},
		}
	}

	repo := &supersededWorkflowRepo{}
	if _, err := NewWorkflowService(&storetest.Transactor{}, repo, workflowdomain.PlanCatalog{}).Create(context.Background(), input("op-canceling")); !errors.Is(err, workflowdomain.ErrTargetsBusy) {
		t.Fatalf("Create with one of two holders superseded err = %v, want ErrTargetsBusy", err)
	}
	if _, err := NewWorkflowService(&storetest.Transactor{}, repo, workflowdomain.PlanCatalog{}).Create(context.Background(), input("op-canceling", "op-other")); err != nil {
		t.Fatalf("Create with every holder superseded err = %v, want nil", err)
	}
	if repo.created == nil {
		t.Fatal("no Operation was created")
	}
}
