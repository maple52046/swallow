# 071. Provisioner-realized fields are swallow's desired state, converged by controllers

- Status: Accepted
- Date: 2026-09-12
- 現行規格：[desired-state-model.md](../development/desired-state-model.md) §4.4, §6, §7, §11；[os-provisioning.md](../development/os-provisioning.md) §7 Provisioner HTTP Proxy, §8 Zone 與 Pool

## Context

[decision 069](069-swallow-owns-desired-state.md) (swallow owns desired state) made swallow's store the
source of truth for desired state, with providers realizing it. The first fields to follow it are the ones
a provisioner realizes with short calls: a Server's manual tags and Zone/Pool placement, the Zone and Pool
catalog, a provisioner's HTTP Proxy and Subnet DNS, SSH Key registration, and OS Image metadata.

These fields used to follow three patterns: swallow-owned fields merged onto a provider fact at read time
(the provider data overlay, used for OS Image names and the Server tag fallback); a swallow catalog
realized best-effort on the provisioner with accepted drift (Zones and Pools); and settings read live and
written through with nothing stored (HTTP Proxy, Subnet DNS). Where the provisioner held the value, swallow
kept no record of what the user set, so a change made in the MAAS UI silently became the value.

Decision 069 leaves open how a migrated resource appears on the API; whether drift is corrected (069 only
reports it unless a field opts in); how existing data and self-enlisted machines get a spec without
changing the provisioner; how a Server arrives ([decision 002](002-server-identity.md) (server identity)):
by network boot, through swallow, or with an OS installed; and who owns a manual tag set in the MAAS UI.

## Decision

### The model

1. **A migrated resource is served as spec and status**: its identity (a Server also its `origin`), a
   `generation`, a `spec` with the desired fields, and a `status` with `observedGeneration`, the
   conditions, and every observed and derived fact. A spec write is stored and answered at once; the
   conditions report convergence. The pre-migration fields were removed in the same change.
2. **Non-destructive provisioner-realized fields correct their own drift, on a schedule.** A spec change
   converges at once. Drift is detected by routine observation, reported as `drifted`, and corrected at the
   controller's periodic resync (every 10 minutes by default), not by watching the provisioner. This covers
   every resource below. A field whose wrong value would lose control of the hardware, such as Power
   Configuration ([decision 054](054-provisioner-power-configuration.md) (provisioner Power Configuration))
   once it migrates, reports drift only; the user adopts the observed value or reapplies the spec.
3. **The first observation adopts.** A resource with no spec takes the provider's current values as its
   spec without writing to the provider. This covers existing data on upgrade and machines a provisioner
   enlists on its own, so a schema migration never needs the provider.
4. **A Server records how it arrived, and swallow decides from then on.** Its `origin` is `discovered`,
   `enrolled`, or `imported`; discovered machines are adopted automatically. Whatever the origin, its
   desired fields belong to swallow. A machine that disappears from the provisioner reports
   `realized=false` with reason `provider_missing`; swallow does not re-create it.
5. **A provider that cannot realize a field keeps the spec** and reports `realized=false` with reason
   `capability_missing`; swallow builds no same-shaped fallback. An Integration-scoped setting (HTTP Proxy,
   Subnet DNS) has its own route behind a provider capability, refused on a provisioner without it.
6. **Controllers run outside the API process**, one active instance each under a store lease with a TTL,
   triggered by the store change feed, their periodic resync, and API requests. A transient provider error
   backs off; a permanent one sets `requires_attention` until the spec changes or a user asks for a retry.

### The resources

- **Server manual tags.** swallow owns every manual tag: the spec is the complete set, so one added or
  removed in the provisioner UI is drift. Deleting or renaming a tag across a Site rewrites every spec
  that names it in one transaction, then calls the provisioner synchronously. Tags a provisioner
  computes or maintains itself (MAAS tags with a `definition`, and the `virtual` tag MAAS commissioning
  keeps) are observed only and never drift, whatever the provisioner does with them; removing one from
  Servers or deleting it is an action written through to the provisioner, which may apply it again.
