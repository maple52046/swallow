// Package localrepo is the MongoDB store of the Local Repository context: one document per Site in
// `local_repositories`, keyed by the Site id, holding the user's spec (written by the API) and the
// status the Local Repository controller observed (written by the worker).
//
// It maps documents to and from the localrepo domain types; the merge of the spec with the modules'
// declarations, and every rule about what the status means, belong to the core localrepo context.
package localrepo
