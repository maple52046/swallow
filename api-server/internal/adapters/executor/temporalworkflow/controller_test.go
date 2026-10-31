package temporalworkflow

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// controlClient fakes the cancel and signal calls the Controller makes, returning one fixed
// error. It embeds client.Client so the other client methods stay nil and unused.
type controlClient struct {
	client.Client
	err error
}

func (c controlClient) CancelWorkflow(context.Context, string, string) error { return c.err }

func (c controlClient) SignalWorkflow(context.Context, string, string, string, any) error {
	return c.err
}

// TestControllerReportsGoneExecution verifies a control aimed at an execution Temporal no longer
// has reports both ErrWorkflowExecutionGone (so Cancel can close the Workflow itself) and
// ErrWorkflowControlConflict (so it maps to 409 and Rerun keeps tolerating it), while any other
// Temporal failure is returned unchanged.
func TestControllerReportsGoneExecution(t *testing.T) {
	operation := &workflowdomain.Workflow{ID: "op", Temporal: workflowdomain.TemporalReference{WorkflowID: "swallow-operation/op", RunID: "run"}}
	controls := map[string]func(*Controller) error{
		"cancel": func(c *Controller) error { return c.Cancel(context.Background(), operation) },
		"retry":  func(c *Controller) error { return c.RetryStep(context.Background(), operation, "step") },
	}
	for name, control := range controls {
		t.Run(name+" on an absent execution", func(t *testing.T) {
			err := control(NewController(controlClient{err: serviceerror.NewNotFound("workflow not found")}))
			if !errors.Is(err, workflowdomain.ErrWorkflowExecutionGone) || !errors.Is(err, workflowdomain.ErrWorkflowControlConflict) {
				t.Errorf("%s error = %v, want ErrWorkflowExecutionGone and ErrWorkflowControlConflict", name, err)
			}
		})
		t.Run(name+" during a Temporal outage", func(t *testing.T) {
			unavailable := errors.New("temporal unavailable")
			err := control(NewController(controlClient{err: unavailable}))
			if !errors.Is(err, unavailable) || errors.Is(err, workflowdomain.ErrWorkflowExecutionGone) {
				t.Errorf("%s error = %v, want the outage error unchanged", name, err)
			}
		})
	}
}
