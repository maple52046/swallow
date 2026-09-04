package application

import (
	"context"
	"errors"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// fencingLeaseRepo lets a test decide whether a resource lease still validates.
type fencingLeaseRepo struct{ validateErr error }

func (fencingLeaseRepo) Acquire(context.Context, []string, string, time.Time) ([]operationdomain.ResourceLease, error) {
	return nil, nil
}
func (fencingLeaseRepo) Renew(context.Context, []operationdomain.ResourceLease, time.Time) error {
	return nil
}
func (r fencingLeaseRepo) Validate(context.Context, operationdomain.ResourceLease) error {
	return r.validateErr
}
func (fencingLeaseRepo) Release(context.Context, []operationdomain.ResourceLease) error { return nil }

// The standalone Ansible executor must fail closed before mutating a host when the
// workflow-held resource lease can no longer be validated (a fencing conflict), and must
// treat an execution that carries no leases (the legacy v2 drain path) as a no-op.
func TestAnsibleWorkerValidateLeasesFailsClosedOnLostFencing(t *testing.T) {
	execution := &operationdomain.AnsibleExecution{
		ResourceLeases: []operationdomain.ResourceLease{{ResourceKey: "server:server-1", FencingToken: 3}},
	}

	fenced := &AnsibleQueueWorker{leases: fencingLeaseRepo{validateErr: errors.New("fencing token is stale")}}
	if err := fenced.validateLeases(context.Background(), execution); err == nil {
		t.Fatal("a lost fencing token must fail closed before host mutation")
	}

	current := &AnsibleQueueWorker{leases: fencingLeaseRepo{}}
	if err := current.validateLeases(context.Background(), execution); err != nil {
		t.Fatalf("a current lease must validate: %v", err)
	}

	// No lease repository and legacy executions without leases must not block the drain.
	legacy := &AnsibleQueueWorker{}
	if err := legacy.validateLeases(context.Background(), &operationdomain.AnsibleExecution{}); err != nil {
		t.Fatalf("legacy execution without leases should be a no-op: %v", err)
	}
}
