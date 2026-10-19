package temporalworkflow

import (
	"context"
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// eventRepo records the timeline events UpdateStep appends. It embeds the repository interface so
// the methods UpdateStep never calls stay nil.
type eventRepo struct {
	operationdomain.WorkflowRepository
	events []operationdomain.TimelineEvent
}

func (r *eventRepo) UpdateStep(context.Context, string, operationdomain.Task) error { return nil }

func (r *eventRepo) AppendEvent(_ context.Context, event operationdomain.TimelineEvent) error {
	r.events = append(r.events, event)
	return nil
}

// TestUpdateStepEventKeepsEachAttemptsError guards lab-afde-mi308-3's lost history: the Task keeps
// only its latest error, so each retry overwrote why the first deployment failed. Every attempt's
// status event now carries that attempt's normalized error, and an event without one carries none.
func TestUpdateStepEventKeepsEachAttemptsError(t *testing.T) {
	repo := &eventRepo{}
	activities := NewActivities(repo, nil, nil)
	step := operationdomain.Task{ID: "provision-a", Kind: "provision-os", Name: "Provision a"}
	updates := []operationdomain.Task{
		{Status: operationdomain.TaskRunning, Attempt: 1},
		{Status: operationdomain.TaskFailed, Attempt: 1, Error: &operationdomain.NormalizedError{
			Code: "deployment_failed", Message: "The provisioner reported that OS deployment failed.", Stage: "deployment", Retryable: true,
		}},
		{Status: operationdomain.TaskFailed, Attempt: 2, Error: &operationdomain.NormalizedError{
			Code: "deployment_preflight_rejected", Message: "The Server provisioning state is not ready.", Stage: "deployment_preflight",
		}},
	}
	for _, update := range updates {
		update.ID, update.Kind, update.Name = step.ID, step.Kind, step.Name
		if err := activities.UpdateStep(context.Background(), StepUpdate{OperationID: "op", Step: update}); err != nil {
			t.Fatalf("UpdateStep(%s attempt %d) error = %v", update.Status, update.Attempt, err)
		}
	}

	if len(repo.events) != 3 {
		t.Fatalf("events = %d, want one per update", len(repo.events))
	}
	if _, ok := repo.events[0].Details["code"]; ok {
		t.Errorf("running event details = %v, want no error fields", repo.events[0].Details)
	}
	wantCodes := map[int]string{1: "deployment_failed", 2: "deployment_preflight_rejected"}
	for index, event := range repo.events[1:] {
		attempt := index + 1
		if got := event.Details["code"]; got != wantCodes[attempt] {
			t.Errorf("attempt %d event code = %v, want %q", attempt, got, wantCodes[attempt])
		}
		if event.Details["stage"] == "" || event.Details["message"] == "" {
			t.Errorf("attempt %d event details = %v, want stage and message", attempt, event.Details)
		}
	}
	if repo.events[1].ID == repo.events[2].ID {
		t.Errorf("both failed attempts share event ID %q; each attempt must keep its own", repo.events[1].ID)
	}
}
