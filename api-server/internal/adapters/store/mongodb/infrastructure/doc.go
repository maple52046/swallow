// Package infrastructure is the MongoDB store of swallow-owned Zones (`zones`) and Pools (`pools`).
// They are swallow's own intent, not provider mirrors, so nothing here goes stale.
//
// It maps documents to and from the infrastructure domain; no bson tag or Mongo filter leaks out.
// Grouping rules and how the intent is realized in a provisioner belong to the core
// infrastructure context and its composition adapters, not here.
package infrastructure
