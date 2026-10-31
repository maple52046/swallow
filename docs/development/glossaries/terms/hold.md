# Hold

- Bounded context: swallow-wide (resource ownership and convergence).
- Definition: A pause on a managed resource's convergence: while held, its Controller observes it and reports Drift but makes no change to the outside world.
- Allowed meaning: A Server is held while it is locked by Server Lock (reason `server_locked`), while it is in Rescue Mode (`rescue_mode`), or while an unfinished Workflow owns it (`workflow_lease`; ownership has no TTL and ends with the Workflow); an operator's explicit hold is planned but not built. A hold is shown as the `held` Condition, whose reason says which; it ends by itself when its cause ends. A Change Plan may be produced during a hold but is applied only after it ends, except for explicitly allowed recovery actions.
- Disallowed meaning: Not Server Lock itself (one cause of a hold), not a Workflow's resource ownership (another cause), and not a failure; a held resource is not `requires_attention`.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "While the Server is locked, its tag Controller reports that a tag was removed in MAAS but does not add it back; `held` is `true` with reason `server_locked`."
- Related terms: Controller, Condition, Server Lock, Rescue Mode, Drift.
- Change note: Added 2026-10-31 for [decision 071](../../../decisions/071-provisioner-realized-desired-state.md), when the first Controllers began honoring holds.
