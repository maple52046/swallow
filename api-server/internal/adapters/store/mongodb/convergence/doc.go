// Package convergence is the MongoDB store of the convergence runtime: the singleton leases that
// keep one active instance per controller (`controller_leases`) and the convergence timeline of
// managed resources (`convergence_events`).
//
// It maps documents to and from the convergence types and owns their indexes. Which controller runs,
// and what an event means, belong to the core convergence package and the resources' contexts.
package convergence
