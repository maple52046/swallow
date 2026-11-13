# Sudo Policy

- Bounded context: Access.
- Definition: The rules that decide, per User and Server, whether the User may use `sudo` on the
  Server and whether `sudo` asks for the User's password.
- Allowed meaning: Three layers, resolved in this order, all desired state:
  - the installation's admin setting: every admin has `sudo` on every Server, without a password
    when `adminSudoPasswordless` is true;
  - a Pool's defaults: `memberSudo` (whether members have `sudo`) and `sudoPasswordless`; owners
    always have `sudo`;
  - a member's overrides in the Pool Membership: `sudo` and `sudoPasswordless`, each
    `inherit | on | off` (`inherit` takes the Pool's default; an owner's `sudo` override is
    ignored because owners always have it).
  The effective values are reported with their source (`role`, `settings`, `pool`, or `member`). A
  Platform may cap a member's `sudo` on its nodes through its Host Access limits (for example no
  member `sudo` on any Slurm node). swallow realizes the result as `sudo` rules in the Directory;
  a password prompt checks the User's swallow password.
- Disallowed meaning: Not the automation account's `sudo`, which comes from the OS Image and the
  Site's become password. Not a file swallow writes under `/etc/sudoers.d`.
- Synonyms: sudo settings.
- Deprecated terms: None.
- Examples: "Pool `research` gives members no `sudo`; the owner turns `sudo` on for `carol` alone,
  with a password." / "With `adminSudoPasswordless` off, an admin's `sudo` asks for the admin's
  swallow password."
- Related terms: Pool Membership, Pool Role, User Role, Host Access, Directory, Server Default User.
- Change note: Added 2026-11-13 ([decision 082](../../../decisions/082-accounts-pool-access-and-directory.md)
  (accounts, pool access, and directory)).
