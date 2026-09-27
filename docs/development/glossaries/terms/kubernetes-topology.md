# Kubernetes Topology

- Bounded context: Platform Management.
- Definition: The placement and availability shape of the control plane and workload
  capacity in a Swallow-deployed Kubernetes Platform.
- Allowed meaning: One of the supported shapes below, inferred from Node Role assignments
  and whether a control-plane Server also runs workloads.
- Disallowed meaning: Not a count of all observed members, a network topology, a Server
  provisioning state, or a guarantee that workloads themselves are highly available.
- Synonyms: Deployment topology.
- Deprecated terms: None.
- Values:
  - `standalone`: One Server runs the control plane and workloads. It has no control-plane
    failover and does not use an API virtual IP.
  - `multi-node`: One Server runs the control plane and one or more Servers supply workload
    capacity. The control-plane Server may also run workloads. It has no control-plane
    failover and does not use an API virtual IP.
  - `high-availability`: An odd number of at least three Servers runs the control plane,
    at least one selected Server supplies workload capacity, and an API virtual IP provides
    a stable endpoint across control-plane failures.
- Examples: A single lab Server is `standalone`; one control-plane Server and six workers
  is `multi-node`; three dedicated control-plane Servers and four workers is
  `high-availability`.
- Related terms: Platform, Node Role, Server, Workflow.
- Change note: Added on 2026-08-30 when Kubernetes deployment was expanded beyond the
  original fixed high-availability shape.
