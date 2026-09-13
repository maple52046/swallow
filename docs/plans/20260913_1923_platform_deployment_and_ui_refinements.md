# Platform Deployment and UI Refinements

## Purpose

Preserve the long-term decisions and remaining work for three related Swallow
improvements: reliable ephemeral Slurm deployment with minimum-resource
eligibility, provider-owned OS image sizing, and consistent dashboard dialog
content and layout. This plan separates confirmed behavior and completed lab
evidence from follow-up implementation work.

## Source Scope

This plan consolidates three AI manuscripts created on 2026-09-13:

- `20260913-slurm-ephemeral-ha.md`, covering the Slurm minimum-resource policy,
  restart-safe deployment orchestration, ephemeral HA lab rollout, observed lab
  results, and later deployment follow-ups;
- `20260913-os-image-size.md`, covering provider-reported OS image sizes from
  MAAS through the API and dashboard;
- `20260913-dialog-layout-content.md`, covering the shared dashboard modal header
  and removal of redundant dialog wording.

The Slurm manuscript records a completed lab deployment and failover exercise as
well as the bounded guard adopted for a later MAAS power-on stall. The image-size
and dialog manuscripts define implementation behavior without recording a final
rollout result.

## Consolidated Background

Ephemeral deployment exposed several conditions that a normal disk deployment
does not: the decompressed image must fit in a memory-backed root filesystem,
OverlayFS paths cannot be exported directly by the lab NFS server, fresh images
may omit `/etc/fstab`, and image-started package services can race Ansible apt
tasks. Long-running provider work also cannot treat a Temporal worker shutdown as
operator cancellation, because aborting the provider operation destroys work that
can otherwise be observed and resumed.

The lab established a working baseline. An 18 GiB ephemeral rootfs on a 24 GiB VM
failed with ENOSPC, while a 24 GiB rootfs on a 32 GiB VM succeeded. Five target
VMs ran the requested image with `ephemeral_deploy=true`; two controllers shared
login-hosted controller state, two compute nodes ran jobs, and the login node
exported workload storage at `/shared`. Controller failover and a controlled
worker restart both succeeded without aborting the accepted MAAS deployment.

A subsequent deployment revealed a separate provider inconsistency: MAAS kept a
machine in `Deploying` while it remained powered off and never began PXE. Manual
power-on allowed the existing accepted deployment to proceed. This was not an
image or ephemeral-rootfs failure and requires an explicit observer-side timeout
rather than an automatic abort/redeploy loop.

Two dashboard gaps are addressed alongside this work. OS image size is useful
deployment context but is owned by the provisioner and was absent from the live
catalog. Platform deployment review also needs to state whether newly provisioned
machines use ephemeral or persistent OS deployment. Separately, dialog consumers
have accumulated repeated titles, descriptions, and warnings that obscure the
action and should be normalized through the shared Modal scaffold.

## Confirmed Decisions

- The first minimum-resource policy is global to Slurm and applies equally to
  controller, compute, and login roles.
- A missing policy or `minimumResources: null` disables resource filtering. An
  enabled policy requires positive CPU cores, memory MiB, and storage GB values.
- The policy is an eligibility floor based on observed Server inventory. It is
  not a resource reservation, Slurm scheduler capacity, or an image-size
  guarantee.
- The backend is authoritative and rejects deficient or unknown resources before
  creating a Platform, Workflow, or related records. Kubernetes is unaffected.
- Ineligible Servers remain visible in Slurm node selection, with every role
  control disabled and each shortfall explained accessibly.
- Deployment requirement management is exposed only on the canonical Platforms
  API surface. Compatibility cluster routes do not own this policy.
- Worker shutdown and workflow cancellation are distinct. Shutdown preserves
  provider and Ansible work for retry; genuine cancellation retains abort
  behavior.
- Provider completion must match both the requested OS image and requested
  ephemeral mode.
- Platform Review displays the resolved OS deployment mode only when Ready
  Servers will be provisioned. The value includes template-owned settings and is
  the same effective value used by the deployment request.
- `sizeBytes` is an optional, provider-owned OS Image field. Swallow does not
  persist it in an overlay or permit operators to edit it.
- MAAS image size comes from the newest complete resource set in each boot
  resource detail. Collapsed subarchitecture variants use their largest selected
  size as a conservative deterministic value.
- The dashboard formats known image sizes in binary units, renders an em dash for
  unknown sizes, and keeps the number and unit together.
- The shared Modal remains the only dialog focus, dismissal, form, and
  accessibility scaffold. Titles are full-width accessible names; descriptions
  are optional supporting content below them.

## Architecture and Design Principles

