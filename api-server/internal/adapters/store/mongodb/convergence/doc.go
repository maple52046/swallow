// Package convergence is the MongoDB store of the convergence runtime: the singleton leases that
// keep one active instance per controller (`controller_leases`) and the convergence timeline of
// managed resources (`convergence_events`), and the requests that ask a controller to converge now
// (`convergence_requests`). It also owns the stored shape of conditions and per-target
// realizations, which every migrated resource's collection embeds.
//
// It maps documents to and from the convergence types and owns their indexes. Which controller runs,
// and what an event means, belong to the core convergence package and the resources' contexts.
package convergence
