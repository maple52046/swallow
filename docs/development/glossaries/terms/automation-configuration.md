# Automation Configuration

An **Automation Configuration** is the single site-scoped configuration that enables
Swallow's embedded Ansible executor. It owns the SSH user and port, verified known-hosts,
and the mapping from each operation kind to a playbook name registered in the release
manifest.

The SSH private key and optional become password are write-only credentials. They are
encrypted at rest and are represented to API consumers only by `hasCredential`.

Automation is not an external integration: Swallow owns the dispatcher, execution state,
logs, artifacts, and retention policy. Automation still respects provider-owned Server
Lock: target protection is checked at acceptance and immediately before runner startup,
because embedded Ansible can mutate a host without going through its provisioner.

See also: [Operation](operation.md), [Server Lock](server-lock.md).
