# Observed State

- Bounded context: swallow-wide (resource ownership and convergence).
- Definition: What a provider reports a managed resource actually is — the observed part of its status — carrying the source it came from and when it was observed.
- Allowed meaning: Fields written only by a provider adapter's observation path, each with its source (which Integration) and observation time, following the Staleness rules. Some observed facts have no desired counterpart (hardware inventory, GPU Inventory, provider-computed tags). The status of a resource holds observed state together with derived values and its Conditions.
- Disallowed meaning: Not authoritative for a write: an observed value is never written back to a provider and never becomes desired state unless a user explicitly adopts it. Not "known to be bad" when it is old — an unreachable provider only ages it.
- Synonyms: Status (the observed part of it); mirrored fact (the [decision 069](../../../decisions/069-swallow-owns-desired-state.md) (swallow owns desired state) name).
- Deprecated terms: None.
- Examples: "MAAS reports the Server's power as `off`; that is observed state, last observed four minutes ago." / "A user adopts the observed tag set, which writes it into the spec."
- Related terms: Desired State, Staleness, Condition, Drift, Provider Adapter, Integration.
- Change note: Added 2026-10-30 for decision 069; it is the role that mirrored facts play in the desired-state model.
