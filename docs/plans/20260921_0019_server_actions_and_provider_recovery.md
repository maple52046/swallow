# Server Action Surface and Provider Recovery — Consolidated Plan

## 1. Purpose

Provide a single long-term plan for the Server operator-action surface. It unifies
two threads of work: (a) making the Server list and Server detail share one
"Take action" menu so the two surfaces cannot drift, and (b) giving Swallow a
first-class, Swallow-owned recovery mechanism for Servers whose provisioning axis
is not usable (`failed`, `broken`, `rescue`), instead of forwarding provider
rejection text. The unifying principle is that Swallow owns action presentation
and recovery policy, while the provisioner only executes the primitives Swallow
selects.

## 2. Source Scope

Consolidated from the AI plan manuscripts under `docs/plans/manuscripts/`:

- `20260919-server-list-shared-actions.md` — Server list actions reuse the detail
  page's nested Take-action UI via a shared component.
- `20260920-provider-recovery-policy.md` — Swallow-owned provider recovery policy
  (Recover / relaxed Release / operator-state gating) for failed / broken / rescue
  Servers, plus contract, glossary, ADR, and platform-deployment documentation.

`README.md` in that directory is the consolidation spec and is not a source plan.

## 3. Consolidated Background

Two problems motivated this work and are addressed together because they share the
same Server action surface:

- The list used a local `ActionMenu` / `BulkActionMenu` while detail used a nested
  `ServerActionMenu` (icons, grouped Power / Hardware checks / State & recovery,
  disabled reasons). The two menus were separate implementations and drifted.
