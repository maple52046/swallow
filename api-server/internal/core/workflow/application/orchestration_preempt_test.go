package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maple52046/swallow/internal/core/store/storetest"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// preemptRepo reports one unfinished Workflow on every target and records the cancels and the
// create.
type preemptRepo struct {
	workflowdomain.WorkflowRepository
	holder   *workflowdomain.Workflow
	canceled map[string]string
	created  *workflowdomain.Workflow
}

func (r *preemptRepo) List(context.Context, workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	return []*workflowdomain.Workflow{r.holder}, 1, nil
}

func (r *preemptRepo) RequestCancel(_ context.Context, id string, command workflowdomain.CancelCommand) error {
	r.canceled[id] = command.Reason
	return nil
}

func (r *preemptRepo) Create(_ context.Context, operation *workflowdomain.Workflow) error {
	r.created = operation
	return nil
}

func (r *preemptRepo) AppendEvent(context.Context, workflowdomain.TimelineEvent) error {
	return nil
}

// A preemptible holder gives way to a Workflow that is not preemptible: the holder's cancel is
// written with the acceptance and the new Workflow is accepted. A preemptible Workflow never
// preempts another one, and a holder that is not preemptible still makes the target busy.
func TestCreatePreemptsPreemptibleHolders(t *testing.T) {
	tests := []struct {
		name                   string
		holderPreemptible, new bool
		wantPreempted          bool
	}{
		{name: "deployment preempts a validation run", holderPreemptible: true, wantPreempted: true},
		{name: "validation run does not preempt another", holderPreemptible: true, new: true},
		{name: "nothing preempts a deployment", holderPreemptible: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &preemptRepo{
				holder:   &workflowdomain.Workflow{ID: "holder", Status: workflowdomain.WorkflowRunning, Preemptible: tc.holderPreemptible},
				canceled: map[string]string{},
			}
			service := NewWorkflowService(&storetest.Transactor{}, repo, workflowdomain.PlanCatalog{}, newMemoryOwnership())
			_, err := service.Create(context.Background(), CreateWorkflowInput{
				Kind: workflowdomain.WorkflowKindCustom, Definition: "test", DefinitionVersion: 1, SiteID: "site-1",
				IntentSummary: "Deploy", TargetServerIDs: []string{"s-1"}, Preemptible: tc.new,
				Steps: []workflowdomain.Task{{ID: "run", Kind: "ansible-playbook", Name: "Run"}},
			})
			if !tc.wantPreempted {
				if !errors.Is(err, workflowdomain.ErrTargetsBusy) {
					t.Fatalf("Create() error = %v, want ErrTargetsBusy", err)
				}
				if len(repo.canceled) > 0 {
					t.Errorf("Create() canceled %v, want nothing preempted", repo.canceled)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create() error = %v, want the holder preempted", err)
			}
			if reason := repo.canceled["holder"]; !strings.HasPrefix(reason, "Preempted by Workflow "+repo.created.ID) {
				t.Errorf("holder cancel reason = %q, want it to name the preempting Workflow %s", reason, repo.created.ID)
			}
		})
	}
}
