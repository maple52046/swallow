# Platform

- Bounded context: Platform Management.
- Definition: A Kubernetes or Slurm runtime environment that Swallow has registered or deployed, including a single-node Kubernetes installation. Swallow owns its registration, policy, and lifecycle intent but observes membership from the runtime's own API.
- Allowed meaning: A Platform may be externally registered or Swallow-deployed. Deploy creates a Platform and a durable Operation. Uninstall removes the Swallow-deployed runtime from its original targets while retaining the Platform record. Delete removes only the record and Swallow-owned projections.
- Disallowed meaning: Platform must not mean a Site, provisioner Integration, arbitrary Server group, Kubernetes workload, or durable copy of the runtime internal state. Delete must not imply host-side uninstall.
- Synonyms: None.
- Deprecated terms: Cluster, when naming the Swallow aggregate or public resource; Forget, when deleting a Platform record.
- Examples: A standalone k0s installation is one Kubernetes Platform. A registered Slurm Platform can be deleted but not uninstalled. A failed uninstall retains the Platform for diagnosis and retry.
- Related terms: Server, Node Role, Kubernetes Topology, Operation, Platform Lifecycle State, Integration, Exporter Ownership.
- Change note: Renamed from Cluster on 2026-09-03 so the aggregate covers single-node and multi-node runtimes without implying a particular topology. Kubernetes cluster remains valid only when referring to the external technology itself.
