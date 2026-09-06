# Allocation State

- Bounded context: Tenancy (swallow-owned policy).
- Definition: The tenancy assignment of a Server, derived from its ownership: whether it is unassigned or allocated to a team or to an individual user.
- Allowed meaning: A swallow-owned value with the closed set `free | team | user` — `free` (no owner), `team` (allocated to a Team), `user` (allocated to an individual User). This is the domain-authoritative set, matching the platform API contract and [decision 001](../../../decisions/001-system-ownership-boundaries.md) ("Tenancy: teams, users, server allocation"). Allocation is swallow-owned policy, not a fact any provisioner or Platform holds.
- Disallowed meaning: Not a provisioning, membership, or health value. The two-value set `free | assigned` used by an earlier dashboard `ServerRepository` port is a superseded implementation variant and must be mapped onto `free | team | user`; do not persist or filter on `assigned` as a domain value.
- Synonyms: None.
- Deprecated terms: `assigned` — it collapses the team/user distinction and is superseded by `team` and `user`.
- Examples: "A spare awaiting allocation is `free`." / "A GPU server allocated to the research team is `team`."
- Related terms: Owner, Team, User, Server.
- Change note: Added 2026-09-05 to converge the contradictory usage recorded in the outline (API contract `free|team|user` vs dashboard `free|assigned`); resolved to `free|team|user` per [decision 001](../../../decisions/001-system-ownership-boundaries.md) tenancy (reorg D7).
