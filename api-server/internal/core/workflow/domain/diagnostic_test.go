package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTaskDiagnosticRedacted(t *testing.T) {
	at := time.Date(2026, 11, 5, 21, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	events := make([]ProviderEvent, maxProviderEvents+5)
	for i := range events {
		events[i] = ProviderEvent{Type: "log", Description: fmt.Sprintf("event %d password=x", i), At: &at}
	}
	in := TaskDiagnostic{
		FailureClass:   "flaky",
		Target:         &ResourceReference{Kind: "server", ID: "s1"},
		Endpoint:       "https://u:p@api.lab/v1?token=t",
		Operation:      "read nodes",
		Cause:          "dial: " + strings.Repeat("x", maxCauseRunes),
		Remediation:    "Power on the Server, then retry the Task.",
		Observation:    "secret=abc still booting",
		ObservedAt:     &at,
		Attempts:       4,
		ProviderEvents: events,
	}
	got := in.Redacted()

	if got.FailureClass != FailureUnknown {
		t.Errorf("FailureClass = %q, want an invalid class stored as unknown", got.FailureClass)
	}
	if got.Endpoint != "https://api.lab/v1?token=[REDACTED]" {
		t.Errorf("Endpoint = %q, want userinfo dropped and the query masked", got.Endpoint)
	}
	if n := utf8.RuneCountInString(got.Cause); n != maxCauseRunes || !strings.HasSuffix(got.Cause, "…") {
		t.Errorf("Cause has %d runes ending %q, want it cut to %d with an ellipsis", n, got.Cause[len(got.Cause)-4:], maxCauseRunes)
	}
	if got.Observation != "secret=[REDACTED] still booting" {
		t.Errorf("Observation = %q, want the secret masked and the rest kept", got.Observation)
	}
	if got.ObservedAt == nil || got.ObservedAt.Location() != time.UTC || !got.ObservedAt.Equal(at) {
		t.Errorf("ObservedAt = %v, want %v in UTC", got.ObservedAt, at)
	}
	if len(got.ProviderEvents) != maxProviderEvents || got.ProviderEvents[0].Description != "event 5 password=[REDACTED]" {
		t.Errorf("ProviderEvents = %d starting %+v, want the latest %d redacted", len(got.ProviderEvents), got.ProviderEvents[0], maxProviderEvents)
	}
	if got.Remediation != in.Remediation || got.Operation != in.Operation || got.Attempts != 4 || *got.Target != *in.Target {
		t.Errorf("Redacted changed fields without secrets: %+v", got)
	}
	if in.ProviderEvents[0].Description != "event 0 password=x" || in.Target == got.Target {
		t.Error("Redacted changed or shares the input it copied")
	}
	again := got.Redacted()
	if again.Cause != got.Cause || again.Endpoint != got.Endpoint || again.Observation != got.Observation ||
		again.ProviderEvents[0].Description != got.ProviderEvents[0].Description {
		t.Errorf("Redacted is not idempotent: %+v then %+v", got, again)
	}
}

func TestNormalizedErrorRedacted(t *testing.T) {
	in := NormalizedError{Code: "x", Message: "login with password=hunter2 failed", Retryable: true, Stage: "auth",
		Diagnostic: &TaskDiagnostic{FailureClass: FailurePermanent, Cause: "token=abc"}}
	got := in.Redacted()
	if got.Message != "login with password=[REDACTED] failed" || got.Code != "x" || !got.Retryable || got.Stage != "auth" {
		t.Errorf("Redacted() = %+v, want only the message's secret masked", got)
	}
	if got.Diagnostic == in.Diagnostic || got.Diagnostic.Cause != "token=[REDACTED]" || got.Diagnostic.FailureClass != FailurePermanent {
		t.Errorf("Diagnostic = %+v, want a redacted copy keeping its class", got.Diagnostic)
	}
	if plain := (NormalizedError{Code: "y", Message: "m"}).Redacted(); plain.Diagnostic != nil {
		t.Errorf("an error without a diagnostic gained one: %+v", plain.Diagnostic)
	}
}

// A wait publishes and fails with the same numbers, and only a bounded wait runs out of budget.
func TestWaitProgress(t *testing.T) {
	start := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	wait := NewWaitProgress(start, 10*time.Minute)
	wait.Observe("booting", start.Add(time.Minute))
	got := wait.Observe("still booting", start.Add(4*time.Minute+500*time.Millisecond))
	want := TaskWaiting{Observation: "still booting", ObservedAt: start.Add(4*time.Minute + 500*time.Millisecond),
		Attempts: 2, ElapsedSeconds: 240, BudgetSeconds: 600}
	if got != want {
		t.Errorf("Observe() = %+v, want %+v", got, want)
	}
	if wait.Exhausted(start.Add(9 * time.Minute)) {
		t.Error("Exhausted before the budget ran out")
	}
	if !wait.Exhausted(start.Add(10 * time.Minute)) {
		t.Error("not Exhausted once the budget ran out")
	}

	cause := fmt.Errorf("%w: last seen booting", ErrWaitBudgetExhausted)
	diagnostic := wait.Diagnostic(FailureTransient, cause, start.Add(10*time.Minute))
	if diagnostic.FailureClass != FailureTransient || diagnostic.Attempts != 2 || diagnostic.ElapsedSeconds != 600 ||
		diagnostic.BudgetSeconds != 600 || diagnostic.Observation != "still booting" || diagnostic.ObservedAt == nil ||
		!diagnostic.ObservedAt.Equal(want.ObservedAt) || diagnostic.Cause != cause.Error() {
		t.Errorf("Diagnostic() = %+v, want the wait's numbers and last observation", diagnostic)
	}
	if !errors.Is(cause, ErrWaitBudgetExhausted) {
		t.Error("the cause does not wrap ErrWaitBudgetExhausted")
	}

	unbounded := NewWaitProgress(start, 0)
	if unbounded.Exhausted(start.Add(1000 * time.Hour)) {
		t.Error("a wait without a budget reported Exhausted")
	}
	if d := unbounded.Diagnostic(FailurePermanent, nil, start); d.Observation != "" || d.ObservedAt != nil || d.Cause != "" {
		t.Errorf("Diagnostic() of a wait that observed nothing = %+v, want no observation or cause", d)
	}
}