- **Zone and Pool.** Site-scoped groupings owned by swallow in their own `infrastructure` feature, which
  depends only on the provisioning domain port. The catalog converges on every provisioner of the Site,
  including one added later; a group that exists only in a provisioner is adopted, never deleted, and the
  provider's default group (MAAS `default`) stays out. Deletion is the only write that calls the
  provisioners synchronously; it is refused while a Server is placed in the group. A Server's placement
  references the catalog by id (`null` is the provider's default group) and follows a rename; its observed
  zone and pool are mirrors.
- **Provisioner HTTP Proxy.** Per provisioner Integration, a closed mode set (`none`, `builtin`,
  `external`, `peer`) plus the URL the mode needs, as one spec that the adapter maps onto the provider's
  settings and derives back by the provider's rule for which proxy a deployed machine gets. A URL with
  credentials is refused: the setting is readable back, and swallow does not return secrets.
- **Subnet DNS.** Each provisioner subnet's DNS server list is a spec; the subnet stays provisioner-owned
  and live-read. A new subnet is adopted with its list; a vanished subnet's setting is removed. How
  deployments use it is [decision 058](058-deployment-dhcp-and-subnet-dns.md) (deployment network intent).
- **SSH Keys.** swallow owns only the keys it registered or adopted (identical key material already there);
  a key a person added in the provisioner is never removed, and the resync re-adds a deleted swallow key.
  Key ownership otherwise follows [decision 039](039-ssh-key-management-and-default-user.md) (SSH keys).
- **OS Image metadata replaces the provider data overlay.** An image's name, OS, release, tags, and default
  user are desired state that needs no realization, because the provisioner cannot rename or relabel an
  image: no controller, generation, or conditions, and merged at read into the live catalog beside the
  provider's values. The artifact stays provider-owned and live-read
  ([decision 009](009-deployment-template-ownership.md) (deployment template ownership)). The overlay is no
  longer a pattern, and its term is retired: swallow-owned data about a provider fact is a desired field.

## Alternatives considered

- **Write through on each request and read back live** (the earlier design): rejected; a change in the
  provisioner silently became the value, could not be reported as drift, and let the Zone and Pool catalog
  drift.
- **Add spec and status beside the existing fields:** rejected; every consumer would read two shapes for
  one fact, and swallow's consumers live in this repository, so no client needs a compatibility period.
- **Report drift only (decision 069's default), or correct it as soon as it is observed:** rejected; these
  fields are non-destructive and a provisioner diverged from what a user set is what the migration ends,
  while immediate correction would fight a person mid-change and multiply provider writes.
- **Seed the spec in the schema migration by reading the provider:** rejected; a migration must run
  without the provider, and adoption at first observation gives the same spec.
- **Require approval before adopting a discovered machine:** deferred; automatic adoption was the existing
  behavior, and an approval step can be added per Site without changing the model.
- **Own only the tags swallow assigned:** rejected; the provisioner is dedicated to swallow, and partial
  ownership leaves "who set this tag" undecidable. Kept for SSH Keys, whose provisioner account people share.
- **Show provider image labels verbatim, or push swallow's name into the provider:** rejected; users need
  a stable name they choose, and the provider offers no rename operation.
- **Keep Zone and Pool as opaque provider labels, or inside the provisioning context:** rejected; swallow
  could not manage them or move Servers between them, and the concept is swallow-owned and
  provider-agnostic. **Fold them into Site:** rejected; a Site is a location, not an arbitrary grouping.
- **Store the HTTP Proxy or Subnet DNS in the Integration's `settings`:** rejected; `settings` describe how
  swallow reaches the provisioner, and an Integration PATCH would gain a provider side effect.
- **Expose MAAS's proxy settings as they are** (`enable_http_proxy`, `use_peer_proxy`, `http_proxy`):
  rejected; several combinations mean the same thing, and MAAS vocabulary would enter the API. **Edit the
  generated proxy (Squid) configuration:** rejected; MAAS regenerates it.

## Consequences

- A change made in the provisioner UI to an auto-corrected field is undone at the next resync; users
  change these fields in swallow.
- Decision 002's identity rules stay; a Server also records its origin, and a missing machine is a
  condition rather than only an absent flag.
- Writing the HTTP Proxy or Subnet DNS needs a MAAS administrator account, otherwise `requires_attention`,
  and affects only later deployments. MAAS takes the proxy as three settings written one at a time; a
  part-way failure leaves a mixed state that the controller retries until it converges.
- Cost: a controller framework and a change feed, and every consumer of a migrated resource changes with it.

## Revision history

- 2026-09-12: decision 071 let a provider-owned fact carry a swallow-owned overlay merged at read time and never written back to the provider.
- 2026-09-14: decision 071 made Zone and Pool swallow-owned, Site-scoped concepts realized best-effort on a grouping-capable provisioner, accepting catalog drift.
- 2026-10-13: decision 071 had swallow read the Provisioner HTTP Proxy live and write it through as a closed mode set, storing nothing.
- 2026-10-31: decision 071 made provisioner-realized fields desired state served as spec and status, auto-corrected at the periodic resync and adopted at first observation, with a Server origin and swallow owning every manual tag.
- 2026-11-02: merged decision 071 into this ADR; the body states the current decision.
- 2026-11-11: a tag the provisioner maintains without a definition (MAAS `virtual`) became automatic and observed only, because removing it as drift fought the provisioner at every commissioning; Site-wide tag deletion and rename were added, calling the provisioner synchronously.
