package domain

import (
	"errors"
	"time"
	"unicode/utf8"

	"github.com/maple52046/swallow/internal/shared/redact"
)

// FailureClass says whether doing the same thing again can succeed (glossary: Failure Class). A
// Runner classifies every failure it can: the class decides whether a wait keeps waiting or fails
// fast, and tells the user whether a retry is worth trying before anything changes. The values are
// persisted and on the wire as `failureClass`.
type FailureClass string

const (
	// FailureTransient is a condition expected to clear by itself — a host still booting, an API
	// briefly unreachable, a provider still working. Waiting longer or retrying the Task can
	// succeed without any change.
	FailureTransient FailureClass = "transient"
	// FailurePermanent is a condition that repeats until something changes — rejected
	// credentials, a missing image, invalid input. A wait that observes it fails at once instead of
	// spending its budget, and a retry succeeds only after the user fixed the cause.
	FailurePermanent FailureClass = "permanent"
	// FailureUnknown is a failure the Runner could not classify, including one whose outcome is
	// itself unknown (the executor stopped before recording it). It is also the class of a stored
	// diagnostic whose class was empty or invalid.
	FailureUnknown FailureClass = "unknown"
)

// Valid reports whether c is one of the three classes. A stored diagnostic never holds another
// value: TaskDiagnostic.Redacted turns an empty or unknown class into FailureUnknown.
func (c FailureClass) Valid() bool {
	switch c {
	case FailureTransient, FailurePermanent, FailureUnknown:
		return true
	default:
		return false
	}
}

// TaskDiagnostic is the structured, durable explanation of a Task failure (glossary: Task
// Diagnostic), attached to the NormalizedError it explains. It has two layers:
//
//   - The user layer says what happened, who is affected, and what to do next: the error's
//     Message, Target, and Remediation. It must be actionable without the admin layer.
//   - The admin layer is the evidence: FailureClass, the Endpoint and Operation that failed, the
//     Cause (the causal chain as text), the wait's Attempts, ElapsedSeconds and BudgetSeconds, its
//     last Observation, and ProviderEvents the provider recorded about the target.
//
// Every text field is redacted, never erased: secrets are masked and the rest of the cause is kept
// (Redacted). The engine redacts and bounds a diagnostic again when it stores it, so the store and
// the API never hold a secret a Runner forgot to remove. The diagnostic is stored with the Task's
// error under the store's default keys (the core adds no store tags), so earlier records without
// one read as nil and need no migration; the Task keeps only its latest error, and the timeline's
// step_status events keep the class and endpoint of earlier attempts.
type TaskDiagnostic struct {
	FailureClass FailureClass `json:"failureClass"`
	// Target is the resource the failure affects (a Server, a Platform), when the Runner knows
	// which of the Task's targets it is.
	Target *ResourceReference `json:"target,omitempty"`
	// Endpoint is the address the failing call went to, already redacted (redact.URL): no
	// userinfo, no query values.
	Endpoint string `json:"endpoint,omitempty"`
	// Operation is a short name of the provider or platform call that failed, for example
	// "read cluster nodes".
	Operation string `json:"operation,omitempty"`
	// Cause is the causal chain of the failure as redacted text, for an administrator.
	Cause string `json:"cause,omitempty"`
	// Remediation is the next step for the user, in their terms ("Power on the Server, then retry
	// the Task.").
	Remediation string `json:"remediation,omitempty"`
	// Observation and ObservedAt are the last thing a wait observed before it failed.
	Observation string     `json:"observation,omitempty"`
	ObservedAt  *time.Time `json:"observedAt,omitempty"`
	// Attempts counts the observations or calls the failing wait made; zero when not counted.
	Attempts int `json:"attempts,omitempty"`
	// ElapsedSeconds is how long the failing wait ran, and BudgetSeconds how long it was allowed
	// to; zero when not measured or unbounded.
	ElapsedSeconds int64 `json:"elapsedSeconds,omitempty"`
	BudgetSeconds  int64 `json:"budgetSeconds,omitempty"`
	// ProviderEvents are what the provider recorded about the target around the failure, oldest
	// first, at most maxProviderEvents (the latest are kept).
	ProviderEvents []ProviderEvent `json:"providerEvents,omitempty"`
}

