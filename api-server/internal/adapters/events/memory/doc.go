// Package memory is the in-process event adapter for Server projection changes: the broker that
// fans Server events out to one API process's stream subscribers. The Server event relay feeds it
// from the store's change feed, so it carries the Server writes of every process.
//
// Events live only in this process's memory: a subscriber that falls behind is dropped and must
// reload, and nothing is replayed after a restart. The durable change feed belongs to the store
// adapter, not here.
package memory