- Keep provider facts provider-owned. Platform Management consumes Server
  inventory and provisioning catalog data through existing contracts rather than
  copying ownership into a second model.
- Enforce cross-component behavior through the provider-owned API contract.
  Dashboard validation improves interaction but never replaces backend
  validation.
- Model the Slurm requirement as a Platform Management aggregate keyed by
  platform type, with a reader/repository port and a Mongo adapter. Absence is the
  disabled state, so no migration is required.
- Read the requirement once per Slurm deployment validation and reuse one pure
  eligibility result for checkboxes, selected IDs, form validity, and submit
  payload construction.
- Keep long-running activities resumable through stable external idempotency
  identities, heartbeat-aware retries, graceful worker shutdown, workflow
  versioning, and replay coverage.
- Treat provider observation separately from provider mutation. A stalled
  accepted deployment should produce actionable diagnosis without automatically
  repeating destructive provider actions.
- Keep shared presentation behavior in shared components. Dialog consumers may
  refine their copy but must not introduce page-local modal header layouts.

## Functional Scope

### Slurm deployment eligibility and review

- Provide administration of the global Slurm minimum CPU, memory, and storage
  floor.
- Apply it to every selected Slurm node and every Slurm role, for both Ready and
  already Deployed Servers.
- Show observed resources, eligibility, and shortfalls in node selection.
- Fail closed when the dashboard cannot load the policy or the backend cannot read
  it.
- Refresh policy and candidate data after a deployment validation rejection; do
  not automatically resubmit.
- Show the active minimum resources and resolved ephemeral/persistent OS mode in
  Platform Review.

### Ephemeral HA orchestration

- Support mixed convergence: provision Ready Servers and reuse Deployed Servers.
- Resume the same MAAS deployment or Ansible execution across activity retries.
- Validate exact image and ephemeral state before marking provisioning complete.
- Support two Slurm controllers, two compute nodes, one login node, managed
  controller-state NFS, and optional self-hosted workload NFS.
- Handle ephemeral-image package locks, absent `/etc/fstab`, and bounded tmpfs NFS
  backing where OverlayFS is not exportable.
- Diagnose an accepted MAAS deployment that never powers on or begins PXE.

### OS image catalog

- Add optional image size to the provisioning API contract and provider-neutral
  image model.
- Load MAAS boot-resource details with bounded concurrency, select valid resource
  sets, and merge collapsed variants deterministically.
- Display Size in the default desktop image table and responsive cards. Keep
  Architecture and Refreshed available through column selection but hidden by
  default.
- Present Name and Image ID in one column, with a monospace ID and adjacent copy
  action.

### Dialog refinement

- Arrange the shared Modal title and optional description vertically, reserving
  room for the close control.
- Review every dashboard Modal consumer and remove descriptions that merely repeat
  the title or duplicate body warnings.
- Keep concise descriptions when they identify the subject or explain data
  ownership before editing.
- Consolidate destructive consequences into one body message while retaining the
  target, safety, and recovery information.

## Constraints and Rules

- Equal-to-threshold Server resources are eligible. Unknown or zero resource
  values are ineligible when the corresponding positive policy is enabled.
- Requirement repository read failures fail closed for Slurm only.
- Do not infer ephemeral feasibility from the eligibility floor. The chosen image
  may require more memory-backed rootfs space than the policy guarantees.
- A worker shutdown must not call MAAS abort or cancel Ansible. True workflow
  cancellation must retain the current stop behavior.
- For the MAAS power-on inconsistency, query live BMC power rather than relying on
  the cached machine field. After ten minutes truly powered off, complete the Step
  as `requires_attention` with `deployment_power_on_timeout`.
- Do not automatically abort or redeploy after the power-on timeout. Providers
  without live power inspection keep the existing two-hour observation timeout.
- Keep an early global dpkg/apt lock gate and independently retry every Slurm role
  apt mutation to close the later unattended-upgrades race.
- Lab-managed NFS uses bounded tmpfs filesystems when its export path would
  otherwise reside on non-exportable OverlayFS: 1 GiB for controller state and
  4 GiB for workload data.
- Preserve explicit NFSv3 for standalone Swallow-managed lab exports and captured
  NFSv4 behavior for external storage.
- Remove the target-only `overlayroot` MAAS tag before returning machines to disk
  deployment.
- Preserve dialog behavior, backend calls, loading/error states, confirmation
  gates, Chakra theme tokens, and accessibility relationships while refining
  layout and copy.
- Do not delete source manuscripts until this consolidated plan has been written
  and verified against every required section.

## Data Model and Format Notes

The Slurm requirement representation is:

