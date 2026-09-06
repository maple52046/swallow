# OS Provisioning Provider

- Bounded context: OS Provisioning.
- Definition: An external system that enumerates provisionable hardware and installs operating systems onto it. Ubuntu MAAS is the first and currently only provider.
- Allowed meaning: Registered as an Integration of kind `provisioner`, scoped to one Site, and identified by its `integrationId` (which instance) and a stable `providerKind` string such as `maas` (which adapter). A fleet has many provider instances — typically one MAAS per Site — so provider connection details are data, not configuration. swallow drives the provider's own actions (deploy, release, power, commissioning, tests, lock, rescue) and mirrors their results; it never reimplements OS installation.
- Disallowed meaning: Not swallow itself, not a Site, not a Machine, not a Server. Not a store of automation content (playbooks or scripts) — post-install automation is an Operation from the signed release bundle. An action a provider cannot do is refused, not silently dropped.
- Synonyms: Provisioner; provider, within the OS Provisioning context.
- Deprecated terms: None.
- Examples: "`providerKind` selects the adapter; `integrationId` selects the instance." / "A provider that cannot do an ephemeral deploy refuses the request rather than deploying normally."
- Related terms: Integration, Machine, OS Image, OS Deployment, Release, Server.
- Change note: Migrated 2026-09-05 from the narrative `docs/glossaries/provisioning.md` (reorg D2).
