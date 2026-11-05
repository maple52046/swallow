package application

import (
	"context"
	"errors"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// WaitingRecorder records what a running attempt waits for through the core (Engine.UpdateWaiting),
// the way LiveRecorder records an Ansible run's live progress. Implementations redact and bound the
// value before storing it, and return ErrDispatchFenced or ErrTaskNotRunning when the attempt was
// superseded or ended.
type WaitingRecorder interface {
	UpdateWaiting(ctx context.Context, ref TaskRef, waiting workflowdomain.TaskWaiting) error
}

var _ WaitingRecorder = (*Engine)(nil)

// WaitingPublisher publishes Task.Waiting for one running attempt: the activity that runs the
// attempt binds it to the attempt's TaskRef and hands it to the Runner with the attempt's input,
// so a Runner cannot publish for another Task, attempt, or dispatch generation. A Runner calls
// Publish after each observation of a long wait (typically with WaitProgress.Observe), at most
// once per observation; the engine clears the value when the attempt ends.
//
// The zero value discards every publication, so an input built without one (a compensation, a
// test) needs no special case. A WaitingPublisher is a small value, safe to copy and for
// concurrent use when its recorder is.
type WaitingPublisher struct {
	recorder WaitingRecorder
	ref      TaskRef
}

// NewWaitingPublisher binds recorder to the attempt ref. Only the activity that runs ref's attempt
// builds one; a nil recorder yields a publisher that discards, like the zero value.
func NewWaitingPublisher(recorder WaitingRecorder, ref TaskRef) WaitingPublisher {
	return WaitingPublisher{recorder: recorder, ref: ref}
}

// Publish records waiting for the bound attempt. Publication is advisory: a Runner logs a failure
// and keeps waiting, and never fails its Task because it could not publish. It returns nil when the
// attempt already ended (there is nothing left to describe) and ErrDispatchFenced when a later
// dispatch superseded the run, which tells the Runner to stop before its next side effect.
func (p WaitingPublisher) Publish(ctx context.Context, waiting workflowdomain.TaskWaiting) error {
	if p.recorder == nil {
		return nil
	}
	err := p.recorder.UpdateWaiting(ctx, p.ref, waiting)
	if errors.Is(err, workflowdomain.ErrTaskNotRunning) {
		return nil
	}
	return err
}
