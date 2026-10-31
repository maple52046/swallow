package temporalworkflow

import (
	"context"
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// ignoringStepRepo rejects every step write as a regression, the way the Mongo guard rejects a
// `canceled` write for a Task that already succeeded.
type ignoringStepRepo struct {
	eventRepo
}

func (r *ignoringStepRepo) UpdateStep(context.Context, string, operationdomain.Task) error {
	return operationdomain.ErrStepTransitionIgnored
}

type recordingObserver struct{ steps []operationdomain.Task }

func (o *recordingObserver) ObserveStep(_ context.Context, _ string, step operationdomain.Task) error {
	o.steps = append(o.steps, step)
	return nil
}

// TestUpdateStepStopsAtIgnoredTransition guards lab-afde-mi308-1 and -2: canceling the platform
// Workflow re-emitted their succeeded provision-os as canceled from the parent's start snapshot.
// The repository kept the Task, but the observer still rewrote the Server deployment result to
// canceled. An ignored transition now reaches neither the observers nor the timeline.
func TestUpdateStepStopsAtIgnoredTransition(t *testing.T) {
	repo := &ignoringStepRepo{}
	observer := &recordingObserver{}
	activities := NewActivities(repo, nil, nil, observer)

	err := activities.UpdateStep(context.Background(), StepUpdate{OperationID: "op", Step: operationdomain.Task{
		ID: "provision-a", Kind: "provision-os", Attempt: 1, Status: operationdomain.TaskCanceled,
	}})

	if err != nil {
		t.Fatalf("UpdateStep error = %v, want nil for an ignored transition", err)
	}
	if len(observer.steps) != 0 {
		t.Errorf("observer saw %d steps, want none", len(observer.steps))
	}
	if len(repo.events) != 0 {
		t.Errorf("timeline events = %d, want none", len(repo.events))
	}
}
