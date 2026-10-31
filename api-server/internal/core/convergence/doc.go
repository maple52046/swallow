// Package convergence is the core's shared model and runtime for desired-state convergence
// (decisions 069 and 071, docs/development/desired-state-model.md §4, §6, §7).
//
// It owns the vocabulary every migrated resource uses in its status — generation and
// observedGeneration, the shared Condition types and reasons, per-target realization — and the
// Runtime that runs each resource's Controller: one active instance per Controller through a store
// lease, woken by the store change feed and a periodic resync, with backoff for transient errors.
//
// It holds no resource-specific rule. What a resource's spec means, how it is realized, which of its
// fields auto-correct, and when it is held belong to that resource's context; its Controller lives
// there and plugs into the Runtime. The package must not import a provider, a database driver, or
// another context.
package convergence
