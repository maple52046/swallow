# Slurm Partition

- Bounded context: Platform Management (Slurm).
- Definition: A Slurm scheduling queue of a Slurm Platform, declared in its spec
  over one or more compute Node Groups, with its own limits; jobs are submitted
  to a partition and run on its nodes.
- Allowed meaning: A name, whether it is the default (exactly one is), the
  compute Node Groups it contains (at least one; a group may be in several
  partitions), a maximum job time, an optional default memory per CPU (otherwise
  the smallest share of its own nodes), an over-subscription mode, and optional
  Unix groups allowed to use it. Every compute group with members is in at least
  one partition. With Omnistat every partition hands out whole nodes.
- Disallowed meaning: Not a Node Group (a partition schedules over groups and
  does not install or scale anything), not a Pool, and not a Kubernetes
  namespace. The live partition state slurmrestd reports is observed, not the
  spec.
- Synonyms: Partition (inside the Slurm context).
- Deprecated terms: None.
- Examples: Partition `main` is the default over `mi308x` and `mi250x`;
  partition `big` contains only `mi308x` with a two-day limit. Removing `big`
  while it still has queued jobs is blocked with the job count.
- Related terms: Node Group, Platform, Platform Module.
- Change note: Added 2026-11-14 with
  [decision 083](../../../decisions/083-platform-member-scaling.md) (Platform
  member scaling), when a Slurm Platform moved from one fixed partition `main`
  over every node to partitions in its spec.
