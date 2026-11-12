package application

import (
	"strings"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// A Workflow past its budget gets a cancel command naming the budget; one within it, or without a
// budget, is left alone.
func TestDispatcherCancelsAWorkflowPastItsBudget(t *testing.T) {
	started := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		budget     int
		now        time.Time
		wantCancel bool
	}{
		{name: "past the budget", budget: 600, now: started.Add(11 * time.Minute), wantCancel: true},
		{name: "within the budget", budget: 600, now: started.Add(9 * time.Minute)},
		{name: "no budget", budget: 0, now: started.Add(72 * time.Hour)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := currentWorkflow("w")
			w.BudgetSeconds = tc.budget
			w.StartedAt = &started
			d, f, _ := newDispatcherFixture(t, w)
			d.now = func() time.Time { return tc.now }
			reconcile(t, d, "w", convergence.TriggerResync)

			cancel := f.workflows.get(t, "w").Commands.Cancel
			if !tc.wantCancel {
				if cancel != nil {
					t.Errorf("Reconcile() stored cancel %+v, want none", cancel)
				}
				return
			}
			if cancel == nil || !strings.HasPrefix(cancel.Reason, BudgetExceededCode) {
				t.Errorf("Reconcile() cancel = %+v, want one with reason %s", cancel, BudgetExceededCode)
			}
		})
	}
}
