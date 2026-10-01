# OS Image

- Bounded context: OS Provisioning.
- Definition: An operating system artifact that an OS Provisioning Provider can
  currently install on a managed machine.
- Allowed meaning: Live provider-owned catalog data identified by the opaque
  value the same provider accepts on an OS Deployment request. Swallow may show
  and select the value, and — on a provisioner whose adapter supports it — may
  drive the provider to upload a new artifact from operator-supplied content, but
  does not own, mirror, or synchronize a durable copy of the artifact: an uploaded
  image is provider-owned exactly like a synced one. Whether an uploaded artifact
  is classified as a custom image is provider-determined by the site's provisioner,
  not chosen by Swallow or the caller (for MAAS an uploaded boot resource surfaces
  as `osSystem: custom`). Upload and delete are optional provider capabilities,
  refused through `ProviderCapabilities` when the provisioner does not offer them.
  Swallow may own a Provider Data Overlay for it — swallow-chosen
  display overrides (name, OS family, release) plus a swallow-owned list of tags,
  keyed by `integrationId` + `imageId` + `architecture` — because the provider
  offers no way to relabel or tag these and they have no external owner; each
  override is merged over the provider value at read, tags are purely additive
  (no provider counterpart), and the artifact and its deployable `id` stay
  provider-owned. The overlay may also carry the image's **default user** — the
  login user the image's cloud-init configures and authorizes provisioner SSH
  keys for (for example `ubuntu` or `cloud-user`). The effective default user is
  the overlay value when set, otherwise a swallow built-in derived from the
  provider OS family (`ubuntu` → `ubuntu`, `centos` → `centos`, `rhel` →
  `cloud-user`), otherwise none; it can be set at upload or afterwards, and
  swallow mirrors it onto each Server deployed with the image so automation logs
  in as that user. Provider-reported artifact metadata such as its current complete
  resource-set size may accompany the live catalog but remains provider-owned and
  is never part of the overlay.   Swallow may additionally hold a swallow-owned
  **verification** for a custom image — a per-[Deploy Target](deploy-target.md)
  outcome of a real proving deploy in that mode, keyed by the same
  `integrationId` + `imageId` + `architecture` identity and stored separately from
  the overlay. Each Deploy Target records the latest run's outcome, which is one of
  three states: verified (a proving deploy succeeded), failed (the most recent
  proving deploy failed), or absent (never attempted). The catalog projects these as
  `verifiedDeployTargets` and `failedDeployTargets`; a Deploy Target appears in at
  most one of them because recording one outcome clears the other. A custom image
  must be verified for a Deploy Target before a normal deploy in that target is
  allowed — a failed or absent target is blocked the same way, so `failedDeployTargets`
  affects only display, letting an operator tell "proven not to deploy this way" apart
  from "not yet tried". Synced provider images are provider-trusted and are never
  gated on verification.
- Disallowed meaning: A Swallow release, container image, package bundle,
  post-install script collection, or durable Swallow-owned copy of the image
  artifact. Driving a provider upload does not make Swallow the owner of the
  resulting artifact — Swallow streams operator content to the provider and keeps
  no copy — nor does the caller decide whether it becomes a custom image. The
  overlay is not a mirror of the artifact and is never written back to the
  provider; it stores only the swallow-owned display overrides, not provider
  catalog data, and never changes what the image deploys.
- Synonyms: Provisioning image (deprecated UI wording).
- Deprecated terms: Provisioning Image.
- Examples: An operator selects the MAAS image `ubuntu/jammy` for Servers managed
  by that MAAS Integration. Another Integration may expose a different catalog. An
  operator relabels that image; the catalog returns the effective `name`/`osSystem`/
  `release`, the provider's `provider*` values, and the swallow `custom*` overrides,
  and can reset to the provider values.
- Related terms: OS Deployment, Deploy Target, Deployment Template, Server, Provider Data Overlay.
- Change note: Added to make the provider ownership and integration scope used by
  the OS provisioning API explicit. Updated 2026-09-13 to allow Swallow to drive a
  provider-owned image upload as an optional capability (the artifact stays
  provider-owned and its custom classification is provider-determined) per
  [decision 027](../../../decisions/027-os-image-upload.md). Updated 2026-09-13 to
  clarify that live image size metadata remains provider-owned. Updated 2026-09-12
  for the swallow-owned display overlay (name, OS, release) per
  [decision 025](../../../decisions/025-provider-data-overlay.md).   Updated 2026-09-21
  for the swallow-owned per-Deploy-Target verification and the custom-image deploy
  gate per [decision 035](../../../decisions/035-os-image-verification-and-deploy-target.md).
  Updated 2026-09-21 so per-Deploy-Target verification records a failed outcome as well
  as a verified one (`failedDeployTargets`), making a failed verification visible and
  distinct from a never-attempted one, per
  [decision 036](../../../decisions/036-provisioning-lifecycle-integrity.md).
  Updated 2026-10-01 for the swallow-owned default user used as the automation
  login user, per
  [decision 039](../../../decisions/039-ssh-key-management-and-default-user.md).
