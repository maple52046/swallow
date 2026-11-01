# Change Plans

## Status

Active.

## Owner Component

`api-server`.

## Consumer Components

`dashboard` (the approval screen) and the `cli` (`swallow plans`).

## Purpose

A destructive change waits for an operator's approval before swallow carries it out
([decision 069](../../../../../docs/decisions/069-swallow-owns-desired-state.md),
[decision 076](../../../../../docs/decisions/076-change-plans.md)). A resource API that accepts a
destructive spec write commits the spec and answers `202 Accepted` with the
[Change Plan](../../../../../docs/development/glossaries/terms/change-plan.md) it proposed; this
contract reads, approves, and rejects plans. The resource's controller applies an approved plan as
a Workflow.

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
- `stateReason` says why a plan reached its state; `decidedBy` and `decidedAt` name the operator
  who approved or rejected it; `workflowId` is the Workflow an applied plan produced.
- `expiresAt` is 24 hours after the plan was computed by default (`changePlanTTL`).

## Approve And Reject

`POST /change-plans/{planId}/approve` approves a `pending_approval` plan as a whole and freezes its
content; the response is the approved plan. The resource's controller then recomputes it: when the
spec's generation changed, or the recomputed actions differ (by target, action, and desired value),
the plan becomes `superseded` and the resource needs a new one; while the resource is held the
plan stays `approved`; otherwise the controller starts the Workflow and the plan becomes `applied`.

`POST /change-plans/{planId}/reject` rejects a plan in `pending_approval` or `approved`.

A new destructive spec write supersedes the resource's unfinished plan. A plan past `expiresAt` is
`expired`; approving or rejecting it answers `409` and expires it.

Creation, approval, rejection, supersession, expiry, and apply appear on the resource's timeline as
`plan_proposed`, `plan_approved`, `plan_rejected`, `plan_superseded`, `plan_expired`, and
`plan_applied` events. While a plan is unfinished the resource's `realized` Condition is `false`
with reason `awaiting_approval`.

## Errors

| Code | Status | When |
| --- | --- | --- |
| `validation_error` | 400 | An unknown `state` filter. |
| `not_found` | 404 | No plan has the id. |
| `conflict` | 409 | Approving a plan that is not `pending_approval`, has blockers, or is past `expiresAt`; rejecting one that is not `pending_approval` or `approved`, or is past `expiresAt`. |
