package application

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// fakeRerunRepo is an in-memory WorkflowRepository for Rerun tests. It embeds the interface so
// the methods a rerun never touches stay nil, and it records the replacement Operation and any
// state/step writes so a test can assert recovery behavior without Mongo or Temporal.
type fakeRerunRepo struct {
	operationdomain.WorkflowRepository
	byID        map[string]*operationdomain.Workflow
	created     *operationdomain.Workflow
	stateWrites map[string]operationdomain.WorkflowStatus
	canceledStp []operationdomain.Task
}

func (r *fakeRerunRepo) FindByID(_ context.Context, id string) (*operationdomain.Workflow, error) {
	op, ok := r.byID[id]
	if !ok {
		return nil, operationdomain.ErrWorkflowNotV3
	}
	return op, nil
}

// List reports no active work so Create's acceptance-time target-busy guard always passes; the
// rerun-specific behavior under test is target freeing and cloning, not that guard.
func (r *fakeRerunRepo) List(context.Context, operationdomain.WorkflowFilter) ([]*operationdomain.Workflow, int, error) {
	return nil, 0, nil
}

func (r *fakeRerunRepo) Create(_ context.Context, op *operationdomain.Workflow) error {
	op.SchemaVersion = 4
	r.created = op
	if r.byID == nil {
		r.byID = map[string]*operationdomain.Workflow{}
	}
	r.byID[op.ID] = op
	return nil
}

func (r *fakeRerunRepo) AppendEvent(context.Context, operationdomain.TimelineEvent) error {
	return nil
}

func (r *fakeRerunRepo) UpdateState(_ context.Context, id string, status operationdomain.WorkflowStatus, _ string, _, _ *time.Time) error {
	if r.stateWrites == nil {
		r.stateWrites = map[string]operationdomain.WorkflowStatus{}
	}
	r.stateWrites[id] = status
	if op, ok := r.byID[id]; ok {
		op.Status = status
	}
	return nil
}

func (r *fakeRerunRepo) UpdateStep(_ context.Context, _ string, step operationdomain.Task) error {
	r.canceledStp = append(r.canceledStp, step)
	return nil
}

// fakeRerunController records cancel targets and returns a configurable error so a test can
// simulate a still-live parked execution (nil) or a lost one (control conflict).
type fakeRerunController struct {
	canceled  []string
	cancelErr error
}

func (c *fakeRerunController) Cancel(_ context.Context, op *operationdomain.Workflow) error {
	c.canceled = append(c.canceled, op.ID)
	return c.cancelErr
}

func (c *fakeRerunController) RetryStep(context.Context, *operationdomain.Workflow, string) error {
	return nil
}

// fakeRerunSecrets records the clone request and returns a fixed source->clone reference map so
// a test can assert Step SecretRefs are remapped to the new Operation's sealed copies.
type fakeRerunSecrets struct {
	cloneFrom, cloneTo string
	refMap             map[string]string
	deleted            []string
}

func (s *fakeRerunSecrets) Store(context.Context, string, string, any) (string, error) {
	return "", nil
}

func (s *fakeRerunSecrets) Resolve(context.Context, string) (any, error) { return nil, nil }

func (s *fakeRerunSecrets) CloneForOperation(_ context.Context, source, target string) (map[string]string, error) {
	s.cloneFrom, s.cloneTo = source, target
	out := make(map[string]string, len(s.refMap))
	for k, v := range s.refMap {
		out[k] = v
	}
	return out, nil
}

func (s *fakeRerunSecrets) DeleteForOperation(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}

// newRerunOldOp builds a two-Step deploy Operation whose readiness Step already succeeded and
// whose install Step is failed-retryable with one sealed secret reference, the shape a recovery
// rerun must handle.
func newRerunOldOp(status operationdomain.WorkflowStatus, installStatus operationdomain.TaskStatus) *operationdomain.Workflow {
	return &operationdomain.Workflow{
		ID: "11111111-1111-1111-1111-111111111111", SchemaVersion: 4,
		Kind:       operationdomain.WorkflowKindDeployKubernetes,
		Intent:     map[string]any{"summary": "Deploy k0s Platform demo"},
		Definition: "platform-deployment", DefinitionVersion: 1,
		Status: status, SiteID: "site-a", PlatformID: "platform-a",
		TargetServerIDs: []string{"server-1"},
		Steps: []operationdomain.Task{
			{
				ID: "wait-for-ssh", Kind: "wait-for-ssh", Name: "Verify SSH",
				Executor: operationdomain.RunnerKindInternal,
				Status:   operationdomain.TaskSucceeded, Attempt: 1,
			},
			{
				ID: "install-platform", Kind: "ansible-playbook", Name: "Install k0s",
				Executor: operationdomain.RunnerKindAnsible, DependsOn: []string{"wait-for-ssh"},
				Status: installStatus, Attempt: 2,
				SecretRefs: map[string]string{"k0sPass": "old-ref"},
				Error:      &operationdomain.NormalizedError{Code: "transient", Message: "boom", Retryable: true},
			},
		},
	}
}

