# Platform Lifecycle and Dashboard

## 1. Purpose

Preserve the durable Platform lifecycle, Change Plan, diagnostics, runtime-workspace, and dashboard
decisions implemented during the November 2026 stabilization work. This plan also identifies the
remaining lifecycle and user-experience work without replaying commit-by-commit execution logs.

## 2. Source Scope

Consolidated from:

- `docs/plans/manuscripts/20261104-dashboard-health-and-review-fixes.md`
- `docs/plans/manuscripts/20261105-change-plan-approve-on-submit.md`
- `docs/plans/manuscripts/20261105-platform-detail-ui-refactor.md`
- `docs/plans/manuscripts/20261105-platform-dhcp-and-diagnostics.md`
- `docs/plans/manuscripts/20261105-platform-uninstall-gaps.md`
- `docs/plans/manuscripts/20261106-dashboard-polish.md`
- Relevant lifecycle and UI findings in `20261102-architecture-review-v2.md`

The architecture-review v3 manuscript was excluded and left untouched.

## 3. Consolidated Background

Platform moved from imperative deployment records to desired state, generation-aware status, Change
Plans, and controller-driven realization. Stabilization then removed unnecessary double approval,
corrected delete/uninstall behavior for running and failed deployments, added durable Task diagnostics and
bounded waits, fixed DHCP endpoint capture, split Kubernetes read/operator credentials, and rebuilt the
Platform detail page as a route-based runtime workspace.

The dashboard was subsequently reorganized around Servers, Platforms, Software, Monitoring, Activity, and
Infrastructure, with shared detail tabs, improved hardware summaries, generated OpenAPI adapters for module
features, and explicit partial-read/error behavior.

## 4. Confirmed Decisions

- A confirmed dashboard action or CLI command with `--approve` approves its Change Plan in the same write
  unless the safety policy requires separate review, blockers exist, or controller recomputation changes
  the actions.
- The runtime Change Plan policy defaults to approve on submit and is immediately configurable by admins.
- Plans remain durable audit records even when approved on submit.
- A Platform whose installation started must be uninstalled; it is not deleted as an unstarted record.
- Rejected, expired, or failed uninstall can be proposed again by repeating Uninstall. Changed removal
  options supersede the old plan.
- A Platform with no installed or applied state may be deleted immediately.
- Automatic addressing is the default. A subnet without provisioner DHCP produces a warning, not an
  automatic switch to DHCP.
- Kubernetes background reads use a least-privilege reader credential; admin mutations use a separate
  operator credential.
- Platform workspace is route-based. Shared shell reads lifecycle facts; each route owns secondary data.
- Platform and accelerator modules declare runtime routes, access, configuration, monitoring, and
  presentation contributions.
- Secondary read failure is shown explicitly and may preserve last-good data; it is never converted to an
  empty collection.

## 5. Architecture and Design Principles

- Desired Platform spec and generation are canonical; presentation-only “starting” state bridges the short
  interval before the controller observes a new generation without inventing lifecycle enums.
- Change Plan proposal, approval, and spec mutation are transactional.
- Controller recomputation protects against stale approval: changed actions supersede the approved plan
  and require a new decision.
- Lifecycle Workflows own resources and use dispatch fencing.
- Diagnostics preserve causal detail through adapter boundaries, redact at durable storage, and provide
  both user and admin views.
- Waits classify permanent, transient, and unknown outcomes and publish current observation when
  supported.
- Dashboard modules consume module-owned ports and generated OpenAPI types; presentation does not import
  infrastructure.
- Shared workspace surfaces avoid Platform-type branches; module manifests supply type-specific facts and
  routes.

## 6. Functional Scope

Implemented:

- Approve-on-submit Change Plans and runtime policy across API, CLI, and dashboard.
- Correct uninstall/delete behavior for planned, deploying, failed, rejected, expired, uninstalling, and
  already-uninstalled Platforms.
- Controller cleanup of orphaned plans and transaction-race protection during uninstall.
- Automatic network default and runtime capture of the post-provision Kubernetes endpoint.
- Structured Task diagnostic/waiting contract, adapter cause preservation, Task correlation logs, and
  two-level dashboard display.
- Bounded Platform validation and provisioner observation waits.
- Separate Kubernetes reader/operator credentials and migration behavior for existing Platforms.
- Route-based Platform workspace: Overview, Monitoring, Configuration, Activity, Kubernetes native routes,
  Slurm native routes, and module contributions.
- Starting-state UX, partial-failure handling, Slurm last-good data, canonical member counts, and Activity
  pagination.
- Dashboard navigation, Activity tabs, OS Images placement, Boot ISO route move, Platform/Server tab reuse,
  hardware facts, and final review fixes.

Remaining:

- Automation for currently blocked Platform spec changes, including disabling Platform Grafana.
- Software module UI migration.
- Server-fleet row-expansion decision.
- A few request-shape defects: template network fields, template-only RAM inference, and batch capability
  status code.
- Complete diagnostic classification outside Workflows and remaining wait publishers/diagnostics.
- Real-machine endpoint-change verification for automatic/DHCP Kubernetes provision.

