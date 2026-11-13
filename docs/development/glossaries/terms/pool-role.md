# Pool Role

- Bounded context: Access.
- Definition: What a Pool Membership lets its User do with the Pool's resources.
- Allowed meaning: The closed set `owner | member`. `owner`: operates the Pool's Servers (power,
  OS deployment, release, recovery, rescue, tags, Server Default User, Managed Software,
  Validation), deploys and changes Platforms whose every node is in the Pool and approves their
  Change Plans, manages the Pool's memberships and Sudo Policy, logs in to the Pool's Servers, and
  always has `sudo` there. `member`: reads the Pool's Servers, Platforms, Workflows, and metrics,
  logs in where the Pool's Host Access allows, and has `sudo` only as the Sudo Policy and the
  Platform's limits allow. An admin (User Role) can do everything an owner can in every Pool
  without a membership.
- Disallowed meaning: Not a User Role. Not the pending `Owner` of a Server in the Tenancy context.
  Not a Node Role (a Server's part in a Platform).
- Synonyms: None. The dashboard labels are "Owner" and "Member".
- Deprecated terms: `user` as the name of the reading role of a Pool (it collided with User and the
  User Role value `user`).
- Examples: "`bob` is an `owner` of Pool `research`, so he may deploy a Slurm Platform on its
  Servers." / "A `member` of `research` can read the Slurm Platform and submit jobs on its login
  node, but cannot log in to its controllers."
- Related terms: Pool Membership, User Role, Pool, Sudo Policy, Host Access.
- Change note: Added 2026-11-13 ([decision 082](../../../decisions/082-accounts-pool-access-and-directory.md)
  (accounts, pool access, and directory)).
