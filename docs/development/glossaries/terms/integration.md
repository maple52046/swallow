# Integration

- Bounded context: swallow-wide.
- Definition: A registered external system that swallow talks to, scoped to exactly one Site. swallow owns the registry of Integrations; no external system knows which systems swallow should talk to.
- Allowed meaning: A swallow-owned record identified by an opaque, stable `integrationId` — persisted in every Server source, so it must never be reissued for a different system — carrying `siteId`, a `kind`, an `endpoint`, a write-only `credentialRef`, and an `enabled` flag. Kinds are `provisioner` (MAAS — read inventory, deploy and release operating systems), `metrics` (central TSDB — query with PromQL), and `cluster` (Kubernetes API or Slurm — read live membership). An Integration may be registered but paused.
- Disallowed meaning: Not a Site, a Server, a Platform, or the external system itself; the Integration is swallow's registration of it. A credential is never returned by any API in any form, including redacted — it is write-only. The `cluster` kind names the external Kubernetes/Slurm technology, not the `Platform` aggregate.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "A `provisioner` Integration reconciles machines into Server projections." / "A MAAS spanning two Sites is two Integrations, because an Integration serves exactly one Site."
- Related terms: Site, Server, OS Provisioning Provider, Staleness, Machine.
- Change note: Migrated 2026-09-05 from the narrative `docs/glossaries/site.md` (reorg D2). The `cluster` kind is the external technology, kept per [decision 014](../../../decisions/014-platform-resource-language.md) / [015](../../../decisions/015-platform-term-disambiguation.md).
