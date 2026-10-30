package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/maple52046/swallow/internal/core/store/storetest"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// fakeLeaseReleaser serves the leases an owner still holds and records which ones were released,
// so a test can assert that only a lost execution's leases are freed.
type fakeLeaseReleaser struct {
	held       map[string][]operationdomain.ResourceLease
	released   []operationdomain.ResourceLease
	releaseErr error
}

func (f *fakeLeaseReleaser) FindByOwner(_ context.Context, owner string) ([]operationdomain.ResourceLease, error) {
	return f.held[owner], nil
}

func (f *fakeLeaseReleaser) Release(_ context.Context, leases []operationdomain.ResourceLease) error {
	if f.releaseErr != nil {
		return f.releaseErr
	}
	f.released = append(f.released, leases...)
	return nil
}

// executionGoneErr is what the Temporal controller returns when the execution no longer exists.
func executionGoneErr() error {
	return fmt.Errorf("%w: %w", operationdomain.ErrWorkflowControlConflict, operationdomain.ErrWorkflowExecutionGone)
}

// newLostReleaseOperation models lab-control-1's stuck release: a requires_attention release-os
// whose Task failed retryably, plus a Task that already succeeded and must keep its outcome.
func newLostReleaseOperation() *operationdomain.Workflow {
	return &operationdomain.Workflow{
		ID: "062067e2-6609-4d75-9eee-3a6c683f5537", SchemaVersion: 4,
		Kind: operationdomain.WorkflowKindReleaseOS, Status: operationdomain.WorkflowRequiresAttention,
		Temporal:        operationdomain.TemporalReference{WorkflowID: "swallow-operation/062067e2-6609-4d75-9eee-3a6c683f5537"},
		TargetServerIDs: []string{"server-1"},
		Steps: []operationdomain.Task{
			{ID: "release-server-0", Kind: "release-os", Status: operationdomain.TaskSucceeded, Attempt: 1},
			{ID: "release-server-1", Kind: "release-os", Status: operationdomain.TaskRequiresAttention, Attempt: 1,
				Error: &operationdomain.NormalizedError{Code: "release_failed", Message: "The provisioner reported that release failed.", Retryable: true}},
		},
	}
}

func TestCancelClosesWorkflowWhoseExecutionIsGone(t *testing.T) {
	operation := newLostReleaseOperation()
	repo := &fakeRerunRepo{byID: map[string]*operationdomain.Workflow{operation.ID: operation}}
	leases := &fakeLeaseReleaser{held: map[string][]operationdomain.ResourceLease{
		operation.Temporal.WorkflowID: {{ResourceKey: "server:server-1", Owner: operation.Temporal.WorkflowID, FencingToken: 7}},
	}}
	service := NewWorkflowService(&storetest.Transactor{}, repo, &fakeRerunController{cancelErr: executionGoneErr()})
	service.AttachLeaseReleaser(leases)

	if err := service.Cancel(context.Background(), operation.ID); err != nil {
		t.Fatalf("Cancel() error = %v, want nil once the projection is closed", err)
	}
	if got := repo.stateWrites[operation.ID]; got != operationdomain.WorkflowCanceled {
		t.Errorf("Workflow status = %q, want %q", got, operationdomain.WorkflowCanceled)
	}
	if got := repo.reasons[operation.ID]; got != CanceledWithoutExecutionReason {
		t.Errorf("Workflow reason = %q, want %q", got, CanceledWithoutExecutionReason)
	}
	if len(repo.canceledStp) != 1 || repo.canceledStp[0].ID != "release-server-1" || repo.canceledStp[0].Status != operationdomain.TaskCanceled {
		t.Errorf("Task writes = %+v, want only release-server-1 canceled (the succeeded Task keeps its outcome)", repo.canceledStp)
	}
	if len(leases.released) != 1 || leases.released[0].ResourceKey != "server:server-1" {
		t.Errorf("released leases = %+v, want the dead execution's server:server-1 lease", leases.released)
	}
}

func TestCancelLeavesWorkflowCancelableWhenLeaseReleaseFails(t *testing.T) {
	operation := newLostReleaseOperation()
	repo := &fakeRerunRepo{byID: map[string]*operationdomain.Workflow{operation.ID: operation}}
	releaseErr := errors.New("lease store unavailable")
	service := NewWorkflowService(&storetest.Transactor{}, repo, &fakeRerunController{cancelErr: executionGoneErr()})
	service.AttachLeaseReleaser(&fakeLeaseReleaser{releaseErr: releaseErr})

	if err := service.Cancel(context.Background(), operation.ID); !errors.Is(err, releaseErr) {
		t.Fatalf("Cancel() error = %v, want the lease release error", err)
	}
	if len(repo.stateWrites) != 0 {
		t.Errorf("state writes = %v, want none so the operator can cancel again", repo.stateWrites)
	}
}

func TestCancelOnlyClosesProjectionForGoneExecution(t *testing.T) {
	unavailable := errors.New("temporal unavailable")
	tests := []struct {
		name      string
		cancelErr error
		wantErr   error
	}{
		{name: "live execution records its own cancellation"},
		{name: "temporal outage is returned unchanged", cancelErr: unavailable, wantErr: unavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			operation := newLostReleaseOperation()
			repo := &fakeRerunRepo{byID: map[string]*operationdomain.Workflow{operation.ID: operation}}
			leases := &fakeLeaseReleaser{held: map[string][]operationdomain.ResourceLease{
				operation.Temporal.WorkflowID: {{ResourceKey: "server:server-1", Owner: operation.Temporal.WorkflowID, FencingToken: 7}},
			}}
			service := NewWorkflowService(&storetest.Transactor{}, repo, &fakeRerunController{cancelErr: tc.cancelErr})
			service.AttachLeaseReleaser(leases)

			err := service.Cancel(context.Background(), operation.ID)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("Cancel() error = %v, want %v", err, tc.wantErr)
			}
			if len(repo.stateWrites) != 0 || len(repo.canceledStp) != 0 {
				t.Errorf("projection writes = %v / %v, want none", repo.stateWrites, repo.canceledStp)
			}
			if len(leases.released) != 0 {
				t.Errorf("released leases = %v, want none", leases.released)
			}
		})
	}
}
