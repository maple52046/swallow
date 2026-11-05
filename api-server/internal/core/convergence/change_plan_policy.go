package convergence

import (
	"context"
	"errors"
	"time"
)

// ChangePlanPolicy is the installation's rule for approving a Change Plan with the spec write that
// proposes it (decision 076). It is one runtime setting, not configuration: an admin changes it
// through the API and it applies to the writes that follow, never to plans already proposed.
type ChangePlanPolicy struct {
	// ApproveOnSubmit lets a write its user approved store its plan approved. False is the safety
	// switch: every plan waits for a separate approval, whatever the write asked.
	ApproveOnSubmit bool
	// UpdatedAt and UpdatedBy record the last replacement; zero and "" for a policy never stored.
	UpdatedAt time.Time
	UpdatedBy string
}

// DefaultChangePlanPolicy is the policy of an installation that never stored one: a write its user
// approved is approved at once.
func DefaultChangePlanPolicy() ChangePlanPolicy {
	return ChangePlanPolicy{ApproveOnSubmit: true}
}

// ErrChangePlanPolicyNotFound means the installation never stored a Change Plan policy, which reads
// as DefaultChangePlanPolicy.
var ErrChangePlanPolicyNotFound = errors.New("no change plan policy is stored")

// ChangePlanPolicyStore keeps the installation's single Change Plan policy. It guards approval, so
// the Change Plan service cannot run without one.
//
// Find returns the stored policy, or ErrChangePlanPolicyNotFound when none was ever stored; it is
// called inside the transaction of a spec write, so it reads through ctx and writes nothing. Save
// replaces the stored policy as a whole. Production implementations are durable and shared by every
// process; in-memory ones are for tests.
type ChangePlanPolicyStore interface {
	Find(ctx context.Context) (ChangePlanPolicy, error)
	Save(ctx context.Context, policy ChangePlanPolicy) error
}