func TestRerunClonesFailedOperationPreservingSucceededSteps(t *testing.T) {
	old := newRerunOldOp(operationdomain.WorkflowFailed, operationdomain.TaskFailed)
	repo := &fakeRerunRepo{byID: map[string]*operationdomain.Workflow{old.ID: old}}
	controller := &fakeRerunController{}
	secrets := &fakeRerunSecrets{refMap: map[string]string{"old-ref": "new-ref"}}
	service := NewWorkflowService(repo, controller, secrets)

	item, err := service.Rerun(context.Background(), old.ID, "operator")
	if err != nil {
		t.Fatalf("Rerun() error = %v, want nil", err)
	}
	if repo.created == nil {
		t.Fatal("Rerun() created no replacement Operation")
	}
	if repo.created.ID == old.ID {
		t.Fatal("Rerun() reused the original id; a fresh Workflow id is required to restart")
	}
	if item.ID != repo.created.ID {
		t.Fatalf("Rerun() returned id %q, want created id %q", item.ID, repo.created.ID)
	}
	if repo.created.RetryOfOperationID != old.ID {
		t.Fatalf("retryOfOperationId = %q, want %q", repo.created.RetryOfOperationID, old.ID)
	}
	if want := "swallow-operation/" + repo.created.ID; repo.created.Temporal.WorkflowID != want {
		t.Fatalf("temporal workflowId = %q, want %q", repo.created.Temporal.WorkflowID, want)
	}
	if len(controller.canceled) != 0 {
		t.Fatalf("a terminal Operation must not be canceled, got %v", controller.canceled)
	}

	steps := repo.created.Steps
	if steps[0].Status != operationdomain.TaskSucceeded {
		t.Errorf("preserved Step status = %v, want succeeded (its side effects must not repeat)", steps[0].Status)
	}
	if steps[1].Status != operationdomain.TaskPending {
		t.Errorf("incomplete Step status = %v, want pending", steps[1].Status)
	}
	if steps[1].Error != nil {
		t.Errorf("incomplete Step error = %v, want cleared", steps[1].Error)
	}
	if got := steps[1].SecretRefs["k0sPass"]; got != "new-ref" {
		t.Errorf("cloned Step secret ref = %q, want remapped new-ref", got)
	}
	if secrets.cloneFrom != old.ID || secrets.cloneTo != repo.created.ID {
		t.Errorf("CloneForOperation(%q,%q), want (%q,%q)", secrets.cloneFrom, secrets.cloneTo, old.ID, repo.created.ID)
	}
}

func TestRerunFinalizesNonTerminalOperationBeforeReplacing(t *testing.T) {
	// A requires_attention Operation whose execution is already gone: cancel reports a control
	// conflict, and Rerun must still force it terminal so the replacement can claim the targets.
	old := newRerunOldOp(operationdomain.WorkflowRequiresAttention, operationdomain.TaskRequiresAttention)
	repo := &fakeRerunRepo{byID: map[string]*operationdomain.Workflow{old.ID: old}}
	controller := &fakeRerunController{cancelErr: fmt.Errorf("%w: gone", operationdomain.ErrWorkflowControlConflict)}
	secrets := &fakeRerunSecrets{refMap: map[string]string{"old-ref": "new-ref"}}
	service := NewWorkflowService(repo, controller, secrets)

	if _, err := service.Rerun(context.Background(), old.ID, "operator"); err != nil {
		t.Fatalf("Rerun() error = %v, want nil", err)
	}
	if len(controller.canceled) != 1 || controller.canceled[0] != old.ID {
		t.Fatalf("cancel calls = %v, want [%q]", controller.canceled, old.ID)
	}
	if repo.stateWrites[old.ID] != operationdomain.WorkflowCanceled {
		t.Fatalf("superseded Operation state = %v, want canceled", repo.stateWrites[old.ID])
	}
	if repo.created == nil || repo.created.ID == old.ID {
		t.Fatal("Rerun() did not create a replacement Operation after freeing the targets")
	}
}

func TestRerunRejectsSucceededOperation(t *testing.T) {
	old := newRerunOldOp(operationdomain.WorkflowSucceeded, operationdomain.TaskSucceeded)
	repo := &fakeRerunRepo{byID: map[string]*operationdomain.Workflow{old.ID: old}}
	service := NewWorkflowService(repo, &fakeRerunController{}, &fakeRerunSecrets{})

	_, err := service.Rerun(context.Background(), old.ID, "operator")
	if !errors.Is(err, operationdomain.ErrWorkflowControlConflict) {
		t.Fatalf("Rerun() error = %v, want ErrWorkflowControlConflict", err)
	}
	if repo.created != nil {
		t.Fatal("a succeeded Operation has nothing to rerun and must not create a replacement")
	}
}

func TestRerunRejectsStillRunningOperation(t *testing.T) {
	old := newRerunOldOp(operationdomain.WorkflowRunning, operationdomain.TaskRunning)
	repo := &fakeRerunRepo{byID: map[string]*operationdomain.Workflow{old.ID: old}}
	service := NewWorkflowService(repo, &fakeRerunController{}, &fakeRerunSecrets{})

	_, err := service.Rerun(context.Background(), old.ID, "operator")
	if !errors.Is(err, operationdomain.ErrWorkflowControlConflict) {
		t.Fatalf("Rerun() error = %v, want ErrWorkflowControlConflict", err)
	}
	if repo.created != nil {
		t.Fatal("a still-running Operation must be retried or canceled, not rerun")
	}
}
