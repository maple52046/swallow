# Exporter Ownership

- Bounded context: Observability, owned by `api-server`; surfaced by `dashboard`.
- Definition: Which subsystem is responsible for installing and running a host's
  Prometheus exporters (node-exporter on every host, the RDC exporter on AMD GPU hosts).
  Each host has exactly one owner at a time, so two exporters never contend for the same
  fixed port (9100 node, 5000 RDC).
- Allowed meaning: The ownership values are:

  | Value | Meaning |
  | --- | --- |
  | `ansible` | swallow installs the exporter as a host container via the Swallow-owned Ansible executor |
  | `k8s` | a Kubernetes DaemonSet installs the exporter on the node |
  | `unmanaged` | swallow does not manage exporters here: the operator installed something by hand, or the machine is locked |

  Ownership is decided as a per-platform policy that generalises `gpuStackOwner`. The
  **effective owner** of a host is resolved in order: a locked machine
  (`provisioning.locked`) is `unmanaged`; otherwise a member of a Kubernetes platform
  follows that platform's policy; otherwise `ansible`. Regardless of owner the exporter
  listens on the same fixed port and is scraped through swallow's `http_sd` with a
  `server_id` label, so the metrics join is identical across owners.
- Disallowed meaning: Not a scrape or health status — ownership says who installs the
  exporter, not whether it is currently up. `unmanaged` does not mean "no exporter"; it
  means swallow will neither install nor remove one. Do not treat a locked machine as
  `ansible` and then skip it; it is `unmanaged` by definition.
- Synonyms: None. "Exporter owner" is the same concept.
- Deprecated terms: None. This supersedes the earlier fixed rule in
  [decision 003](../../../decisions/003-metrics-label-contract.md) that the host
  node-exporter always stays and a platform DaemonSet is always disabled.
- Examples: "A deployed CPU server in no platform has effective owner `ansible`, so
  reaching `deployed` auto-installs node-exporter." / "When a platform's `exporterOwner`
  is switched to `k8s`, its members' Ansible exporters are removed and a DaemonSet takes
  over on the same ports." / "The three locked physical servers are `unmanaged`; swallow
  never installs, removes, or otherwise changes them."
- Related terms: Server Type, Server Status, Operation, Automation Configuration.
- Change note: Added for the Prometheus monitoring integration, generalising the
  per-platform `gpuStackOwner` idea into a single switchable exporter owner per host so
  Ansible and Kubernetes deployment can coexist without duplicating exporters.
