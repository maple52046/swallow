# Condition

- Bounded context: swallow-wide (resource ownership and convergence).
- Definition: A named judgement in a managed resource's status about how it stands against its Desired State, written only by that resource's controller.
- Allowed meaning: Each Condition has a `type`, a `status` of `true`, `false`, or `unknown`, a short `reason` code, a human `message`, `lastTransitionTime`, and the `observedGeneration` it was computed for. The shared types, which every resource uses with the same meaning, are:
  - `realized` — `true` when observed state matches the current spec. Common `false` or `unknown` reasons: `converging`, `capability_missing` (the provider cannot realize a field; the desired value is kept), `awaiting_approval` (a Change Plan is pending), `failed`, `provider_unreachable`, `provider_missing` (the provider no longer has the resource, for example a Server whose machine disappeared).
  - `drifted` — `true` when an observed field changed away from the spec without swallow asking for it; the message lists the fields.
  - `requires_attention` — `true` when the controller stopped trying automatically and an operator must act.
  - `held` — `true` while convergence is paused (see Hold); the reason says which: `server_locked`, `rescue_mode`, `workflow_lease`.
  A feature may add its own types but must not change the meaning of these.
- Disallowed meaning: Not a lifecycle state machine and not a replacement for an existing value set (OS Provisioning State, Platform Lifecycle State) until that feature migrates. Not written by a provider adapter or the API.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The Server's `realized` Condition is `false` with reason `capability_missing`, because its provisioner cannot set a Pool." / "`held` is `true` with reason `rescue_mode` while the Server is in Rescue Mode; drift is still reported."
- Related terms: Desired State, Observed State, Drift, Convergence, Change Plan.
- Change note: Added 2026-10-30 for [decision 069](../../../decisions/069-swallow-owns-desired-state.md) as the closed set of shared condition types in the desired-state model. 2026-10-31 ([decision 071](../../../decisions/071-provisioner-realized-desired-state.md)): added the `provider_missing` reason and the `held` reason codes.
