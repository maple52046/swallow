# OS Image Deletion and Platform List UX

## 1. Purpose

Consolidate the 2026-09-11 plans that improve two operator surfaces in the swallow
dashboard: the provisioning **OS image catalog** (`/provisioning/images`) and the
**platform list** (`/platforms`). Together they add a real destructive action
(delete an OS image), rework the image catalog table, and make the platform list
filterable, multi-selectable for bulk lifecycle actions, and consistently ordered.
This document is the long-term record of what was decided, why, and where the
boundaries are.

## 2. Source Scope

Consolidated from the manuscripts under `docs/plans/manuscripts/`:

- `20260911-os-image-delete-and-catalog-ui.md` — delete an OS image + catalog UI
  rework (`api-server` provisioning + `dashboard` + `docs`).
- `20260911-platform-list-filter-multiselect-sort.md` — platform list type filter,
  multi-select bulk uninstall/delete, and lifecycle ordering (`dashboard` only).

Both were implemented and verified in the same session.

## 3. Consolidated Background

- OS images are **provider-owned, read-only live proxies**: swallow never persists
  them. The catalog is served by `GET /api/v1/provisioning/images?integrationId=…`
  and backed by MAAS `GET /boot-resources/`. An uploaded (custom) image keeps its
  provider resource name as its `id` and reports `osSystem: "custom"`.
- Provider optional behaviours follow a **capability-interface** pattern: a base
  `OSProvisioningProvider` plus optional interfaces reached by type assertion, each
  paired with a `ProviderCapabilities` flag (e.g. `MachineRemover` /
  `MachineRemoval`). New provider actions mirror this pattern.
- Platforms expose a lifecycle derived from durable deploy/uninstall operations
  (`registered`, `deploying`, `deploy_failed`, `active`, `uninstalling`,
  `uninstall_failed`, `uninstalled`) and an `origin` (`registered` | `deployed`).
  Per-platform `uninstallPlatform` / `deletePlatform` already exist; there is **no**
  bulk platform endpoint.

## 4. Confirmed Decisions

- **Image rename is dropped.** Verified empirically against the live MAAS
  `GET /MAAS/api/2.0/describe/`: the individual boot-resource handler exposes only
  `GET` and `DELETE` — no `PUT`/`PATCH`/update. MAAS's own UI cannot rename custom
  images either, and swallow does not persist image metadata. The user chose
  `drop_rename`. Any future rename would require a swallow-owned display-name alias
  (new persistence + contract + ADR) — see Future Work.
- **Delete is scoped to uploaded custom images only.** Synced images are
  provider-owned mirrors MAAS would re-sync, so the adapter refuses them.
- **Image catalog columns are separate `Name` and `Image ID`** (an earlier merged
  single field was reverted on request), with a **column-visibility picker** and
  **left-aligned icon-button actions** using lucide icons.
- **Delete action is always shown but disabled** (`isAriaDisabled`) for
  non-custom images, so every row has a consistent action set.
- **Platform list bulk actions** fan out over the existing per-platform API
  (`Promise.allSettled`); there is no new bulk endpoint.
- **Platform ordering**: `deploying` floats to the top, `uninstalled` always sinks
  to the bottom, everything else between; within a band the prior issue-first then
  name ordering applies.
