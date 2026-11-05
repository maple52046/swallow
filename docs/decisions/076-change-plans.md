# 076. Change Plans: a stored plan per resource, applied by the resource's controller

- Status: Accepted
- Date: 2026-11-01
- 現行規格：[desired-state-model.md](../development/desired-state-model.md) §5.4、§11

## Context

[ADR 069](069-swallow-owns-desired-state.md) made a destructive change wait for a user's
approval, and desired-state-model §5.4 specifies what a [Change Plan](../development/glossaries/terms/change-plan.md)
contains, its states, whole-plan approval, recomputation before apply, expiry, at most one unfinished
plan per resource, and that applying it produces a Workflow. Nothing implements it yet: the
convergence package has controllers, leases, conditions, and events, but no plan.

The Platform is about to become a spec/status resource, and creating, uninstalling, changing a Node
Role of, or removing a member from a Platform are destructive (desired-state-model §5.2). The spec
leaves open where a plan is stored, who computes and applies it, how applying it relates to the
resource's controller and holds, how long it lives, and how users reach it.

## Decision

1. **A Change Plan is its own document in the core's convergence context**, stored in the
   `change_plans` collection. It records the resource it plans (kind and id), the spec `generation`
   and observation time it was computed from, its actions (each with its target, the action, the
   current and desired values, whether it is destructive, and why), the expected effect, its
   blockers, its state, who approved or rejected it and when, the Workflow it produced, and when it
   expires. A unique partial index on the resource over the unfinished states (`pending_approval`,
   `approved`) keeps one unfinished plan per resource.
2. **Each resource kind plans through a core port.** A resource's planner reads its spec and
   observed state outside any transaction and returns the draft (actions, effect, blockers); it also
   prepares the Workflow that carries out an approved plan, writing nothing. The Platform's planner
   asks the Platform module of the Platform's type for the type-specific actions.
3. **A destructive spec write commits the spec and its plan together.** The write increments the
   `generation`, stores the plan, and supersedes the resource's earlier unfinished plan, in one
   transaction. The API answers `202 Accepted` with the plan. While the plan is `pending_approval` the
   resource's `realized` condition is `false` with reason `awaiting_approval`.
4. **Approval is of the whole plan and freezes it, and a confirmed request is an approval.** A write
   the user sends with `approve=true` (a dialog they confirmed, a CLI flag) stores its plan `approved`
   in the same transaction, decided by that user, and asks the controller to apply it; the plan stays
   the record of what was decided. A plan waits for a separate approval or rejection by an admin when
   the write did not approve it, when it has blockers (approving one with blockers is refused), when
   swallow proposed it on its own, and while the installation's Change Plan policy turns approval on
   submit off. The policy is a runtime setting stored by swallow and changed through the API; it is
   on (approve on submit) until an admin turns it off, and it applies to the writes that follow.
5. **The resource's controller applies an approved plan.** Approving requests a convergence of the
   resource. The controller recomputes the plan: when the `generation` changed, the plan becomes
   `superseded` and the newer write's plan replaces it; when the recomputed actions differ (by target,
   action, and desired value; observation times alone do not count), the plan becomes `superseded`
   and, when the change is still destructive, a new plan is proposed in the same transaction and waits
   for approval, since nobody approved what it now does. While the resource is held, an approved plan
   waits. Otherwise the controller writes the Workflow, which records the plan id, and marks the plan
   `applied` with the Workflow id, in one transaction.
6. **A plan expires 24 hours after it is computed**, configurable with
   `SWALLOW_API_CHANGE_PLAN_TTL`; the controller marks an unfinished plan past that time `expired`.
7. **Plans have their own API and appear on the resource's timeline.** `GET /api/v1/change-plans`
   (filtered by resource and state), `GET /api/v1/change-plans/{id}`, `POST .../approve` and
   `POST .../reject`, and `GET`/`PUT /api/v1/change-plan-policy` (admin). Creation, approval,
   rejection, supersession, expiry, and apply are timeline events of the resource. The CLI and the
   dashboard approval screen consume this API.

## Alternatives considered

- **An approval Task inside a Workflow.** Rejected: a plan has to be recomputed and superseded before
  anything runs, and a Workflow waiting for approval would own the resource for the whole wait,
  holding every other convergence and plan of it.
- **The plan embedded in the resource's document.** Rejected: listing what awaits approval across
  resources, keeping rejected and superseded plans, and enforcing one unfinished plan need a
  collection, and embedding would tie every resource's schema to the plan's.
- **Apply in the approve request.** Rejected: the request would bypass the controller's lease and
  hold checks and race with it, and a held resource must defer the apply.
- **Partial approval.** Deferred; the first version approves whole plans (desired-state-model §5.4).
- **Always a separate approval step.** Rejected: every user is an admin, and the user who confirmed a
  deploy or uninstall dialog has already decided; making them approve the same change again on
  another screen added a step and no review. The safety switch keeps the separate step available.
- **No Change Plans at all.** Rejected: the plan still previews a change, records who decided it and
  which Workflow carried it out, and catches a change whose actions moved between approval and apply.

## Consequences

- One collection with its unique partial index, created by a schema migration, and one holding the
  Change Plan policy, whose absence reads as approve on submit.
- The convergence context gains the plan model, the planner port, and the `awaiting_approval`
  reason; the Platform is the first resource that plans, and the Server's destructive changes can
  adopt the same mechanism later.
- A destructive change confirmed by its user takes one step and at least one controller pass; one
  that waits takes two (write, approve), and a plan nobody approves expires instead of applying stale
  intent.
- The dashboard and the CLI gain an approval surface and a policy switch over a new API contract.

## Revision history

- 2026-11-02: referred to the people who use swallow as users instead of operators; the decision is unchanged.
- 2026-11-05: a write the user sends with `approve=true` approves its plan; plans swallow proposes, plans with blockers, and the installation's Change Plan policy switch keep the separate approval; a recomputation that changes an approved plan's actions proposes a new plan to approve.
