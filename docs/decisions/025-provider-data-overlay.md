# 025. Provider-owned data carries a swallow overlay merged at read

- Status: Accepted
- Date: 2026-09-12

## Context

[ADR 001](001-system-ownership-boundaries.md) settled that swallow integrates a system
that already owns a capability rather than rebuilding it, and split every stored field into
**owned data** (swallow is the source of truth) or **mirrored facts** (a cached copy of what
an external system owns, carrying source and `observedAt`). [ADR 009](009-deployment-template-ownership.md)
applied that to OS provisioning: OS Images are read live from the provider and are never
mirrored; swallow owns only reusable deployment intent (Deployment Template), which references
an image by the provider's opaque id.

That leaves a gap the operator keeps hitting. A provider is authoritative for a fact, but the
provider offers **no way to attach the small piece of presentation or organization data
swallow's users want on top of it**. The archetype is image naming: MAAS presents a boot
resource label an operator cannot change, and MAAS exposes no image-rename operation at all,
so today swallow can only show the provider's label verbatim (the provisioning contract even
recorded "Rename is intentionally absent"). The same shape recurs whenever a provider is the
source of truth but lacks a display or bookkeeping capability that has **no external owner** —
a swallow-chosen name, a note, an operator tag.

ADR 001's two categories do not, by themselves, tell you what to do here. The swallow-chosen
name is unambiguously **owned data**: nothing external owns it, so there is no source and no
staleness. What was missing was the explicit, repeatable pattern for *combining* that owned
field with a provider-owned fact so the user sees one coherent value — without copying the
provider's data model into swallow, and without swallow ever writing the overlay back to the
provider.

This decision names that pattern so every future integration applies it the same way instead
of inventing an ad-hoc merge each time.

## Decision

**A provider-owned fact may carry a swallow-owned *overlay*: a small set of owned fields that
swallow stores itself and merges onto the provider's data at read time to form what the user
sees. The provider remains authoritative for its own fields; the overlay is authoritative only
for the fields it adds, and is never written back to the provider.**

The rules:

1. **Provider is the source; overlay is additive.** The read path fetches the provider's data
   (live, or from the ADR 001 reconcile-cache for fleet queries) and layers the overlay on top.
   Merge precedence is explicit and one-directional: for a field the overlay owns, the effective
   value is `overlay.value ?? provider.value`. The response also carries the original provider
   value so a client can show "renamed from" and offer a reset.
2. **Overlay is owned data (ADR 001).** It has no external owner, so it carries no `observedAt`
   and no staleness. Reading it back is authoritative for the fields it owns. It is stored in a
   swallow-owned collection, keyed by the same identity the provider uses to name the entity
   (for an OS Image: `integrationId` + `imageId` + `architecture`, the identity `DELETE /images`
   already uses).
3. **An overlay never mutates the provider.** Setting or clearing an overlay is a swallow-local
   write only. This does not weaken ADR 001's "a mirror is never authoritative for a write":
   the overlay is not a mirror of a provider fact, it is a *different* fact swallow owns.
4. **Overlays do not resurrect deleted providers of a capability.** An overlay exists only for
   a display/bookkeeping capability that has **no external owner**. If the provider itself can
   perform the action, swallow drives the provider instead of overlaying. This keeps the overlay
   from becoming a shadow copy of a provider-owned field.
5. **Orphan overlays are harmless and pruned on the owning entity's deletion.** If the provider
   catalog changes and the overlaid entity disappears, the overlay simply stops being merged
   (no user impact). Where swallow drives the entity's deletion (e.g. deleting a custom image),
   it deletes the overlay in the same path so stale rows do not accumulate.

### Relationship to the capability model

This overlay pattern is the **display-side** complement to swallow's existing
**operation-side** capability model. Where a provider cannot perform an *operation* (for
example an adapter that does not implement image deletion, or a future provider that cannot
erase a disk before release), swallow already declares that through `ProviderCapabilities` and
its optional provider interfaces, and the UI presents the action as unavailable rather than
offering a button that always fails (see the OS provisioning provider port and its optional
interfaces). That mechanism is unchanged. The two together give a single rule for a missing
provider capability:

- **Display / bookkeeping capability missing** → swallow stores it as an overlay and merges it
  at read.
- **Operation capability missing** → swallow refuses it via `ProviderCapabilities`, surfaced as
  unavailable.

## Alternatives considered

- **Keep image labels provider-verbatim (status quo of ADR 009).** Rejected: it leaves a real
  operator need — a stable, human-chosen name — permanently unmet for a provider that will never
  add rename, while the data has no external owner and is cheap for swallow to own.
- **Mirror the provider entity and edit the mirror.** Rejected: it violates ADR 001's rule that
  a mirror is never authoritative for a write, invites divergence with the provider, and copies a
  data model swallow deliberately does not own. The overlay stores only the swallow-owned delta,
  not a copy of the provider fact.
- **Push the swallow name into the provider.** Rejected: the provider offers no such operation
  (that is the whole reason the overlay exists), and even where one existed it would make swallow
  responsible for a provider-owned field it should not own.
- **Model each case bespoke (image name here, server tags later, each different).** Rejected:
  the point of this ADR is one repeatable pattern so future integrations do not each reinvent the
  merge and its ownership story.

## Consequences

- OS Images gain a swallow-owned display name overlaid on the provider catalog: the API returns
  an effective `name`, the provider's original `providerName`, and the swallow `customName`
  (nullable). ADR 009 still holds — the image artifact stays provider-owned, live-read, and never
  mirrored; only the name overlay is swallow-owned. The provisioning contract's "Rename is
  intentionally absent" note is removed and replaced by the overlay endpoints.
- Every future overlay (server display name, operator tags, notes) follows the same rules:
  owned data, keyed by provider identity, merged at read with explicit precedence, never written
  back, pruned on entity deletion. This is what makes the pattern a norm rather than a one-off.
- swallow carries a new small owned collection per overlaid entity type. This is owned data, so
  it needs no staleness surface; it does need the same care as other owned collections (unique
  key on the provider identity, validation of the swallow-owned fields).

## Current status

Partial. Implemented for the OS Image overlay (`os_image_overlays` collection, read-time merge in
the image catalog use case, `PATCH`/`DELETE /provisioning/images/overlay`), which overrides the
image's display name, OS family, and release and adds swallow-owned tags (a purely additive field
with no provider counterpart), while leaving the deployable image identity provider-owned. Other
overlays (for example Server display metadata) are future work under this same decision.

The image name overlay also participates in a mirrored read: the reconciler resolves each
deployed Server's image to its effective name (provider title overlaid with the custom name here)
and mirrors that string onto the Server provisioning axis so the fleet list shows the swallow name
without a per-request catalog fan-out (see [ADR 009](009-deployment-template-ownership.md)).

## Related

- [ADR 001](001-system-ownership-boundaries.md) — owned data vs mirrored facts (refined here:
  an owned overlay may be merged onto a provider fact at read; the overlay is owned data, not a
  mirror).
- [ADR 009](009-deployment-template-ownership.md) — OS Images are provider-owned and live-read
  (refined here: swallow may own a name overlay for an image without mirroring the artifact).
