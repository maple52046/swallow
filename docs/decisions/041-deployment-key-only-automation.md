# 041. Automation uses only the Deployment Key

- Status: Accepted
- Date: 2026-10-02

## Context

[ADR 039](039-ssh-key-management-and-default-user.md) introduced the Deployment Key and kept
the Site Automation Configuration's private key as an optional override, for two kinds of
hosts that never received the Deployment Key: Servers deployed before ADR 039, and hosts
swallow manages but did not deploy.

swallow is still in development. A Server swallow deployed can be released and redeployed,
which puts the Deployment Key on it, and a host swallow does not manage is out of scope. The
override therefore protects no one, while it keeps a second key source alive in the domain.
It also left a defect behind: Ansible acceptance and the Ansible Task still required a
stored Site credential record, so a Site that relied on the Deployment Key alone was
refused unless it had stored at least a become password.

## Decision

**Every automation SSH login uses the installation's Deployment Key.** This covers SSH
readiness (`wait-for-ssh`), the per-host login-user probe, and every Ansible run.

- The Site credential holds only the write-only become password. A credential request that
  contains `sshPrivateKey` is refused with `400 validation_error`, not silently ignored, so
  no caller believes an override took effect. To use another key installation-wide, replace
  the Deployment Key (`PUT /ssh-keys/deployment`).
- A private key stored by an earlier release is ignored on read and dropped on the next
  credential write.
- `credentialSource` reports only `deploymentKey` or `none`.
- Ansible may run for a Site when automation is enabled and the Deployment Key exists. That
  single rule replaces the stored-credential checks at acceptance and in the Ansible Task.

## Alternatives considered

- **Keep the override and only fix the acceptance check:** rejected — it keeps a key source
  nobody needs and two paths to test, against hosts that can simply be redeployed.
- **Accept and ignore `sshPrivateKey` for one release:** rejected — a request that appears to
  succeed while changing nothing misleads the caller.

## Consequences

- One key path for every login; the Site credential no longer carries key material.
- Breaking for any client that set a Site private key; such a Site's existing hosts must be
  redeployed (or their Deployment Key authorized) before automation can log in.
- Supersedes the "Effective credential" override of ADR 039; the rest of ADR 039 stands.

## Current status

Implemented in `api-server` (operation domain, application, delivery, and the automation
configuration repository), the site-automation contract, the CLI help, and the installation
and development seeds.
