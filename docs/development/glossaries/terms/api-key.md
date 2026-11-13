# API Key

- Bounded context: Access.
- Definition: A named, long-lived secret that a swallow User creates so a non-interactive
  client (the `swallow` CLI, a script, CI) can call the swallow API as that User without a
  password login.
- Allowed meaning: Owned by exactly one User and acting with that User's current User Role and
  Pool Memberships, read on every request. swallow shows the secret exactly once, at creation, and afterwards keeps only
  a one-way hash and a short display prefix, so a lost secret is replaced, never recovered. It
  may carry an expiry; deleting it revokes it immediately. swallow records when it was last
  used. A request authenticated by an API Key cannot create API Keys.
- Disallowed meaning: Not an SSH Key or an Access Key — those are SSH public keys authorized on
  deployed Servers, while an API Key authenticates to the swallow API. Not a Session — a Session
  comes from a password login and expires on its own, while an API Key stays valid until it
  expires or is deleted. Not the installation's machine token, which belongs to no User and only
  opens discovery and metrics. Not a scoped credential: it cannot hold less than its owner's
  role (scopes are future work).
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The admin creates an API Key named `ci-runner` with a 90-day expiry and stores the
  `swk_…` secret in the pipeline's secret store." / "Deleting the `laptop` API Key makes the
  next CLI call with it fail with `401`." / "An API Key cannot be used to create another API
  Key; that needs a password login."
- Related terms: User, User Role, Pool Membership, Session, SSH Key.
- Change note: Added for CLI authentication by API key
  ([decision 042](../../../decisions/042-authentication-sessions-and-api-keys.md) (authentication sessions and api keys)). Ownership is
  by User so keys survive the planned multi-user work. On 2026-11-13
  ([decision 082](../../../decisions/082-accounts-pool-access-and-directory.md) (accounts, pool
  access, and directory)) any User can hold API Keys, which act with the owner's role and Pool
  Memberships.
