# User

- Bounded context: Access. `api-server`, `dashboard`, and `cli` use this term with the same meaning.
- Definition: A person's swallow account: the identity that signs in to the API, owns Sessions,
  API Keys, and Access Keys, holds one installation-wide User Role, and belongs to Pools through
  Pool Memberships. The same account is the person's login on the managed Servers swallow's
  Directory reaches.
- Allowed meaning: Identified by a swallow-issued id and by a username that is also the person's
  Linux login name, so it follows the POSIX login-name rule (lowercase letters, digits, `_` and
  `-`, at most 32 characters, starting with a letter or `_`) and may not be a reserved name (`root`,
  system accounts, the automation accounts of the supported OS Images such as `ubuntu` and
  `cloud-user`, and the service accounts swallow deploys such as `slurm` and `munge`). Only an
  admin creates, changes, or deletes Users; the bootstrap admin is created by the installation.
  Every User has a POSIX identity that swallow allocates once and never reuses: a `uidNumber` from
  the installation's POSIX ID range (starting at the installation setting, default 10000) and the
  shared primary group `swallow-users`. The password is kept only as a one-way hash, which swallow
  verifies itself at sign-in and also projects into the Directory for `sudo` on the Servers;
  Servers accept only SSH keys for login. Deleting a User removes its Sessions, API Keys, Access
  Keys, and Pool Memberships; its `uidNumber` stays allocated.
- Disallowed meaning: Not the Server Default User or any other automation account, which stays a
  local account on each Server and never enters the Directory. Not a Team, and not the pending
  `Owner` of a Server (the ownership relation in the Tenancy context): a User's power over a Pool
  comes from a Pool Membership with a Pool Role. Not a Session or a credential.
- Synonyms: Account.
- Deprecated terms: The global role value `owner` (a User Role is `admin` or `user`; `owner` is now
  a Pool Role).
- Examples: "The admin creates the User `alice` with role `user`; swallow allocates uid 10003 and
  she can sign in, but sees nothing until an owner adds her to a Pool." / "Deleting `bob` signs him
  out everywhere, removes his Access Keys from the Directory, and leaves uid 10002 allocated so no
  later account reuses his files."
- Related terms: User Role, Pool Membership, Pool Role, Session, API Key, SSH Key, Directory,
  Host Access, Server Default User.
- Change note: Added 2026-11-13 for multi-user accounts and Pool-scoped access ([decision 082](../../../decisions/082-accounts-pool-access-and-directory.md)
  (accounts, pool access, and directory)), promoting the pending term `User`. The earlier usage
  listed three global roles `admin | owner | user` without defining them; the resolution keeps
  `admin` and `user` as User Roles and makes `owner` a Pool Role, because owning is always owning
  a Pool's resources.
