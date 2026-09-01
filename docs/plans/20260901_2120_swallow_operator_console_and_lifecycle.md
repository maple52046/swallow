# Swallow Operator Console and Infrastructure Lifecycle

## 1. Purpose

Preserve the durable product, architecture, workflow, and safety decisions from
the completed Dashboard, provisioning, Server, and Cluster implementation
manuscripts. This document is the long-term reference for evolving the Swallow
operator experience without reopening settled ownership boundaries or regressing
verified behavior.

## 2. Source Scope

This consolidation covers 10 manuscripts created from 2026-08-27 through
2026-09-01:

- `20260827-dashboard-brand-server-table.md`
- `20260828-dashboard-table-alignment-spacing.md`
- `20260828-os-provisioning-workflow.md`
- `20260829-cluster-uninstall-delete.md`
- `20260830-flexible-kubernetes-deployment.md`
- `20260831-dashboard-fluid-content.md`
- `20260831-dashboard-list-toolbar-monitoring.md`
- `20260831-dashboard-surface-polish.md`
- `20260831-server-deletion.md`
- `20260901-sites-integrations-management.md`

The sources describe completed implementation and validation as well as durable
constraints for future work. This document synthesizes them; it is not a new
feature proposal.

## 3. Consolidated Background

Swallow is an operator console for infrastructure inventory, provider-backed OS
provisioning, monitoring, and Cluster lifecycle management. The Dashboard has
converged on PatternFly primitives and an information-dense, responsive console
instead of a centered application card. Provider systems continue to own their
resources and enforcement rules, while Swallow owns the registry, projections,
intent, and durable Operations needed to coordinate them.

The completed work established a visible Site-to-Integration hierarchy, moved OS
deployment into a dedicated provisioning workflow, made Kubernetes topology
flexible, and separated destructive Cluster and Server actions by their real
side effects. It also established shared presentation rules for inventory
tables, navigation, spacing, surfaces, monitoring hierarchy, and responsive
overflow.

Live validation demonstrated that Kubernetes uninstall is idempotent across its
frozen target set and that completion cleanup can safely clear membership,
Cluster sync state, and a proven Swallow-owned Integration while leaving Servers
present and deployed.

## 4. Confirmed Decisions

### Operator Console

- Use PatternFly as the Dashboard design foundation and keep authenticated pages
  as a flat, full-width workspace beside the docked navigation.
- Keep the Swallow brand and navigation control on one row. The product mark is
  an original inline SVG side-profile swallow that remains legible in masthead,
  login, and error contexts.
- The expanded desktop dock is 186px, the collapsed dock is 64px, and the mobile
  drawer retains focus-managed behavior.
- Use a restrained 6px radius for framed data surfaces without recreating a large
  outer page container. Links have no default or hover underline and retain a
  visible keyboard focus outline.
- Table cells are vertically centered by default. Horizontal alignment remains
  appropriate to the data type. Mixed inline controls share a visual center
  line, and separators have at least a medium PatternFly content-side spacer.
- List search toolbars for Servers, Operations, Deployment Templates, and OS
  Images are unframed. Monitoring headings and descriptions precede their data
  containers.

### Infrastructure Registry

- A Site represents a higher-level infrastructure location or scope. An
  Integration belongs to exactly one Site and connects that Site to one external
  provider role.
- Operators can view and configure Sites and Integrations from a top-level
  Infrastructure area. Site mutations refresh the global Site selector.
- An Integration cannot be transferred between Sites. Its credential is accepted
  only on creation or explicit replacement and is never readable.
- OS Image source presentation links back to both the Site and its concrete
  Provider Integration.

### OS Provisioning

- OS deployment is a dedicated Provisioning workflow, not a Server Summary form.
- One deployment configuration may target 1-100 ready Servers belonging to the
  same provisioner Integration.
- OS Images are live, provider-owned data. Swallow does not mirror or manage
  their lifecycle.
- Deployment Templates are Swallow-owned deployment intent scoped to one
  provisioner Integration. They contain no automation content.
- Template cloud-init is encrypted and write-only. Customized deployment uses
  explicit `inherit`, `replace`, or `omit` semantics.
- Batch deployment performs an all-target preflight before dispatch. Provider
  refusals after preflight are reported per Server and do not roll back accepted
  targets.
- Deployment progress remains on each Server provisioning axis; no separate
  durable OS deployment job is introduced.
- MAAS network readiness is provider-owned. Swallow checks that a target has an
  interface linked to a subnet, reports the exact target and corrective action,
  and never guesses or silently assigns provider network configuration.

