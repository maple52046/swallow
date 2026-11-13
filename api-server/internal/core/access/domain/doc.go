// Package domain defines swallow's access model (decision 082; docs/development/identity-and-access.md):
// Pool Memberships and their Pool Roles, the Sudo Policy (admin setting, Pool defaults, member
// overrides) and how it resolves, the caller's Scope that authorization checks against, the access
// audit, and the Directory provider interface with the desired directory content swallow projects
// into it.
//
// The User itself belongs to the auth context (internal/core/auth/domain); this package refers to
// Users by id and username. It is part of the core: it must not name a directory product or a host
// directory client, and must not import MongoDB, Fiber, or an adapter. The Directory's provider
// adapter translates its users, groups, and sudo rules into the product's entries.
package domain
