# Operator Workflows and Provider Integration

## Purpose

Provide a coherent long-term direction for Swallow's operator-facing infrastructure and
provisioning workflows. The plan joins provider capability integration, Swallow-owned intent,
live execution feedback, and stable dashboard interactions without weakening component
boundaries or inventing provider-independent behavior that the underlying systems cannot
support.

## Source Scope

This consolidation covers eight AI manuscripts written from 2026-09-13 through 2026-09-16:

- OS image upload to a Site provisioner.
- Ephemeral k0s deployment on a minimal MAAS Ubuntu image.
- Swallow-owned Zone and Pool management.
- Installation-time provisioner bootstrap and catalog correspondence.
- Live Ansible task progress for platform deployment.
- Single and batch Server tag editing.
- The Server Detail page redesign.
- Stable multi-selection toolbars for dashboard tables.

The source directory was `docs/plans/manuscripts/`. This document synthesizes decisions,
implemented outcomes, deferred work, and unresolved questions rather than concatenating the
drafts.

## Consolidated Background

Swallow owns operator intent while provider adapters realize capabilities such as machine
grouping, tagging, image management, and lifecycle actions. Provider-observed facts remain
valuable, but they do not automatically become Swallow-owned configuration. The recent work
extends that boundary in several related directions:

- Operators can manage more provider-backed resources through Swallow instead of leaving the
  dashboard for routine MAAS work.
- Long-running automation exposes honest, incremental progress rather than one opaque step.
- The dashboard uses stable, shared interaction patterns for Server details and bulk actions.
- Provider-specific data is exposed through narrow, documented contracts with explicit secret
  handling.
- Installation should eventually reconcile an existing provider with Swallow-owned catalogs so
  the post-install environment does not require manual seeding.

The ephemeral k0s lab effort also established a working reference for volatile clusters. The
retained R4 deployment used three controllers, four workers, and the reserved
`192.168.100.200/24` API VIP. It passed control-plane, etcd, CNI, DNS, Service routing,
authenticated API, and cross-worker traffic checks on memory-backed hosts.

## Confirmed Decisions

- Use capability-first behavior with a Swallow-owned fallback where ownership permits it.
  Provider adapters are driven when capable; otherwise Swallow keeps the intent locally. This
  applies to Server tags and is consistent with OS image name overlays and Zone/Pool ownership.
- OS image artifacts remain provider-owned. Swallow may drive create and delete operations but
  does not retain, mirror, or synchronize a persistent artifact copy.
- Image classification is provider-owned. In particular, the MAAS adapter determines that an
  uploaded resource is `custom`; callers do not hard-code that classification.
- Zone and Pool are Swallow-owned, Site-scoped concepts with full CRUD and Server assignment.
  Provider realization is capability-aware. Observed provider zone/pool values remain mirrors of
  the realized assignment rather than the owned catalog.
- Provisioner optional interfaces and `ProviderCapabilities` remain the extension mechanism for
  image upload, grouping, tagging, and similar capabilities.
- Ephemeral Kubernetes reuses `machinePreparation.settings.ephemeral`; it does not introduce a
  new API field. Its OS, etcd, runtime, and workload state are intentionally lost on reboot.
- Ephemeral host compatibility is fail-closed. Automation verifies cgroup v2, commands, kernel
  modules, and `k0s sysinfo` before starting nodes.
- Live Ansible progress stores compact, output-free task events in MongoDB while raw stdout,
  stderr, logs, and artifacts remain filesystem-backed. Existing dashboard polling stays in
  place; the backend supplies useful mid-run state.
- Ansible progress uses honest result counts and current play/task text. It does not fabricate a
  percentage because the total task count is not known in advance.
- Server tag batch editing uses an email-label-style tri-state model: all, some, or none across
  selected Servers. Only changed tags are sent.
- Server Detail is an operator workspace, not a raw provider dump. Summary is a fast scan; large
  network, storage, PCI, and inspection datasets belong in dedicated views.
