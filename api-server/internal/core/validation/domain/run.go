package domain

import (
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/maple52046/swallow/internal/shared/redact"
)

// Mode is how a run is executed.
type Mode string

// The modes.
const (
	// ModeReport is a run in its own unattended, budgeted, usually preemptible Workflow.
	ModeReport Mode = "report"
	// ModeGate is a suite composed into a deployment's Workflow, whose final Task fails the
	// deployment when a gating check does not pass.
	ModeGate Mode = "gate"
)

// Status is where a run stands.
type Status string

// The run statuses.
const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusCanceled  Status = "canceled"
)

// Active reports whether the run has not ended.
func (s Status) Active() bool {
	return s == StatusPending || s == StatusRunning
}

// Verdict is the Validation Verdict of a check on a target, and of a run.
type Verdict string

// The verdicts. A run's verdict is never VerdictSkipped.
const (
	VerdictPassed  Verdict = "passed"
	VerdictFailed  Verdict = "failed"
	VerdictError   Verdict = "error"
	VerdictSkipped Verdict = "skipped"
)

func (v Verdict) valid() bool {
	return v == VerdictPassed || v == VerdictFailed || v == VerdictError || v == VerdictSkipped
}

// TriggerKind is what started a run.
type TriggerKind string

// The triggers.
const (
	TriggerManual     TriggerKind = "manual"
	TriggerSchedule   TriggerKind = "schedule"
	TriggerDeployment TriggerKind = "deployment"
)

// Trigger is what started a run: a user, a schedule (and the deployment that set it off), or a
// deployment that composed the suite as a gate.
type Trigger struct {
	Kind       TriggerKind
	ScheduleID string
	WorkflowID string
}

// RunSuite records which suite a run ran, as it was when the run was accepted.
type RunSuite struct {
	ID      string
	Source  Source
	Name    string
	Version string
	// Revision, ScriptSHA256, and Script record what a user suite ran.
	Revision     int
	ScriptSHA256 string
	Script       string
}

// Target is what a run validates. ServerIDs are the target Servers of a servers suite, or the
// access Servers a platform suite entered through; MemberIDs are the Platform's members at
// acceptance, the Servers a platform suite's results may name.
type Target struct {
	Kind       TargetKind
	ServerIDs  []string
	PlatformID string
	MemberIDs  []string
}

// ResultTarget is the Server or Platform one result judges.
type ResultTarget struct {
	Kind string
	ID   string
}

// The result target kinds.
const (
	ResultTargetServer   = "server"
	ResultTargetPlatform = "platform"
)

// Measurement is one value a check measured, with what it expected.
type Measurement struct {
	Name     string
	Value    float64
	Unit     string
	Expected string
}

// Result is one Validation Check's verdict on one target, observed from the Task that judged it.
type Result struct {
	Check        string
	Target       ResultTarget
	Verdict      Verdict
	Summary      string
	Measurements []Measurement
	Evidence     string
	ObservedAt   time.Time
	// TaskID and Attempt name the Task attempt that judged it; empty for an error swallow recorded
	// because no Task did.
	TaskID  string
	Attempt int
}

// The result bounds (validation.md §4.2).
const (
	maxSummaryBytes      = 1024
	maxEvidenceBytes     = 8 * 1024
	maxMeasurements      = 20
	maxResultsPerRecord  = 10000
	noResultSummary      = "No result was recorded for this check."
	unattendedTaskFailed = "The Task that judges this check ended without a result: "
)

// Run is a Validation Run: the request written once at acceptance, the results observed from its
// Tasks, and the status and verdict derived from both.
type Run struct {
	ID           string
	SiteID       string
	Suite        RunSuite
	Checks       []Check
	Target       Target
	Parameters   map[string]any
	Mode         Mode
	Trigger      Trigger
	WorkflowID   string
	Status       Status
	StatusReason string
	Verdict      Verdict
	Results      []Result
	RequestedBy  string
	RequestedAt  time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	// ExpiresAt is when the store may delete the run (its retention).
	ExpiresAt time.Time
}

// RecordResults folds what a Task returned into the run. A result for a check the run does not
// declare, for a target it does not judge, with an unknown verdict, or past the per-record bound is
// refused (and named in rejected); the rest replace any earlier result for the same check and target,
// with their text redacted and bounded. It records nothing once the run ended.
func (r *Run) RecordResults(results []Result) (rejected []string) {
	if !r.Status.Active() {
		return []string{"the run already ended"}
	}
	for index, result := range results {
		if index >= maxResultsPerRecord {
			rejected = append(rejected, fmt.Sprintf("%d results past the first %d", len(results)-index, maxResultsPerRecord))
			break
		}
		if _, ok := r.check(result.Check); !ok {
			rejected = append(rejected, fmt.Sprintf("check %q is not declared", result.Check))
			continue
		}
		if !r.judges(result.Target) {
			rejected = append(rejected, fmt.Sprintf("%s %q is not a target of the run", result.Target.Kind, result.Target.ID))
			continue
		}
		if !result.Verdict.valid() {
			rejected = append(rejected, fmt.Sprintf("verdict %q is not a verdict", result.Verdict))
			continue
		}
		result.Summary = bounded(redact.Text(result.Summary), maxSummaryBytes)
		result.Evidence = bounded(redact.Text(result.Evidence), maxEvidenceBytes)
		if len(result.Measurements) > maxMeasurements {
			result.Measurements = result.Measurements[:maxMeasurements]
		}
		r.put(result)
	}
	return rejected
}

// Start records that the run's first check began.
func (r *Run) Start(now time.Time) {
	if r.Status == StatusPending {
		r.Status, r.StartedAt = StatusRunning, &now
	}
}