### Kubernetes Deployment and Lifecycle

- Kubernetes deployment supports Standalone, Multi-node, and High availability
  topologies.
- Node Role vocabulary remains `control-plane | worker`. A control-plane
  assignment uses `runWorkloads` to express workload co-location; no combined
  role is introduced.
- One control-plane is non-HA. An odd count of at least three control-plane
  Servers is HA. Unsupported even control-plane counts are invalid.
- Standalone is one workload-capable control-plane with no separate worker.
  Non-HA multi-node uses one control-plane and one or more workers. HA requires
  an API virtual IP and keepalived; non-HA does not.
- The deployment wizard order is Basics, Machines, Networking, Review. Network
  guidance is derived only after machine selection and distinguishes suggested
  values from values the operator must allocate.
- Accepted deployment stays in a Cluster-specific progress experience. Generic
  Operation Detail is a secondary troubleshooting destination.
- Failed deployment exposes **Repair deployment** on Cluster Detail. Repair uses
  the existing durable Operation Retry contract, preserves lineage, and reuses
  original targets, roles, network variables, and sealed secrets.
- Durable deployment claims and observed membership keep Servers unavailable to
  another Cluster while the owning Cluster is deploying, failed, active,
  uninstalling, or uninstall-failed. Successful uninstall releases them.
- Summary projects topology, control-plane count, and total workload-capable
  count. A workload-capable control-plane is never misrepresented as a
  worker-only member.
- **Uninstall** removes k0s from the frozen targets of a proven Swallow deployment
  through a durable Operation and retains the Cluster record.
- **Delete** removes only the Swallow Cluster record, membership projection, and
  a conservatively proven Cluster-owned credential Integration. It never changes
  hosts or cancels accepted Operations.
- External Kubernetes and Slurm Clusters can be deleted but cannot be
  uninstalled.

### Server Deletion

- `DELETE /api/v1/servers/{id}` is an admin-only provider-backed deletion.
- The backing Machine is deleted before the Swallow Server projection. Provider
  refusal or unavailability leaves the projection intact.
- A provider Machine already missing is converged cleanup and permits deleting
  the remaining projection.
- MAAS deletion uses its normal Machine delete endpoint without an implicit
  force option.

## 5. Architecture and Design Principles

- Preserve monorepo component boundaries. The API server owns contracts and
  backend behavior; the Dashboard consumes them through application ports and
  infrastructure adapters.
- Make API evolution under `/api/v1` additive unless an explicit versioned
  migration is approved. Existing routes and legacy single-Server deployment
  remain compatible.
- Respect provider ownership. Swallow may validate, orchestrate, or project a
  provider resource, but must not invent unsupported provider controls or local
  lifecycle ownership.
- Keep application ports narrow and use-case oriented. Dashboard presentation
  depends on repository ports rather than HTTP implementation details.
- Keep sensitive values out of read APIs, URLs, browser storage, logs,
  notifications, and reusable non-secret projections.
- Derive Cluster lifecycle and topology from durable Operations and their
  non-secret intent. Do not infer authoritative state solely from incomplete live
  membership.
- Treat destructive workflows as distinct domain actions whose confirmations
  describe their actual host and record effects.
- Use shared PatternFly composition and CSS rules for recurring layout behavior;
  avoid page-specific copies and nested card chrome.
- Keep horizontal overflow inside the table or detail component that owns it.
  The document and authenticated shell must not overflow the viewport.

## 6. Functional Scope

### Shell and Shared Presentation

- Inline Swallow brand asset across masthead, login, and route errors.
- Docked desktop navigation and focus-managed mobile navigation.
- Fluid authenticated content area with stable responsive padding.
- Shared rounded surfaces, focus treatments, separator spacing, vertical
  alignment, unframed list toolbars, and external section headings.
- Login composition with one responsive PatternFly header/body pair.

### Server Inventory

- Independent selection, Machine, MAC address, Zone, Pool, and Power columns.
- Independent hardware columns for architecture, CPU cores, CPU model, memory,
  storage, system vendor, and system product.
- GPU inventory shown as count times accessible vendor mark and model context.
- Missing observations shown as `-`; explicit domain values such as `unknown`
  remain explicit.
- Persisted hidden-column preferences migrate from the former placement and
  hardware columns to their replacements.
- Shared list/detail Server actions include provider-backed deletion with typed
  confirmation and OS deployment entry points.

### Sites and Integrations

