// Package application implements swallow's access use cases (decision 082;
// docs/development/identity-and-access.md): the Authorizer that turns a caller into a Scope and
// checks it against Servers, Platforms, and Workflows; admin management of Users; Pool Memberships,
// Pool Sudo Policy, access settings, and the audit log; and the Directory projection and the
// controller that realizes it through the Directory provider interface.
//
// It depends only on domain ports and the store's Transactor, so it runs without MongoDB, HTTP, or
// a live directory. Delivery parses requests and maps errors; this package owns the rules.
package application
