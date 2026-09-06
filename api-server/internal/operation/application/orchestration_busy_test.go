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
