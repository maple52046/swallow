# Destructive Change

- Bounded context: swallow-wide (resource ownership and convergence).
- Definition: A change to Desired State whose realization would erase or replace data on a host, take a Server out of service or out of a Platform, interrupt running workloads, or could not be undone by applying the previous spec.
- Allowed meaning: Reinstalling an OS on a deployed Server, Release, Recover, creating or uninstalling a Platform, changing a Node Role or removing a Server from a Platform, uninstalling Managed Software, and powering off a host in use. A Destructive Change always produces a Change Plan. When it is unclear whether a change is destructive, it is treated as destructive; a feature spec may classify a change as non-destructive only with a written reason.
- Disallowed meaning: Not defined by how long the change takes or whether it runs as a Workflow — deploying an OS onto a `ready` Server is long but non-destructive. Not an error or a risk rating.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "Moving a Server to another Pool is non-destructive; releasing it is a Destructive Change." / "Changing a Slurm node's role from compute to controller is destructive."
- Related terms: Change Plan, Desired State, Convergence, Release, Provider Recovery, Platform.
- Change note: Added 2026-10-30 for [decision 069](../../../decisions/069-swallow-owns-desired-state.md), which decides which changes need operator approval.
