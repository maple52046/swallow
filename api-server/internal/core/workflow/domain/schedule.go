package domain

// ScheduleOutcome is what a Workflow does next when no Task can start.
type ScheduleOutcome int

const (
	// OutcomeContinue means Tasks are running or ready to start.
	OutcomeContinue ScheduleOutcome = iota
	// OutcomePark means a retryable failure blocks every remaining Task: the Workflow waits in
	// requires_attention for an operator retry or cancel.
	OutcomePark
	// OutcomeSucceeded means every Task succeeded or was skipped.
	OutcomeSucceeded
	// OutcomeFailed means no Task can run any more and none succeeded.
	OutcomeFailed
	// OutcomePartiallySucceeded means no Task can run any more and at least one succeeded.
	OutcomePartiallySucceeded
)

// Schedule is the next step for a Workflow, decided from its Tasks alone (no store, no clock), so
// every scheduling rule is a table test (decision 072). Indexes refer to the Tasks given to Plan.
type Schedule struct {
	// Skip lists pending Tasks that can never run: every dependency settled and one is not
	// permitted by the Task's ContinueOn. They are skipped now.
	Skip []int
	// Ready lists pending Tasks that may start, in Task order, at most the free slots.
	Ready []int
	// Outcome is OutcomeContinue whenever a Task is running or ready.
	Outcome ScheduleOutcome
	// Reason is the first failure's message for OutcomeFailed and OutcomePartiallySucceeded.
	Reason string
}

// Plan schedules tasks with slots free places in the sliding window. A Task belongs to the Job it
// names, or to the implicit Job when it names none. Within the window a Task waits until every
// dependency has settled: its DependsOn, and — when its Job depends on another named Job through
// a cross-Job DependsOn — every Task of that Job. A settled dependency permits the Task when it
// succeeded or was skipped, or when the Task's ContinueOn lists its outcome; otherwise the Task is
// skipped, which can settle further dependencies, so skipping repeats until nothing changes. A
// retryable failure is not settled: its dependents wait through the operator's retry instead of
// being skipped past a failure that may yet succeed.
func Plan(tasks []Task, slots int) Schedule {
	status := make([]TaskStatus, len(tasks))
	index := make(map[string]int, len(tasks))
	for i, task := range tasks {
		status[i] = task.Status
		index[task.ID] = i
	}
	deps := effectiveDependencies(tasks, index)

	var schedule Schedule
	for {
		skipped := false
		for i := range tasks {
			if !pendingStatus(status[i]) {
				continue
			}
			if decide(tasks[i], deps[i], tasks, status) == decisionSkip {
				status[i] = TaskSkipped
				schedule.Skip = append(schedule.Skip, i)
				skipped = true
			}
		}
		if !skipped {
			break
		}
	}

	running := false
	for i := range tasks {
		switch {
		case status[i] == TaskRunning || status[i] == TaskWaitingExternal:
			running = true
		case pendingStatus(status[i]) && decide(tasks[i], deps[i], tasks, status) == decisionRun:
			if len(schedule.Ready) < slots {
				schedule.Ready = append(schedule.Ready, i)
			}
			running = true
		}
	}
	if running {
		return schedule
	}

	succeeded, retryable := false, false
	reason := ""
	done := true
	for i, task := range tasks {
		switch status[i] {
		case TaskSucceeded:
			succeeded = true
		case TaskSkipped:
		default:
			done = false
		}
		if (status[i] == TaskFailed || status[i] == TaskRequiresAttention) && task.Error != nil && task.Error.Retryable {
			retryable = true
		}
		if reason == "" && task.Error != nil {
			reason = task.Error.Message
		}
	}
	switch {
	case done:
		schedule.Outcome = OutcomeSucceeded
	case retryable:
		schedule.Outcome = OutcomePark
	default:
		if reason == "" {
			reason = "An Operation Step failed."
		}
		schedule.Reason = reason
		schedule.Outcome = OutcomeFailed
		if succeeded {
			schedule.Outcome = OutcomePartiallySucceeded
		}
	}
	return schedule
}

type decision int

const (
	decisionWait decision = iota
	decisionRun
	decisionSkip
)

// decide resolves one pending Task against the current status of its dependencies.
func decide(task Task, deps []int, tasks []Task, status []TaskStatus) decision {
	permitted := true
	for _, dep := range deps {
		if dep < 0 || !settled(status[dep], tasks[dep].Error) {
			return decisionWait
		}
		if !dependencyPermits(status[dep], task.ContinueOn) {
			permitted = false
		}
	}
	if permitted {
		return decisionRun
	}
	return decisionSkip
}

// effectiveDependencies lists, per Task, the indexes it waits for: its DependsOn (an unknown ID is
// -1 and never settles), plus every Task of each named Job its own named Job depends on.
func effectiveDependencies(tasks []Task, index map[string]int) [][]int {
	jobTasks := map[string][]int{}
	for i, task := range tasks {
		if task.Job != "" {
			jobTasks[task.Job] = append(jobTasks[task.Job], i)
		}
	}
	jobDeps := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, task := range tasks {
		for _, dep := range task.DependsOn {
			i, ok := index[dep]
			if !ok || task.Job == "" || tasks[i].Job == "" || tasks[i].Job == task.Job {
				continue
			}
			if seen[task.Job] == nil {
				seen[task.Job] = map[string]bool{}
			}
			if !seen[task.Job][tasks[i].Job] {
				seen[task.Job][tasks[i].Job] = true
				jobDeps[task.Job] = append(jobDeps[task.Job], tasks[i].Job)
			}
		}
	}
	deps := make([][]int, len(tasks))
	for i, task := range tasks {
		included := map[int]bool{}
		for _, dep := range task.DependsOn {
			j, ok := index[dep]
			if !ok {
				j = -1
			}
			if !included[j] {
				included[j] = true
				deps[i] = append(deps[i], j)
			}
		}
		for _, job := range jobDeps[task.Job] {
			for _, j := range jobTasks[job] {
				if !included[j] {
					included[j] = true
					deps[i] = append(deps[i], j)
				}
			}
		}
	}
	return deps
}

func pendingStatus(status TaskStatus) bool {
	return status == TaskPending || status == TaskWaitingDependency
}

// settled reports whether an outcome will not change by itself. A retryable failure (or
// requires_attention) is not settled: it waits for an operator retry.
func settled(status TaskStatus, failure *NormalizedError) bool {
	switch status {
	case TaskSucceeded, TaskSkipped, TaskCanceled:
		return true
	case TaskFailed:
		return failure == nil || !failure.Retryable
	default:
		return false
	}
}

// dependencyPermits reports whether a settled dependency outcome lets a dependent run: succeeded
// or skipped by default, or any outcome the dependent's ContinueOn lists.
func dependencyPermits(status TaskStatus, continueOn []TaskStatus) bool {
	if len(continueOn) == 0 {
		return status == TaskSucceeded || status == TaskSkipped
	}
	for _, allowed := range continueOn {
		if status == allowed {
			return true
		}
	}
	return false
}
