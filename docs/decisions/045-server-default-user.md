# 045. Server Default User for existing Servers, and the docker group

- Status: Accepted
- Date: 2026-10-02

## Context

Since [decision 039](039-ssh-key-management-and-default-user.md) automation logs in to a Server as
the default user of the OS Image the Server was deployed with, using the Deployment Key, which the
provisioner injects at deploy time. That covers Servers swallow deployed. It does not cover a
Server whose OS was installed outside swallow (an existing Server that reached swallow through the
provisioner's inventory): the image-derived default user may not be the real account, and the
Deployment Key was never installed on the host, so wait-for-ssh and Ansible cannot log in at all.

Docker CE also needs an account to grant the `docker` group to, so operators can run Docker
without sudo; that account is the same one automation logs in as.

The user ruled that security is not a concern for this version: every deployment target is an
internal datacenter host.

## Decision

**A Server Default User, set on the Server.** Any deployed Server may carry an operator-set
default user. The effective value is resolved in one place: the Server's value (source `server`),
else the deployed OS Image's default user (source `os_image`), else unknown (the existing fallback
probe). Wait-for-ssh, the dynamic inventory (`default_user`), and the runner's `ansible_user` all
use the effective value. The Server's value belongs to one OS installation: it is cleared when
swallow starts a new OS deployment on the Server and when the Server is observed with no OS
(`ready`, `allocated`).

**Verified before saved, key installed with a one-time password.** `PUT
/api/v1/servers/{id}/default-user` takes the account and, optionally, its password. With a
password, api-server logs in once over SSH (password or keyboard-interactive) and appends the
current Deployment Key public key to that account's `authorized_keys` idempotently. In every case
it then proves a Deployment Key login as the account and checks whether sudo works without a
password, and only then stores the account. The password is used for that single login and never
stored or logged. Operators who prefer not to type a password run a shown one-line command that
adds the key themselves. Host keys are not verified on these logins, like the existing login-user
probe; the authoritative host-key check stays with the Ansible run.

**Docker CE grants `docker` to the login user.** The `docker_ce` role adds `ansible_user` — the
effective Server Default User — to the `docker` group unless it is `root`. Swallow-deployed and
existing Servers are handled by the same rule; a re-apply of Docker CE adds the account on hosts
installed before this change.

## Alternatives considered

- **Only a manual command, no password:** rejected as the primary path — every existing Server
  would need a shell session first; kept as the fallback.
- **Store the password (or use it for become):** rejected — swallow would hold a per-host login
  secret; the Site become password already covers sudo, and the key login replaces the password.
- **Install the key through an Ansible run:** rejected — Ansible needs a working login first; a
  single SSH login from api-server is the bootstrap.
- **Only existing Servers may set it:** rejected by the user; any deployed Server may override its
  image default.
- **Capture the default user at commissioning:** the ideal flow — the account that runs the
  enrollment/commissioning step on an existing host becomes its default user and the Deployment
  Key is imported in the same step, with no password typed into swallow. Deferred: swallow has no
  enrollment step for existing hosts today (Servers arrive only through provisioner inventory).
  When one is built, it sets the same Server Default User.

  > Update 2026-10-06: [ADR 053](053-server-enrollment-and-automatic-inspection.md) added the
  > existing-host enrollment step (`swallow servers enroll`). Capturing the Server Default User
  > during it remains deferred.

## Consequences

- New Server field `defaultUser` (Mongo, swallow-owned, not touched by reconcile), new endpoints
  `PUT`/`DELETE /api/v1/servers/{id}/default-user`, and `defaultUser {user, source}` on the Server
  projection; contracts `server-detail-actions.md` and `servers-list.md`.
- The inventory gains `default_user` (effective); `image_default_user` stays as the image value.
- Existing Servers become automatable (software and platform deploys) once their default user is
  set; the account still needs sudo, passwordless or through the Site become password.
- A password travels to the host over SSH once; with host keys unverified, a host impersonating
  the address could capture it — acceptable only under the internal-network assumption above.

## Current status

Implemented.
