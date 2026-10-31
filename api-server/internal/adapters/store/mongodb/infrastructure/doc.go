// Package infrastructure is the MongoDB store of swallow-owned Zones (`zones`) and Pools (`pools`):
// their spec (name and description, with a generation) and the status the catalog controller
// writes about their realization on each provisioner.
//
// It maps documents to and from the infrastructure domain; no bson tag or Mongo filter leaks out.
// Grouping rules and how the spec is realized in a provisioner belong to the core infrastructure
// context, not here.
package infrastructure
