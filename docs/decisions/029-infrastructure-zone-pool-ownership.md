# 029. swallow-owned Zone and Pool with optional provider realization

- Status: Accepted
- Date: 2026-09-14

## Context

A provisioner such as MAAS groups machines into *physical zones* and *resource pools*.
Until now swallow treated those labels as provider-owned facts carried through opaquely on
`provisioning` `Machine.Zone` / `Machine.ResourcePool`, explicitly "not mapped onto any
swallow hierarchy". Operators, however, need to manage these groupings from swallow — under
the dashboard *Infrastructure* area — and to move Servers between them, independently of
which provisioner backs a Site (and even before a provisioner is grouping-capable).

This forces a decision about ownership: are Zone and Pool observed provider facts, or
swallow-owned managed concepts? The two contradict each other, so the opaque pass-through
model had to be revisited rather than extended.

## Decision

Zone and Pool become **swallow-owned, Site-scoped managed concepts** with full CRUD, defined
in swallow's glossary and owned by a new `internal/infrastructure/` feature. swallow persists
its own catalog of Zones and Pools (name unique within a Site) and does not depend on a
provisioner to hold them.

When a Site's provisioner exposes an equivalent grouping capability, swallow **realizes** the
intent in that provisioner as part of the same operation:

- Creating, renaming, or deleting a Zone/Pool drives the provisioner's zone / resource-pool
  API. Realization is best-effort *ensure*: a Site whose provisioner is not grouping-capable
  is not an error (the record exists only in swallow), and a provider that already holds the
  group (MAAS ships `default`) is treated as satisfied.
- Assigning a Server to a Zone/Pool drives the provisioner to set that machine's zone / pool.
  Because a Server is always projected from a provisioner, a provisioner that lacks the
  grouping capability is surfaced as an unsupported error rather than silently skipped.

Provider realization uses the existing optional-capability pattern: `ProviderCapabilities`
gains a `Grouping` flag, `provisioning` gains `GroupingInspector` / `GroupingController`
optional interfaces reached by type assertion, and the MAAS adapter implements them. All MAAS
vocabulary stays inside the `provisioning` adapter; the `infrastructure` feature depends only
on the `provisioning` domain port (`ProviderFactory`) and its own `GroupingRealizer` port.

A Server's `Observed.ProviderZone` / `Observed.ProviderResourcePool` remain
**provider-observed mirrors** refreshed by reconcile — the observed effect of an assignment,
never the swallow-owned catalog.

## Alternatives considered

- **Keep zones/pools as opaque provider labels (status quo).** Rejected: it cannot satisfy
  swallow-driven management or Server reassignment, and leaves the concept unowned.
- **Model zones/pools inside the existing `provisioning` feature.** Rejected: the concept is
  swallow-owned and provider-agnostic (it exists without a capable provisioner), so binding it
  to the provisioner feature would leak provider assumptions into the domain. `provisioning`
  keeps only the provider realization capability.
- **Mirror the provider's zone/pool set into Mongo and reconcile it.** Rejected for this
  iteration: swallow owns its own catalog and reads observed membership from the Server
  projection; importing provider-defined groups into the catalog is deferred.
- **Fold Zone/Pool into Site.** Rejected: Site is deliberately thin and is a location, not a
  grouping; the glossary forbids treating a Site as an arbitrary Server group.

## Consequences

- swallow can manage Zones and Pools and reassign Servers regardless of provisioner support,
  while still configuring MAAS when it can. The optional-capability pattern keeps a
  non-capable provider from turning management into a dead end.
- The opaque pass-through note on `Machine.Zone` / `Machine.ResourcePool` is superseded; those
  fields are now documented as provider-observed effects of the swallow-owned model.
- swallow's catalog and the provider's actual set can drift (for example a group created
  directly in MAAS, or a failed realization). This is accepted for now; a future import/reconcile
  can close the gap. Assignment failures surface to the operator rather than being hidden.
- New glossary terms (Zone, Pool), a new provider-owned API contract
  (`infrastructure.md`), and a new feature slice are introduced. This refines rather than
  replaces ADR 001 (system ownership), ADR 012 (swallow owns intent, provider adapters
  realize and verify), and ADR 025 (swallow-owned data over provider facts).

## Current status

Planned → Implemented in this change: glossary, contract, `provisioning` grouping capability
(MAAS), and the `infrastructure` feature (Zone/Pool CRUD and Server placement). Dashboard
consumption and provider→catalog import are follow-ups.