- Swallow was a near-passthrough for not-usable Servers: the list collapsed
  `failed` and `broken` into one badge, availability gating mostly checked only
  the lock, durable Release hardcoded a `deployed`-only precondition, and
  Mark fixed / Rescue had no Swallow-side state policy. Operators met raw provider
  rejections (for example "cannot mark a non-broken node Ready", "Server is not
  deployed") with no coherent recovery path.

## 4. Confirmed Decisions

- Extract one shared presentation component, `ServerTakeActionMenu`, reused by the
  list row kebab, the bulk "Take action" bar, the mobile card "Actions", and the
  detail page's "Take action".
- Detail composes the shared menu (with Deploy OS and Query power) alongside its
  separate Edit menu; list composes the same menu while keeping Deploy OS / Edit
  tags as independent selection-toolbar buttons.
- List shows all action groups regardless of capabilities; detail keeps its
  capability filtering. Bulk excludes `delete` (`bulk: false`).
- Introduce a single Swallow-owned recovery policy (`RecoveryPolicy`) as the one
  source of truth for which recovery intent is allowed from each normalized
  `provisioning.state`, and which provider primitive realizes it.
- Primary path A: relax durable Release to be allowed from `deployed`, `failed`,
  `broken`, and `rescue`, always converging to `ready`.
- Orchestration path B: add a Swallow-owned Recover ("Return to Ready") durable
  Operation that selects the primitive by state — `broken` → mark fixed;
  `rescue` → exit rescue then release if still not ready; `failed` → release —
  and succeeds only after observing `ready`.
- Rescue is a diagnostic environment, allowed from `deployed` / `broken` /
  `failed`; exiting rescue restores the previous state and does not by itself
  reach `ready`. Reaching `ready` is Recover's or Release's job.
- Operator-state primitives (Mark fixed / Mark broken / Rescue enter / exit) are
  gated on live state by the policy and refused with a Swallow-authored reason
  rather than the provider's message.
- Split the "Failed" badge so `failed` and `broken` remain distinguishable.
- On conflict between the sources, the newer recovery-policy manuscript governs
  the action semantics; the older manuscript governs only the shared-menu
  presentation. The two do not otherwise conflict.

## 5. Architecture and Design Principles

- Reuse-first UI: one shared, composable component expresses a shared feature;
  per-page differences are injected via props (trigger style, capability filter,
  optional Deploy OS / Query power slots), never by copy-pasting JSX.
- Single source of truth for behavior: the action catalogue and the recovery
  matrix each live in exactly one place, consumed by list, detail, bulk, the
  durable Release, the Recover Operation, and the operator-state use cases.
- Swallow owns policy and state control; the provisioner executes the selected
  primitive. Acceptance-time refusals carry Swallow reasons; provider rejections
  are retained only as secondary diagnostics.
- Clean Architecture boundaries hold: the recovery policy is pure provisioning
  domain; presentation consumes application ports and never reaches infrastructure.

## 6. Functional Scope

- Unify list and detail Take-action menus into `ServerTakeActionMenu` (nested
  groups, icons, disabled reasons).
- Add a Recover intent and a relaxed Release across list (row, bulk, mobile) and
  detail, both converging a Server to `ready`.
- Gate Mark fixed / Mark broken / Rescue enter / Rescue exit / Release / Recover
  by live state with Swallow reasons, mirrored consistently on the dashboard.
- Show operator-visible guidance: Rescue menu items carry a fixed explanation that
  rescue is diagnostic and its exit does not reach Ready; the detail page shows a
  short recovery alert for `failed` / `broken` / `rescue`.
- Split provider `failed` vs `broken` (and surface `rescue`) in the deployment
  badge, distinguishable at a glance with tooltips, never by color alone.

## 7. Constraints and Rules

- Bulk menus never include `delete`; list shows every catalogue group while detail
  hides unsupported groups by capability.
- The shared-menu work must not change action execution, confirmation dialogs, or
  the availability policy beyond what the recovery work explicitly introduces.
- Do not push zone/pool Edit into the list row.
- Recovery gating is all-or-nothing per batch and must match the backend so the UI
  never offers a control the Operation would refuse.
- Locked, active-Operation / active-Task, and Absent-Server conflict rules
  (including ADR-013 lock protection) continue to apply to Recover and the relaxed
  Release.
- Recover / Release / Mark* / Rescue must refuse before calling the provider when
  state disallows them, returning `ErrServerMutationConflict` (or a validation
  error at durable acceptance) with a Swallow reason.
- Menu items carry a single operator-readable summary; full definitions live in
  the docs, not inline in each item.

## 8. Data Model and Format Notes

- `ServerAction` (dashboard) gains `recover`; the shared catalogue places Recover
  and Release as the primary lifecycle actions.
- New durable input `RecoverServersOperationInput` (serverIds, optional comment,
  optional unbindStaticIPs); no disk-erase controls because Recover picks its
  primitive by state.
- api-server domain: `RecoveryPolicy` keyed by `MachineStatus`, returning an
  allow/deny decision with a Swallow reason and a no-op flag, plus a `RecoverPlan`
  that yields the ordered primitives for a state.
- Operation domain: new `WorkflowKindRecoverServer = "recover-server"` and a
  matching `recover-server` provider Step kind.
- Deployment badge distinguishes normalized `failed`, `broken`, and `rescue`.

## 9. CLI / API / Config Notes

- New endpoint `POST /api/v1/provisioning/recover-operations` (bounded 1–100
  Servers, single Site, duplicate-target / active-work / lock validation), returns
  `202` with an Operation reference; one `recover-server` Step per Server.
- `POST /api/v1/provisioning/release-operations` relaxes its allowed source states
  from `deployed`-only to the policy set (`deployed` / `failed` / `broken` /
  `rescue`); acceptance rejection uses the shared error envelope with Swallow
  reasons.
- `server-detail-actions.md` documents the operator-state gating and the Rescue
  semantics; `provisioning.md` documents the relaxed Release and the Recover
  Operation.
- Dashboard provisioning port and API adapter gain `createRecoverOperation`.
- No new configuration or environment variables.

## 10. Implementation Plan

1. Shared menu: extract `ServerTakeActionMenu`; refactor detail `ServerActionMenu`
   to compose it (Edit menu + Take action with Deploy OS / Query power); replace
   the list's local `ActionMenu` / `BulkActionMenu` with the shared component;
   keep Deploy OS / Edit tags as toolbar buttons.
2. Docs first for recovery: ADR 033; glossary `rescue-mode` and `provider-recovery`
   terms plus updates to `release.md`, `server-status.md`, and `outline.md`;
   contract updates to `provisioning.md` and `server-detail-actions.md`.
3. Domain: add `RecoveryPolicy`; relax `LaunchRelease` to use it; gate
   `machine_actions.go` operator-state actions through it.
4. Recover Operation: extend `DurableOperationLauncher` with `LaunchRecover`; add
   `CreateRecoverOperation` handler and route; add the `recover-server` Step
   executor that reads state, runs the `RecoverPlan` primitives, and observes to
   `ready`.
5. Dashboard: add the `recover` action and state-based availability gating; wire
   `createRecoverOperation` through the port/adapter and the list/detail flows;
   add Rescue menu guidance and the detail recovery alert; split the Failed / Broken
   badges.
6. Platform playbook: add a failure-recovery section to `platform-deployment.md`
   linking the rescue-mode glossary, and align Repair / Uninstall with the same
   recovery policy.
7. Verify: api-server `go build` / `go test` (policy unit tests, Release / machine
   actions gating, recover Step); dashboard `npm run lint` / `npm run build` and
   e2e (shared nested menu; failed / broken / rescue behavior); coding-style gate
   (shared-component JSDoc, accessibility labels).

## 11. Non-goals

- Automatic recovery after a failure (Recover and the relaxed Release stay
  operator-initiated, consistent with ADR-016 / ADR-007).
- Collapsing the three status axes into a single persisted status.
- A distinct UI action per raw provider `status_name`; the UI branches on the
  normalized state plus a small number of diagnostic cases.
- Changing action execution, confirmation dialogs, or availability policy as part
  of the shared-menu extraction (beyond the recovery work's explicit additions).
- Pushing zone/pool Edit into the list row.

## 12. Open Questions

- None blocking. Both manuscripts are internally consistent and were consolidated
  without unresolved conflicts; the recovery-policy manuscript is authoritative for
  action semantics where the two overlap.

## 13. Future Work

- Deepen Platform Repair so that a `provision-os` failure whose Server is not yet
  `ready` routes the operator to Recover as the actionable next step rather than
  re-running a Task that will fail preflight.
- Ensure the uninstall+release shortcut and standalone Release keep sharing the one
  recovery policy so their accepted-target rules never diverge.
- Reconsider whether any future recovery escalation for a Server that cannot leave
  rescue belongs in the shared policy, if such cases recur in operation.
