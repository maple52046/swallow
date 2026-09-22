# OS Image Verification, Provisioning Lifecycle Integrity, and Deploy Diagnostics — Consolidated Plan

## 1. Purpose

Provide a single long-term plan for making Swallow's OS-image deploy path
trustworthy end to end. It unifies three threads that turned out to be one arc:
the work started as OS-image *verification* (prove a custom image works before
trusting it), which then exposed a set of provisioning/Server *lifecycle holes*
(un-recoverable states, dishonest projections, no failure compensation), and in
parallel a *deploy-diagnostics / preflight* thread that refuses undeployable
images up front and explains provider-side install failures.

The unifying principle is the same throughout: **Swallow owns the attestation,
the recovery policy, and the operator-facing truth**, while the provisioner only
executes the primitives Swallow selects and reports raw facts Swallow normalizes.
A deploy should either be proven safe before dispatch or fail *legibly*, never
strand a Server or lie about its state.

## 2. Source Scope

Consolidated from the AI plan manuscripts under `docs/plans/manuscripts/`:

- `20260921-image-verification.md` — OS Image Verification and the Deploy Target
  (`disk` | `ram`) vocabulary: verify a custom image by a real proving deploy,
  gate unverified custom images per target, dashboard verify UX.
- `20260921-provisioning-lifecycle-holes.md` — the lifecycle holes the
  verification work uncovered: `allocated` as an un-recoverable dead-end,
  dishonest "Deployed" projections, no failure compensation in `verify-os-image`,
  the borrow/return contract for verification, failed-target facts, disk-erase
  recovery escalation, `errorDescription` diagnostics, `keepServer`, SSH-readiness
  observability, and provisioner-step diagnostics on the Operation detail page.
- `20260921-rocky-deploy-diagnostics.md` — image-problem diagnostics and deploy
  preflight: image completeness metadata, architecture/completeness acceptance
  checks, install-failure classification, and clearing stuck `deploying`
  projections.

`README.md` in that directory is the consolidation spec and is not a source plan.

## 3. Consolidated Background

A disk deploy of a defective custom Rocky image failed opaquely inside MAAS
(missing grub + broken install-time network) while an in-memory boot succeeded,
which raised the core question: how does an operator know an image is trustworthy
for a given deploy target *before* relying on it? The answer — verification by a
real proving deploy — surfaced a chain of related defects observed live in the lab
(`lab-control-2`, `lab-compute-3`, `lab-compute-4`):

- `allocated` was an un-modeled dead-end: absent from the recovery matrix, the
  dashboard gate, and the deploy-observe switch, so a reserved-but-not-deployed
  machine could neither advance nor be released.
- The Server list painted "Deployed" from a stale `deployment.succeeded`
  projection even when the provisioner was no longer `deployed`, because stale
  deployment axes were only cleared when the provider was `ready`.
- `verify-os-image` had no failure compensation: hard `DependsOn`-success edges
  plus a *retryable* proving failure parked the Operation in `requires_attention`,
  never recording, never releasing, and holding the target busy (so Release
  returned `409`).
- ADR 033 ("do not auto-recover after a deploy failure") was misapplied to
  verification borrowing, whose contract is *borrow a ready Server and return it*.
- Undeployable images (incomplete uploads, wrong CPU architecture) were accepted
  and only failed later at the provider, with the failure reason lost.

These are addressed together because they share one deploy resolve chokepoint, one
verification fact model, and one operator-facing truth about Server state.

## 4. Confirmed Decisions

- **Deploy Target is the operator vocabulary.** `DeployTarget` (`disk` | `ram`) is
  canonical; `ram` is the fact that the wire, MAAS adapter, MongoDB, durable
  snapshot, and k0s var still call `ephemeral`. They map one-to-one at every
  boundary (`disk ↔ false`, `ram ↔ true`). DTOs accept `deployTarget` (preferred)
  and keep `ephemeral` as a still-accepted deprecated alias. Dashboard label for
  `ram` is "RAM deploy (ephemeral)".
- **Verification is a Swallow-owned attestation**, not a provider capability:
  "Swallow ran a real deploy in this target and it worked." Keyed by
  `(integrationId, imageId, architecture)` per the Provider Data Overlay identity
  rule (ADR 025), stored in its own `os_image_verifications` collection separate
  from the display overlay so pruning one never erases the other.
- **Only custom images are gated.** Synced provider images are provider-trusted
  and are never verified or blocked.
- **Verification = borrow / return.** The proving deploy borrows an admin-chosen
  ready Server; whatever the outcome (success, failure, or cancel) the Server is
  returned to `ready` by default. Only a *successful* provision records the
  attestation.
