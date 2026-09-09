# Node Role

- Bounded context: Platform.
- Definition: The part a server plays in a platform — either running the control plane or
  joining as a workload-only member.
- Allowed meaning: One of the closed value set below, used both to describe an observed
  platform member and to assign a server's role when deploying a platform.
- Disallowed meaning: Not a server's provisioning state, health, or ownership; those are
  separate axes. Not the k0s-specific installation word `controller`, which is an
  implementation detail of the deployment playbook and must not appear in domain language,
  API payloads, or the UI.
- Synonyms: None.
- Deprecated terms: `master` — an older Kubernetes label for a control-plane node; read
  from a platform as `control-plane`, never emitted.
- Values:
  - `control-plane`: the server runs the platform's control plane. A deployment may
    explicitly allow this Server to run workloads too; that placement choice does not
    create a third Node Role. A dedicated k0s control-plane Server is read from the
    control-plane lease because it does not register as a Kubernetes node.
  - `worker`: the server runs workloads. In Kubernetes it is a registered node; in Slurm
    it is a compute node in a partition.
- Examples:
  - "Assign three servers the `control-plane` role and four the `worker` role" describes a
    deployment request.
  - "Assign one Server `control-plane` and allow workloads on it" describes a standalone
    deployment without inventing a combined role.
  - "The member's role is `control-plane`" describes what a platform reports about a server.
- Related terms: Platform, Server, Server Status (its membership axis carries the role),
  Operation (a deployment operation assigns roles).
- Change note: Added for platform deployment. Clarified on 2026-08-30 that workload
  co-location is a deployment placement choice rather than a third Node Role, enabling
  standalone and non-HA multi-node topology without changing observed role vocabulary.
  Clarified on 2026-09-07 (see [decision 019](../../../decisions/019-slurm-platform-deployment.md))
  that Node Role is the Kubernetes-shaped **observed** membership axis; a Slurm **deployment**
  instead assigns Slurm daemons per node (a server may run the controller daemon `slurmctld`,
  the compute daemon `slurmd`, or both). Those per-daemon deployment assignments are not Node
  Role values, and here `controller`/`compute` are the SchedMD daemon names in Slurm's own
  vocabulary, not the disallowed k0s installation word. A deployed Slurm member is still read
  back on the membership axis with its partition as the role.
