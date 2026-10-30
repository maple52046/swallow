# 045. Server Default User for existing Servers, and the docker group

- Status: Accepted
- Date: 2026-10-02
- 現行規格：[access-and-credentials.md](../development/access-and-credentials.md) §7 Automation 登入帳號解析、§8 Server Default User 設定

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
default user. The effective value is resolved in one place: the Server's value, else the deployed
OS Image's default user, else unknown (the existing fallback probe). Every automation login
(wait-for-ssh and Ansible) uses the effective value. The Server's value belongs to one OS
installation: it is cleared when swallow starts a new OS deployment on the Server and when the
Server is observed with no OS.

**Verified before saved, key installed with a one-time password.** Setting the value takes the
account and, optionally, its password. With a password, api-server logs in once over SSH and adds
the current Deployment Key public key to that account's `authorized_keys`. In every case it then
proves a Deployment Key login as the account and checks whether sudo works without a password, and
only then stores the account. The password is used for that single login and never stored or
logged. Operators who prefer not to type a password add the key themselves. Host keys are not
verified on these logins, like the existing login-user probe; the authoritative host-key check
stays with the Ansible run.

**Docker CE grants `docker` to the login user.** Docker CE adds the effective Server Default User
to the `docker` group unless it is `root`. Swallow-deployed and existing Servers are handled by the
same rule.

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
  Key is imported in the same step, with no password typed into swallow. Deferred: swallow had no
  enrollment step for existing hosts when this was decided (Servers arrived only through
  provisioner inventory). [ADR 053](053-server-enrollment-and-automatic-inspection.md) later added
  that step (`swallow servers enroll`); capturing the Server Default User during it remains
  deferred. When it is built, it sets the same Server Default User.

## Consequences

- Existing Servers become automatable (software and platform deploys) once their default user is
  set; the account still needs sudo, passwordless or through the Site become password.
- A password travels to the host over SSH once; with host keys unverified, a host impersonating
  the address could capture it — acceptable only under the internal-network assumption above.