// ProviderEvent is one event a provider recorded about a Task's target, copied into a diagnostic as
// evidence; it is not a timeline event of the Workflow.
type ProviderEvent struct {
	Type        string     `json:"type"`
	Description string     `json:"description"`
	At          *time.Time `json:"at,omitempty"`
}

// TaskWaiting is what a running Task is waiting for, published by its Runner while it waits
// (glossary: Task, wait observation) so the API shows progress before the Task ends. It is
// advisory, like TaskLive: the authoritative outcome is the Task's status and error. The engine
// clears it when the attempt ends, is retried, or is canceled, and redacts and bounds it before it
// is stored. It is stored under the store's default keys.
type TaskWaiting struct {
	// Observation is the latest observed state of what the Task waits for, in user terms
	// ("Server srv-1 is still Deploying (installing the OS)").
	Observation string    `json:"observation"`
	ObservedAt  time.Time `json:"observedAt"`
	// Attempts counts the observations made so far in this wait.
	Attempts int `json:"attempts"`
	// ElapsedSeconds is how long the wait has run; BudgetSeconds how long it may run before the
	// Task fails, zero for a wait without a budget.
	ElapsedSeconds int64 `json:"elapsedSeconds"`
	BudgetSeconds  int64 `json:"budgetSeconds,omitempty"`
}

// The bounds of stored diagnostic text. A diagnostic lives in the Workflow document and is served
// on every read of it, so a provider dump or a long log cannot grow either without bound; text
// over a bound is cut at a rune boundary and ends with an ellipsis.
const (
	maxCauseRunes       = 2000
	maxTextRunes        = 500
	maxProviderEvents   = 20
	maxEventDescription = 500
)

// NewTaskDiagnostic returns a diagnostic of class whose Cause is the redacted text of cause (none
// for a nil cause). The caller adds the user layer (Target, Remediation) and the admin detail it
// knows (Endpoint, Operation, ProviderEvents) to the returned value, and attaches it to the
// NormalizedError it returns. An invalid class is stored as FailureUnknown.
func NewTaskDiagnostic(class FailureClass, cause error) *TaskDiagnostic {
	return &TaskDiagnostic{FailureClass: class, Cause: redact.Error(cause)}
}

// Redacted returns a copy of d that is safe to store and serve: every text field passes through
// redact.Text (Endpoint through redact.URL), text is bounded, at most the latest
// maxProviderEvents are kept, times are UTC, and an empty or invalid FailureClass becomes
// FailureUnknown. Redacting a redacted diagnostic changes nothing.
func (d TaskDiagnostic) Redacted() TaskDiagnostic {
	if !d.FailureClass.Valid() {
		d.FailureClass = FailureUnknown
	}
	if d.Target != nil {
		target := *d.Target
		d.Target = &target
	}
	d.Endpoint = bound(redact.URL(d.Endpoint), maxTextRunes)
	d.Operation = bound(redact.Text(d.Operation), maxTextRunes)
	d.Cause = bound(redact.Text(d.Cause), maxCauseRunes)
	d.Remediation = bound(redact.Text(d.Remediation), maxTextRunes)
	d.Observation = bound(redact.Text(d.Observation), maxTextRunes)
	d.ObservedAt = utcPointer(d.ObservedAt)
	if len(d.ProviderEvents) > maxProviderEvents {
		d.ProviderEvents = d.ProviderEvents[len(d.ProviderEvents)-maxProviderEvents:]
	}
	if d.ProviderEvents != nil {
		events := make([]ProviderEvent, len(d.ProviderEvents))
		for i, event := range d.ProviderEvents {
			events[i] = ProviderEvent{
				Type:        bound(redact.Text(event.Type), maxTextRunes),
				Description: bound(redact.Text(event.Description), maxEventDescription),
				At:          utcPointer(event.At),
			}
		}
		d.ProviderEvents = events
	}
	return d
}

