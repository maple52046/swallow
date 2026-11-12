package domain

import (
	"reflect"
	"testing"
)

// An unattended Workflow never parks: a retryable failure and a Task handed to the user settle at
// once, so their dependents are skipped or, through ContinueOn, run, and the Workflow ends.
func TestPlanUnattended(t *testing.T) {
	finish := task("finish", TaskPending, "check")
	finish.ContinueOn = []TaskStatus{TaskFailed, TaskSkipped, TaskCanceled}
	attention := task("check", TaskRequiresAttention)
	attention.Error = &NormalizedError{Code: "executor_outcome_unknown", Message: "lost", Retryable: true}

	tests := []struct {
		name       string
		tasks      []Task
		unattended bool
		want       Schedule
	}{
		{
			name:  "attended: a retryable failure parks the Workflow",
			tasks: []Task{failedTask("check", true), task("next", TaskPending, "check")},
			want:  Schedule{Outcome: OutcomePark},
		},
		{
			name:       "unattended: a retryable failure skips its dependents and the Workflow fails",
			tasks:      []Task{failedTask("check", true), task("next", TaskPending, "check")},
			unattended: true,
			want:       Schedule{Skip: []int{1}, Outcome: OutcomeFailed, Reason: "check failed"},
		},
		{
			name:       "unattended: a final Task that continues on failure still runs",
			tasks:      []Task{failedTask("check", true), finish},
			unattended: true,
			want:       Schedule{Ready: []int{1}},
		},
		{
			name:       "unattended: an outcome handed to the user counts as failed",
			tasks:      []Task{attention, finish},
			unattended: true,
			want:       Schedule{Ready: []int{1}},
		},
		{
			name:       "unattended: the Workflow partially succeeds once the final Task ran",
			tasks:      []Task{failedTask("check", true), task("finish", TaskSucceeded, "check")},
			unattended: true,
			want:       Schedule{Outcome: OutcomePartiallySucceeded, Reason: "check failed"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PlanWith(tc.tasks, 4, PlanPolicy{Unattended: tc.unattended})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("PlanWith() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