- **The platform Name cell no longer shows the `origin` subtitle** ("Swallow
  deployed" / "Registered"), since external-platform management is not a current
  feature.

## 5. Architecture and Design Principles

- **api-server** keeps Clean Architecture layering: domain port + optional
  capability interface, an application use case that type-asserts the capability
  (else a `ProviderErrorRejected` "unsupported" refusal), a MAAS infra adapter that
  owns provider translation, and a thin delivery handler. The API contract in the
  `api-server` component is the source of truth.
- **Refuse, don't silently drop**: an action a provider cannot honour returns a
  clear rejection rather than a no-op.
- **Dashboard** follows its existing component conventions: shared `CopyButton`,
  PatternFly `Dropdown` + `DropdownItem hasCheckbox` for checkbox menus (PF6 keeps
  the menu open on select, closing only on outside-click/Escape/Tab), `useToast`
  for summaries, and per-target fan-out mirroring `useServerBulkActions`.
- **Column sizing via PatternFly custom properties**: PatternFly applies
  `width: var(--pf-v6-c-table--cell--Width)` to every cell, so to actually resize a
  column you override that variable (and `--pf-v6-c-table--cell--MinWidth`) rather
  than setting `width` directly, which loses on cascade order.

## 6. Functional Scope

OS image catalog:

- Delete a provider-owned (uploaded/custom) OS image, with a danger confirmation
  modal (permanent, irreversible; inline error; loading state) and a success toast
  that refreshes the catalog.
- Separate `Name` and `Image ID` columns; an inline copy button sits tight after
  the (long, unspaced) image ID via `overflow-wrap: anywhere`.
- Column-visibility picker persisted in `localStorage` (`sw.osImages.visibleColumns`),
  always keeping at least one column; a single `ImageColumn[]` model renders header
  and body in sync.
- Left-aligned icon-button actions (lucide): Deploy (`Rocket`), Create template
  (`FilePlus2`), Delete (`Trash2`, danger; disabled for non-custom). Fixed
  Actions/Refreshed columns; capped+truncated Release; min-width Image ID.

Platform list:

- Type filter (Kubernetes / Slurm) as a checkbox dropdown with a count badge; empty
  selection means all; changing it clears selection.
- Multi-select: header select-all (indeterminate when partial) + per-row checkbox
  (does not trigger row navigation); a minimal-width selection column.
- Bulk toolbar when a selection exists: "N selected", Uninstall (disabled when none
  of the selected are uninstall-eligible), Delete (danger), Clear selection.
- Bulk confirmation dialog: lists affected platforms, typed-verb gate
  (`uninstall`/`delete`), uninstall reuses the standalone "also release servers"
  option + release options, lists skipped ineligible platforms, and stays open with
  an inline error only when every target failed.
- Lifecycle ordering with uninstalled always last.
- Minor delivered UI polish under the same topics: platform Name `origin` subtitle
  removed; selection columns narrowed to the checkbox plus a little padding (platform
  list via the cell width custom property; server list via its sticky fixed width
  with the adjacent sticky column offset kept in sync); the server-list power icon
  button centred by collapsing PatternFly's `__text` wrapper to a zero-line-height
  flex box.

## 7. Constraints and Rules

- Do **not** delete or rename provider data beyond the explicit, confirmed action;
  never touch synced images. Do not perform a real deletion of an in-use image
  without explicit user approval.
- Rename must not be faked; it is genuinely unsupported by the provider.
- Bulk platform actions must never target a hidden (filtered-out) row — clearing
  selection on filter change enforces this.
- Uninstall eligibility is governed by `platformUninstallDisabledReason`; delete is
  always allowed (removes only the swallow record; hosts untouched).
- Dashboard changes must pass `tsc -b` and `eslint`; api-server changes must pass
  `go build ./...` and `go test ./...`.
- Follow the AGENTS.md reading workflow and the component API-contract ownership;
  the `api-server` provisioning contract is authoritative for the delete endpoint.

## 8. Data Model and Format Notes

- Domain `OSImage.ID` is the MAAS resource **name** (e.g. `custom/<name>` or the
  bare uploaded name), not the numeric boot-resource id. Deletion resolves the
  numeric id from `GET /boot-resources/` by matching `Name == imageID`, CPU part of
  `Architecture == architecture`, and `Type == Uploaded`.
- `ProviderCapabilities` gains `ImageRemoval bool`, paired with the new
  `OSImageRemover { DeleteOSImage(ctx, imageID, architecture string) error }`.
- Platform lifecycle rank for ordering: `deploying` = 0, `uninstalled` = 2, all
  others = 1.

## 9. CLI / API / Config Notes

- New endpoint: `DELETE /api/v1/provisioning/images?integrationId&imageId&architecture`.
  Query parameters are used because an `imageId` can contain `/` and one image name
  can back several architectures. Returns `204` on success; `400` for any missing
  parameter or a provider refusal (`validation_error` carrying the provider's own
  wording — e.g. only uploaded custom images are removable); `404` unknown
  integration; `503` provider transport failure. Rename is intentionally absent.
- Dashboard port/adapter: `ProvisioningRepository.deleteOSImage(integrationId,
  imageId, architecture)` → the DELETE call with query params.
- Client-side config: OS image column visibility persisted in `localStorage` under
  `sw.osImages.visibleColumns`.

## 10. Implementation Plan

Delivered in this order (all completed):

1. api-server domain: add `OSImageRemover` + `ImageRemoval` capability.
2. MAAS adapter: implement `DeleteOSImage` (resolve numeric id, refuse synced /
   missing) and set `ImageRemoval: true`.
3. Application: `DeleteOSImageUseCase` (type-assert remover; `unsupported` fallback).
4. Delivery + wiring: `DeleteImage` handler, `api.go` route, `tests/setup_test.go`.
5. Contract: document `DELETE /provisioning/images` in the provisioning contract.
6. Dashboard: `deleteOSImage` on port + adapter; rework `OSImagesPage`
   (Name/Image ID columns, copy button, column picker, icon actions, delete modal,
   column sizing).
7. Tests: MAAS delete (resolve + refuse), capability assertions/fakes, route-level
   validation + success; then `go build`/`go test` and dashboard `tsc`/`eslint`.
8. Platform list: `lifecycleRank` sort; type filter dropdown; multi-select
   checkboxes; `usePlatformBulkActions` hook; `PlatformBulkActionDialog`; bulk
   toolbar; filtered-empty state.
9. Platform list polish: remove Name `origin` subtitle; narrow selection column;
   centre server-list power icon.

## 11. Non-goals

- No image **rename** (provider cannot support it; no swallow-side alias built).
- No swallow-side persistence of image metadata.
- No new bulk platform API endpoint (fan-out over existing per-platform calls).
- No external-platform management features (hence dropping the `origin` subtitle).
- No changes to synced-image handling beyond refusing their deletion.

## 12. Open Questions

- Bulk platform confirmation currently gates on typing the action verb
  (`uninstall`/`delete`) rather than each platform name; whether a stricter gate is
  wanted (e.g. count or per-item re-confirmation) is left to the user.
- Exact minimal widths/paddings for the narrowed selection columns and the power
  icon centring may need small visual tuning once viewed in the browser (browser
  MCP was unavailable, so no screenshot verification was possible this session).

## 13. Future Work

- If image rename is ever required, design a **swallow-owned display-name alias**:
  a persisted `(integrationId, imageId) → display name` mapping shown in the catalog,
  with a new API contract and an ADR — explicitly deviating from the current
  "images are not persisted" principle.
- Optionally apply the same selection-column sizing pattern uniformly across other
  list tables (e.g. the server list) for visual consistency.
- Consider surfacing `ImageRemoval` (and other curated capabilities) in the
  provisioner detail capabilities DTO if a client needs to hide the delete action by
  capability rather than by `osSystem`.
