# Pool Membership

- Bounded context: Access (with Infrastructure Grouping).
- Definition: The record that one User belongs to one Pool with a Pool Role, which decides what the
  User may see and do with the Pool's Servers and Platforms, and whether and how the User may log
  in and use `sudo` on them.
- Allowed meaning: Desired state written through the API by an admin or by an owner of the Pool;
  unique per Pool and User. It carries the Pool Role and the member's Sudo Policy overrides
  (`inherit | on | off` for "has sudo" and for "sudo without a password"). Adding a User to a Pool
  needs an existing User (only admins create Users). A User may be a member of one Pool and an owner
  of another. Removing the membership, or deleting the User or the Pool, ends the access; the
  Directory stops listing the User for the Pool's Servers at its next pass. Every change is
  recorded in the access audit.
- Disallowed meaning: Not a Server's tenancy (Allocation State) or the pending `Owner` relation of a
  Server; a Server belongs to a Pool through its placement, and people belong to the Pool through
  memberships. Not Platform membership (the Server's membership axis), which is what a Platform
  runtime reports about its nodes.
- Synonyms: Member (of a Pool), pool member.
- Deprecated terms: None.
- Examples: "The owner of Pool `research` adds `alice` as a `member`; she can now read the Pool's
  Servers and log in to them with her Access Key." / "Removing `alice` from `research` lets her
  existing SSH sessions finish, but new logins are refused after the Servers refresh their cache."
- Related terms: User, Pool, Pool Role, Sudo Policy, Host Access, Directory.
- Change note: Added 2026-11-13 ([decision 082](../../../decisions/082-accounts-pool-access-and-directory.md)
  (accounts, pool access, and directory)).
