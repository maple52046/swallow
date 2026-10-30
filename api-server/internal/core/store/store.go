// Package store defines the store ports that every core context uses to keep cross-document
// invariants atomic (decision 069, docs/development/desired-state-model.md §10).
//
// It is part of the core: it states what the core needs from a store, in the core's terms, and
// must not import a database driver or name a database. The MongoDB implementation lives in
// internal/adapters/store/mongodb; application tests use storetest. Repository contracts stay with
// their contexts; this package only owns the transaction boundary and the commit hooks that run
// after it.
package store

import (
	"context"
	"sync"
)

// Transactor runs a unit of work atomically: every write that fn makes through a repository
// receiving the ctx passed to fn commits together, or none of them does.
//
// Callers put only store reads and writes inside fn. fn must not call a provider, HTTP, Temporal,
// or the file system, because the store may run fn more than once (a transient transaction error
// retries the whole unit) and a long transaction conflicts with concurrent writers. Prepare such
// inputs before InTransaction and publish their effects with AfterCommit.
//
// A call made with a ctx that is already inside a transaction joins that transaction instead of
// starting a nested one, so a use case can compose other use cases that open their own unit of
// work. InTransaction returns fn's error unchanged (so errors.Is works on domain errors) after the
// transaction is aborted; any other error means nothing was committed, or, for a commit whose
// result is unknown after the store's own retries, that the caller must treat the outcome as
// unknown. Implementations are safe for concurrent use.
type Transactor interface {
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// AfterCommit registers f to run once the transaction that ctx belongs to has committed, in
// registration order and on the caller's goroutine. A rolled-back or retried attempt drops the
// hooks it registered, so f observes only committed state; this is how events (for example the
// Server event stream) avoid announcing writes that never happened. Outside a transaction, f runs
// immediately. f must not fail the unit of work: a hook runs after the commit and cannot undo it.
func AfterCommit(ctx context.Context, f func()) {
	if scope, ok := ctx.Value(scopeKey{}).(*CommitScope); ok {
		scope.add(f)
		return
	}
	f()
}

// InTransaction reports whether ctx belongs to a unit of work started by a Transactor. Fakes use
// it to assert that a write happens inside the transaction, and Transactor implementations use it
// to join an enclosing transaction.
func InTransaction(ctx context.Context) bool {
	_, ok := ctx.Value(scopeKey{}).(*CommitScope)
	return ok
}

// CommitScope collects the AfterCommit hooks of one transaction. Only Transactor implementations
// create one (through WithCommitScope); everything else uses AfterCommit.
type CommitScope struct {
	mu    sync.Mutex
	hooks []func()
}

// WithCommitScope returns a ctx that marks a new unit of work and the scope that collects its
// hooks. The Transactor calls Reset at the start of every attempt and Run only after a successful
// commit.
func WithCommitScope(ctx context.Context) (context.Context, *CommitScope) {
	scope := &CommitScope{}
	return context.WithValue(ctx, scopeKey{}, scope), scope
}

// Reset drops the hooks registered so far. A Transactor calls it before each attempt, because a
// retried attempt re-registers its own hooks.
func (s *CommitScope) Reset() {
	s.mu.Lock()
	s.hooks = nil
	s.mu.Unlock()
}

// Run executes the registered hooks once, in registration order, and clears them.
func (s *CommitScope) Run() {
	s.mu.Lock()
	hooks := s.hooks
	s.hooks = nil
	s.mu.Unlock()
	for _, hook := range hooks {
		hook()
	}
}

func (s *CommitScope) add(f func()) {
	s.mu.Lock()
	s.hooks = append(s.hooks, f)
	s.mu.Unlock()
}

// scopeKey is the context key of the active CommitScope; unexported so only this package can set it.
type scopeKey struct{}
