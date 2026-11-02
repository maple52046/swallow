package app

import (
	"context"
	"testing"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// cancelRecordingWorkflows lists fixed Workflows (newest first) and records cancel commands.
type cancelRecordingWorkflows struct {
	workflowdomain.WorkflowRepository
	workflows []*workflowdomain.Workflow
	canceled  map[string]string
}

func (r *cancelRecordingWorkflows) List(context.Context, workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	return r.workflows, len(r.workflows), nil
}

func (r *cancelRecordingWorkflows) FindByID(_ context.Context, id string) (*workflowdomain.Workflow, error) {
	for _, workflow := range r.workflows {
		if workflow.ID == id {
			return workflow, nil
		}
	}
	return nil, workflowdomain.ErrWorkflowNotFound
}

func (r *cancelRecordingWorkflows) RequestCancel(_ context.Context, id string, command workflowdomain.CancelCommand) error {
	r.canceled[id] = command.Reason
	return nil
}

// The canceler follows a superseded realization to its newest rerun and cancels that one with the
// superseding reason; a finished chain is left alone and reported finished.
func TestPlatformWorkflowCancelerCancelsTheNewestRerun(t *testing.T) {
	repo := &cancelRecordingWorkflows{canceled: map[string]string{}, workflows: []*workflowdomain.Workflow{
		{ID: "rerun", RetryOfOperationID: "deploy", Status: workflowdomain.WorkflowRequiresAttention},
		{ID: "deploy", Status: workflowdomain.WorkflowFailed},
		{ID: "old", Status: workflowdomain.WorkflowSucceeded},
	}}
	canceler := platformWorkflowCanceler{
		orchestrations: repo,
		workflows:      newTestWorkflowService(repo),
	}
	unfinished, err := canceler.CancelUnfinished(context.Background(), "plat-1", "deploy")
	if err != nil || !unfinished {
		t.Fatalf("CancelUnfinished(deploy) = %v, %v; want the waiting rerun unfinished", unfinished, err)
	}
	if reason := repo.canceled["rerun"]; reason != supersededBySpecReason || len(repo.canceled) != 1 {
		t.Errorf("canceled = %v, want only the rerun with the superseding reason", repo.canceled)
	}
	if unfinished, err := canceler.CancelUnfinished(context.Background(), "plat-1", "old"); err != nil || unfinished {
		t.Errorf("CancelUnfinished(old) = %v, %v; want a finished Workflow reported finished", unfinished, err)
	}
}