- Management-controller access is first-class information for physical Servers. The admin-only
  live detail may return an explicitly allowlisted BMC password for manual administration. The
  dashboard masks it by default and provides explicit reveal/hide and copy controls.
- Dashboard tabs retain Chakra's default visual treatment with icon-plus-text labels. On narrow
  screens they remain swipeable without compressed labels or a visible native scrollbar.
- Multi-selection actions use one shared floating dock so selecting rows does not move tables.
  Selection state and action eligibility remain page-owned.

## Architecture and Design Principles

- Preserve Clean Architecture boundaries within every component. Cross-component behavior flows
  through provider-owned API contracts and shared domain documentation.
- Keep one MAAS transport implementation under `provisioning/infra/maas`; other feature slices
  reuse provisioning ports instead of importing provider vocabulary or duplicating clients.
- Model provider support through small optional capability interfaces and explicit capability
  flags. Unsupported operations must produce a useful error or a documented no-realization result,
  not a silent partial success.
- Separate owned intent from observed effect. Swallow-owned Zone, Pool, tag-overlay, and deployment
  intent live in owned models; Server projection fields represent reconciled provider observation.
- Keep live provider-only detail out of durable Server projections unless a separate ownership and
  staleness contract is deliberately introduced.
- Make streaming and polling idempotent. Incremental Ansible events use `(runId, seq)` upserts, and
  targeted live-projection updates must not overwrite workflow-owned task state.
- Prefer shared, composable dashboard components for repeated behavior: selection docks, tag
  editors, description lists, copy controls, status badges, and table surfaces.
- Accessibility is part of the interaction contract. Icon-only controls require accessible names
  and tooltips; status is never conveyed by color alone; keyboard access and explicit clear
  actions are preserved.

## Functional Scope

### OS image management

- Accept a multipart upload targeted at one Integration, spool it to a temporary file to calculate
  size and SHA-256, create provider metadata, and stream chunks to MAAS without loading the whole
  image into memory.
- Expose upload progress in the dashboard and return the created catalog row on success.
- Continue to support existing delete, overlay-name, catalog, and deployment behavior.

### Ephemeral Kubernetes

- Accept ephemeral intent for Ready Kubernetes targets and carry it through the existing
  deployment launcher.
- Run host compatibility checks after installing the pinned k0s binary and before bootstrap.
- Warn operators clearly that the cluster is volatile.
- Retain validation evidence for MAAS state, root filesystem, kernel, services, etcd, nodes,
  networking, DNS, Service routing, and cross-node traffic.

### Infrastructure grouping

- Manage Site-scoped Zones and Pools through shared CRUD semantics.
- Ensure, rename, and delete corresponding MAAS groups when the Integration supports grouping.
- Assign or clear a Server's Zone and Pool through Swallow and realize the change in the provider.
- Treat MAAS's resource-pool ID `0` as a valid built-in pool, using an explicit found flag rather
  than zero as a sentinel.

### Live automation progress

- Tail Ansible Runner job events during execution on an approximately two-second cadence.
- Persist bounded per-host task results and update counts plus current play/task.
- Attach external execution identity as soon as it is observed so events are available before the
  Temporal activity finishes.
- Render current work, counts, and the growing event list in the operation detail view.

### Server metadata and detail

- List, create, add, and remove editable tags through a shared single/batch tri-state editor.
- Keep provider-defined automatic tags read-only and surface provider refusals.
- Present independent operational axes, capacity, management access, hardware profile, and
  placement on Server Summary.
- Group the long action catalog into Power, Hardware checks, and State & recovery while keeping
  primary and destructive lifecycle actions discoverable.
- Link a successfully Swallow-deployed OS to its scoped OS Images catalog entry.

### Stable list selection

- Use a portalled viewport-level action dock for Servers, OS Images, and Platforms.
- Show selected count, grouped actions, and a clearly labelled clear control without adding an
  in-flow row or moving the table.
- Keep narrow-screen actions horizontally reachable without exposing a scrollbar.

## Constraints and Rules

- All relevant endpoints and dashboard routes are admin-only under the existing authentication
  model.
