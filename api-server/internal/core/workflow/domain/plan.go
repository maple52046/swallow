package domain

import (
	"errors"
	"fmt"
)

// A Workflow is a declarative execution plan (decision 072): besides its Tasks and dependencies it
// declares how each Job retries, what each Task does when it completes or when the Workflow is
// canceled, and — through each Task's kind — how a rerun and a resume treat it. The engine acts
// only on these declarations and never on a Workflow kind, Task kind, or Task ID.

// RetryGranularity says what an operator retry of a failed Task in a Job runs again.
type RetryGranularity string

const (
	// RetryTask runs only the failed Task again at its next attempt. Tasks without a Job (the
	// implicit Job) always retry this way.
	RetryTask RetryGranularity = "task"
	// RetryJob runs every Task of the failed Task's Job again at its next attempt, so each Runner
	// redoes its work under a new idempotency key; converged ensure Tasks finish quickly.
	RetryJob RetryGranularity = "job"
)

// JobDeclaration declares one named Job of a Workflow. Every Job a Task names must be declared,
// so how a retry behaves is part of the accepted plan rather than a property of the engine.
type JobDeclaration struct {
	Name  string           `json:"name" bson:"name"`
	Retry RetryGranularity `json:"retry" bson:"retry"`
}

// Completion is one completion behavior a Task declares: a registered Type whose handler the core
// calls on the Task's transitions, and the Params that handler reads. A handler never reads the
// Task's Runner Parameters, so a Runner's input and a completion's input can change separately.
type Completion struct {
	Type   string         `json:"type" bson:"type"`
	Params map[string]any `json:"params,omitempty" bson:"params,omitempty"`
}

// CompletionClass says when a completion handler runs.
type CompletionClass string

const (
	// CompletionProjection handlers only write the store. They run inside the transaction of
	// every transition of the Task (begin, outcome, skip, retry, cancel), so the projection and
	// the Task state commit together.
	CompletionProjection CompletionClass = "projection"
	// CompletionResult handlers need the data the Runner returned (for example a credential). They
	// run in the activity that executed the Task, after a success and before the outcome is
	// recorded; the Runner's data never leaves that process. A failing result handler turns the
	// outcome into a retryable failure.
	CompletionResult CompletionClass = "result"
)

// CancelBehavior is what a Task declares for the cancellation of its Workflow. Both run before
// the Workflow releases its resources, so no other Workflow acts on them meanwhile.
type CancelBehavior struct {
	// Compensation names a registered compensation run once for this Task when it was running
	// (interrupted) as the cancel arrived, for example returning an interrupted deployment to
	// Ready.
	Compensation string `json:"compensation,omitempty" bson:"compensation,omitempty"`
	// Run runs this Task itself once on cancel unless it already succeeded, for example the
	// recover Task that gives back a Server a verification borrowed. Its outcome is not recorded.
	Run bool `json:"run,omitempty" bson:"run,omitempty"`
}

// RerunPolicy says whether a rerun runs a Task again (decision 060).
type RerunPolicy string

const (
	// RerunKeep keeps a succeeded or skipped Task (a one-time action) and restarts the rest at
	// attempt 1.
	RerunKeep RerunPolicy = "keep"
	// RerunConverge runs the Task again whatever its outcome, at the attempt after its last, so
	// its Runner observes before acting.
	RerunConverge RerunPolicy = "converge"
	// RerunConvergeOnLiveHosts converges like RerunConverge unless the Task already succeeded and
	// a Task that releases hosts also succeeded: a released host no longer runs the OS the Task
	// needs.
	RerunConvergeOnLiveHosts RerunPolicy = "converge-on-live-hosts"
)

// ResumePolicy says what a new dispatch does with a Task an earlier dispatch left running.
type ResumePolicy string

const (
	// ResumeObserve runs the Task again at the same attempt, marked as resumed, so its Runner
	// observes the external work it already started and continues it.
	ResumeObserve ResumePolicy = "observe"
	// ResumeAttention records the Task requires_attention (executor_outcome_unknown): its outcome
	// cannot be proven, so only an operator retry runs it again.
	ResumeAttention ResumePolicy = "attention"
)

// TaskKind is the registered properties of one Task kind. The engine reads them instead of
// matching kind names.
type TaskKind struct {
	// Runner is the only Runner that may execute the kind.
	Runner RunnerKind
	Rerun  RerunPolicy
	// ReleasesHosts marks a kind whose success releases its hosts (see RerunConvergeOnLiveHosts).
	ReleasesHosts bool
	Resume        ResumePolicy
}

// PlanCatalog is everything a plan may declare: the registered Task kinds, completion types with
// their class, and compensations. Every process builds it from the same registration, so the API
// accepts only plans the worker and the executor can run.
type PlanCatalog struct {
	TaskKinds     map[string]TaskKind
	Completions   map[string]CompletionClass
	Compensations map[string]bool
}

// ErrPlanInvalid reports a plan that declares something the catalog does not register. Delivery
// maps it to validation_error.
var ErrPlanInvalid = errors.New("workflow plan is invalid")

// Validate checks every declaration of a plan against the catalog: each Task's kind is registered
// and run by its registered Runner, each completion and compensation type is registered, and each
// named Job is declared once with a valid granularity. Structural checks (IDs, dependencies) are
// the WorkflowService's.
func (c PlanCatalog) Validate(tasks []Task, jobs []JobDeclaration) error {
	declared := make(map[string]bool, len(jobs))
	for _, job := range jobs {
		if job.Name == "" || declared[job.Name] {
			return fmt.Errorf("%w: Job %q is declared twice or has no name", ErrPlanInvalid, job.Name)
		}
		if job.Retry != RetryTask && job.Retry != RetryJob {
			return fmt.Errorf("%w: Job %q has retry granularity %q", ErrPlanInvalid, job.Name, job.Retry)
		}
		declared[job.Name] = true
	}
	for _, task := range tasks {
		kind, ok := c.TaskKinds[task.Kind]
		if !ok {
			return fmt.Errorf("%w: Task %q has unregistered kind %q", ErrPlanInvalid, task.ID, task.Kind)
		}
		if task.Executor != kind.Runner {
			return fmt.Errorf("%w: Task %q of kind %q must run on the %s Runner, not %s",
				ErrPlanInvalid, task.ID, task.Kind, kind.Runner, task.Executor)
		}
		if task.Job != "" && !declared[task.Job] {
			return fmt.Errorf("%w: Task %q names undeclared Job %q", ErrPlanInvalid, task.ID, task.Job)
		}
		for _, completion := range task.Completion {
			if _, ok := c.Completions[completion.Type]; !ok {
				return fmt.Errorf("%w: Task %q declares unregistered completion %q", ErrPlanInvalid, task.ID, completion.Type)
			}
		}
		if task.OnCancel != nil && task.OnCancel.Compensation != "" && !c.Compensations[task.OnCancel.Compensation] {
			return fmt.Errorf("%w: Task %q declares unregistered compensation %q", ErrPlanInvalid, task.ID, task.OnCancel.Compensation)
		}
	}
	return nil
}

// JobRetry returns the retry granularity of the Job a Task belongs to; the implicit Job (no name)
// and an undeclared name retry by Task.
func JobRetry(jobs []JobDeclaration, job string) RetryGranularity {
	for _, declaration := range jobs {
		if job != "" && declaration.Name == job {
			return declaration.Retry
		}
	}
	return RetryTask
}
