# Provider Data Overlay and Provisioning/Platform Dashboard UX

Consolidated long-term plan. Synthesized from the manuscript drafts listed under
Source Scope; supersedes those drafts.

## 1. Purpose

Establish a reusable, repo-wide strategy for how swallow combines provider-owned
facts with swallow-owned supplementary data, deliver its first reference
implementation (the OS Image overlay), and align the provisioning and platform
dashboard UX (copy, tables, and detail views) with the resulting model.

The through-line: the provider stays authoritative for its own facts; swallow
owns only the supplementary/display data the provider cannot hold, and merges the
two at read time. UX copy must reflect the provider-neutral, intent-accurate
model rather than vendor-specific or misleading wording.

## 2. Source Scope

Consolidated from:

- `20260912-provider-data-overlay.md` — the Provider Data Overlay refactor
  (ADR 025 + glossary), the OS Image overlay (name / OS / release / tags), OS
  image multi-select bulk operations, table refinements, and mirroring the
  deployed image name onto the Server projection. Dominant source.
- `20260912-delete-image-warning-copy.md` — provider-neutral, correctly-tensed
  copy for single and bulk OS Image deletion.
- `20260913-platform-uninstall-release-warning.md` — platform uninstall dialog
  copy describing the mutually exclusive "uninstall software" vs "release
  servers" paths, and removal of hardcoded `k0s` wording.

## 3. Consolidated Background

swallow integrates external systems (provisioners such as MAAS) rather than
rebuilding them. Per ADR 001 each stored field is either owned data (swallow is
the source of truth, no staleness) or a mirrored fact (a cached copy carrying
source and `observedAt`), and fleet-scale reads use the reconcile cache rather
than per-request provider fan-out. Per ADR 009 OS Images are a live, provider-
owned catalog that is not mirrored.

That left a recurring gap: a provider is authoritative for an entity but offers
no way to attach the small presentation/bookkeeping data operators want on top of
it (the archetype: MAAS cannot rename an image, and its labels for OS/release are
provider-owned strings that read poorly for custom images). The dashboard also
carried vendor-specific and tense-incorrect copy (MAAS "custom image" wording;
k0s-specific uninstall wording; deletion phrased as already-done).

## 4. Confirmed Decisions

- Read model stays reconcile-cache for fleet lists plus live read for single-
  entity/detail; no live pass-through (consistent with ADR 001).
- A provider-owned fact may carry a swallow-owned overlay merged at read
  (ADR 025), which refines ADR 001 (the overlay is owned data, not a mirror) and
  ADR 009 (an image may carry swallow display data without mirroring the artifact).
- OS Image is the first reference overlay: name, OS family, and release are
  per-field display overrides; tags are additive swallow-owned labels with no
  provider counterpart. The deployable image identity (`id`) is never overridable.
- The Server list merges deployment state and the deployed OS image name into one
  column on purpose (density + at-a-glance OS); the Server detail page must not
  merge them — state, deployed OS, and ephemeral each get their own field.
- The deployed image's effective name is mirrored onto the Server projection by
  reconcile (one catalog read per integration per pass), so the fleet list shows a
  real name without per-request fan-out. This is a narrow, ADR-sanctioned mirror.
- OS Image deletion copy is provider-neutral and prospective; deletion eligibility
  and behavior are unchanged.
- Platform uninstall copy describes two mutually exclusive paths (uninstall
  software keeping servers, or release servers which skips software uninstall) and
  is platform-appropriate/generic rather than k0s-specific; payloads unchanged.
- Checkbox controls default to Chakra `sm` (a 20% smaller box, same label size);
  table frames use the search-input corner radius (`radii.sm`).
- The Server detail page uses default Chakra tab styling (no custom pill tabs, no
  forced horizontal scrollbar) and drops the identity summary table above the tabs.

## 5. Architecture and Design Principles

- Provider Data Overlay (ADR 025): overlay is owned data keyed by the provider's
  identity for the entity; the read fetches provider data (live or reconcile
  cache) and layers the overlay with explicit one-directional precedence
  (`overlay ?? provider` per field); the response also carries the provider value
  so a client can show and reset an override; overlays never mutate the provider
  and are pruned when the owning entity is deleted.
- Capability split: a missing provider display/bookkeeping capability is filled by
  an overlay; a missing operation capability is refused via `ProviderCapabilities`
  and surfaced as unavailable (unchanged).
