# 071. Provisioner-realized fields converge from swallow's desired state

- Status: Accepted
- Date: 2026-10-31
- 現行規格：[desired-state-model.md](../development/desired-state-model.md) §4.4, §6, §7, §11

## Context

[ADR 069](069-swallow-owns-desired-state.md) decided that swallow's store owns desired state and that
providers realize it, but left the first migration to decide how that looks in practice. The first
fields to migrate are the ones a provisioner realizes with short calls: a Server's tags and Zone/Pool
placement, the Zone and Pool catalog, a provisioner's HTTP Proxy and subnet DNS servers, SSH Key
registration, and OS Image metadata. Today swallow writes most of them through to MAAS and mirrors the
result back, keeps no copy of what the operator asked for, and patches the gaps with read-time overlays
([ADR 025](025-provider-data-overlay.md)) or accepts that its catalog drifts
([ADR 029](029-infrastructure-zone-pool-ownership.md)).

Migrating them raised questions ADR 069 does not answer:

- How a migrated resource appears on the API while the dashboard and CLI in the same repository consume
  the old shape.
- ADR 069 reports drift and corrects it only for fields that opt in. Operators expect swallow to keep
  the provisioner in line with what they set, not to watch it change.
- Existing installations, and machines a provisioner enlists on its own, have no spec. Deriving one
  must not change anything on the provisioner.
- A Server only exists today because a provisioner reported a machine
  ([ADR 002](002-server-identity.md)). Large sites enlist machines by network boot, smaller ones add
  them through swallow, and some hosts arrive with an existing OS.
- A provisioner's machine carries both its own automatic tags and manual ones, and either swallow or a
  person in the provisioner UI may have set a manual one.

## Decision

1. **A migrated resource is served as spec and status.** Its representation is its identity, its
   `generation`, a `spec` holding the desired fields, and a `status` holding `observedGeneration`, the
   conditions, and every observed and derived fact. The pre-migration fields are removed in the same
   change; the dashboard and CLI move to the new shape in the same commit. Resources that are not
   migrated yet keep their current shape.
2. **Non-destructive provisioner-realized fields correct their own drift, on a schedule.** A spec
   change converges at once. Drift is detected from the routine observation swallow already does and is
   corrected at the controller's periodic resync (every 10 minutes by default), not by watching the
   provisioner continuously. This applies to Server manual tags and placement, the Zone and Pool
   catalog, HTTP Proxy, subnet DNS servers, and SSH Key registration. A field whose stale value would
   break control of the hardware, such as Power Configuration, reports drift only, and the operator
   adopts the observed value or reapplies the spec.
3. **The first observation adopts.** A resource with no spec yet takes the provider's current values as
   its spec. Adopting never writes to the provider. This covers existing data on upgrade and machines a
   provisioner enlists on its own, so a migration never needs the provider.
4. **A Server records how it arrived, and swallow decides from then on.** Its `origin` is `discovered`
   (the provisioner enlisted it), `enrolled` (swallow registered it), or `imported` (an existing OS was
   registered without reinstalling). Discovered machines are adopted automatically, as today; an
   approval step may be added later. Whatever the origin, the Server's desired fields belong to swallow.
   A Server whose machine disappears from the provisioner reports `realized=false` with reason
   `provider_missing`; swallow does not re-create it.
5. **swallow owns every manual tag of a managed Server.** The spec is the complete set of manual tags,
   so a manual tag added or removed in the provisioner UI is drift. A provisioner's automatic tags (MAAS
   tags with a `definition`) are observed only. SSH Keys differ: swallow owns only the keys it
   registered, because the provisioner account may hold keys that belong to people.
6. **Controllers run outside the API process, one active instance each.** A store lease with a TTL
   keeps a single active instance per controller. A controller is triggered by the store change feed,
   by its periodic resync, and by API requests. A transient provider error backs off and retries; a
   permanent one sets `requires_attention` until the spec changes.

## Alternatives considered

- **Add spec and status beside the existing fields.** Rejected: every consumer would read two shapes
  for the same fact, and swallow has no stable release whose clients need a compatibility period; its
  consumers live in this repository.
- **Report drift only, as ADR 069's default.** Rejected for these fields: they are non-destructive, and
  leaving the provisioner diverged from what an operator set is what the migration is meant to end.
- **Correct drift as soon as it is observed.** Rejected: corrections at the observation cadence would
  fight a person in the middle of a change and multiply provider writes; a resync bounds both.
- **Seed the spec in the schema migration by reading the provider.** Rejected: a migration must run
  without the provider, and adoption at first observation produces the same spec.
- **Require approval before adopting a discovered machine.** Deferred: automatic adoption is today's
  behavior, and an approval step can be added per Site without changing the model.
- **Own only the tags swallow assigned.** Rejected for tags: the provisioner is dedicated to swallow,
  and partial ownership leaves "who set this tag" undecidable. Kept for SSH Keys, whose provisioner
  account is shared with people.

## Consequences

- The Server tag fallback and the OS Image overlay of ADR 025 retire; OS Image metadata becomes desired
  state that needs no provider realization.
- The catalog drift ADR 029 accepted no longer holds: Zones and Pools converge on every provisioner of
  their Site, including one added later.
- ADR 002's identity rules stay; a Server additionally records its origin, and a missing machine is a
  condition rather than only the `absent` flag.
- A change made in the provisioner UI to an auto-corrected field is undone at the next resync; operators
  change these fields in swallow.
- Cost: a controller framework and a change feed, and every consumer of the migrated resources changes
  with them.
