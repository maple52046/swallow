# User Role

- Bounded context: Access.
- Definition: The installation-wide role of a User, which decides whether the User administers
  swallow as a whole.
- Allowed meaning: The closed set `admin | user`. `admin`: may do everything, including Sites,
  Integrations, OS Images, the Deployment Key, the Zone and Pool catalog, placing Servers in Pools,
  Users, and access settings; may log in to every Server the Directory reaches, with `sudo`. `user`:
  may sign in and manage the User's own password, API Keys, and Access Keys; everything else comes
  from the User's Pool Memberships. A Session and an API Key act with the owner's current role,
  read on every request.
- Disallowed meaning: Not a Pool Role: `owner` and `member` say what a User may do in one Pool, a
  User Role says what the User may do everywhere. Not a scope on a credential.
- Synonyms: Role (when the context is unambiguous).
- Deprecated terms: `owner` as a User Role value (it was listed but never meant anything; see Pool
  Role).
- Examples: "There must always be at least one User with role `admin`; demoting or deleting the
  last one is refused." / "A `user` with no Pool Membership sees an empty Servers page."
- Related terms: User, Pool Role, Pool Membership.
- Change note: Added 2026-11-13 ([decision 082](../../../decisions/082-accounts-pool-access-and-directory.md)
  (accounts, pool access, and directory)) to define the values the auth contracts already listed;
  `owner` moved to Pool Role.