- Scope-preserving Infrastructure navigation with Sites and Integrations routes.
- Site and Integration list, create, edit, and delete flows using existing Active
  contracts and dependency-conflict errors.
- Explicit Integration credential replacement and provider-specific settings
  limited to keys consumed by backend adapters.
- Site selector synchronization and OS Image source links.

### Provisioning

- Dedicated Deploy OS wizard, Deployment Template management, and live OS Image
  catalog.
- Single- and multi-Server deployment entry points.
- Atomic target eligibility and provider-readiness preflight before leaving the
  target step, repeated at final submission to protect against stale readiness.
- Locked template configuration until customization, explicit cloud-init mode,
  and partial provider result reporting.
- Rounded wizard surface and dark-theme-visible empty Integration placeholder.

### Cluster Deployment and Lifecycle

- Topology presets with editable per-machine roles where valid.
- Selected-machine network context and actionable VIP/CIDR guidance.
- Backend validation and automation for standalone, non-HA multi-node, and HA
  k0s deployment.
- Deployment target claim resolution and visible occupied-Cluster context in
  machine selection.
- Cluster-specific progress, Repair, lifecycle projection, Uninstall, and Delete.
- Related Operation lineage and exporter restoration as an independent Operation
  after successful uninstall when eligible.

### Monitoring and Operations

- Monitoring Alert and Server metrics headings and descriptions outside their
  framed data containers.
- Compact, unframed filtering surfaces where defined.
- Clear spacing between Operation Detail tabs and active tab content.
- Operation Detail remains the full event/stdout troubleshooting workspace even
  when a domain workflow provides the primary progress experience.

## 7. Constraints and Rules

- Do not restore a centered maximum-width body wrapper or floating shell panel.
- Do not remove deliberate table overflow containment or sticky inventory
  behavior to make the document itself scroll horizontally.
- Do not persist cloud-init or Integration credentials in browser storage or
  expose them through read responses.
- Do not transfer Deployment Templates or Integrations between Integrations or
  Sites respectively.
- Do not treat absent metrics or inventory observations as zero; use `-` or the
  established `No data` language as appropriate.
- Do not bypass MAAS safeguards with force deletion or fabricate network
  configuration on an operator's behalf.
- Do not dispatch any OS deployment when batch preflight fails for any target.
- Do not derive uninstall targets from live membership. Use the most recent
  durable deployment Operation target snapshot.
- Uninstall removes k0s state, services, configuration, installer artifacts, and
  binary, but preserves the OS, users, `conntrack`, shared packages, and avoids
  reboot.
- Delete never implies uninstall. Cluster Delete and Server Delete must retain
  their separately documented provider and host effects.
- Do not delete an operator-owned Integration. Legacy ownership cleanup requires
  both durable deployment provenance and the exact generated Integration
  signature.
- Do not allow retry of a Cluster-owned deployment or uninstall after its Cluster
  record has been deleted; already accepted Operations may finish.
- Do not support cross-platform Server co-residency through the current single
  membership axis.

## 8. Data Model and Format Notes

- `Site` is the infrastructure scope. `Integration.siteId` records its single
  owning Site.
- Integration read models expose credential presence, never credential content.
- `DeploymentTemplate` is scoped to a provisioner Integration and stores image
  intent, ephemeral mode, metadata, and encrypted optional cloud-init. It is not
  a post-install automation profile.
- Batch OS deployment accepts 1-100 unique Server targets belonging to one
  Integration and reports accepted provisioning snapshots plus per-Server
  failures after successful preflight.
- Kubernetes `RoleAssignment` contains a Node Role and, for control-plane nodes,
  `runWorkloads`. Workload-capable count includes workers and co-located
  control-plane nodes.
- Cluster responses carry an additive deployment projection derived from
  non-secret Operation variables and lifecycle fields derived from durable
  deployment/uninstall history.
- Cluster origin distinguishes registered resources from Swallow-deployed
  resources. Lifecycle includes deploying, deploy-failed, active, uninstalling,
  uninstall-failed, and uninstalled states as applicable.
- Uninstall retry retains `retryOfOperationId` lineage and reuses the same frozen
  target set. Its playbook remains idempotent on already-clean hosts.
- Server provisioning state remains the durable user-visible projection for OS
  deployment progress.
- Missing Dashboard observations use `-`, while explicit domain states retain
  their canonical labels.

## 9. CLI / API / Config Notes

