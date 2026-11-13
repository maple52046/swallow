# 082. Accounts, Pool-scoped access, and a swallow-written Directory for host logins

- Status: Accepted
- Date: 2026-11-13
- 現行規格：[identity-and-access.md](../development/identity-and-access.md)；
  [access-and-credentials.md](../development/access-and-credentials.md) §4、§5、§9；
  [core-and-extensions.md](../development/core-and-extensions.md) §3.1

## Context

swallow had one account, the bootstrap admin, and every API route required role `admin`. The auth
contracts listed three roles (`admin | owner | user`) that nothing defined or enforced. Sessions,
API Keys, and Access Keys were already owned by a User so they would survive a multi-user model.

Teams share a datacenter: an admin runs swallow, groups of people own sets of Servers (and the
Platforms on them), and others only use those resources: they read them and log in to them, and on
Slurm they submit jobs. Swallow already groups Servers into Pools, swallow-owned desired state
realized into the provisioner ([decision 071](071-provisioner-realized-desired-state.md)
(provisioner realized desired state)).

On the Servers themselves there was no notion of people: automation logs in as the image's default
account with the Deployment Key ([decision 039](039-ssh-key-management-and-default-user.md) (ssh key
management and default user)); the admin's Access Keys were injected into that same account by the
provisioner at deployment time, so anyone given an Access Key got the automation account and its
`sudo`; and keys could not reach a Server after it was deployed. Slurm explicitly left consistent
UIDs and GIDs across nodes to the user
([decision 024](024-slurm-login-and-workload-storage.md) (slurm login and workload storage)).

## Decision

**Roles.** A User has one installation-wide User Role, `admin` or `user`. Power over resources
comes from a Pool Membership: a User is an `owner` or a `member` of a Pool. Admins may do
everything; owners operate the Pool's Servers, deploy and change Platforms whose every node is in
the Pool, approve their Change Plans, and manage the Pool's members and Sudo Policy; members read
the Pool's resources and log in. A Server in no Pool belongs to admins alone, and a Platform belongs
to the Pool that holds every one of its nodes (otherwise only admins manage it). Only admins create
Users; owners add existing Users to their Pool. The API enforces this on every route, filters every
list to what the caller may read, and records every access change in an audit log.

**A User is a Linux account.** The username is the person's login name on the managed Servers.
swallow allocates each User a `uidNumber` once, from an installation-wide range whose start is an
installation setting (default 10000) and cannot change after the first allocation; numbers are
never reused. All Users share the primary group `swallow-users`; Pools add supplementary groups.
Automation accounts stay local to each Server.

**swallow writes a Directory; Servers read it.** The Directory is a new core Provider Interface,
installation-wide. Its current adapter is an LDAP server bundled with the installation, reached
over TLS only, that swallow writes alone: a Directory controller in the worker projects Users (with
POSIX identity, password hash, and Access Keys), groups, Host Access, and `sudo` rules into it and
replaces whatever differs, so it can always be rebuilt from swallow's store. Signing in to swallow
checks the password in swallow's store and never depends on the Directory. Each Server runs SSSD as
its directory client, a Managed Software kind that swallow installs by policy on every deployed
Server while a Directory is configured.

**Host logins.** Servers accept only SSH keys from Users. sshd asks the directory client for the
User's keys (`AuthorizedKeysCommand`), so key changes reach deployed Servers without automation. Who
may log in where (Host Access) and who has `sudo` (Sudo Policy: admin setting, Pool defaults, member
overrides, Platform limits) are computed by swallow and written into the Directory; the Servers
enforce them and need no reconfiguration when memberships, placement, or Platforms change. A Platform
Module may limit members by Node Role (Slurm: members log in only to login nodes and never get
`sudo`). Password-protected `sudo` checks the User's swallow password through the directory client.
The provisioner receives only the Deployment Key and admins' Access Keys, the admins' way in when the
Directory is unreachable.

## Alternatives considered

- **Three global roles (`admin`, `owner`, `user`):** rejected — owning is always owning some
  resources; a global owner role cannot say whose Servers someone owns, and one person may own one
  Pool and only use another.
- **A separate tenancy object (team, project) instead of the Pool:** rejected for now — the Pool
  already partitions Servers, is swallow-owned, and is realized in the provisioner; a second grouping
  would have to be kept consistent with it.
- **Push `authorized_keys` and `/etc/sudoers.d` files with Ansible:** rejected — every membership,
  key, or placement change would become a Workflow on every affected Server, offline Servers would
  miss changes, and a malformed sudoers file breaks `sudo` on the host. Reading a directory keeps the
  Servers' configuration static.
- **Integrate the organization's directory first (read-only LDAP or Active Directory):** deferred —
  such directories do not let swallow write keys, Host Access, or `sudo` rules, and many sites have
  none; the Directory is a Provider Interface so a read-only adapter can come later.
- **FreeIPA:** rejected — it brings host-based access control, `sudo`, and keys, but wants to own DNS
  and Kerberos, enrolls every host, and is heavy to run in the single-VM installation.
- **Keep passwords out of the Directory:** rejected — owners asked for password-protected `sudo`,
  which the Server can only check against the directory; swallow's own sign-in still uses its store.
- **Start uidNumbers at 1001:** rejected as the default — local accounts on Servers imported with
  their OS, accounts owners create with `sudo`, and container images that run as 1000 or 1001 would
  collide; the start is an installation setting and the directory client refuses a Server whose
  local accounts collide.
- **Per-user private groups:** rejected — a User named `admin` would get a group `admin`, which
  Ubuntu's default sudoers grants `sudo`; a shared primary group avoids privileged group names.

## Consequences

- Every API contract states its authorization in terms of User Role and Pool Role; lists are
  filtered; a Platform reports the Pool it belongs to.
- New store records: Pool Memberships, Pool access policies, access settings, the access audit, POSIX
  group allocations, and the Directory status; Users gain a `uidNumber`.
- The installation runs an LDAP server, generates an installation CA for it, and publishes its TLS
  port to the Servers; backup covers the CA.
- Servers deployed before a Directory existed get the directory client from the policy once a
  Directory is configured; a Server whose local accounts collide with the Directory's identities
  reports the conflict instead of being changed.
- Slurm gets consistent identities on every node from the Directory; shared home directories and
  workload storage remain a separate decision.

## Revision history

- 2026-11-13: accepted.