- **A proving failure is a verification failure, not a transient error.** Under
  `VerificationRun`, `deployment_failed` / `deployment_ssh_unreachable` are made
  non-retryable so the DAG runs its compensation and the Operation reaches a
  terminal `failed`/`partially_succeeded`; a short-lived `provider_unavailable`
  stays retryable.
- **Failed targets are persisted facts too** (newer decision, supersedes the
  earlier "provision-os error is enough" note): record per-target *failure*
  facts so the catalog can distinguish "proven not to deploy" from "never tried".
  Verified and failed are mutually exclusive per target (recording one clears the
  other).
- **`allocated` is recoverable.** It joins `failed` / `deployed` / `broken` /
  `rescue` as a legal source for Recover / Release; a normal (non-verification)
  deploy failure still does *not* auto-recover (ADR 033 preserved).
- **Recovery escalates when a plain release cannot converge.** For
  `failed`/`deployed`/`allocated`, when a straight release cannot converge
  (`release_failed`, typically a disk that will not erase), `recover-server`
  escalates once to `mark-broken → mark-fixed` (return to `ready` without erasing),
  mirroring the rescue-stuck path, bounded to avoid loops.
- **`keepServer` is opt-in.** Verification auto-releases by default;
  `keepServer=true` omits the `recover-server` step so a success leaves the Server
  `deployed` (for continued use/inspection) and a failure leaves it `failed`
  (one-click Recover from server detail).
- **The UI must not lie.** A stale `deployment.succeeded` must never render as
  "Deployed"; `allocated` must be recognizable at a glance; failure states carry
  the provider's reason.
- **Refuse undeployable images at acceptance.** The shared deploy resolve blocks
  an incomplete image and a known CPU-architecture mismatch with a Swallow reason,
  covering batch deploy, the durable operation, and platform deploy.
- **`lease_fenced` stays non-retryable** (fencing is a safety mechanism); its
  consequence (a stranded borrowed machine) is recoverable via recovery escalation
  plus cancel-and-recover.

## 5. Architecture and Design Principles

- **Swallow owns policy; the provisioner executes primitives.** Recovery, gating,
  and verification live in Swallow's domain/application layers; MAAS only deploys,
  releases, and reports raw facts.
- **Provider Data Overlay identity (ADR 025)** for verification rows, keyed by
  `(integrationId, imageId, architecture)`, in a dedicated collection.
- **Provider recovery policy (ADR 033), extended by ADR 036** to include
  `allocated` and the borrow/return contract; deploy-diagnostics/preflight is
  ADR 034.
- **Task DAG with `ContinueOn`.** The Operation workflow engine resolves pending
  tasks by dependency *outcome*: `verify-os-image` runs
  `record-image-verification` on success, `record-image-verification-failure` on
  failure (exactly one), then `recover-server` after either — no retryable-error
  hack required for failure-path compensation. Cancel also returns the borrowed
  Server.
- **Normalized provider failure vs. Swallow fact.** The provider's machine-level
  reason is normalized (`errorDescription`, install-failure classification) and
  kept distinct from Swallow-owned verification facts.
- **Full-chain propagation through Clean Architecture layers.** New facts
  (`errorDescription`, `Complete`, verified/failed targets, deployment `Code`)
  travel domain → infra (MAAS mapping, Mongo) → application (reconcile, projection)
  → delivery (DTO, SSE) → contract → dashboard, populated only where meaningful
  (for example `errorDescription` only in failure states).
- **One deploy resolve chokepoint.** `DeployServersUseCase.preflight` is the single
  place the verification gate, architecture check, and completeness check apply, so
  every deploy entry point inherits them.

## 6. Functional Scope

- **Verification workflow (`verify-os-image`).** For an admin-chosen ready Server +
  target: `provision-os` (a real deploy carrying `VerificationRun` so the
  unverified-custom gate does not block the proving run) → exactly one of
  `record-image-verification` (success) or `record-image-verification-failure`
  (failure) → `recover-server` after either (omitted when `keepServer=true`).
  Acceptance preflight: Server exists, `ready`, unlocked, same Integration as the
  image, architecture matches.
- **Deploy gating.** In the shared resolve, a custom image not verified for the
  requested target is refused (`409`, naming the target); a known architecture
  mismatch is refused; an incomplete image is refused (`409`, naming the image);
  synced images bypass. The deprecated single-server `POST /servers/{id}/deploy` is
  superseded by the gated durable path.
- **Recovery.** `allocated` added to Recover/Release sources; `RefreshServer`
  before Launch; escalation `mark-broken → mark-fixed` on `release_failed`; a
  cancel-and-recover action that cancels a blocking parked Operation and then
  Recovers.
