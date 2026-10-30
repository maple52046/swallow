# Drift

- Bounded context: swallow-wide (resource ownership and convergence).
- Definition: Observed State that departed from Desired State without swallow asking for it — typically because someone changed the resource directly in a provider or a Platform.
- Allowed meaning: Detected by the resource's controller when an observed field that matched the spec (or was not being converged) changes. Reported through the `drifted` Condition, listing the fields, and a timeline event; not corrected by default. An operator either adopts the observed value into the spec or reapplies the desired state (directly when non-destructive, through a Change Plan when destructive). A field may opt into automatic correction only when correcting it is non-destructive and its feature spec gives the reason.
- Disallowed meaning: Not a difference swallow is still converging (that is `realized=false` with reason `converging`). Not a change to an observed-only fact such as hardware, which is just a new observation. Not possible for facts swallow never owns (metrics, alerts, workload scheduling, resources inside a Platform).
- Synonyms: None.
- Deprecated terms: None.
- Examples: "Someone moved the Machine to another Pool in the MAAS UI; the Server reports drift instead of silently taking the new Pool." / "The operator adopts the drifted tags, which updates the spec."
- Related terms: Desired State, Observed State, Condition, Convergence, Change Plan.
- Change note: Added 2026-10-30 for [decision 069](../../../decisions/069-swallow-owns-desired-state.md), which makes drift reported rather than corrected by default.
