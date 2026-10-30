// Package storetest provides a fake store.Transactor for application tests.
//
// The fake cannot roll back in-memory repositories, so a test with it can prove that a use case
// writes inside a transaction (store.InTransaction in a fake repository) and that its events wait
// for the commit, but not that a failed unit of work leaves nothing behind. That atomicity is
// verified by the store adapter's integration tests against a replica set. Production code must
// not import this package.
package storetest

import (
	"context"
	"sync"

	"github.com/maple52046/swallow/internal/core/store"
)

// Transactor is a fake store.Transactor. It runs fn once, runs the AfterCommit hooks only when fn
// succeeds and CommitErr is nil, and counts the outermost transactions it started. A call inside
// an existing transaction joins it, like the real implementation. The zero value is ready to use
// and safe for concurrent use.
type Transactor struct {
	// CommitErr, when set, is returned as the commit failure after fn succeeds; the hooks do not
	// run. It simulates a store that fails at commit time.
	CommitErr error

	mu    sync.Mutex
	calls int
}

var _ store.Transactor = (*Transactor)(nil)

// InTransaction implements store.Transactor.
func (t *Transactor) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if store.InTransaction(ctx) {
		return fn(ctx)
	}
	t.mu.Lock()
	t.calls++
	commitErr := t.CommitErr
	t.mu.Unlock()

	txCtx, scope := store.WithCommitScope(ctx)
	if err := fn(txCtx); err != nil {
		return err
	}
	if commitErr != nil {
		return commitErr
	}
	scope.Run()
	return nil
}

// Calls reports how many outermost transactions were started; joined calls are not counted.
func (t *Transactor) Calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}
