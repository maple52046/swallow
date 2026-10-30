# Change Plan

- Bounded context: swallow-wide (resource ownership and convergence).
- Definition: A preview of what realizing a Destructive Change will do, which an operator must approve before swallow carries it out as a Workflow.
- Allowed meaning: Lists the target resources and, for each, the planned action (from the current observed state to the desired state, whether it is destructive, and why), the spec `generation` and observation times it was computed from, the expected effect (for example which Servers leave service), and anything blocking it. Its states are:
  - `pending_approval` — computed and waiting for an operator.
  - `approved` — approved as a whole; its content is now immutable.
  - `applied` — rechecked against current state and turned into a Workflow, whose id it records.
  - `rejected` — an operator declined it.
  - `superseded` — the spec changed, or the observed state changed the action list, before it was applied; it needs a new plan.
  - `expired` — it outlived its validity period, because the observations it relied on aged.
  A resource has at most one unfinished Change Plan.
- Disallowed meaning: Not needed for a non-destructive change, which converges directly. Not partially approvable in the first version. Not a Workflow: it produces one when applied.
- Synonyms: Plan, when the context is clear.
- Deprecated terms: None.
- Examples: "Changing a deployed Server's desired OS Image produces a Change Plan that says the disk will be erased." / "The operator edited the spec again before approving, so the plan became `superseded`."
- Related terms: Destructive Change, Desired State, Convergence, Workflow, Condition.
- Change note: Added 2026-10-30 for [decision 069](../../../decisions/069-swallow-owns-desired-state.md), which adds a plan and approval step for destructive changes only.
