# 039. SSH key management, Deployment Key, and OS Image default user

- Status: Accepted
- Date: 2026-10-01

> The Site private-key override in "Effective credential" below is superseded by
> [ADR 041](041-deployment-key-only-automation.md): automation now uses only the Deployment Key.

## Context

swallow logs in to the Servers it deploys for SSH readiness (`wait-for-ssh`) and Ansible
execution. Until now the only key material was a write-only private key stored per Site in
the Automation Configuration, and swallow never knew its public key. Getting that public key
onto a freshly deployed host was left to the operator (a Deployment Template's cloud-init or
keys added by hand in the provisioner), and a fresh installation could not deploy a Platform
until someone wired a key into every Site.

The login user had the same shape of problem: each OS Image family ships a different default
user (`ubuntu`, `cloud-user`, …) while a Site has one `sshUser`, so swallow probed a candidate
list per host.

swallow has no multi-user model yet (only the bootstrap admin), but one is planned, so the
key model must not assume a single human.

## Decision

**Two key purposes, one private key.** swallow owns an `SSH Key` record per public key with
purpose `deployment` or `access`:

- The **Deployment Key** is the single system-owned key pair. The installation creates it as
  ed25519 with the idempotent one-shot command `swallow-api deployment-key ensure`, which
  `swallowctl install` and `upgrade` (Compose and native) run right after `migrate` and the
  dev/testing seeds run post-install. swallow holds its private key sealed with the existing
  credential key and never returns it. It can be regenerated or replaced with an uploaded
  unencrypted private key, never deleted. A partial unique index keeps it single even when the
  command runs concurrently.
- **API startup is independent of the key**: the API process neither creates nor requires it.
  Only OS deployment (including image verification) and Platform deploy require it, and they
  reject at acceptance with `409 conflict` while it is missing.
- **Access Keys** belong to a User (today the admin) and are public-key only. Generating one
  returns the private key exactly once; importing one accepts a public key only.

**Effective credential.** A Site Automation Configuration's private key, when set, overrides
the Deployment Key for that Site; otherwise automation uses the Deployment Key. The become
password stays site-scoped. Existing Sites keep working unchanged.

**Provisioner realization is capability-first** ([ADR 031](031-provider-capability-first-with-swallow-owned-fallback.md)).
A provisioner that can hold SSH keys advertises `SSHKeyRegistration` and implements the
optional `SSHKeyRegistrar` interface; MAAS does so through the API-key owner's
`/account/prefs/sshkeys/`, and MAAS injects those keys into the image's default user at deploy.
swallow lists first, adopts identical key material, adds what is missing, and removes only the
provider keys it recorded; keys an operator added in the provisioner are never touched. Sync runs
immediately after a key change, periodically in the API process, and — so a Server is never
deployed without the Deployment Key — as a pre-deploy ensure that fails the provision Task
(retryable `ssh_key_registration_failed`) when a capable provisioner cannot be synced. A
non-capable provisioner reports sync state `unsupported`.

**OS Image default user** is a swallow-owned field on the existing OS Image Provider Data
Overlay ([ADR 025](025-provider-data-overlay.md)), settable at upload or later. The effective
value is the overlay value, else a built-in derived from the provider OS family
(`ubuntu`→`ubuntu`, `centos`→`centos`, `rhel`→`cloud-user`), else none. The deployed-image
resolver that already mirrors `deployedImageName` also mirrors the effective default user onto
each Server, and automation uses it **exclusively** as the login user; only when a Server has
none does swallow fall back to the previous `[site sshUser, cloud-user, ubuntu]` probe.

## Alternatives considered

- **Any key may carry a stored private key and one is marked default:** rejected — it widens
  private-key custody from one key to many, mixes personal and machine identity (a departing
  user's key could be the automation credential), and makes swallow a personal key vault.
- **Only a Deployment Key, no Access Keys:** rejected — operators also need their own keys on
  deployed hosts, and those keys must map cleanly to the future multi-user model.
- **Generate the key in `prepare-secrets.sh` with `ssh-keygen`:** rejected — it adds a host
  dependency, differs across compose/native/dev/testing, and still needs an import path into the
  database; a `swallow-api` command shares the API's configuration and sealing on every path.
- **Generate the key on API start (`bootstrap`, the first implementation):** replaced at the
  user's direction — it couples service startup to installation state and hides a missing key
  until deployment; installation is the place that creates durable installation state, and the
  deployment paths are the ones that need the key.
- **Remove the Site private key and `sshUser`:** rejected — breaking for existing Sites; the
  override keeps them working while new installations need neither.
- **Keep probing when the image default user is known:** rejected — a known user should be
  deterministic so a wrong value fails with a clear message instead of silently logging in as
  another account.
- **Inject keys through cloud-init for non-capable provisioners now:** deferred — MAAS, the only
  provisioner today, is capable; merging into operator user-data needs its own design.

## Consequences

- New `api-server` feature slice `sshkey` and HTTP contract `ssh-keys.md`; `provisioning.md`,
  `site-automation.md`, and the Server contract gain `defaultUser` / `credentialSource` /
  `deployedImageDefaultUser`.
- Fresh installations can deploy with no Site credential once automation is enabled; an
  installation that skipped the step is told, at deploy acceptance, which command to run.
- The Deployment Key's private key lives only in MongoDB, sealed with `credentialKey`; a restore
  needs both.
- MAAS injects keys only at deploy time: regenerating the Deployment Key or deleting an Access
  Key does not change already-deployed Servers. Pushing keys to deployed hosts is future work.

## Current status

Implemented.
