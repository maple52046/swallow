# 035. OS image verification and deploy-target vocabulary

- Status: Accepted
- Date: 2026-09-21

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
  `ram` is exactly today's `ephemeral`; the two map one-to-one at every existing
  boundary (`disk ↔ ephemeral=false`, `ram ↔ ephemeral=true`). Deploy request
  DTOs accept an optional `deployTarget` (preferred) mapping onto the internal
  bool, with `ephemeral` kept as a still-accepted deprecated alias. No wire,
  BSON, durable-snapshot, MAAS, or k0s field is renamed.
- **Verification** is a swallow-owned fact (Swallow ran a real deploy and it
  worked), not a provider capability. It follows the Provider Data Overlay
  identity rule ([ADR 025](025-provider-data-overlay.md)), keyed by
  `(integrationId, imageId, architecture)`, stored separately from the display
  overlay, with per-target evidence `{ verifiedAt, operationId, serverId }`.
- A new durable `verify-os-image` Workflow proves an image for a target: a real
  `provision-os` deploy on an operator-chosen ready Server, then an internal
  `record-image-verification` finalize step, then an auto-`release-os`. The
  proving deploy is exempt from the gate below.
- A **deploy gate**: a *custom* image not verified for the requested deploy
  target is refused at deploy acceptance with a Swallow reason. Synced provider
  images are provider-trusted and never gated.

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

- Operators see per-target trust at a glance (OS Images "Verification" column)
  and can prove an image with one action; a verified target is required before a
  normal custom-image deploy in that target, preventing a known-bad image from
  reaching production.
- One new swallow-owned collection (`os_image_verifications`) and one new durable
  Workflow kind; both follow existing overlay and operation patterns.
- `deployTarget` becomes the vocabulary in new surface (verify endpoint, catalog
  projection, dashboard) while `ephemeral` keeps working, so existing clients and
  platform deploys are unaffected.
- Verification runs a real deploy: it consumes a ready Server for several minutes
  and auto-releases it; a failed verification leaves the target unverified with
  the provider's reason.

## Current status

Implemented. Deploy-target mapping, the verification model + Mongo repo, the
`verify-os-image` Workflow, the deploy gate, and the dashboard surfaces are in
place. Pruning verification on Integration delete is a small follow-up (orphan
rows are harmless); the deprecated single-server deploy endpoint is not
separately gated (superseded by the gated durable path).