- Dashboard routes include `/infrastructure/sites`,
  `/infrastructure/integrations`, the Provisioning routes, Server list/detail,
  Cluster list/detail, Monitoring, and Operations without changing established
  route meanings.
- `?site=<id>` remains the Dashboard Site scope and is preserved by
  Infrastructure and OS Image navigation.
- Provisioning uses the Active template CRUD and write-only credential/user-data
  endpoints plus batch target preflight and deployment endpoints under
  `/api/v1/provisioning`.
- Legacy `POST /api/v1/servers/{id}/deploy` remains available.
- Cluster lifecycle adds the admin
  `POST /api/v1/clusters/{clusterId}/uninstall` action while preserving existing
  Cluster Delete behavior.
- Server provider-backed deletion uses
  `DELETE /api/v1/servers/{id}` and returns `204 No Content` on success.
- Provider and dependency failures use the established API error envelopes and
  preserve local projections where the destructive provider action did not
  succeed.
- The development installation remains the repository Compose workflow. The
  verified bindings are Dashboard `0.0.0.0:5173` and API
  `0.0.0.0:30051`.

## 10. Implementation Plan

The source manuscripts record the following baseline as implemented. Future
changes should preserve it through these workstreams:

1. Maintain the PatternFly shell, shared surface, alignment, spacing, overflow,
   toolbar, and monitoring hierarchy rules in common primitives and styles.
2. Maintain the expanded Server inventory projection and preference migrations;
   keep destructive actions in the shared Server action UI.
3. Keep Sites and Integrations visible and editable through the Infrastructure
   registry, synchronizing global scope after mutation and retaining write-only
   secret handling.
4. Maintain provisioning template persistence, provider image validation,
   bounded batch dispatch, repeated readiness preflight, and the dedicated
   PatternFly workflow.
5. Maintain Kubernetes topology validation, role projection, conditional HA
   variables, worker directory prerequisites, target claims, Cluster progress,
   and Repair behavior.
6. Maintain the idempotent uninstall catalog entry/playbook, lifecycle
   aggregation, completion cleanup, owned-Integration protection, and independent
   exporter restoration.
7. Maintain provider-first Server deletion ordering and preserve the Swallow
   projection on provider failure.
8. For changes in any workstream, update the provider-owned API contract and
   glossary where applicable, add narrow backend/frontend tests, then run Go
   formatting, vet, build, and tests plus Dashboard lint, build, E2E,
   accessibility, responsive overflow, light/dark, and architecture review.

The completed baseline was validated with backend tests, Dashboard lint/build,
and Playwright coverage. The live `lab-k0s` uninstall and its idempotent retry
also verified frozen-target cleanup, stale-sync protection, Integration removal,
and Server preservation.

## 11. Non-goals

- Managing, mirroring, uploading, importing, or deleting provider OS Images from
  Swallow.
- Adding packages, scripts, playbooks, or post-install automation to Deployment
  Templates.
- Creating a durable OS deployment job separate from Server provisioning state.
- Guessing provider network configuration or bypassing provider safeguards.
- Supporting even-sized control-plane quorum or configuring an external load
  balancer.
- Adding Kubernetes workload, CNI, storage, or application deployment settings.
- Rolling back partially accepted provider deployments or partially completed
  Cluster uninstall.
- Uninstalling external Kubernetes or Slurm Clusters.
- Removing the OS, shared packages, users, or rebooting hosts during k0s
  uninstall.
- Force-deleting MAAS Machines, deleting hosted pods, or deleting only a Server
  projection while its provider still owns the Machine.
- Reintroducing visual regression screenshot baselines or an outer Dashboard
  page card through these presentation refinements.

## 12. Open Questions

- What platform-scoped membership model should replace the current single
  Cluster membership axis before a Server may safely host multiple platform
  types, such as Kubernetes plus a storage platform?
- No other unresolved conflict was found among the source manuscripts. External
  load balancers, unsupported quorum shapes, and broader provider lifecycle
  controls remain intentionally outside the confirmed design rather than
  undecided requirements.

## 13. Future Work

- Define a platform-scoped multi-membership model and migration before enabling
  cross-platform Server co-residency.
- Add new provider capabilities only through explicit provider-owned contracts;
  keep unsupported firmware, console, image-lifecycle, networking, and workload
  controls absent from the UI until those contracts exist.
- Consider post-install automation as a separate domain workflow rather than
  expanding Deployment Templates beyond provider deployment intent.
- Continue measuring and containing Dashboard bundle growth and component-level
  horizontal overflow while retaining the established full-width operator
  workspace.