- Never persist or log provider credentials merely to display them. The BMC password is the only
  allowlisted secret in live Server detail; K_g values, tokens, private keys, URL credentials,
  query secrets, fragments, and unknown power parameters do not cross the adapter boundary.
- Mark live detail that may contain a password as `Cache-Control: no-store`. Keep the password out
  of projections, local storage, logs, and screenshots; mask it by default using a fixed-length
  placeholder.
- Large image uploads must remain streaming and bounded in memory. Temporary files are cleaned up,
  and long provider uploads do not inherit inappropriate short request timeouts.
- Provider errors retain their actionable classification while client responses and structured
  logs avoid raw credentials and request bodies.
- Raw Ansible output never enters MongoDB. Compact event records remain output-free and idempotent.
- Final workflow updates remain authoritative over live progress projections.
- Ephemeral image validation fails closed. Failed builds are not uploaded; failed deployments keep
  diagnostics and are cleaned up through Swallow uninstall and Server release.
- Server Lock rules remain unchanged. Tag editing is metadata and is not blocked by Server Lock;
  lifecycle actions retain their existing lock, state, capability, and confirmation gates.
- Monitoring must render real loading, unavailable, unmanaged, and no-data states; it must not
  invent history or samples.
- Table filtering, pagination, hidden-row selection, and bulk API eligibility are unchanged by the
  floating selection dock.

## Data Model and Format Notes

- `UploadOSImageRequest` carries Integration identity, name, architecture, optional title/filetype,
  file size, SHA-256, and a readable content source. The provider adapter normalizes MAAS
  architecture to `<arch>/generic`.
- `Zone` and `Pool` are Site-scoped with names unique within a Site. `ServerPlacement` refers to
  their owned identities; provider-observed grouping stays on the Server observation.
- `ServerTagOverlay` is keyed by Server ID and is merged into observed tags only for providers that
  do not support tagging. It remains inert for MAAS.
- `MachineTag` carries `Name` and `Editable`; automatic MAAS definition tags are non-editable.
- `AnsibleRunProgress` records ok/changed/failed/unreachable/skipped counts plus current play/task.
  `StreamedTaskEvent` is stored in `ansible_task_events` using `(runId, seq)` as the idempotency key.
- `Task.Live` carries the current projected execution identity and compact progress without
  replacing the workflow's durable task definition or final result.
- Provider-neutral Server detail represents BMC data as an optional `BMC` section. Allowlisted
  fields are Protocol, Address, Username, Password, Node ID, Driver, Boot type, Privilege level,
  Cipher suite, Power MAC, and an explicit availability status.
- The validated minimal Noble image matched MAAS's observed `ga-24.04` ABI
  `6.8.0-139-generic` and included the complete matching `linux-modules-extra` package without
  embedding k0s. The retained uploaded artifact was 559,855,862 bytes with SHA-256
  `cad7ff961f63fa649a02e7fa7476c58a1d209eb8afd069eb5729b0363f68f61d`.

## CLI / API / Config Notes

- Image upload: `POST /api/v1/provisioning/images` with multipart fields `integrationId`, `name`,
  `architecture`, optional `title`, optional `filetype`, and `content`; success returns `201` with
  one OS Image row.
- Image upload errors use the shared envelope: validation/unsupported/duplicate input is `400`, an
  unknown Integration is `404`, and provider transport or server failure is `503`.
- Fiber must enable streaming request bodies and a body limit suitable for multi-gigabyte images.
- Infrastructure APIs:
  - `GET/POST /api/v1/infrastructure/zones`
  - `GET/PATCH/DELETE /api/v1/infrastructure/zones/{id}`
  - `GET/POST /api/v1/infrastructure/pools`
  - `GET/PATCH/DELETE /api/v1/infrastructure/pools/{id}`
  - `PUT /api/v1/servers/{id}/placement`
- Tag APIs:
  - `GET /api/v1/provisioning/tags?siteId=`
  - `POST /api/v1/provisioning/tags`
- Server BMC information is obtained from MAAS's dedicated admin-only `power_parameters` operation
  and returned through `GET /api/v1/servers/{id}/provisioner-detail`.
