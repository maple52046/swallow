# 039. Automation logs in with swallow's Deployment Key as the Server's default user

- Status: Accepted
- Date: 2026-10-01
- 現行規格：[access-and-credentials.md](../development/access-and-credentials.md) §3 Deployment Key、§4 Access Key、§5 Provisioner realization、§6 Automation 的 SSH 金鑰與 Site automation credential、§7 Automation 登入帳號解析、§8 Server Default User 設定

## Context

swallow logs in to the Servers it manages for SSH readiness (`wait-for-ssh`) and Ansible. That
needs a key the host trusts and an account that exists on it. The only key material used to be a
write-only private key stored per Site, whose public key swallow never knew: getting it onto a
fresh host was left to the user, and a new installation could not deploy a Platform until someone
wired a key into every Site. Each OS Image family ships a different default user (`ubuntu`,
`cloud-user`, …) while a Site has one `sshUser`, so swallow had to probe candidates per host.

Servers whose OS was installed outside swallow (from the provisioner's inventory or enrolled with
their existing OS) never received swallow's key, and the image-derived user may not be their real
account. Docker CE also needs an account to grant the `docker` group to, and that is the account
automation logs in as.

swallow has no multi-user model yet (only the bootstrap admin), but one is planned, so the key model
must not assume a single human. The user ruled that security is not a concern for this version:
every deployment target is an internal datacenter host.

## Decision

- **swallow owns SSH Keys: two purposes, one private key.**
  - The **Deployment Key** is the single system-owned key pair. The installation creates it with
    the idempotent command `swallow-api deployment-key ensure`; the API process neither creates nor
    needs it to start. Its private key is sealed with the installation's credential key and never
    returned by any API. It can be replaced or regenerated, never deleted. Deploying an OS and every
    action that logs in to hosts require it and are refused at acceptance while it is missing.
  - **Access Keys** belong to a User (today the admin) and are public-key only.
- **The Deployment Key is the only automation key.** Every automation SSH login uses it: SSH
  readiness, the login-user probe, every Ansible run, and the Server Default User verification. The
  Site credential holds only the write-only become password; a request carrying `sshPrivateKey` is
  refused (`400 validation_error`) rather than ignored, and a key stored by an earlier release is
  ignored. To use another key installation-wide, replace the Deployment Key. Ansible may run for a
  Site when its automation is enabled and the Deployment Key exists.
- **Keys reach hosts through the provisioner.** SSH Keys are desired state
  ([decision 069](069-swallow-owns-desired-state.md) (swallow owns desired state),
  [decision 071](071-provisioner-realized-desired-state.md) (provisioner-realized desired state)).
  The SSH Key controller registers every key's public key on each provisioner that can hold SSH
  keys, and the provisioner writes them into the image's default user at deploy. swallow adopts
  identical key material, adds what is missing, puts back keys deleted in the provisioner, and
  removes only keys it registered or adopted; keys a person added in the provisioner are never
  touched. Every OS deployment first ensures the Deployment Key on its provisioner. A provisioner
  without the capability reports the keys as not realized.
- **The login user is resolved per Server, in one place.** The effective Server Default User is the
  value set on the Server; else the default user of the OS Image it was deployed with (the
  `defaultUser` of the image's metadata, which is swallow's desired state, else a built-in derived
  from the provider's OS family); else unknown. A known value is used exclusively; only an unknown
  one falls back to probing candidate users with the Deployment Key.
- **A Server Default User is verified before it is saved.** Any deployed Server may set one. With an
  optional one-time password, api-server logs in once and adds the current Deployment Key public key
  to that account's `authorized_keys`. In every case it then proves a Deployment Key login as the
  account, reports whether sudo works without a password, and stores the account only after that
  login succeeds. The password is used for that login only, never stored, logged, or used for
  become. These logins, like the probe, do not verify host keys; the authoritative host-key check
  stays with Ansible. The value belongs to one OS installation: it is cleared when swallow starts a
  new OS deployment on the Server and when the Server is observed with no OS.
- **Docker CE grants `docker` to the login user.** It adds the effective Server Default User to the
  `docker` group unless that user is `root`, for swallow-deployed and existing Servers alike.

## Alternatives considered

- **Any key may carry a stored private key and one is marked default:** rejected — it widens
  private-key custody to many keys, mixes personal and machine identity (a departing user's key
  could become the automation credential), and makes swallow a personal key vault.
- **Only a Deployment Key, no Access Keys:** rejected — users need their own keys on deployed hosts,
  and those keys must map cleanly to the future multi-user model.
- **Create the Deployment Key with `ssh-keygen` in `deploy/production/prepare-secrets.sh`, or at API
  start (the first implementation):** rejected — the script adds a host dependency, differs across
  installation paths, and still needs an import path; creation at API start was replaced at the
  user's direction because it couples startup to installation state and hides a missing key until
  deployment.
- **Keep the Site private-key override, fixing only its acceptance check:** rejected — it served
  only Servers deployed before the Deployment Key, which can be redeployed, and hosts swallow did not
  deploy, which are out of scope; it kept a second key source and test path for no one.
- **Accept and ignore `sshPrivateKey` for one release:** rejected — a request that appears to
  succeed while changing nothing misleads the caller.
- **Keep probing when the default user is known:** rejected — a known user must be deterministic, so
  a wrong value fails with a clear message instead of silently logging in as another account.
- **Install the key only by hand, with no password:** rejected as the primary path — every existing
  Server would need a shell session first; kept as the fallback.
- **Store the password, or use it for become:** rejected — swallow would hold a per-host login
  secret; the Site become password already covers sudo, and the key login replaces the password.
- **Install the key through an Ansible run:** rejected — Ansible needs a working login first; one
  SSH login from api-server is the bootstrap.
- **Only existing Servers may set a Server Default User:** rejected by the user; any deployed Server
  may override its image default.
- **Deferred:** injecting keys through cloud-init for provisioners without the SSH key capability
  (MAAS is capable; merging into user-data needs its own design), and capturing the Server Default
  User and importing the Deployment Key during existing-OS enrollment
  ([decision 053](053-server-enrollment-and-automatic-inspection.md) (Server Enrollment and automatic
  inspection)) without a password; when built, it sets the same Server Default User.

## Consequences

- A fresh installation can deploy with no Site credential once automation is enabled; one that
  skipped the installation step is told at acceptance which command to run.
- One key path for every login. A host without the Deployment Key must be redeployed, or have its
  Server Default User set with a password, before automation can log in.
- The Deployment Key's private key lives only in MongoDB, sealed with the credential key; a restore
  needs both.
- The provisioner injects keys only at deploy: regenerating the Deployment Key or deleting an Access
  Key does not change deployed Servers, and pushing keys to deployed hosts is future work.
- Existing Servers become automatable once their default user is set; the account still needs sudo,
  passwordless or through the Site become password.
- The one-time password travels to the host with host keys unverified, so a host impersonating the
  address could capture it; this is acceptable only under the internal-network assumption and must
  be revisited if it changes.

## Revision history

- 2026-10-01: decision 039 made swallow the owner of SSH Keys (one Deployment Key and per-User Access Keys) realized on the provisioner, let a Site private key override the Deployment Key, and made the OS Image default user the login user.
- 2026-10-02: decision 039 removed the Site private-key override, so every automation login uses the Deployment Key and the Site credential holds only the become password.
- 2026-10-02: decision 039 added the Server Default User, which takes precedence over the OS Image default user, is verified with a Deployment Key login before it is saved, and is granted the `docker` group by Docker CE.
- 2026-11-02: merged decision 039 into this ADR; the body states the current decision.
