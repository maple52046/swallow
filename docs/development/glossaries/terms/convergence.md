# Convergence

- Bounded context: swallow-wide (resource ownership and convergence).
- Definition: The continuous process by which a resource's controller brings Observed State toward Desired State, changing the outside world only through provider interfaces.
- Allowed meaning: Level-triggered and idempotent: the controller acts on the current spec and observation, not on the content of an event, so a missed event loses no work. Non-destructive differences converge automatically (directly, or through a Workflow the controller starts for long work); a Destructive Change produces a Change Plan instead. Paused while the resource is held. Failures back off up to a limit, then set `requires_attention`.
- Disallowed meaning: Not provider reconcile — the existing reconcile that syncs provider inventory into swallow is observation (it writes observed state), while Convergence changes the outside world to match the spec. Not automatic correction of Drift, which is reported unless a field opts in. Not the convergent ("ensure") execution of a single Task, which is how one Workflow step stays idempotent.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "An operator adds a tag; the controller converges it to MAAS without a Change Plan." / "The Pool controller backs off while MAAS is unreachable and reports `provider_unreachable`."
- Related terms: Desired State, Observed State, Condition, Drift, Change Plan, Destructive Change, Workflow, Provider Interface.
- Change note: Added 2026-10-30 for [decision 069](../../../decisions/069-swallow-owns-desired-state.md), distinguishing it from the existing provider reconcile.
