# Provider Data Overlay

- Bounded context: swallow-wide (integration boundary).
- Definition: A small set of swallow-owned fields that swallow stores itself and merges onto a
  provider-owned fact at read time, so a user sees one coherent value that a provider is
  authoritative for plus the presentation or bookkeeping data the provider cannot hold.
- Allowed meaning: Owned data (there is no external owner, so it has no source and no staleness),
  keyed by the same identity the provider uses to name the entity, additive to the provider's
  data with explicit one-directional precedence (`overlay value ?? provider value`), and never
  written back to the provider. It exists only for a display or bookkeeping capability the
  provider does not offer and that has no external owner. The read that merges it must also
  expose the original provider value so a client can show the change and reset it.
- Disallowed meaning: Not a mirrored fact and not a copy of a provider-owned field — it stores
  only the swallow-owned delta, never the provider's data model. Not a way to change the
  provider: setting or clearing an overlay is a swallow-local write only. Not a substitute for
  driving the provider when the provider can perform the action itself. Not an operation-side
  capability flag (an operation the provider cannot perform is refused through provider
  capabilities, not overlaid).
- Synonyms: Overlay (within a specific entity, e.g. OS Image name overlay).
- Deprecated terms: None.
- Examples: "MAAS exposes no way to relabel an image, so swallow stores an OS Image overlay
  (name, OS family, release) keyed by `(integrationId, imageId, architecture)` and merges each
  non-empty field over the provider value at read." / "An operator relabels an image; the catalog
  returns the effective `name`/`osSystem`/`release`, the provider's `provider*` values, and the
  swallow `custom*` overrides." / "If the image disappears from the provider catalog the overlay
  is simply not merged; deleting the image also deletes its overlay."
- Related terms: Integration, OS Image, OS Provisioning Provider, Machine, Server, Staleness,
  Deployment Template.
- Change note: Added for the provider-data-overlay refactor to name the reusable pattern of
  merging swallow-owned display data onto provider-owned facts. See
  [decision 025](../../../decisions/025-provider-data-overlay.md), which refines
  [decision 001](../../../decisions/001-system-ownership-boundaries.md) and
  [decision 009](../../../decisions/009-deployment-template-ownership.md).