```json
{
  "platformType": "slurm",
  "minimumResources": {
    "cpuCores": 4,
    "memoryMiB": 24576,
    "storageGB": 80
  },
  "updatedAt": "2026-09-13T00:00:00Z"
}
```

`minimumResources: null` disables the requirement. CPU and memory are positive
integers; storage is a positive GB value. Mongo persistence is uniquely keyed by
platform type and uses upsert behavior.

OS Image adds optional `sizeBytes`. The explicit byte unit keeps transport and UI
formatting separate. MAAS `sets[version].size` is the total resource-set size;
incomplete sets and non-positive values do not produce a catalog value. The
catalog does not sum mutually exclusive kernel variants.

Machine preparation continues to derive `existing_os` versus `provision_os` from
the selected Servers. For provisioned machines, the effective ephemeral value may
come from custom settings or a selected template. Review and request construction
must share that resolved value.

## CLI / API / Config Notes

- `GET /api/v1/platforms/deployment-requirements/slurm` reads the current policy.
- `PUT /api/v1/platforms/deployment-requirements/slurm` enables, updates, or
  disables it.
- `POST /api/v1/platforms/deploy` automatically applies the current policy to
  Slurm requests; the deployment request does not carry a policy override.
- The experimental policy remains `4 cores / 24576 MiB / 80 GB` even though the
  successful target VMs required 32 GiB for the selected image and overlay size.
- The successful ephemeral lab uses the exact requested image, automatic network
  preparation, `machinePreparation.mode=provision_os`, ephemeral mode, workload
  NFS at `/shared`, and login-hosted managed controller state.
- The successful fallback kernel option is `overlayroot=tmpfs:size=24G`; the
  original canary used `overlayroot=tmpfs:size=18G` and failed with ENOSPC.
- The provisioning OS Image API exposes optional `sizeBytes`; no deployment
  request or image identity field changes are required.
- No new CLI surface is required by these manuscripts.

## Implementation Plan

1. Maintain the shared glossary, ADR, API contracts, and platform deployment
   documentation for minimum-resource eligibility and ephemeral semantics.
2. Maintain the Platform Management requirement aggregate, repository port,
   Mongo upsert adapter, GET/PUT services, and pre-side-effect Slurm validation.
3. Maintain the dashboard settings page, API repository support, pure eligibility
   helper, accessible resource table, review summary, and stale-policy recovery.
4. Maintain worker-shutdown-aware activity behavior, graceful worker stop,
   heartbeat retry, stable idempotency, workflow version gates, and replay tests.
5. Preserve the verified ephemeral Slurm adaptations for apt locking, `/etc/fstab`,
   OverlayFS-safe NFS exports, mount protocol selection, exact image/mode checks,
   login submission, and controller failover.
6. Maintain the bounded live-power observer guard for MAAS deployments accepted
   in `Deploying` but still powered off, including timeout, no-abort behavior, and
   provider-fallback tests.
7. Maintain and verify optional OS Image `sizeBytes` end to end: contract,
   provider-neutral model, MAAS detail loading and merge rules, dashboard display,
   fixtures, and focused backend/browser tests.
8. Maintain the refined shared Modal header and consumer copy with focused
   Playwright coverage for layout and accessible title/description behavior.
9. Run relevant Go tests, dashboard lint, TypeScript/Vite build, Playwright tests,
   and manual JSDoc, accessibility, state, and layering review for each workstream.

## Non-goals

- Reserve Server resources or configure Slurm scheduler capacity through the
  minimum-resource policy.
- Automatically derive a safe ephemeral rootfs requirement from arbitrary future
  images.
- Add Slurm policy controls to Kubernetes deployment or compatibility cluster
  APIs.
- Persist, overlay, edit, or sum provider-reported OS image sizes.
- Display image download progress or per-file image sizes.
- Change deployment request image identity as part of image-size reporting.
- Redesign non-dialog pages, drawers, menus, or alerts while refining Modal copy.
- Introduce another modal abstraction or duplicate the shared dialog scaffold.

## Open Questions

The source manuscripts contain no unresolved conflicts. Any expansion from a
global Slurm floor to per-role or other platform requirements requires a new
decision and is not part of the first version.

## Future Work

- Evaluate minimum-resource requirements for other platform types or role-specific
  Slurm thresholds only after the global Slurm policy has sufficient operational
  evidence.
- Generalize provider-reported image size to additional provisioners when they can
  supply an equivalent trustworthy total.
- Replace the lab-grade single NFS export host with a production-grade shared
  storage failure model when production HA requirements are defined.
- Continue improving provider diagnostics from MAAS power-on timeout evidence
  without weakening the no-abort worker-restart guarantee.
