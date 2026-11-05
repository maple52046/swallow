# Change Plans

## Status

Active.

## Owner Component

`api-server`.

## Consumer Components

`dashboard` (the approval screen and its policy card) and the `cli` (`swallow plans`, including
`swallow plans policy`).

## Purpose

A destructive change is approved by a user before swallow carries it out
([decision 069](../../../../../docs/decisions/069-swallow-owns-desired-state.md) (swallow owns desired state),
[decision 076](../../../../../docs/decisions/076-change-plans.md) (change plans)). A resource API that accepts a
destructive spec write commits the spec and answers `202 Accepted` with the
[Change Plan](../../../../../docs/development/glossaries/terms/change-plan.md) it proposed. The user may
approve the plan with the write itself ([Approval On Submit](#approval-on-submit)); otherwise it waits
for this contract's approve route. This contract reads, approves, and rejects plans and holds the
installation's Change Plan policy. The resource's controller applies an approved plan as a Workflow.

## Related Glossary Terms

[Change Plan](../../../../../docs/development/glossaries/terms/change-plan.md),
[Destructive Change](../../../../../docs/development/glossaries/terms/destructive-change.md),
[Condition](../../../../../docs/development/glossaries/terms/condition.md),
[Workflow](../../../../../docs/development/glossaries/terms/workflow.md).

## Endpoints

All endpoints require the `admin` role (an access token or an API Key), per
[conventions](conventions.md).

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/change-plans` | List plans, newest first. |
| `GET` | `/api/v1/change-plans/{planId}` | Read one plan. |
| `POST` | `/api/v1/change-plans/{planId}/approve` | Approve a pending plan as a whole. |
| `POST` | `/api/v1/change-plans/{planId}/reject` | Reject a plan that has not been applied. |
| `GET` | `/api/v1/change-plan-policy` | Read whether a change approved with its submission is approved at once. |
| `PUT` | `/api/v1/change-plan-policy` | Replace that policy. |

`GET /change-plans` takes optional `resourceKind` and `resourceId` (one resource's plans) and
`state`, which may repeat (`?state=pending_approval&state=approved`). An unknown `state` is
`400 validation_error`. The list is small, so it is an `{ "items": [...] }` object without
pagination.

## Change Plan Resource

```json
{
  "id": "6f0c…",
  "resource": { "kind": "platform", "id": "p1" },
  "generation": 4,
  "observedAt": "2026-11-01T10:00:00Z",
  "actions": [
    {
      "target": { "kind": "server", "id": "s1" },
      "action": "remove-member",
      "from": "worker",
      "to": "",
      "destructive": true,
      "reason": "s1 was removed from the Platform's spec"
    }
  ],
  "effects": ["s1 leaves the Platform and stops running its workloads"],
  "blockers": [],
  "state": "pending_approval",
  "stateReason": "",
  "decidedBy": "",
  "decidedAt": null,
  "workflowId": "",
  "expiresAt": "2026-11-02T10:00:00Z",
  "createdAt": "2026-11-01T10:00:00Z",
  "updatedAt": "2026-11-01T10:00:00Z"
}
```

- `generation` is the spec generation the plan realizes; `observedAt` is when the observed state
  it relied on was read.
- Each action names its target, the change, the current and desired values (for people; empty
  when there is none), whether it is destructive, and why.
- `effects` are the expected consequences; `blockers` are failed preflight checks, and a plan with
  blockers cannot be approved.
- `state` is `pending_approval`, `approved`, `applied`, `rejected`, `superseded`, or `expired`
  (glossary). A resource has at most one plan in `pending_approval` or `approved`.
- `stateReason` says, for people, why a plan reached its state; for a `pending_approval` plan, why
  it waits (see [Approval On Submit](#approval-on-submit)). `decidedBy` and `decidedAt` name the user
  who approved or rejected it; `workflowId` is the Workflow an applied plan produced.
- `expiresAt` is 24 hours after the plan was computed by default (`changePlanTTL`).

## Approval On Submit

A resource API whose spec writes can be destructive takes the query parameter `approve=true` on
those writes (for Platforms: `POST /platforms`, `PUT /platforms/{id}/spec`, and
`DELETE /platforms/{id}`, see [platforms.md](platforms.md)). It means the user who sends the write
approves the change with it, as a user who confirmed a dialog or passed a CLI flag does. When the
write proposes a plan, the plan is stored `approved` in the same transaction, with `decidedBy` the
user and `decidedAt` the write's time, and the resource's controller is asked to apply it at once;
the response carries the approved plan. The timeline records `plan_proposed` and then
`plan_approved`.

The plan is stored `pending_approval` instead, and waits for
[`POST .../approve`](#approve-and-reject), when:

- the write did not send `approve=true` (`stateReason` `submitted without approval`);
- the [Change Plan policy](#change-plan-policy) has `approveOnSubmit: false`
  (`stateReason` `the Change Plan policy requires a separate approval`); or
- the plan has blockers (`stateReason` `it has blockers and cannot be approved`).

A plan swallow proposes on its own, not in answer to a write, is always `pending_approval`. That
includes the plan proposed when the controller's recomputation of an approved plan differs (see
[Approve And Reject](#approve-and-reject)).

## Change Plan Policy

`GET /api/v1/change-plan-policy` reads the installation's policy; `PUT` replaces it.

```json
{
  "approveOnSubmit": true,
  "updatedAt": "2026-11-05T02:00:00Z",
  "updatedBy": "admin"
}
```

- `approveOnSubmit` says whether a write sent with `approve=true` approves its plan at once. `false`
  is the safety switch: every Change Plan waits for a separate approval, whatever the write sent.
- `updatedAt` and `updatedBy` are when and by whom it was last replaced; a policy that was never
  stored reads `{ "approveOnSubmit": true, "updatedAt": null, "updatedBy": "" }`, not `404`.

The `PUT` body is `{ "approveOnSubmit": <boolean> }`; a missing or non-boolean `approveOnSubmit` is
`400 validation_error`. The response is the stored policy. The new policy applies to the writes
that follow; plans proposed before keep their state.

## Approve And Reject

`POST /change-plans/{planId}/approve` approves a `pending_approval` plan as a whole and freezes its
content; the response is the approved plan. The resource's controller then recomputes the approved
plan, whether it was approved here or with its submission. When the spec's generation changed, the
plan becomes `superseded` and the newer write's plan replaces it. When the recomputed actions differ
(by target, action, and desired value), the plan becomes `superseded` and, if the recomputed change
is still destructive, a new `pending_approval` plan is proposed in the same transaction
(`stateReason` `the approved plan no longer matched what the change would do`); it is never
approved on its own. While the resource is held the plan stays `approved`. Otherwise the controller
starts the Workflow and the plan becomes `applied`.

`POST /change-plans/{planId}/reject` rejects a plan in `pending_approval` or `approved`.

A new destructive spec write supersedes the resource's unfinished plan. A plan past `expiresAt` is
`expired`; approving or rejecting it answers `409` and expires it.

Creation, approval, rejection, supersession, expiry, and apply appear on the resource's timeline as
`plan_proposed`, `plan_approved`, `plan_rejected`, `plan_superseded`, `plan_expired`, and
`plan_applied` events. While a plan is `pending_approval` the resource's `realized` Condition is
`false` with reason `awaiting_approval`; once it is `approved` and until its Workflow is written, the
reason is `converging`.

## Errors

| Code | Status | When |
| --- | --- | --- |
| `validation_error` | 400 | An unknown `state` filter; a policy `PUT` without a boolean `approveOnSubmit`. |
| `not_found` | 404 | No plan has the id. |
| `conflict` | 409 | Approving a plan that is not `pending_approval`, has blockers, or is past `expiresAt`; rejecting one that is not `pending_approval` or `approved`, or is past `expiresAt`. |

## Compatibility Notes

- Since 2026-11-05 a destructive write sent with `approve=true` stores its plan `approved` when the
  Change Plan policy allows it and the plan has no blockers, and the policy routes were added. This
  is additive: a write without the parameter behaves as before. Two readings changed for every plan:
  a `pending_approval` plan's `stateReason` says why it waits (it used to be empty), and a resource
  whose plan is `approved` but not yet applied reports `realized` reason `converging` (it used to
  report `awaiting_approval`). A client that treated every unfinished plan as waiting for a user
  reads `state` instead.
