# Automation Configuration

An **Automation Configuration** is the single site-scoped configuration that enables
Swallow's embedded Ansible executor. It owns the SSH user and port, verified known-hosts,
and the mapping from each operation kind to a playbook name registered in the release
manifest.

The SSH private key and optional become password are write-only credentials. They are
encrypted at rest and are represented to API consumers only by `hasCredential`.

The site SSH private key is an optional override. When it is absent, automation uses the
installation's Deployment Key; the effective source is reported as `credentialSource`
(`site`, `deploymentKey`, or `none`). The become password is always site-scoped.

The login user is resolved per host: when the Server's deployed OS Image has an effective
default user, that user is used exclusively; otherwise the site SSH user is tried first,
then swallow's built-in candidates (`cloud-user`, `ubuntu`). The site SSH user is therefore
a fallback, not a requirement.

Automation is not an external integration: Swallow owns the dispatcher, execution state,
logs, artifacts, and retention policy. Automation still respects provider-owned Server
Lock: target protection is checked at acceptance and immediately before runner startup,
because embedded Ansible can mutate a host without going through its provisioner.

See also: [Operation](operation.md), [Server Lock](server-lock.md),
[Deployment Key](deployment-key.md), [OS Image](os-image.md).

Change note: Updated 2026-10-01 so the site private key is an optional override of the
Deployment Key and the login user prefers the OS Image default user, per
[decision 039](../../../decisions/039-ssh-key-management-and-default-user.md).