- **Honest projections.** `DeploymentBadge` shows `Allocated`; reconcile clears a
  stale terminal deployment axis on `allocated`; reconcile clears a
  `deploying`/`verifying` axis when the machine is idle (Ready/Allocated) and the
  axis is older than a grace window (fixing a stuck deploy after cancel/abort);
  `observeDeploy` handles `allocated`.
- **Image-problem diagnostics.** The deploy failed/broken branch reads provider
  machine events and classifies the cause as `deployment_install_failed`
  (curtin/install) or `deployment_image_unusable` (missing boot/kernel), including
  the provider's own lines, with `deployment_failed` as fallback; the failed
  Step's stable error `code` is preserved onto the deployment axis.
- **Diagnostics surfaces (dashboard).**
  - OS Images page: Verification column with five distinct states — spinner
    (verifying), green check (verified), amber (attention), red ✗ (failed), grey
    ban (never verified) — plus a "Verify" action (pick ready Server + target,
    `keepServer` checkbox).
  - Server detail: `ProviderFailureAlert` (provider reason, disk-erase guidance,
    the blocking active Operation, one-click cancel-and-recover); an in-progress
    info alert for `deploying`/`verifying` with a "View operation" link;
    `SummaryCards` "Provider reason" row; failure-state badge tooltips carry the
    reason.
  - Operation detail: for non-ansible provisioner/internal steps, Stderr shows a
    `StepErrorReport` (Code / Stage / Retryable + message) instead of a misleading
    "No errors"; Events embeds the target Server's provider event timeline inline
    (no link back to server detail — the circular navigation was the rejected
    first design); Stdout shows a short note.

## 7. Constraints and Rules

- Do not rename the existing `ephemeral` wire / BSON / durable-snapshot / MAAS
  fields or the k0s var; `deployTarget` is the new vocabulary with a compatibility
  mapping.
- Synced provider images are never gated, never verified, never blocked.
- No curtin / storage / OS-install changes and no auto-fallback to RAM to make a
  bad image install.
- A normal (non-verification) deploy failure never auto-Releases/Recovers (ADR
  033); only verification borrowing returns its Server automatically.
- `errorDescription` is mirrored only in failure states so healthy machines never
  carry a stale error.
- `VerificationRun` must be propagated into the frozen durable input, or the
  executor's re-resolve re-applies the unverified-custom gate and the verify
  workflow blocks itself (pinned regression).
- Recovery escalation is bounded (one `mark-broken → mark-fixed` step) to avoid
  loops.
- Verified and failed targets are mutually exclusive per target.

## 8. Data Model and Format Notes

- **`os_image_verifications` collection**, keyed `(integrationId, imageId,
  architecture)`. Per-target verified evidence `{ verifiedAt, operationId,
  serverId }`; `FailedTargets map[DeployTarget]OSImageVerificationFailure` with
  `FailedTargetsList()`. Repo `RecordTarget` / `RecordFailedTarget` each `$unset`
  the opposing sub-document. Rows are pruned when the image is deleted.
- **`OSImage`** gains `Complete` (the provider's "fully staged" signal, mapped from
  boot-resource *set* completeness; kernel variants collapse and are complete if
  any variant is complete) and `BootResourceKinds`, plus the existing `SizeBytes`
  (latest complete set). `bootResourceFileJSON.filetype` feeds the kind metadata.
- **Catalog projection** surfaces `verifiedDeployTargets` and `failedDeployTargets`
  (both always arrays; a target appears in at most one), `complete`, and
  `bootResourceKinds`.
- **Provisioning axis** gains `errorDescription`, sourced from MAAS
  `error_description` and set only in failure states, propagated through domain /
  reconcile / refresh / Mongo / SSE / DTO.
