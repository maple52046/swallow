# Desired State

- Bounded context: swallow-wide (resource ownership and convergence).
- Definition: What an operator wants a managed resource to be — its spec. swallow owns it, and swallow's store is its only source of truth; providers realize it.
- Allowed meaning: The spec of a managed resource, written only through the API (by an operator, the dashboard, or the CLI) or by the higher-level resource that owns the field (for example a Platform deciding its Servers' Node Roles), with exactly one traceable writer per field. Every spec change increments the resource's `generation`. Secrets are desired state too, stored sealed and write-only. Existing swallow-owned intent (deployment network intent, a Deployment Template, Zone and Pool) is desired state under this name. Features adopt the term as they migrate to the desired-state model; until then their specs describe current behavior.
- Disallowed meaning: Not a value read back from a provider, and not a cache of one — a provider never writes desired state. Not a request or an action: it is a target the controller keeps converging to, not a one-time command. Not a fact swallow never owns (metrics, alert firing, workload scheduling, resources inside a Platform).
- Synonyms: Spec.
- Deprecated terms: None.
- Examples: "The Server's desired Zone is `rack-a`; MAAS still reports `default`, so the Server is not yet realized." / "Changing a deployed Server's desired OS Image is a Destructive Change and produces a Change Plan."
- Related terms: Observed State, Condition, Drift, Convergence, Change Plan, Destructive Change, Provider Adapter.
- Change note: Added 2026-10-30 for [decision 069](../../../decisions/069-swallow-owns-desired-state.md), which makes swallow the owner of desired state for every managed resource.