// Redacted returns a copy of e that is safe to store and serve: its Message passes through
// redact.Text and its Diagnostic is redacted. Code, Stage, and Retryable are kept as they are.
func (e NormalizedError) Redacted() NormalizedError {
	e.Message = redact.Text(e.Message)
	if e.Diagnostic != nil {
		diagnostic := e.Diagnostic.Redacted()
		e.Diagnostic = &diagnostic
	}
	return e
}

// Redacted returns a copy of w that is safe to store and serve: the Observation is redacted and
// bounded, ObservedAt is UTC, and negative counters are zero.
func (w TaskWaiting) Redacted() TaskWaiting {
	w.Observation = bound(redact.Text(w.Observation), maxTextRunes)
	w.ObservedAt = w.ObservedAt.UTC()
	w.Attempts = max(w.Attempts, 0)
	w.ElapsedSeconds = max(w.ElapsedSeconds, 0)
	w.BudgetSeconds = max(w.BudgetSeconds, 0)
	return w
}

// ErrWaitBudgetExhausted reports that a bounded wait ran out of budget before what it waited for
// happened. Runners wrap it into the cause of their own error.
var ErrWaitBudgetExhausted = errors.New("the wait ran out of its budget")

// WaitProgress follows one bounded wait of a running attempt, so a Runner publishes the same
// numbers while it waits (TaskWaiting) and when it gives up (TaskDiagnostic). A wait declares
// what it observes, how it classifies what it sees, and its budget (decision 077); WaitProgress
// holds the bookkeeping, the Runner keeps the policy. It reads no clock — every method takes the
// time of the observation — so it is deterministic in tests. It is not safe for concurrent use;
// one wait belongs to one goroutine.
type WaitProgress struct {
	startedAt   time.Time
	budget      time.Duration
	attempts    int
	observation string
	observedAt  time.Time
}

// NewWaitProgress starts following a wait that began at startedAt and may run for budget; a
// non-positive budget is a wait without a budget, which never reports Exhausted.
func NewWaitProgress(startedAt time.Time, budget time.Duration) *WaitProgress {
	return &WaitProgress{startedAt: startedAt, budget: max(budget, 0)}
}

// Observe records one observation made at at and returns the TaskWaiting to publish for it.
func (w *WaitProgress) Observe(observation string, at time.Time) TaskWaiting {
	w.attempts++
	w.observation, w.observedAt = observation, at
	return TaskWaiting{
		Observation: observation, ObservedAt: at, Attempts: w.attempts,
		ElapsedSeconds: w.elapsedSeconds(at), BudgetSeconds: int64(w.budget / time.Second),
	}
}

// Exhausted reports whether the budget has run out at at. A wait without a budget never does.
func (w *WaitProgress) Exhausted(at time.Time) bool {
	return w.budget > 0 && at.Sub(w.startedAt) >= w.budget
}

// Diagnostic returns the admin layer of the wait's failure at at: class, the redacted cause,
// the attempts, elapsed time and budget, and the last observation. The Runner adds the user
// layer (Target, Remediation) and what it knows of the endpoint and operation.
func (w *WaitProgress) Diagnostic(class FailureClass, cause error, at time.Time) *TaskDiagnostic {
	diagnostic := NewTaskDiagnostic(class, cause)
	diagnostic.Attempts = w.attempts
	diagnostic.ElapsedSeconds = w.elapsedSeconds(at)
	diagnostic.BudgetSeconds = int64(w.budget / time.Second)
	if w.attempts > 0 {
		observedAt := w.observedAt
		diagnostic.Observation, diagnostic.ObservedAt = w.observation, &observedAt
	}
	return diagnostic
}

func (w *WaitProgress) elapsedSeconds(at time.Time) int64 {
	return max(int64(at.Sub(w.startedAt)/time.Second), 0)
}

// bound cuts text to at most limit runes, ending a cut text with an ellipsis.
func bound(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit-1]) + "…"
}

// utcPointer returns a copy of at in UTC, so a stored time never shares the caller's value.
func utcPointer(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	utc := at.UTC()
	return &utc
}
