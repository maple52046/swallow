# Node Group

- Bounded context: Platform Management.
- Definition: A named set of a Platform's members that share one role and one
  Deployment Template, and that the Platform scales together: members are added
  to or removed from a Node Group, and a new member is installed from the
  group's template.
- Allowed meaning: A name, one role of the Platform type (for Slurm
  `controller`, `login`, or `compute`), the Deployment Template its new members
  install from, an optional expected GPU count per node, and its members. A
  member's roles are the union of the roles of the groups it is in. Today only a
  Slurm Platform declares Node Groups, and only its `compute` groups grow or
  shrink; a Platform from before groups reads as one group per role.
- Disallowed meaning: Not a Pool or a Zone (those place Servers in the
  installation, independent of any Platform), not a Server Type (a derived GPU
  classification), and not a Slurm Partition (scheduling, which a group belongs
  to but does not define). Not a hardware class: the GPU model only suggests the
  first grouping, after which the group is the identity.
- Synonyms: Group (inside a Platform's context).
- Deprecated terms: None.
- Examples: A Slurm Platform has groups `controller`, `login`, `mi308x`, and
  `mi250x`; adding a Server to `mi250x` installs it from that group's template
  and leaves the `mi308x` nodes and their jobs alone.
- Related terms: Platform, Deployment Template, Slurm Partition, Node Role,
  Platform Claim, Destructive Change.
- Change note: Added 2026-11-14 with
  [decision 083](../../../decisions/083-platform-member-scaling.md) (Platform
  member scaling), when compute scaling and per-group templates needed a name for
  the unit a Platform scales; checked against Pool, Zone, and Server Type, which
  group Servers for placement or classification rather than inside a Platform.
