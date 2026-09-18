# Kubernetes Application

- Bounded context: Platform Management.
- Definition: A workload grouping presented by the Kubernetes cluster explorer, aggregated live from a deployed Kubernetes Platform's own API — one of a Deployment, DaemonSet, StatefulSet, or an owner-less Pod — never a stored Swallow entity.
- Allowed meaning: A read-time projection the explorer builds by listing Pods and resolving their `ownerReferences` back to the controlling workload (a Pod owned by a ReplicaSet owned by a Deployment groups under that Deployment; a Pod with no controller is its own bare-Pod Application). It carries a `kind` (`Deployment | DaemonSet | StatefulSet | Pod`), a namespace, and observed replica/ready and pod state read from the cluster. The explorer may scale (adjust replicas), restart (roll pods), or delete an Application by acting directly on the underlying Kubernetes object.
- Disallowed meaning: Not a Kubernetes Custom Resource, not a Helm release (Helm grouping is future work), not a Swallow-owned record or a Mongo document, and not the same as a `Platform`. It is not persisted and carries no Swallow identity; it exists only for the duration of a live read. It must not be conflated with the pending `Workload` umbrella term or with a Server's Container inventory.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The explorer lists an `nginx` Deployment in namespace `web` as one Application with 3/3 pods ready." / "A bare Pod created without a controller appears as its own Pod-kind Application." / "Scaling an Application sets its Deployment's `replicas`; Swallow stores nothing."
- Related terms: Platform, Node Role, Server, Integration.
- Change note: Added on 2026-09-19 when the Kubernetes cluster explorer was introduced for self-deployed Platforms, borrowing Portainer's Application aggregation while keeping it a live view rather than a stored entity ([decision 032](../../../decisions/032-self-deployed-platform-management.md)).