- Mirroring for fleet reads (ADR 001): derived display data needed across the
  fleet (the deployed image's effective name) is mirrored during reconcile with
  `observedAt`, never resolved per request; the catalog itself stays live-read.
- Clean Architecture boundaries and the reuse-first rule hold: overlay logic lives
  in provisioning domain/application/infra; the dashboard consumes the provider-
  owned contract; shared UI (SelectionToolbar, Checkbox, ResourceCard, badges) is
  extended, not duplicated.
- UX copy conveys domain intent, not vendor terms; destructive dialogs keep typed
  confirmation, accessible labels, and Clean Architecture layering.

## 6. Functional Scope

- OS Image catalog returns effective `name`/`osSystem`/`release` plus `provider*`
  and `custom*` fields, and a swallow-owned `tags` array.
- Edit an OS Image overlay (name, OS, release, tags) and reset it to provider
  values; delete a provider-owned custom image (unchanged eligibility) which also
  prunes its overlay.
- OS Image multi-select: bulk delete (custom images only) and bulk reset overrides
  (images carrying any override/tag), with per-item fan-out and a summary toast.
- OS Image table: selection column with left padding, full release string, Tags
  column shown by default, Release column hidden by default (toggleable), custom
  names shown without a "renamed from" note.
- Server list "Deployment" column shows the deployed image name (mirrored),
  falling back to OS + release then a state label; ephemeral shown as a compact
  icon.
- Server detail page: default tabs, no info table above tabs, deployment state and
  deployed OS and ephemeral each shown as separate fields; "Deployed OS" shows the
  image name, not the id-like `osSystem/distroSeries`.
- OS Image delete dialogs and platform uninstall dialogs use provider-neutral,
  intent-accurate, correctly-tensed copy.

## 7. Constraints and Rules

- Overlay is never authoritative for a provider write and never changes what an
  image deploys; setting/clearing is swallow-local.
- No per-request provider fan-out for fleet lists; the deployed-image-name mirror
  runs only in reconcile (one catalog read per integration per pass) and degrades
  to empty names if the catalog read fails.
- `swallow_`-style trusted boundaries and existing ADR 001/009 rules remain; this
  work refines, not replaces, them (documented in ADR 009/025).
- UI changes must pass the dashboard completion gate (JSDoc, accessibility, real
  data, layering) and Go changes the api-server comment gate; status is never
  conveyed by colour alone.
- Copy-only and behavior changes must leave request payloads, routes, domain
  values, and destructive-confirmation semantics unchanged unless explicitly in
  scope.

## 8. Data Model and Format Notes

- `OSImageOverlay { IntegrationID, ImageID, Architecture, DisplayName, OSSystem,
  Release, Tags[], UpdatedAt }`; `HasOverride()` includes tags; Mongo collection
  `os_image_overlays` with a unique index on `(integrationId, imageId,
  architecture)`.
- Overlay validation: each override/tag <= 200 chars; tags trimmed, de-duplicated
  (order preserved), max 50; an all-empty edit deletes the overlay.
- `OSImageItem` DTO: `id`, `name`, `providerName`, `customName?`, `osSystem`,
  `providerOsSystem`, `customOsSystem?`, `release`, `providerRelease`,
  `customRelease?`, `tags[]` (always a non-null array), `architecture`.
- `ProvisioningStatus.DeployedImageName` (domain, Mongo `deployedImageName`, view
  DTO) is the effective deployed-image display name, mirrored by reconcile with
  the axis `observedAt`; empty when not deployed or unresolved.
- Machine-to-catalog matching normalizes architecture to its primary token
  (`amd64/generic` -> `amd64`) with an id/OS+release/id fallback chain.

## 9. CLI / API / Config Notes

- `GET /api/v1/provisioning/images` returns the overlay-merged fields and `tags`.
- `PATCH /api/v1/provisioning/images/overlay?integrationId&imageId&architecture`
  with body `{ name?, osSystem?, release?, tags? }`; empty field clears that
  override, empty/omitted `tags` clears tags, all-empty removes the overlay (same
  effect as DELETE). `DELETE /api/v1/provisioning/images/overlay` clears it.
- `DELETE /api/v1/provisioning/images` also prunes the overlay; deletion is refused
  for non-deletable images as before.
- Domain error renamed `ErrOSImageNameInvalid` -> `ErrOSImageOverlayInvalid`
  (mapped to 400).
- servers-list contract documents `deployedImageName` on the provisioning axis.
- Platform uninstall endpoints and payloads are unchanged (copy only).

## 10. Implementation Plan

1. Governance: ADR 025 (refine ADR 001/009), glossary `provider-data-overlay` +
   `os-image` updates + outline, decisions README — done.
2. Backend overlay: domain type + repository port + sentinel error; Mongo repo and
   index; list use case merge; set/clear use case; delete-image prune — done.
3. Delivery + contract: overlay handlers and routes; provisioning contract — done.
4. Deployed-image-name mirror: `ProvisioningStatus.DeployedImageName`; reconcile
   resolver (catalog + overlay, primary-arch match); carry-forward on
   deploy/refresh; view DTO; servers-list contract — done.
5. Dashboard: repository port/adapter; `OSImage` type; edit dialog (name/OS/
   release/tags) + reset; multi-select bulk delete/reset; table columns (tags on,
   release off), selection spacing, full release; Server list deployment name;
   Server detail default tabs, separate state/OS/ephemeral fields, remove info
   table.
6. UX copy: provider-neutral, prospective OS Image delete dialogs; platform
   uninstall dialog release-vs-uninstall paths and generic wording; correct stale
   dashboard port/adapter comments.
7. Shared control polish: Checkbox default `sm`; table frame radius.
8. Tests/gates: Go use-case/repo/handler/reconcile tests; dashboard lint/build and
   targeted E2E; component completion gates.

## 11. Non-goals

- Live pass-through replacing the reconcile cache.
- Other integrations/providers (e.g. Ironic) and other overlays (e.g. Server
  display metadata) — future work under the same ADR.
- Bulk deploy / bulk rename of images (each needs a per-item target or distinct
  values).
- Any change to platform uninstall/release request payloads, routes, or backend
  behavior (copy only).

## 12. Open Questions

- None recorded from the source drafts. Future overlays should confirm their
  fleet-read cost against ADR 001 before mirroring additional derived fields.

## 13. Future Work

- Extend the overlay pattern to additional integrations/providers and to Server
  display metadata (name/tags), reusing ADR 025's merge/ownership rules.
- Optional image-tag filtering and cross-image tag aggregation in the catalog UI.
- Revisit provider-title resolution for externally deployed images if a cheaper
  mirror than reconcile is ever needed.
- Broader adoption of default tab styling across other detail pages if desired.
