# Change Plan

- Bounded context: swallow-wide (resource ownership and convergence).
- Definition: A preview of what realizing a Destructive Change will do, which a user approves — with the request that submits the change, or afterwards — before swallow carries it out as a Workflow.
- Allowed meaning: Lists the target resources and, for each, the planned action (from the current observed state to the desired state, whether it is destructive, and why), the spec `generation` and observation times it was computed from, the expected effect (for example which Servers leave service), and anything blocking it. Its states are:
  - `pending_approval` — computed and waiting for a user: the change was submitted without approval, the plan has blockers, swallow proposed it on its own (for example because an approved plan's actions changed before it was applied), or the Change Plan policy requires a separate approval.
  - `approved` — approved as a whole, by a user who confirmed the change when submitting it or who approved the waiting plan; its content is now immutable.
  - `applied` — rechecked against current state and turned into a Workflow, whose id it records.
  - `rejected` — a user declined it.
  - `superseded` — the spec changed, or the observed state changed the action list, before it was applied; it needs a new plan.
  - `expired` — it outlived its validity period, because the observations it relied on aged.
  A resource has at most one unfinished Change Plan. The Change Plan policy is the installation's runtime switch over approval on submit: on by default, and when an admin turns it off every Change Plan waits for a separate approval.
- Disallowed meaning: Not needed for a non-destructive change, which converges directly. Not partially approvable in the first version. Not a Workflow: it produces one when applied. Not skipped by approving on submit: the plan is still computed, stored, and recomputed before apply.
- Synonyms: Plan, when the context is clear.
- Deprecated terms: None.
- Examples: "Changing a deployed Server's desired OS Image produces a Change Plan that says the disk will be erased." / "The user confirmed the uninstall dialog, so its Change Plan was approved with the request and the uninstall started." / "The safety switch is on, so the confirmed uninstall's plan waits in `pending_approval` for a separate approval." / "The user edited the spec again before approving, so the plan became `superseded`."
- Related terms: Destructive Change, Desired State, Convergence, Workflow, Condition.
- Change note: Added 2026-10-30 for [decision 069](../../../decisions/069-swallow-owns-desired-state.md) (swallow owns desired state), which adds a plan and approval step for destructive changes only. On 2026-11-05 a change the user confirms when submitting it is approved with the request, and the Change Plan policy was added as the switch that restores a separate approval ([decision 076](../../../decisions/076-change-plans.md) (change plans)).
