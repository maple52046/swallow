# Directory

- Bounded context: Access (Core and Extensions: a Provider Interface).
- Definition: The account directory the managed Servers read their users from: swallow projects its
  Users, their POSIX identities and Access Keys, its groups, Host Access, and Sudo Policy into it,
  and each Server's directory client resolves logins, keys, and `sudo` against it.
- Allowed meaning: A Provider Interface of the core, installation-wide (one for every Site). Its
  current adapter is a bundled LDAP server that runs next to the control plane, reached over TLS
  only, and that swallow writes alone: everything in it is derived from swallow's store and can be
  rebuilt from it, so the Directory controller replaces whatever differs at every pass. The
  Directory has a status (conditions, last applied time, counts) but no spec of its own. The
  Servers' directory client is the Managed Software kind `sssd`, which swallow installs by policy on
  every deployed Server while a Directory is configured. A later adapter may read an existing
  directory instead (the organization's own LDAP or Active Directory).
- Disallowed meaning: Not swallow's store of Users (the store is the source of truth; the Directory
  is a realization target). Not the authentication of the swallow API: signing in checks the
  password in swallow's store and never depends on the Directory. Not an Integration of a Site.
- Synonyms: Account directory.
- Deprecated terms: None.
- Examples: "An admin deletes `bob`; at the Directory controller's next pass his entry is removed
  and the Servers refuse his logins once their cache refreshes." / "The Directory reports
  `realized` false with reason `provider_unreachable` while the LDAP server is down; Servers keep
  admitting users they have cached."
- Related terms: User, Host Access, Sudo Policy, SSH Key, Pool Membership, Provider Interface,
  Provider Adapter, Managed Software, Controller.
- Change note: Added 2026-11-13 ([decision 082](../../../decisions/082-accounts-pool-access-and-directory.md)
  (accounts, pool access, and directory)).