- Live operation delivery continues to use the existing dashboard polling interval of roughly two
  seconds. A future SSE transport is possible but is not required by the current model.
- A future installation configuration may declare desired Zones/Pools and credentials for an
  idempotent bootstrap reconcile; its schema and credential-delivery mechanism remain undecided.

## Implementation Plan

1. Preserve the delivered provider capability foundations: image upload, grouping, tagging, live
   detail, and their active API contracts, glossary terms, ADRs, tests, and DI wiring.
2. Preserve the delivered dashboard workflows: image upload, infrastructure grouping, Server
   placement and tags, Server Detail hierarchy/actions, and the shared floating selection dock.
3. Maintain the ephemeral k0s reference flow and its fail-closed preflight. Keep the retained lab
   report and cleanup discipline as acceptance evidence for future image/kernel changes.
4. Complete or maintain live Ansible progress end to end: incremental event reader, Mongo event
   repository, executor tail loop, early external-execution projection, targeted `Task.Live`
   updates, API reads, and dashboard current-task/count/event rendering.
5. Align retention for compact Ansible events with workflow and artifact retention, including
   indexes and cleanup behavior.
6. Design installation-time provider correspondence using the Swallow API as the preferred control
   surface. Define idempotency, readiness ordering, conflict policy, desired-state config, and safe
   credential delivery before implementing provider-to-catalog import.
7. Continue regression coverage around streaming bounds, provider capability fallbacks, MAAS pool
   ID zero, read-only automatic tags, BMC secret filtering, non-jumping table selection, hidden tab
   scrollbars, accessibility, and responsive visual baselines.

## Non-goals

- Swallow does not persist a duplicate OS image artifact or become the image distribution store.
- Image upload does not become a durable Operation in the current design.
- Ephemeral clusters are not made durable across reboot, and k0s is not embedded in the base image.
- Zone/Pool ownership does not imply a continuous mirror of every provider-defined group.
- The current grouping effort does not import provider catalogs during ordinary reconciliation.
- Raw Ansible stdout is not streamed to MongoDB, and live progress does not claim a percentage.
- BMC data does not become part of the durable Server projection.
- Server Summary does not become a complete inspection, network, storage, or PCI inventory page.
- The selection dock does not redesign table rows, filters, pagination, mobile cards, backend bulk
  APIs, or action eligibility.
- Existing lifecycle, lock, release, deployment, and reconciliation semantics remain intact unless
  separately specified by their owning contracts.

## Open Questions

- Should installation-time correspondence be a one-time import, an ongoing reconcile, or a
  combination of desired-state push and existing-state import?
- When installer-declared groups and pre-existing provider groups conflict, which source is
  canonical and how should names or deletions converge?
- What readiness signal guarantees MAAS region availability before bootstrap reconciliation?
- Where should installation desired state live, and how are provider credentials delivered without
  leaking them to logs, process arguments, or generated artifacts?
- What retention duration and cleanup trigger should apply to `ansible_task_events`, and should it
  exactly follow workflow or artifact retention?
- Should a future dedicated inspection experience expose the complete provider inspection dataset,
  and what provider-neutral schema or raw-view boundary would it use?
- Should Server Detail eventually use SSE for live events, or is bounded polling sufficient for the
  expected scale?

## Future Work

- Add provider-to-catalog import behind an explicit Installation workflow after the conflict and
  idempotency policy is settled.
- Generalize capability-first ownership to future providers and additional provider-backed
  metadata without leaking provider vocabulary into shared domains.
- Add retention tooling and operational metrics for streamed Ansible task events.
- Revalidate the minimal ephemeral image whenever the MAAS PXE kernel ABI changes; preserve the
  native containerd snapshotter choice required by nested overlayfs behavior.
- Expand real-environment browser verification for Infrastructure and Server operator workflows
  when a running API, dashboard, MongoDB, and MAAS Integration are available.
- Consider a dedicated, comprehensive hardware inspection view rather than reintroducing partial
  inspection data into Summary.
