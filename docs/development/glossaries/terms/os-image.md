# OS Image

- Bounded context: OS Provisioning.
- Definition: An operating system artifact that an OS Provisioning Provider can
  currently install on a managed machine.
- Allowed meaning: Live provider-owned catalog data identified by the opaque
  value the same provider accepts on an OS Deployment request. Swallow may show
  and select the value but does not own, mirror, upload, or synchronize the
  artifact. Swallow may own a Provider Data Overlay for it — swallow-chosen
  display overrides (name, OS family, release) plus a swallow-owned list of tags,
  keyed by `integrationId` + `imageId` + `architecture` — because the provider
  offers no way to relabel or tag these and they have no external owner; each
  override is merged over the provider value at read, tags are purely additive
  (no provider counterpart), and the artifact and its deployable `id` stay
  provider-owned. Provider-reported artifact metadata such as its current complete
  resource-set size may accompany the live catalog but remains provider-owned and
  is never part of the overlay.
- Disallowed meaning: A Swallow release, container image, package bundle,
  post-install script collection, or durable Swallow-owned copy of the image
  artifact. The overlay is not a mirror of the artifact and is never written back
  to the provider; it stores only the swallow-owned display overrides, not provider
  catalog data, and never changes what the image deploys.
- Synonyms: Provisioning image (deprecated UI wording).
- Deprecated terms: Provisioning Image.
- Examples: An operator selects the MAAS image `ubuntu/jammy` for Servers managed
  by that MAAS Integration. Another Integration may expose a different catalog. An
  operator relabels that image; the catalog returns the effective `name`/`osSystem`/
  `release`, the provider's `provider*` values, and the swallow `custom*` overrides,
  and can reset to the provider values.
- Related terms: OS Deployment, Deployment Template, Server, Provider Data Overlay.
- Change note: Added to make the provider ownership and integration scope used by
  the OS provisioning API explicit. Updated 2026-09-13 to clarify that live image
  size metadata remains provider-owned. Updated 2026-09-12 for the swallow-owned
  display overlay (name, OS, release) per
  [decision 025](../../../decisions/025-provider-data-overlay.md).
