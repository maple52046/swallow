# Automation Configuration

An **Automation Configuration** is the single site-scoped configuration that enables
Swallow's embedded Ansible executor. It owns the SSH user and port, verified known-hosts,
and the mapping from each operation kind to a playbook name registered in the release
manifest.

The SSH private key and optional become password are write-only credentials. They are
encrypted at rest and are represented to API consumers only by `hasCredential`.

Automation is not an external integration: Swallow owns the dispatcher, execution state,
logs, artifacts, and retention policy.

See also: [Operation](operation.md).
