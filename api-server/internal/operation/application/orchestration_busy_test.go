package application

import (
	"context"
	"errors"
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// busyWorkflowRepo reports every target as already inside an unfinished Operation.
// Only List is exercised: Create returns at the active-work guard before any other
// repository method is reached, so the embedded nil interface is never dereferenced.
type busyWorkflowRepo struct {
	operationdomain.WorkflowRepository
}

func (busyWorkflowRepo) List(
	context.Context, operationdomain.WorkflowFilter,
) ([]*operationdomain.Workflow, int, error) {
	return nil, 1, nil
}

// TestCreateClassifiesActiveDurableWorkAsBusyConflict guards the acceptance-time reason
// operators actually read. A target already inside an unfinished Operation must surface
// as ErrTargetsBusy (a 409 conflict everywhere) and must not be classified as a generic
// ErrInvalidOperation, which the provisioning delivery mapper would otherwise leak as an
// opaque Internal error.
func TestCreateClassifiesActiveDurableWorkAsBusyConflict(t *testing.T) {
	service := NewWorkflowService(busyWorkflowRepo{}, nil)

	_, err := service.Create(context.Background(), CreateWorkflowInput{
		Kind:              operationdomain.WorkflowKindReleaseOS,
		Definition:        "os-release",
		DefinitionVersion: 1,
		SiteID:            "site-1",
		TargetServerIDs:   []string{"server-1"},
		Steps: []operationdomain.Task{{
			ID: "release-server-1", Kind: "release-os", Name: "Release server-1",
		}},
	})

	if err == nil {
		t.Fatal("expected a target with active durable work to be rejected")
	}
	if !errors.Is(err, operationdomain.ErrTargetsBusy) {
		t.Fatalf("want ErrTargetsBusy, got %v", err)
	}
	if errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("active durable work must not be classified as ErrInvalidOperation: %v", err)
	}
}

// supersededWorkflowRepo reports two unfinished Operations on the target and records the create.
type supersededWorkflowRepo struct {
	operationdomain.WorkflowRepository
	created *operationdomain.Workflow
}

func (r *supersededWorkflowRepo) List(context.Context, operationdomain.WorkflowFilter) ([]*operationdomain.Workflow, int, error) {
	return []*operationdomain.Workflow{{ID: "op-canceling"}, {ID: "op-other"}}, 2, nil
}

func (r *supersededWorkflowRepo) Create(_ context.Context, operation *operationdomain.Workflow) error {
	r.created = operation
	return nil
}

func (r *supersededWorkflowRepo) AppendEvent(context.Context, operationdomain.TimelineEvent) error {
	return nil
}

// TestCreateIgnoresOnlySupersededOperations covers decision 061: Operations whose cancellation the
// caller just requested no longer make the target busy, while any other unfinished Operation still
// does, so a Release or Recover can be accepted behind a cancel without racing unrelated work.
func TestCreateIgnoresOnlySupersededOperations(t *testing.T) {
	input := func(superseded ...string) CreateWorkflowInput {
		return CreateWorkflowInput{
			Kind: operationdomain.WorkflowKindReleaseOS, Definition: "os-release", DefinitionVersion: 1,
			SiteID: "site-1", TargetServerIDs: []string{"server-1"}, SupersededOperationIDs: superseded,
			Steps: []operationdomain.Task{{ID: "release-server-1", Kind: "release-os", Name: "Release server-1"}},
		}
	}

	repo := &supersededWorkflowRepo{}
	if _, err := NewWorkflowService(repo, nil).Create(context.Background(), input("op-canceling")); !errors.Is(err, operationdomain.ErrTargetsBusy) {
		t.Fatalf("Create with one of two holders superseded err = %v, want ErrTargetsBusy", err)
	}
	if _, err := NewWorkflowService(repo, nil).Create(context.Background(), input("op-canceling", "op-other")); err != nil {
		t.Fatalf("Create with every holder superseded err = %v, want nil", err)
	}
	if repo.created == nil {
		t.Fatal("no Operation was created")
	}
}
