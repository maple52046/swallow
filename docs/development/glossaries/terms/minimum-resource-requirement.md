# Minimum Resource Requirement

- Bounded context: Platform Management.
- Definition: An optional, system-wide deployment eligibility floor for the observed CPU, memory, and storage of every Server assigned to a Platform type.
- Allowed meaning: When enabled for Slurm, every controller, compute, and login Server must meet or exceed all configured dimensions before Swallow may create the Platform or Workflow. Equality is eligible. A missing or explicitly disabled requirement imposes no resource floor.
- Disallowed meaning: It is not a resource reservation, quota, Slurm scheduler capacity, role-specific sizing rule, image compatibility promise, or a value automatically derived from an OS Image's compressed or expanded size.
- Synonyms: Deployment requirement, when the Platform Management context is explicit.
- Deprecated terms: Mini resource requirement.
- Examples: A Slurm requirement of 4 CPU cores, 24576 MiB memory, and 80 GB storage makes a 4-core, 16 GiB Server ineligible for every Slurm role. A Server exactly at all three thresholds remains eligible. Disabling the requirement restores the ordinary deployment rules.
- Related terms: Platform, Server, Node Role, OS Deployment, OS Image.
- Change note: Added on 2026-09-13 to define the cross-component Slurm node-selection and deployment-preflight policy recorded by decision 026.
