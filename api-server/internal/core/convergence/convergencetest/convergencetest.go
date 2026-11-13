// Package convergencetest holds in-memory fakes of the convergence ports for tests in any package
// that builds a convergence.ChangePlanService. Production code must not import it.
package convergencetest

import (
	"context"
	"sync"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// Policies is an in-memory convergence.ChangePlanPolicyStore. The zero value has no stored policy,
// so the service reads convergence.DefaultChangePlanPolicy (approve on submit); FindErr, when set,
// is returned by every Find. It is safe for concurrent use.
type Policies struct {
	mu      sync.Mutex
	policy  *convergence.ChangePlanPolicy
	FindErr error
}

var _ convergence.ChangePlanPolicyStore = (*Policies)(nil)

// Find implements convergence.ChangePlanPolicyStore.
func (p *Policies) Find(context.Context) (convergence.ChangePlanPolicy, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.FindErr != nil {
		return convergence.ChangePlanPolicy{}, p.FindErr
	}
	if p.policy == nil {
		return convergence.ChangePlanPolicy{}, convergence.ErrChangePlanPolicyNotFound
	}
	return *p.policy, nil
}

// Save implements convergence.ChangePlanPolicyStore.
func (p *Policies) Save(_ context.Context, policy convergence.ChangePlanPolicy) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.policy = &policy
	return nil
}

// RequireSeparateApproval stores the safety switch turned on: every plan waits for a separate
// approval.
func (p *Policies) RequireSeparateApproval() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.policy = &convergence.ChangePlanPolicy{ApproveOnSubmit: false}
}

// Requests is an in-memory convergence.Requester that records every controller asked to converge,
// in order. It is safe for concurrent use.
type Requests struct {
	mu        sync.Mutex
	requested []string
}

var _ convergence.Requester = (*Requests)(nil)

// Request implements convergence.Requester.
func (r *Requests) Request(_ context.Context, controller string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requested = append(r.requested, controller)
	return nil
}

// Requested returns the controllers asked so far.
func (r *Requests) Requested() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.requested...)
}

// Events is an in-memory convergence.EventRecorder that keeps every recorded event. It is safe for
// concurrent use.
type Events struct {
	mu     sync.Mutex
	events []convergence.Event
}

var _ convergence.EventRecorder = (*Events)(nil)

// Record implements convergence.EventRecorder.
func (e *Events) Record(_ context.Context, event convergence.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
	return nil
}

// List implements convergence.EventRecorder, newest first.
func (e *Events) List(_ context.Context, resourceKind, resourceID string, limit int) ([]convergence.Event, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []convergence.Event
	for i := len(e.events) - 1; i >= 0 && (limit <= 0 || len(out) < limit); i-- {
		if e.events[i].ResourceKind == resourceKind && e.events[i].ResourceID == resourceID {
			out = append(out, e.events[i])
		}
	}
	return out, nil
}
