package temporalworkflow

import (
	"context"
	"errors"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// reconcilerRepo is an in-memory WorkflowRepository for the lost-execution sweep. It embeds the
// interface so unused methods stay nil, returns fixed candidates, and records which Operations
// were marked and with what status.
type reconcilerRepo struct {
	operationdomain.WorkflowRepository
	candidates []*operationdomain.Workflow
	marked     map[string]operationdomain.WorkflowStatus
}

func (r *reconcilerRepo) ListNonTerminalStarted(context.Context, int) ([]*operationdomain.Workflow, error) {
	return r.candidates, nil
}

func (r *reconcilerRepo) UpdateState(_ context.Context, id string, status operationdomain.WorkflowStatus, _ string, _, _ *time.Time) error {
	if r.marked == nil {
		r.marked = map[string]operationdomain.WorkflowStatus{}
	}
	r.marked[id] = status
	return nil
}

// describeClient fakes only the DescribeWorkflowExecution call the sweep uses, keyed by workflow
// id. It embeds client.Client so the many other client methods stay nil and unused by the test.
type describeClient struct {
	client.Client
	byWorkflowID map[string]describeResult
}

type describeResult struct {
	status enumspb.WorkflowExecutionStatus
	err    error
}

func (c describeClient) DescribeWorkflowExecution(_ context.Context, workflowID, _ string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	result := c.byWorkflowID[workflowID]
	if result.err != nil {
		return nil, result.err
	}
	return &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: result.status},
	}, nil
}

func candidate(id string, status operationdomain.WorkflowStatus) *operationdomain.Workflow {
	return &operationdomain.Workflow{
		ID: id, Status: status,
		Temporal: operationdomain.TemporalReference{WorkflowID: "swallow-operation/" + id, RunID: "run-" + id},
	}
}

// TestSweepLostExecutionsMarksOnlyLostOnes verifies the sweep marks an Operation as repairable
// only when its execution is closed or absent, and leaves a running one and a record whose
// describe merely errored transiently untouched.
func TestSweepLostExecutionsMarksOnlyLostOnes(t *testing.T) {
	running := candidate("op-running", operationdomain.WorkflowRunning)
	closed := candidate("op-closed", operationdomain.WorkflowRunning)
	gone := candidate("op-gone", operationdomain.WorkflowWaitingExternal)
	transient := candidate("op-transient", operationdomain.WorkflowRunning)

	repo := &reconcilerRepo{candidates: []*operationdomain.Workflow{running, closed, gone, transient}}
	fakeClient := describeClient{byWorkflowID: map[string]describeResult{
		running.Temporal.WorkflowID:   {status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING},
		closed.Temporal.WorkflowID:    {status: enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED},
		gone.Temporal.WorkflowID:      {err: serviceerror.NewNotFound("execution not found")},
		transient.Temporal.WorkflowID: {err: errors.New("temporal unavailable")},
	}}
	reconciler := NewReconciler(fakeClient, repo, time.Minute)

	reconciler.SweepLostExecutions(context.Background())

	if status, ok := repo.marked[running.ID]; ok {
		t.Errorf("a running execution was marked %v; it must be left untouched", status)
	}
	if status, ok := repo.marked[transient.ID]; ok {
		t.Errorf("a transient describe error marked %v; it must be retried next sweep, not marked", status)
	}
	if repo.marked[closed.ID] != operationdomain.WorkflowRequiresAttention {
		t.Errorf("closed execution mark = %v, want requires_attention", repo.marked[closed.ID])
	}
	if repo.marked[gone.ID] != operationdomain.WorkflowRequiresAttention {
		t.Errorf("absent execution mark = %v, want requires_attention", repo.marked[gone.ID])
	}
}