// Complete ends the run: every declared check without a result on a target it judges gets an
// error, its summary taking failures (the error of each Task that ended without judging, keyed by
// nothing in particular: the first is used) when there is one, and the verdict is derived. A
// servers suite judges every check on every target Server; a platform suite's check without any
// result is judged on the Platform. It does nothing once the run ended.
func (r *Run) Complete(taskFailure string, now time.Time) {
	if !r.Status.Active() {
		return
	}
	summary := noResultSummary
	if taskFailure != "" {
		summary = bounded(redact.Text(unattendedTaskFailed+taskFailure), maxSummaryBytes)
	}
	for _, check := range r.Checks {
		switch r.Target.Kind {
		case TargetServers:
			for _, serverID := range r.Target.ServerIDs {
				target := ResultTarget{Kind: ResultTargetServer, ID: serverID}
				if !r.hasResult(check.ID, target) {
					r.put(Result{Check: check.ID, Target: target, Verdict: VerdictError, Summary: summary, ObservedAt: now})
				}
			}
		case TargetPlatform:
			if !r.hasAnyResult(check.ID) {
				r.put(Result{Check: check.ID, Target: ResultTarget{Kind: ResultTargetPlatform, ID: r.Target.PlatformID},
					Verdict: VerdictError, Summary: summary, ObservedAt: now})
			}
		}
	}
	if r.StartedAt == nil {
		r.StartedAt = &now
	}
	r.Status, r.FinishedAt, r.Verdict = StatusCompleted, &now, DeriveVerdict(r.Results)
}

// Reopen starts a gate run over for its deployment's next attempt at the gate: a retry of the
// composed Job, or a rerun of the deployment (workflowID is then the rerun's Workflow). The results
// of the earlier attempt are cleared, because the retried checks judge the targets again. A report
// run is never reopened: it is not retried.
func (r *Run) Reopen(workflowID string, now time.Time) {
	if r.Mode != ModeGate {
		return
	}
	r.WorkflowID, r.Status, r.StatusReason, r.Verdict = workflowID, StatusRunning, "", ""
	r.Results, r.StartedAt, r.FinishedAt = nil, &now, nil
}

// Cancel ends the run without a verdict, keeping the results recorded so far.
func (r *Run) Cancel(reason string, now time.Time) {
	if !r.Status.Active() {
		return
	}
	r.Status, r.StatusReason, r.FinishedAt, r.Verdict = StatusCanceled, reason, &now, ""
}

// DeriveVerdict is a completed run's verdict: failed when any result failed, else error when any is
// error, else passed (every result skipped included).
func DeriveVerdict(results []Result) Verdict {
	verdict := VerdictPassed
	for _, result := range results {
		switch result.Verdict {
		case VerdictFailed:
			return VerdictFailed
		case VerdictError:
			verdict = VerdictError
		}
	}
	return verdict
}

// Counts are a run's results by verdict.
type Counts struct {
	Passed, Failed, Error, Skipped int
}

// Counts counts the run's results by verdict.
func (r Run) Counts() Counts {
	var counts Counts
	for _, result := range r.Results {
		switch result.Verdict {
		case VerdictPassed:
			counts.Passed++
		case VerdictFailed:
			counts.Failed++
		case VerdictError:
			counts.Error++
		case VerdictSkipped:
			counts.Skipped++
		}
	}
	return counts
}

// GateFailures are the results of gating checks that neither passed nor were skipped, plus an error
// result for every gating check no result judged: what fails a gate run's deployment.
func (r Run) GateFailures() []Result {
	var failures []Result
	for _, check := range r.Checks {
		if !check.Gating {
			continue
		}
		judged := false
		for _, result := range r.Results {
			if result.Check != check.ID {
				continue
			}
			judged = true
			if result.Verdict != VerdictPassed && result.Verdict != VerdictSkipped {
				failures = append(failures, result)
			}
		}
		if !judged {
			failures = append(failures, Result{Check: check.ID, Verdict: VerdictError, Summary: noResultSummary,
				Target: ResultTarget{Kind: ResultTargetPlatform, ID: r.Target.PlatformID}})
		}
	}
	return failures
}

func (r Run) check(id string) (Check, bool) {
	for _, check := range r.Checks {
		if check.ID == id {
			return check, true
		}
	}
	return Check{}, false
}

// judges reports whether a result about target belongs to this run: one of its target Servers, a
// member of its Platform, or the Platform itself.
func (r Run) judges(target ResultTarget) bool {
	switch target.Kind {
	case ResultTargetServer:
		return slices.Contains(r.Target.ServerIDs, target.ID) || slices.Contains(r.Target.MemberIDs, target.ID)
	case ResultTargetPlatform:
		return r.Target.Kind == TargetPlatform && target.ID == r.Target.PlatformID
	}
	return false
}

func (r *Run) put(result Result) {
	for index, existing := range r.Results {
		if existing.Check == result.Check && existing.Target == result.Target {
			r.Results[index] = result
			return
		}
	}
	r.Results = append(r.Results, result)
}

func (r Run) hasResult(check string, target ResultTarget) bool {
	return slices.ContainsFunc(r.Results, func(result Result) bool { return result.Check == check && result.Target == target })
}

func (r Run) hasAnyResult(check string) bool {
	return slices.ContainsFunc(r.Results, func(result Result) bool { return result.Check == check })
}

// bounded cuts text to at most limit bytes on a rune boundary, keeping its end: a check's output
// is most telling where it stopped.
func bounded(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := len(text) - limit
	for cut < len(text) && !utf8.RuneStart(text[cut]) {
		cut++
	}
	return text[cut:]
}
