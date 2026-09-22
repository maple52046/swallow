# Provider Recovery

- Bounded context: OS Provisioning.
- Definition: The Swallow-owned policy and operator intents that return a Server whose provisioning axis is not usable — `failed`, `broken`, `rescue`, or `allocated` (reserved but not deployed) — to the provider's `ready` pool. Swallow decides which intent is allowed from which normalized `provisioning.state` and which provider primitive realizes it; the provisioner only executes the chosen primitive.
- Allowed meaning: Two operator-facing recovery intents plus a gating policy.
  - **Recover** (labelled "Return to Ready"): a durable Operation that reaches `ready` by choosing the primitive per state — `broken` uses Mark fixed; `rescue` exits rescue and then Releases if still not `ready`; `failed` and `allocated` use Release.
  - **Release**: returns a Machine to `ready`, allowed from `deployed`, `allocated`, `failed`, `broken`, and `rescue`.
  - The policy also gates the advanced primitives (Mark fixed, Mark broken, Rescue enter/exit) on live state and refuses with a Swallow-authored reason, never only the provider's rejection text.
- Disallowed meaning: Not deletion of a Server or its projection; not OS Deployment; not a platform (Kubernetes/Slurm) repair, rerun, or uninstall, which act on a Workflow rather than on the provider lifecycle of one Machine. Provider Recovery does not automatically run after a failure — it is operator-initiated.
- Synonyms: None. "Return to Ready" is the UI label for Recover; `recover` is the action value.
- Deprecated terms: None.
- Examples: "A node left `failed` by a platform deploy is made usable again with Recover, then the Task is retried." / "Recover on a `broken` Server runs Mark fixed; on a `failed` Server it Releases; both converge to `ready`."
- Related terms: Release, Rescue Mode, Server Status, Server Lock, OS Provisioning Provider, Workflow.
- Change note: Added 2026-09-20 with decision 033 to name Swallow's provider recovery policy and its Recover intent, and to record that Release is allowed from failed/broken/rescue, not only deployed. Updated 2026-09-21 (decision 036) to add `allocated` as a Recover/Release source so a machine parked as reserved — including a verification borrow whose deploy ended early — is recoverable instead of a dead end.
