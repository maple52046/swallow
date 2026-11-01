package app

import (
	"context"
	"testing"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// listedWorkflows is a Workflow repository whose List returns the Workflows given, newest first.
type listedWorkflows struct {
	workflowdomain.WorkflowRepository
	newestFirst []*workflowdomain.Workflow
}

func (r listedWorkflows) List(context.Context, workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	return r.newestFirst, len(r.newestFirst), nil
}

// A realizing Workflow's outcome is that of its newest rerun, so a repaired change is seen as
// realized; another Workflow of the Platform does not count, and a Workflow that is gone reads "".
func TestWorkflowOutcomeFollowsReruns(t *testing.T) {
	reader := workflowPlatforms{orchestrations: listedWorkflows{newestFirst: []*workflowdomain.Workflow{
		{ID: "other", Status: workflowdomain.WorkflowFailed},
		{ID: "rerun-2", RetryOfOperationID: "rerun-1", Status: workflowdomain.WorkflowSucceeded},
		{ID: "rerun-1", RetryOfOperationID: "original", Status: workflowdomain.WorkflowFailed},
		{ID: "original", Status: workflowdomain.WorkflowFailed},
	}}}
	for workflowID, want := range map[string]string{"original": "succeeded", "rerun-1": "succeeded", "other": "failed", "gone": ""} {
		got, err := reader.WorkflowOutcome(context.Background(), "plat-1", workflowID)
		if err != nil {
			t.Fatalf("WorkflowOutcome(%s) error = %v", workflowID, err)
		}
		if got != want {
			t.Errorf("WorkflowOutcome(%s) = %q, want %q", workflowID, got, want)
		}
	}
}
