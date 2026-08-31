# Cluster

- Bounded context: Cluster Management.
- Definition: A Kubernetes or Slurm cluster that Swallow has registered or deployed, with Swallow-owned registration, policy, and lifecycle intent.
- Allowed meaning: A Cluster may originate as an externally registered cluster or as a Swallow deployment. Its membership is an observed projection read from the cluster API. Deploy creates a Cluster and an Operation. Uninstall removes k0s from the original deployment targets and retains the Cluster. Delete removes the Swallow record and projections without changing hosts.
- Disallowed meaning: Cluster must not mean a Server group whose membership Swallow assigns, a Kubernetes workload, or a durable copy of the cluster's internal configuration. Delete must not be used to mean uninstall, and uninstall must not be used for externally registered or Slurm clusters.
- Synonyms: None.
- Deprecated terms: Forget, when it means deleting a Cluster record.
- Examples: A registered Kubernetes Cluster can be deleted but not uninstalled. A Swallow-deployed k0s Cluster can be uninstalled and later deleted. A failed uninstall leaves the Cluster record so an operator can inspect and retry its Operation.
- Related terms: Server, Node Role, Cluster Topology, Operation, Cluster Lifecycle State, Integration, Exporter Ownership.
- Change note: Moved from the legacy glossary and clarified on 2026-08-29 when host-side uninstall and record-only delete became separate lifecycle actions. On 2026-08-30, deployment expanded from a fixed high-availability shape to the supported Cluster Topologies.
