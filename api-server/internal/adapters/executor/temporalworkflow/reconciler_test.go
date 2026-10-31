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

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// reconcilerRepo is an in-memory WorkflowRepository for the lost-execution sweep. It embeds the
// interface so unused methods stay nil, returns fixed forward-progress and parked candidates, and
// records which Operations were marked, with what status and reason, and which Tasks were written.
type reconcilerRepo struct {
	workflowdomain.WorkflowRepository
	candidates []*workflowdomain.Workflow
	parked     []*workflowdomain.Workflow
	marked     map[string]workflowdomain.WorkflowStatus
	reasons    map[string]string
	steps      map[string]workflowdomain.TaskStatus
}

func (r *reconcilerRepo) ListNonTerminalStarted(context.Context, int) ([]*workflowdomain.Workflow, error) {
	return r.candidates, nil
}

func (r *reconcilerRepo) ListParkedStarted(context.Context, int) ([]*workflowdomain.Workflow, error) {
	return r.parked, nil
}

func (r *reconcilerRepo) UpdateState(_ context.Context, id string, status workflowdomain.WorkflowStatus, reason string, _, _ *time.Time) error {
	if r.marked == nil {
		r.marked = map[string]workflowdomain.WorkflowStatus{}
		r.reasons = map[string]string{}
	}
	r.marked[id] = status
	r.reasons[id] = reason
	return nil
}

func (r *reconcilerRepo) UpdateStep(_ context.Context, _ string, step workflowdomain.Task) error {
	if r.steps == nil {
		r.steps = map[string]workflowdomain.TaskStatus{}
	}
	r.steps[step.ID] = step.Status
	return nil
}

func (r *reconcilerRepo) AppendEvent(context.Context, workflowdomain.TimelineEvent) error {
	return nil
}

// reconcilerLeases serves the leases each owner still holds and records which owners' leases the
// sweep released.
type reconcilerLeases struct {
	held     map[string][]workflowdomain.ResourceLease
	released []string
}

func (l *reconcilerLeases) FindByOwner(_ context.Context, owner string) ([]workflowdomain.ResourceLease, error) {
	return l.held[owner], nil
}

func (l *reconcilerLeases) Release(_ context.Context, leases []workflowdomain.ResourceLease) error {
	for _, lease := range leases {
		l.released = append(l.released, lease.Owner)
	}
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

func candidate(id string, status workflowdomain.WorkflowStatus) *workflowdomain.Workflow {
	return &workflowdomain.Workflow{
		ID: id, Status: status,
		Temporal: workflowdomain.TemporalReference{WorkflowID: "swallow-operation/" + id, RunID: "run-" + id},
	}
}

// TestSweepLostExecutionsMarksOnlyLostOnes verifies the sweep marks an Operation as repairable
// only when its execution is closed or absent, and leaves a running one and a record whose
// describe merely errored transiently untouched.
func TestSweepLostExecutionsMarksOnlyLostOnes(t *testing.T) {
	running := candidate("op-running", workflowdomain.WorkflowRunning)
	closed := candidate("op-closed", workflowdomain.WorkflowRunning)
	gone := candidate("op-gone", workflowdomain.WorkflowWaitingExternal)
	transient := candidate("op-transient", workflowdomain.WorkflowRunning)

	repo := &reconcilerRepo{candidates: []*workflowdomain.Workflow{running, closed, gone, transient}}
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
	if repo.marked[closed.ID] != workflowdomain.WorkflowRequiresAttention {
		t.Errorf("closed execution mark = %v, want requires_attention", repo.marked[closed.ID])
	}
	if repo.marked[gone.ID] != workflowdomain.WorkflowRequiresAttention {
		t.Errorf("absent execution mark = %v, want requires_attention", repo.marked[gone.ID])
	}
}

// TestSweepLostExecutionsSettlesParkedOperations covers lab-control-1's failure: a release parked
// in requires_attention whose execution Temporal terminated for outgrowing its history limit was
// never checked, so nothing told the operator it could no longer be retried. A parked Operation
// whose execution is gone now carries the lost-execution reason (written once), a canceling one
// is completed and its leases freed, and live or transiently unreadable ones are left alone.
func TestSweepLostExecutionsSettlesParkedOperations(t *testing.T) {
	parkedLive := candidate("op-parked-live", workflowdomain.WorkflowRequiresAttention)
	parkedLost := candidate("op-parked-lost", workflowdomain.WorkflowRequiresAttention)
	alreadyMarked := candidate("op-already-marked", workflowdomain.WorkflowRequiresAttention)
	alreadyMarked.StatusReason = lostExecutionReason
	cancelingLive := candidate("op-canceling-live", workflowdomain.WorkflowCanceling)
	cancelingLost := candidate("op-canceling-lost", workflowdomain.WorkflowCanceling)
	cancelingLost.Steps = []workflowdomain.Task{{ID: "release-a", Kind: "release-os", Status: workflowdomain.TaskWaitingExternal}}
	transient := candidate("op-parked-transient", workflowdomain.WorkflowRequiresAttention)

	notFound := describeResult{err: serviceerror.NewNotFound("workflow not found")}
	running := describeResult{status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING}
	repo := &reconcilerRepo{parked: []*workflowdomain.Workflow{parkedLive, parkedLost, alreadyMarked, cancelingLive, cancelingLost, transient}}
	fakeClient := describeClient{byWorkflowID: map[string]describeResult{
		parkedLive.Temporal.WorkflowID:    running,
		parkedLost.Temporal.WorkflowID:    notFound,
		alreadyMarked.Temporal.WorkflowID: notFound,
		cancelingLive.Temporal.WorkflowID: running,
		cancelingLost.Temporal.WorkflowID: notFound,
		transient.Temporal.WorkflowID:     {err: errors.New("temporal unavailable")},
	}}
	leases := &reconcilerLeases{held: map[string][]workflowdomain.ResourceLease{
		cancelingLost.Temporal.WorkflowID: {{ResourceKey: "server:a", Owner: cancelingLost.Temporal.WorkflowID, FencingToken: 2}},
	}}
	reconciler := NewReconciler(fakeClient, repo, time.Minute)
	reconciler.AttachLeaseReleaser(leases)

	reconciler.SweepLostExecutions(context.Background())

	for _, untouched := range []*workflowdomain.Workflow{parkedLive, alreadyMarked, cancelingLive, transient} {
		if status, ok := repo.marked[untouched.ID]; ok {
			t.Errorf("%s was rewritten to %v; it must be left untouched", untouched.ID, status)
		}
	}
	if repo.marked[parkedLost.ID] != workflowdomain.WorkflowRequiresAttention || repo.reasons[parkedLost.ID] != lostExecutionReason {
		t.Errorf("lost parked Operation = (%v, %q), want requires_attention with the lost-execution reason",
			repo.marked[parkedLost.ID], repo.reasons[parkedLost.ID])
	}
	if repo.marked[cancelingLost.ID] != workflowdomain.WorkflowCanceled {
		t.Errorf("lost canceling Operation status = %v, want canceled", repo.marked[cancelingLost.ID])
	}
	if repo.steps["release-a"] != workflowdomain.TaskCanceled {
		t.Errorf("lost canceling Operation Task = %v, want canceled", repo.steps["release-a"])
	}
	if len(leases.released) != 1 || leases.released[0] != cancelingLost.Temporal.WorkflowID {
		t.Errorf("released lease owners = %v, want only %s", leases.released, cancelingLost.Temporal.WorkflowID)
	}
}
