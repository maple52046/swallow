# Exporter Ownership

- Bounded context: Observability, owned by `api-server`; surfaced by `dashboard`.
- Definition: Which subsystem is responsible for installing and running a host's
  Prometheus exporters. Each host has exactly one owner at a time, so two exporters never
  contend for the same fixed port (9100 node-exporter, 5000 RDC).
- Allowed meaning: The ownership values are:

  | Value | Meaning |
  | --- | --- |
  | `ansible` | swallow's host automation installs the host exporters: the Node Exporter, as Managed Software. swallow does not install the RDC exporter on hosts |
  | `k8s` | a Kubernetes DaemonSet installs the exporters on the node |
  | `unmanaged` | swallow does not manage exporters here: the user installed something by hand, or the machine is locked. It is only ever resolved, never a Platform's policy |

  Ownership is decided as a per-platform policy that generalises `gpuStackOwner`. The
  **effective owner** of a host is resolved in order: a locked machine
  (`provisioning.locked`) is `unmanaged`; otherwise a member of a Kubernetes platform
  follows that platform's policy; otherwise `ansible`. Regardless of owner the exporter
  listens on the same fixed port and is scraped through swallow's `http_sd` with a
  `server_id` label, so the metrics join is identical across owners. Changing a
  platform's `exporterOwner` only records the policy; it does not install or remove
  exporters. The policy is read when swallow composes the Node Exporter: a Platform
  deployment installs it on the Servers it provisions unless the policy is `k8s`, a
  standalone Node Exporter install is refused on a Server whose effective owner is not
  `ansible`, and a kept-servers uninstall installs it on the kept Servers whatever the
  policy, since the policy ends with the Platform.
- Disallowed meaning: Not a scrape or health status — ownership says who installs the
  exporter, not whether it is currently up. `unmanaged` does not mean "no exporter"; it
  means swallow will neither install nor remove one. Do not treat a locked machine as
  `ansible` and then skip it; it is `unmanaged` by definition. Not an action: switching
  the policy installs or removes nothing.
- Synonyms: None. "Exporter owner" is the same concept.
- Deprecated terms: None. This supersedes the earlier fixed rule in
  [decision 003](../../../decisions/003-metrics-label-contract.md) (metrics label contract) that the host
  node-exporter always stays and a platform DaemonSet is always disabled.
- Examples: "A CPU server in no platform has effective owner `ansible`, so the OS
  deployment that provisions it also installs the Node Exporter." / "Switching a
  platform's `exporterOwner` to `k8s` records the policy; the Node Exporter already on
  its members stays until a user uninstalls it." / "The three locked physical servers
  are `unmanaged`; swallow never installs, removes, or otherwise changes them."
- Related terms: Server Type, Server Status, Workflow, Automation Configuration, Managed Software.
- Change note: Added for the Prometheus monitoring integration, generalising the
  per-platform `gpuStackOwner` idea into a single switchable exporter owner per host so
  Ansible and Kubernetes deployment can coexist without duplicating exporters. 2026-11-03:
  `ansible` means the Node Exporter installed as Managed Software, composed into every OS
  deployment instead of installed when a Server reaches `deployed`; swallow does not install
  the RDC exporter on hosts; and the definition now states that changing `exporterOwner`
  only records the policy, which earlier examples described as switching the exporters.
