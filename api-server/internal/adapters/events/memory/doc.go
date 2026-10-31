// Package memory is the in-process event adapter for Server projection changes: a decorator that
// publishes every successful Server write (after the store transaction commits, when there is
// one), and the broker that fans those events out to stream subscribers.
//
// Events live only in this process's memory: a subscriber that falls behind is dropped and must
// reload, and nothing is replayed after a restart. Durable change feeds belong to a store change
// stream adapter, not here.
package memory
