# Controller

- Bounded context: swallow-wide (resource ownership and convergence).
- Definition: The convergence loop for one kind of managed resource: it compares the resource's Desired State with its Observed State and converges the difference through Provider Interfaces, reports Drift, or waits for a Change Plan.
- Allowed meaning: Level-triggered and idempotent — it acts on the current spec and status, and an event only wakes it. Triggered by the store change feed, its periodic resync, and operator requests. Only one instance of each Controller is active at a time, held by a store lease with a TTL, and it never runs in the API process. It is the only writer of its resource's Conditions and `observedGeneration`.
- Disallowed meaning: Not the provider reconcile that copies a provisioner's inventory into Observed State (that is observation, not convergence). Not a Workflow, although a Controller may create one for a long change. Not a provider adapter.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The Server tag Controller adds the missing manual tag in MAAS when the spec changes, and corrects a tag removed in the MAAS UI at its next resync."
- Related terms: Convergence, Desired State, Observed State, Condition, Drift, Hold.
- Change note: Added 2026-10-31 for [decision 071](../../../decisions/071-provisioner-realized-desired-state.md), when the first Controllers were built.
