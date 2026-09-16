# 031. Provider-capability-first with a swallow-owned fallback

- Status: Accepted
- Date: 2026-09-15

## Context

swallow keeps deriving the same ownership answer, one feature at a time, for facts a
provisioner *may* own. [ADR 001](001-system-ownership-boundaries.md) split every stored field
into **owned data** or **mirrored facts** but did not say what to do when whether a fact is
owned or mirrored *depends on the concrete provider*. Later decisions each answered that
question for one field and then stopped:

- [ADR 025](025-provider-data-overlay.md) let a provider-owned fact carry a swallow-owned
  overlay merged at read, but scoped itself to display/bookkeeping capabilities that have **no
  external owner at all** (image name), explicitly *not* the case where a provider can perform
  the action.
- [ADR 029](029-infrastructure-zone-pool-ownership.md) made Zone/Pool swallow-owned and
  *realized* them in the provisioner through an optional `Grouping` capability, so a
  non-capable provider still leaves a working swallow record.
- [ADR 027](027-os-image-upload.md) added an optional `OSImageUploader` capability and drove
  the provider when present.

These are the same shape wearing three different coats: **when the provisioner can do it, drive
the provisioner; when it cannot, swallow owns the fact itself.** Because that shape was never
written down as one rule, every new feature (most recently Server tags) re-opens the ownership
debate from scratch, and reviewers have to re-explain it. The immediate trigger is server tag
editing: MAAS owns tags and can edit them, so swallow must drive MAAS — but a future provisioner
without tagging must not lose the ability to tag Servers.

## Decision

**For any fact a provisioner MAY own, ownership is capability-first with a swallow-owned
fallback.** swallow declares the capability on `ProviderCapabilities` and reaches an optional
provider interface by type assertion. Then, per Site's provisioner:

1. **Capable → the provider is the source of truth.** swallow drives the provider to perform
   the write and mirrors the result back through the normal reconcile path (the fact is a
   *mirrored fact* per ADR 001). swallow does **not** keep a second owned copy for capable
   providers; a shadow copy would be the divergence ADR 025 warns about.
2. **Not capable → swallow owns the fact.** swallow persists the fact in an owned collection
   keyed by the swallow identity (`serverId`, or the provider identity the entity is named by)
   and merges it into the effective value at read using the ADR 025 overlay mechanics (owned
   data: no `observedAt`, no staleness, never written back to the provider, pruned with the
   owning entity).
3. **One central merge, one vocabulary boundary.** The capable/not-capable branch and the
   read-time merge live in one place (for Servers, the reconcile projection) so every consumer
   — list, detail, discovery, derived classifications like Server Type — sees the same effective
   value without knowing which branch produced it. All provider vocabulary stays inside the
   provider adapter.
4. **The fallback is always built to the same shape,** even when today's only provider is
   capable (MAAS is), so it stays inert but correct and the next provider needs no new design.
   A feature MAY ship the capability seam plus the provider-driven path first and add the
   owned-fallback store as a follow-up, but the seam must exist from the start.

This subsumes the two earlier partial rules into one: ADR 025's "display capability with **no**
external owner → overlay" is the *degenerate* case where **no** provider is ever capable, and
ADR 029's "grouping realized when capable" is the *provider-can-write* case. This ADR states the
general rule so neither has to be re-derived.

## Alternatives considered

- **Leave the three decisions separate and cite them ad hoc.** Rejected: that is the status quo
  that keeps producing the same argument per feature; the cost is repeated re-litigation and
  inconsistent implementations.
- **Always let swallow own the fact and optionally push to the provider.** Rejected: it makes
  swallow authoritative for a provider-owned field (violating ADR 001) and invites drift with
  the provider for capable Sites, which is exactly what ADR 025 rule 4 forbids.
- **Always require the provider and refuse the feature when it is not capable.** Rejected: it
  strands operators on non-capable provisioners for facts (tags, names) that swallow can own
  perfectly well, and contradicts ADR 029's working-record-without-a-capable-provider outcome.
- **A generic key-value "owned attributes" store for every such fact.** Rejected for now: each
  fact still needs typed validation, a typed capability, and its own read-merge point; a generic
  bag hides those. The rule is shared; the per-fact type is not.

## Consequences

- New features that touch a maybe-provider-owned fact start from a decided position: add the
  capability + optional interface, drive the provider when capable, and add the owned-fallback
  overlay when not — no per-feature ownership debate.
- Server tags are the first feature to cite this ADR directly: `ProviderCapabilities.Tagging`
  with a `MachineTagController`, MAAS as the capable provider, and a `ServerTagOverlay` merged
  by reconcile only for non-capable provisioners (inert while MAAS is the provider).
- The rule is additive to existing ADRs; it does not change any implemented behavior. It refines
  ADR 001 (owned vs mirrored can be provider-dependent), and generalizes ADR 025 and ADR 029
  into their common rule. ADR 027 is an instance of the capable-path half.
- A one-paragraph statement of this rule is added to the root architecture spec so agents and
  reviewers apply it without reading this full ADR each time.

## Current status

Accepted. Stated in the root architecture spec in the same change. First applied by server tag
editing (`Tagging` capability + `ServerTagOverlay` fallback). Already retroactively describes the
OS Image name overlay (ADR 025, no-capable-provider case), OS Image upload (ADR 027,
capable case), and Zone/Pool (ADR 029, both cases).

## Related

- [ADR 001](001-system-ownership-boundaries.md) — owned data vs mirrored facts (refined here:
  which one a fact is can depend on the concrete provisioner's capabilities).
- [ADR 025](025-provider-data-overlay.md) — read-time swallow overlay on a provider fact
  (generalized here: the no-capable-provider case of this rule; the overlay mechanics are reused
  for the fallback).
- [ADR 029](029-infrastructure-zone-pool-ownership.md) — swallow-owned Zone/Pool realized in a
  grouping-capable provider (generalized here: the provider-can-write case of this rule).
- [ADR 027](027-os-image-upload.md) — optional provider capability driven when present (an
  instance of the capable half).
