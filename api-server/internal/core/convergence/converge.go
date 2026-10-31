package convergence

import (
	"context"
	"time"
)

// SingleTarget describes one Reconcile of a resource realized on a single target, for controllers
// whose resource has no hold and no fields of its own to iterate (a provisioner setting, for
// example). Drift is how the observed state departs from the spec, "" when it matches. Realize
// changes the target to the spec and returns how the target still departs after reading it back.
// Rejected reports whether a Realize error is a permanent refusal, and its operator-facing detail.
type SingleTarget struct {
	Now                time.Time
	Trigger            Trigger
	Generation         int64
	ObservedGeneration int64
	Conditions         []Condition
	Drift              string
	Realize            func(ctx context.Context) (remaining string, err error)
	Rejected           func(err error) (detail string, ok bool)
}

// SingleTargetResult is the judgement to store: the new conditions and observed generation,
// whether either changed, the timeline events to record, and an error the runtime should retry.
type SingleTargetResult struct {
	Conditions         []Condition
	ObservedGeneration int64
	Changed            bool
	Events             []ResultEvent
	Err                error
}

// ResultEvent is one timeline entry a Reconcile produced.
type ResultEvent struct {
	Type    EventType
	Message string
}

// ConvergeSingleTarget applies the shared convergence rules (desired-state-model.md §6, §7) to one
// resource on one target:
//   - a stopped convergence (requires_attention for this generation) waits for a spec change;
//   - a resource that matches is realized, and its observed generation advances;
//   - a spec change (observed generation behind) converges on any trigger;
//   - drift of a realized spec is reported at once and corrected only on the periodic resync;
//   - a rejection sets requires_attention; any other Realize error is transient and returned;
//   - a target that still differs after Realize is converging and retried.
func ConvergeSingleTarget(ctx context.Context, in SingleTarget) SingleTargetResult {
	r := &SingleTargetResult{Conditions: in.Conditions, ObservedGeneration: in.ObservedGeneration}
	set := func(t ConditionType, status ConditionStatus, reason, message string) {
		var changed bool
		r.Conditions, changed = SetCondition(r.Conditions, Condition{
			Type: t, Status: status, Reason: reason, Message: message, ObservedGeneration: in.Generation,
		}, in.Now)
		r.Changed = r.Changed || changed
	}
	event := func(t EventType, message string) {
		r.Events = append(r.Events, ResultEvent{Type: t, Message: message})
	}
	realized := func(corrected, pending bool) {
		wasDrifted := IsTrue(r.Conditions, ConditionDrifted)
		set(ConditionRealized, ConditionTrue, ReasonConverged, "")
		set(ConditionDrifted, ConditionFalse, ReasonInSync, "")
		set(ConditionRequiresAttention, ConditionFalse, ReasonAttentionNotNeeded, "")
		if r.ObservedGeneration != in.Generation {
			r.ObservedGeneration, r.Changed = in.Generation, true
		}
		switch {
		case corrected:
			event(EventDriftCorrected, "Drift was corrected to the spec.")
		case pending:
			event(EventConverged, "The spec was realized.")
		case wasDrifted:
			event(EventDriftResolved, "The target matches the spec again.")
		}
	}

	if attention, ok := FindCondition(r.Conditions, ConditionRequiresAttention); ok &&
		attention.Status == ConditionTrue && attention.ObservedGeneration == in.Generation {
		return *r
	}
	pending := in.ObservedGeneration < in.Generation
	if in.Drift == "" {
		realized(false, pending)
		return *r
	}
	if !pending {
		if !IsTrue(r.Conditions, ConditionDrifted) {
			event(EventDriftDetected, in.Drift)
		}
		set(ConditionDrifted, ConditionTrue, ReasonDriftDetected, in.Drift)
		set(ConditionRealized, ConditionFalse, ReasonConverging, "Drift is corrected at the next resync.")
		if in.Trigger != TriggerResync {
			return *r
		}
	}
	remaining, err := in.Realize(ctx)
	if err != nil {
		if detail, ok := in.Rejected(err); ok {
			set(ConditionRequiresAttention, ConditionTrue, ReasonProviderRejected, detail)
			set(ConditionRealized, ConditionFalse, ReasonFailed, detail)
			event(EventConvergenceFailed, detail)
			return *r
		}
		set(ConditionRealized, ConditionFalse, ReasonProviderUnreachable, "The provisioner could not be reached; retrying.")
		r.Err = err
		return *r
	}
	if remaining != "" {
		set(ConditionRealized, ConditionFalse, ReasonConverging, remaining)
		r.Err = errStillDiffers
		return *r
	}
	realized(!pending, pending)
	return *r
}

// errStillDiffers is returned when the target accepted a change but reports something else, so
// the runtime observes it again with backoff.
var errStillDiffers = stillDiffersError{}

type stillDiffersError struct{}

func (stillDiffersError) Error() string {
	return "the target still differs from the spec after converging"
}
