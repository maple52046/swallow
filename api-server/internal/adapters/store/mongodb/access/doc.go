// Package access is the MongoDB store of swallow's access model (decision 082): Pool Memberships
// (`pool_memberships`), Pools' Sudo Policy defaults (`pool_access_policies`), the installation's
// access settings (`access_settings`), the access audit (`access_audit`), POSIX uid and gid
// allocation (`posix_ids`), and the Directory controller's status (`directory_status`).
//
// It maps documents to and from the access and auth domains; no bson tag or Mongo filter leaks out.
// Every method uses its ctx, so it joins the caller's store transaction. Access rules and the
// Directory projection belong to the core access context, not here.
package access
