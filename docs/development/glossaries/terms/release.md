# Release

- Bounded context: OS Provisioning.
- Definition: Returning a Machine to its OS Provisioning Provider's available pool, making it `ready` again.
- Allowed meaning: A provider-backed action that changes only the Server's `provisioning` axis (through `releasing` back to `ready`). It does not remove the Server: the physical machine still exists and swallow still manages it. It is allowed from `deployed` and, as the primary provider-recovery path, from the not-usable states `failed`, `broken`, and `rescue`; the intent is always to converge the Machine to `ready`. Provider-network cleanup that must outlive the asynchronous transition is coordinated by a Provisioning Task.
- Disallowed meaning: Not deletion of a Server or its projection; not an OS Deployment; and not the signed Swallow "release bundle" that ships playbooks. This term is the provider action only. Release is not restricted to `deployed`: treating it as deployed-only is the superseded behaviour.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "Release moves the provisioning axis through `releasing` to `ready`; the Server remains managed." / "Release acts on the provider only." / "Release a `failed` Server to return it to the ready pool without deleting it."
- Related terms: Provider Recovery, Rescue Mode, OS Deployment, Machine, Provisioning Task, Server Status, OS Provisioning Provider.
- Change note: Migrated 2026-09-05 from the narrative `docs/glossaries/provisioning.md` (reorg D2). The name collides with the Swallow release bundle; this term is scoped to the provider action. Updated 2026-09-20 (decision 033): Release is allowed from `deployed`, `failed`, `broken`, and `rescue`, as the primary provider-recovery path back to `ready`.