## 7. Constraints and Rules

- The safety policy affects new writes only; changing it does not approve already-pending plans.
- A plan with blockers is never approved on submit.
- Controller action recomputation must match the approved action set before apply.
- Uninstall in progress is idempotent: repeated delete returns current state without creating a parallel
  plan.
- Concurrent controller apply and user spec/delete writes must re-read lifecycle inside the transaction.
- A non-HA Kubernetes initial control plane that will be reprovisioned must not freeze its old observed
  address for automatic or DHCP networking.
- Static networking uses explicit assignment; an already-deployed reused Server uses its observed address.
- Route changes must preserve Site scope and old Platform-detail bookmarks.
- Route-level requests discard late results when Platform ID, Site, or route changes.
- User-visible dashboard changes require desktop/mobile visual inspection and accessibility review.
- The dashboard may use concise visible labels while preserving full accessible names.

## 8. Data Model and Format Notes

- Change Plan policy exposes `approveOnSubmit`, `updatedAt`, and `updatedBy`; no stored record reads as
  approve-on-submit enabled.
- Platform delete results distinguish `deleted` (204) from accepted/running uninstall.
- Platform spec/status use generation, observed generation, applied spec, and Conditions; UI does not infer
  deployment history from the current lifecycle Workflow.
- Task errors may contain a structured diagnostic; executing Tasks may contain advisory `waiting`.
- Kubernetes Integration records reader and operator credentials separately and reports only credential
  presence.
- `PlatformModuleUI` and `AcceleratorModuleUI` own icons, routes, access, configuration, monitoring, and
  topology derivation.
- Activity is server-paginated and newest-first.

## 9. CLI / API / Config Notes

- Platform create, set-spec, delete, and GPU Operator changes accept explicit approval.
- CLI exposes Change Plan policy read/write commands.
- `DELETE /platforms/{id}` may return 204 for immediate deletion, 202 with a plan, or 200 when uninstall is
  already running.
- Automatic is the normalized default network mode; DHCP remains an explicit choice.
- A network object that omits `mode` preserves the Deployment Template mode.
- Remaining bug: an included network object still overwrites omitted template `subnetId` and
  `defaultGateway`; the boolean needs optional representation to distinguish omission.
- Remaining bug: a template-only Platform request without `deployTarget` may infer disk even when the
  template is RAM.
- Remaining contract inconsistency: unsupported RAM capability in a batch returns 409 rather than the
  capability-error 400 rule.
- Dashboard routes retain old URLs where practical; `/provisioning/boot-isos` redirects to
  `/infrastructure/boot-isos`.

## 10. Implementation Plan

1. Finish Platform change automation one action family at a time: member removal/role change, version
   upgrade, GPU stack changes, storage/runtime changes, and Platform Grafana disable.
2. Migrate Software dashboard behavior into module-owned UI, ports, adapters, and registry contributions.
3. Resolve request-shape defects:
   - preserve omitted template subnet and gateway fields;
   - infer template-only RAM deployment correctly;
   - align unsupported RAM capability with the 400 rule.
4. Extend structured diagnostics to background reconciliation, API detail responses, and Ansible failure
   classification.
5. Add `Task.waiting` and diagnostics to remaining SSH/enrollment waits and introduce a Workflow-level
   total budget.
6. Re-run Kubernetes automatic/DHCP deployment in a lab configuration where the control-plane address
   actually changes, then verify Integration endpoint and stale-endpoint detection.
7. Decide the Server fleet expansion UX and either preserve it or move unique identity/freshness fields to
   Server detail before removal.
8. Continue generated OpenAPI adapter migration and nullable-field hardening.

## 11. Non-goals

- A single generic live-management interface for Kubernetes and Slurm.
- Embedded monitoring charts in the Platform workspace.
- Backend fan-out solely to populate one dashboard page.
- A new Platform lifecycle enum for the controller-observation gap.
- Role-based dashboard hiding before non-admin API access exists.
- The broader Workflow UI redesign.
- AI NIC or disk hardware summaries before requirements and data are defined.
- A dashboard unit-test runner in this stabilization work.

## 12. Open Questions

- Should disabling Platform Grafana be classified as destructive or non-destructive?
- Should Server fleet row expansion stay, be removed after moving unique fields, or be removed without
  replacement?
- Should Platform explorer/provisioning errors expose structured admin detail in ordinary API errors?
- What Workflow-level budget should cap aggregate Task time plus user/dependency waits?
- How should old Workflows without `apiServerId` be treated when retried?
- Should the dashboard expose request IDs for Fleet metrics failures rather than only a failure count?

## 13. Future Work

- Full Platform reconfiguration automation and supported upgrade paths.
- Declarative Workflow templates shared by validation and existing launchers.
- Software module UI and dynamic Software kind registration.
- Role-based navigation once API authorization includes non-admin roles.
- Generic generated transport types for every dashboard adapter.
- Formal testing-stack release and upgrade rehearsal, release notes, and support-matrix enforcement.
