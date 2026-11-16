# Host Access

- Bounded context: Access.
- Definition: Which Users may log in to a Server, and with which `sudo`, as swallow computes it from
  the Server's Pool placement, the Pool's memberships, the Sudo Policy, and the limits of the
  Platform the Server belongs to.
- Allowed meaning: Derived, never written by a user. Admins may log in to every Server. A Pool's
  owners and members may log in to the Pool's Servers, except where the Platform that claims the
  Server limits members by Node Role through its Platform Module (for example a Slurm Platform lets
  members log in only to login nodes and gives them no `sudo`; a Kubernetes Platform gives members
  no login on its nodes). A Server in no Pool admits only admins. swallow projects Host Access into
  the Directory, where the Servers' directory client enforces it; a change of membership,
  placement, or Platform takes effect at the Directory's next pass and the Servers' cache refresh,
  without running automation on the Servers. swallow also checks it itself before it runs anything on
  a Server as a User (a user-defined Validation Suite runs as the User who starts it): switching to
  that User from the automation account does not pass through the login check the directory client
  enforces.
- Disallowed meaning: Not the automation account's access (the Deployment Key and the Server
  Default User). Not a firewall or network rule. Not Platform membership.
- Synonyms: Login access.
- Deprecated terms: None.
- Examples: "Moving `gpu-07` from Pool `research` to Pool `prod` removes it from the research
  members' Host Access and adds it to prod's." / "`alice`, a member, can log in to the Slurm login
  node `lab-login-1` but not to the compute nodes, where she runs jobs through Slurm."
- Related terms: Pool Membership, Pool Role, Sudo Policy, Directory, Platform Module, Node Role,
  Pool, Server.
- Change note: Added 2026-11-13 ([decision 082](../../../decisions/082-accounts-pool-access-and-directory.md)
  (accounts, pool access, and directory)). Changed 2026-11-17: swallow checks it before running a
  user-defined Validation Suite as a User, because a lab probe showed `sudo -u` from the automation
  account bypasses the directory client's login check.
