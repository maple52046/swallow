# Node Role

- Bounded context: Cluster.
- Definition: The part a server plays in a cluster — either running the control plane or
  running workloads.
- Allowed meaning: One of the closed value set below, used both to describe an observed
  cluster member and to assign a server's role when deploying a cluster.
- Disallowed meaning: Not a server's provisioning state, health, or ownership; those are
  separate axes. Not the k0s-specific installation word `controller`, which is an
  implementation detail of the deployment playbook and must not appear in domain language,
  API payloads, or the UI.
- Synonyms: None.
- Deprecated terms: `master` — an older Kubernetes label for a control-plane node; read
  from a cluster as `control-plane`, never emitted.
- Values:
  - `control-plane`: the server runs the cluster's control plane. In a highly available
    k0s cluster these are dedicated controllers that do not also run workloads, and k0s
    does not register them as Kubernetes nodes, so their membership is read from the
    control-plane lease rather than the node list.
  - `worker`: the server runs workloads. In Kubernetes it is a registered node; in Slurm
    it is a compute node in a partition.
- Examples:
  - "Assign three servers the `control-plane` role and four the `worker` role" describes a
    deployment request.
  - "The member's role is `control-plane`" describes what a cluster reports about a server.
- Related terms: Cluster, Server, Server Status (its membership axis carries the role),
  Operation (a deployment operation assigns roles).
- Change note: Added for cluster deployment. The role vocabulary already existed implicitly
  on the server membership axis (`control-plane` / `worker`); this records it as a closed
  value set so that deployment role assignment and observed membership use the same words,
  and so that the k0s term `controller` is explicitly excluded from domain language.
