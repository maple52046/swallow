package domain

import (
	"reflect"
	"testing"
)

func task(id string, status TaskStatus, deps ...string) Task {
	return Task{ID: id, Status: status, DependsOn: deps}
}

func failedTask(id string, retryable bool, deps ...string) Task {
	t := task(id, TaskFailed, deps...)
	t.Error = &NormalizedError{Code: "x", Message: id + " failed", Retryable: retryable}
	return t
}

func inJob(job string, t Task) Task {
	t.Job = job
	return t
}

func TestPlan(t *testing.T) {
	cleanup := task("cleanup", TaskPending, "b")
	cleanup.ContinueOn = []TaskStatus{TaskSkipped, TaskSucceeded}
	jobCleanup := inJob("j", task("cleanup", TaskPending, "b"))
	jobCleanup.ContinueOn = []TaskStatus{TaskSkipped, TaskSucceeded}

	tests := []struct {
		name    string
		tasks   []Task
		slots   int
		want    Schedule
		comment string
	}{
		{
			name:  "independent tasks start up to the free slots",
			tasks: []Task{task("a", TaskPending), task("b", TaskPending), task("c", TaskPending)},
			slots: 2,
			want:  Schedule{Ready: []int{0, 1}},
		},
		{
			name:  "a dependent waits for its dependency",
			tasks: []Task{task("a", TaskRunning), task("b", TaskPending, "a")},
			slots: 4,
			want:  Schedule{},
		},
		{
			name:  "sliding window: a finished task frees a slot while another still runs",
			tasks: []Task{task("a", TaskSucceeded), task("b", TaskRunning), task("c", TaskPending, "a")},
			slots: 1,
			want:  Schedule{Ready: []int{2}},
		},
		{
			name:  "a non-retryable failure skips its dependents and the cascade reaches a compensation",
			tasks: []Task{failedTask("a", false), task("b", TaskPending, "a"), cleanup},
			slots: 4,
			want:  Schedule{Skip: []int{1}, Ready: []int{2}},
		},
		{
			name:  "a retryable failure parks and its dependents wait",
			tasks: []Task{failedTask("a", true), task("b", TaskPending, "a"), task("c", TaskSucceeded)},
			slots: 4,
			want:  Schedule{Outcome: OutcomePark},
		},
		{
			name:  "everything succeeded or skipped",
			tasks: []Task{task("a", TaskSucceeded), task("b", TaskSkipped)},
			slots: 4,
			want:  Schedule{Outcome: OutcomeSucceeded},
		},
		{
			name:  "a terminal failure after a success is partial",
			tasks: []Task{task("a", TaskSucceeded), failedTask("b", false, "a")},
			slots: 4,
			want:  Schedule{Outcome: OutcomePartiallySucceeded, Reason: "b failed"},
		},
		{
			name:  "a terminal failure alone fails the Workflow",
			tasks: []Task{failedTask("a", false), task("b", TaskPending, "a")},
			slots: 4,
			want:  Schedule{Skip: []int{1}, Outcome: OutcomeFailed, Reason: "a failed"},
		},
		{
			name:  "a canceled dependency settles and skips its dependent",
			tasks: []Task{task("a", TaskCanceled), task("b", TaskPending, "a")},
			slots: 4,
			want:  Schedule{Skip: []int{1}, Outcome: OutcomeFailed, Reason: "An Operation Step failed."},
		},
		{
			name: "a Job waits for every Task of the Job it depends on",
			tasks: []Task{
				inJob("ensure", task("p1", TaskSucceeded)),
				inJob("ensure", task("p2", TaskRunning)),
				inJob("configure", task("install", TaskPending, "p1")),
			},
			slots: 4,
			want:  Schedule{},
		},
		{
			name: "a Job starts once the Job it depends on settled",
			tasks: []Task{
				inJob("ensure", task("p1", TaskSucceeded)),
				inJob("ensure", task("p2", TaskSucceeded)),
				inJob("configure", task("install", TaskPending, "p1")),
			},
			slots: 4,
			want:  Schedule{Ready: []int{2}},
		},
		{
			name: "ContinueOn works inside a Job",
			tasks: []Task{
				inJob("j", failedTask("a", false)),
				inJob("j", task("b", TaskPending, "a")),
				jobCleanup,
			},
			slots: 4,
			want:  Schedule{Skip: []int{1}, Ready: []int{2}},
		},
		{
			name:  "an unknown dependency never settles",
			tasks: []Task{task("a", TaskPending, "missing")},
			slots: 4,
			want:  Schedule{Outcome: OutcomeFailed, Reason: "An Operation Step failed."},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Plan(tc.tasks, tc.slots); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Plan() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPlanCatalogValidate(t *testing.T) {
	catalog := PlanCatalog{
		TaskKinds:     map[string]TaskKind{"deploy": {Runner: RunnerKindProvisioner}, "run": {Runner: RunnerKindAnsible}},
		Completions:   map[string]CompletionClass{"project": CompletionProjection},
		Compensations: map[string]bool{"undo": true},
	}
	good := []Task{
		{ID: "a", Kind: "deploy", Executor: RunnerKindProvisioner, Job: "j",
			Completion: []Completion{{Type: "project"}}, OnCancel: &CancelBehavior{Compensation: "undo"}},
		{ID: "b", Kind: "run", Executor: RunnerKindAnsible},
	}
	if err := catalog.Validate(good, []JobDeclaration{{Name: "j", Retry: RetryJob}}); err != nil {
		t.Fatalf("Validate(good) = %v", err)
	}
	bad := map[string]struct {
		tasks []Task
		jobs  []JobDeclaration
	}{
		"unregistered kind":       {tasks: []Task{{ID: "a", Kind: "nope", Executor: RunnerKindAnsible}}},
		"wrong runner":            {tasks: []Task{{ID: "a", Kind: "run", Executor: RunnerKindInternal}}},
		"undeclared job":          {tasks: []Task{{ID: "a", Kind: "run", Executor: RunnerKindAnsible, Job: "j"}}},
		"unregistered completion": {tasks: []Task{{ID: "a", Kind: "run", Executor: RunnerKindAnsible, Completion: []Completion{{Type: "x"}}}}},
		"unregistered compensation": {tasks: []Task{{ID: "a", Kind: "run", Executor: RunnerKindAnsible,
			OnCancel: &CancelBehavior{Compensation: "x"}}}},
		"invalid granularity": {tasks: good[1:], jobs: []JobDeclaration{{Name: "j", Retry: "sometimes"}}},
	}
	for name, tc := range bad {
		if err := catalog.Validate(tc.tasks, tc.jobs); err == nil {
			t.Errorf("Validate(%s) = nil, want an error", name)
		}
	}
}
