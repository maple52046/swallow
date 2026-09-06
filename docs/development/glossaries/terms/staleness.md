# Staleness

- Bounded context: swallow-wide.
- Definition: The freshness of a mirrored fact — which source it came from and when that source was last observed — surfaced as part of the API rather than hidden as an implementation detail.
- Allowed meaning: Every mirrored projection carries `observedAt` per status axis, and each Integration carries `lastSyncStartedAt`, `lastSyncSucceededAt`, `lastSyncError`, and `syncIntervalSeconds`. A reader must always be able to tell "this Site last synced 14 minutes ago" from "this Site is up to date". A view assembled from a stale source and a fresh one reports both, not the worse of the two. An unreachable Integration does not change or invalidate the data it last reported — it only ages it.
- Disallowed meaning: Not a status value. "We do not know / it is stale" must never be presented as "we know it is bad" (see Server Status). Staleness alone is not an error.
- Synonyms: Freshness.
- Deprecated terms: None.
- Examples: "MAAS is unreachable, so the provisioning axis is stale while the health axis stays current." / "A cross-Site listing shows each Site's own last-sync time."
- Related terms: Integration, Server Status, Machine, Server.
- Change note: Migrated 2026-09-05 from `docs/glossaries/site.md`; elevated because [decision 001](../../../decisions/001-system-ownership-boundaries.md) makes freshness part of the API (reorg D2).
