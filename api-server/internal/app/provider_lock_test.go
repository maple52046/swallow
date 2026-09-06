package app

import (
	"context"
	"errors"
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

type stubMutationGuard struct{ err error }

func (g stubMutationGuard) RequireUnlocked(context.Context, []string) error { return g.err }

// The provider executor must re-check the Server Lock before a host mutation and pause the
// Step for operator attention (retryable) when the target is locked, rather than mutating
// it. A nil guard (unwired) must not block.
func TestProviderExecutorRequireUnlockedFailsClosed(t *testing.T) {
	locked := providerStepExecutor{protection: stubMutationGuard{err: &serverdomain.ServerLockedError{Name: "node-1"}}}
	result := locked.requireUnlocked(context.Background(), "server-1")
	if result == nil {
		t.Fatal("a locked target must block the mutation")
	}
	if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil || result.Error.Code != "target_locked" {
		t.Fatalf("locked target must pause for attention as target_locked, got %+v", result)
	}

	if (providerStepExecutor{}).requireUnlocked(context.Background(), "server-1") != nil {
		t.Fatal("an unwired guard must not block mutation")
	}
}

// An unclassified error from a mutating provider call is an unknown outcome: the request
// may have taken effect before the transport failed, so it must reconcile under operator
// attention rather than auto-retry into a possible duplicate side effect.
func TestNormalizeProviderErrorTreatsUnclassifiedAsUnknownOutcome(t *testing.T) {
	result := normalizeProviderError(errors.New("connection reset by peer"), "deployment_preflight")
	if result.Status != operationdomain.TaskRequiresAttention {
		t.Fatalf("unclassified provider error must require attention, got status %q", result.Status)
	}
	if result.Error == nil || result.Error.Code != "provider_outcome_unknown" {
		t.Fatalf("unclassified provider error must be provider_outcome_unknown, got %+v", result.Error)
	}
}
