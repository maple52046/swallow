# 035. OS image verification and deploy-target vocabulary

- Status: Accepted (verification failure handling refined by [ADR 036](036-provisioning-lifecycle-integrity.md))
- Date: 2026-09-21
- 現行規格：[os-provisioning.md](../development/os-provisioning.md) §5 OS Image 驗證與 Deploy Target

## Context

Custom (uploaded) OS Images are provider-owned but not provider-certified: the
provider does not promise an uploaded image actually boots and installs. A real
Rocky failure — missing grub packages plus broken install-time DNS — installed
fine as an in-memory (ephemeral) deploy but failed a disk install, and Swallow
had no way to know an image was untrustworthy for a given deploy mode until an
operator hit the failure in production.

Separately, the operator-facing vocabulary for "install to disk" vs "run from
memory" was the single boolean `ephemeral`, which is wire/BSON/MAAS/k0s
vocabulary leaking into the UI, and gave no room to talk about per-mode
trust.

## Decision

Introduce a **Deploy Target** vocabulary and a **swallow-owned OS Image
verification** attestation.

- **Deploy Target** (`disk` | `ram`) is the canonical operator-facing concept.
  `ram` is exactly the existing `ephemeral`; the two map one-to-one at every
  boundary (`disk ↔ ephemeral=false`, `ram ↔ ephemeral=true`). Deploy requests
  accept `deployTarget` (preferred), with `ephemeral` kept as a still-accepted
  deprecated alias. No wire, BSON, durable-snapshot, MAAS, or k0s field is
  renamed.
- **Verification** is a swallow-owned fact (Swallow ran a real deploy and it
  worked), not a provider capability. It follows the Provider Data Overlay
  identity rule ([ADR 025](025-provider-data-overlay.md)), keyed by
  `(integrationId, imageId, architecture)`, stored separately from the display
  overlay, with per-target evidence of the run that produced it.
- A durable `verify-os-image` Workflow proves an image for a target with a real
  `provision-os` deploy on an operator-chosen ready Server, records the outcome,
  and returns the Server. The proving deploy is exempt from the gate below.
- A **deploy gate**: a *custom* image not verified for the requested deploy
  target is refused at deploy acceptance with a Swallow reason. Synced provider
  images are provider-trusted and never gated.

Revision history: this ADR recorded only a successful verification and released
the Server only after success. On 2026-09-21
[ADR 036](036-provisioning-lifecycle-integrity.md) made verification a
borrow-and-return contract: the Server is returned on success, failure, or
cancel, a failed outcome is recorded per target as well, and a proving
install/SSH failure is non-retryable so the Workflow terminates (original text in
git history).

## Alternatives considered

- **Keep reacting to install failures per image.** Rejected: the failure only
  shows up mid-deploy in production, and disk-vs-RAM trust is invisible.
- **Model verification as a provider capability / read the provider.** Rejected:
  the provider does not certify custom images; verification is a Swallow
  observation, so it is swallow-owned data (ADR 025/031), not a mirrored fact.
- **Rename `ephemeral` everywhere to a deploy-target enum.** Rejected: churns the
  wire, stored deployments, durable snapshots, the MAAS adapter, and the k0s var
  for no behavioural gain; a boundary mapping is enough.
- **Fold verification into the display overlay row.** Rejected: the overlay
  deletes itself when it has no overrides, which would silently erase a
  verification; a separate collection keeps the attestation durable.
- **Gate synced images too.** Rejected: provider-synced images are already
  provider-trusted; gating them adds friction with no safety gain.

## Consequences

- Operators see per-target trust at a glance and can prove an image with one
  action; a verified target is required before a normal custom-image deploy in
  that target, preventing a known-bad image from reaching production.
- One new swallow-owned collection and one new durable Workflow kind; both follow
  existing overlay and operation patterns.
- `deployTarget` becomes the vocabulary in new surface while `ephemeral` keeps
  working, so existing clients and platform deploys are unaffected.
- Verification runs a real deploy: it consumes a ready Server for several minutes.
