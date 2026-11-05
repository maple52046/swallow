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