- **Server deployment axis** preserves the failed Step's stable error `code`; while
  waiting on SSH readiness it records a non-terminal stage/reason ("OS installed,
  waiting for SSH") with no code.

## 9. CLI / API / Config Notes

- `POST /provisioning/image-verifications` → `202` + `operationId`; request body
  accepts `keepServer` (default `false`).
- Deploy request DTOs accept `deployTarget` (`disk` | `ram`); `ephemeral` remains a
  deprecated alias.
- Shared deploy resolve returns `409` for: unverified custom image for the target,
  known architecture mismatch, and incomplete image (each with a Swallow reason).
- `POST /provisioning/recover-operations` accepts `allocated` sources (previously
  `400`), returning `202`.
- `servers.getProviderEvents` backs the inline provider timeline on the Operation
  detail Events tab.
- Contracts updated: `provisioning.md` (Deploy Target, verification workflow,
  catalog `verifiedDeployTargets`/`failedDeployTargets`/`complete`/
  `bootResourceKinds`, acceptance completeness + architecture + verification
  checks, `keepServer`), `workflows.md` (verify-os-image flow, Task error schema),
  `servers-list.md` (`errorDescription`), `server-detail-actions.md` (axis code +
  failure codes). Glossary: `os-image.md` (deployability metadata),
  `release.md` / `provider-recovery.md` / `server-status.md` (`allocated`). ADRs:
  034 (deploy diagnostics/preflight), 036 (lifecycle integrity), with a
  cross-reference from 033.

## 10. Implementation Plan

1. **Verification foundation.** Add the `os_image_verifications` collection, domain
   model, repo, catalog projection (`verifiedDeployTargets`), the Deploy Target
   vocabulary and DTO mapping, and the `verify-os-image` launcher/workflow
   (provision → record → release). Wire the unverified-custom gate into the shared
   deploy resolve; propagate `VerificationRun` into the frozen durable input.
2. **Recovery matrix + honest projections.** Add `allocated` to the recovery
   policy, dashboard gate, contract, glossary, and ADR 036; `RefreshServer` before
   Launch. Make projections honest: `DeploymentBadge` shows Allocated; reconcile
   clears stale terminal deployment on `allocated` and clears stale
   `deploying`/`verifying` axes past a grace window; `observeDeploy` handles
   `allocated`.
3. **Failure compensation.** Add `ContinueOn` to the Task DAG engine; evolve
   `verify-os-image` to run `record-image-verification-failure` on a failed
   provision and `recover-server` after either record; make `VerificationRun`
   proving failures non-retryable; ensure cancel also returns the Server. Persist
   failed-target facts (`FailedTargets`, `record-image-verification-failure`
   executor, `failedDeployTargets` projection).
4. **Deploy diagnostics + preflight.** Preserve the deployment axis `code`; enrich
   the failed/broken branch with provider-event classification
   (`deployment_install_failed` / `deployment_image_unusable`); add
   `OSImage.Complete` + `BootResourceKinds` mapping; add the completeness and
   architecture acceptance checks at the deploy resolve chokepoint.
5. **Disk-erase escalation + `errorDescription`.** Add the bounded `mark-broken →
   mark-fixed` escalation on `release_failed`; map MAAS `error_description` into the
   failure-only `errorDescription` axis across the full chain.
6. **`keepServer`.** Add the flag to `ImageVerificationInput` / request DTO and omit
   `recover-server` when set; add the dashboard checkbox and helper/toast copy.
7. **Dashboard surfaces.** OS Images verification column (five states) + Verify
   dialog; `ProviderFailureAlert` with cancel-and-recover; in-progress info alert +
   View-operation link; Operation-detail `StepErrorReport` + inline provider events
   (no circular link); deploy-wizard warn/block on incomplete or unverified image.
8. **Docs + verification.** Update contracts, glossary, and ADRs; run
   `go build` / `go vet` / `gofmt` / `go test ./...` and dashboard `tsc` / lint /
   build; validate end to end against the live lab.

## 11. Non-goals

- Renaming any `ephemeral` field (wire / BSON / durable snapshot / MAAS / k0s).
- Gating, verifying, or blocking synced provider images.
- Any curtin / storage / OS-install change, or auto-fallback to RAM, to make a bad
  image install.
- An ephemeral-only file-kind blocker (it would miss real defects and risk
  false-blocking valid images).
- Auto Release/Recover after a normal (non-verification) deploy failure.
- Collapsing the provisioning / deployment / membership / health axes into a single
  status.
- Re-introducing the previously reverted MAAS machine-event *display* enrichment
  beyond the normalized classification and inline timeline described here.

## 12. Open Questions

- **Integration-delete verification pruning.** The site context deliberately avoids
  importing provisioning, so pruning verification rows on Integration delete would
  need a new cross-context interface. Orphan rows are harmless (never read again),
  so this is deferred; image-delete pruning (the normal lifecycle) is implemented.
- **`lease_fenced` handling.** Kept non-retryable by design; the residual risk of a
  stranded borrowed machine is mitigated by recovery escalation and
  cancel-and-recover. Whether to add explicit follow-up handling is open.
- **Empty MAAS event descriptions.** MAAS may store empty event descriptions; the
  inline timeline currently relies on timestamps + event types as the workaround.

## 13. Future Work

- Implement Integration-delete verification pruning as a proper cross-context
  interface if orphan rows ever become observable or costly.
- Add first-class `lease_fenced` follow-up handling (e.g. an automatic
  return-to-ready sweep for borrowed machines abandoned by a worker restart).
- Consider surfacing `complete` / `bootResourceKinds` more richly in the OS Images
  UI (pre-deploy warnings beyond the acceptance-time block).
