# GPU Allocation Mode

- Bounded context: Platform Management, owned by `api-server`.
- Definition: How a Swallow-managed GPU Operator exposes a node's GPUs to Kubernetes workloads.
  Exactly one mode is active at a time; the two are alternatives, not layers.
- Allowed meaning: The values are:

  | Value | Meaning |
  | --- | --- |
  | `device-plugin` | GPUs are an extended resource (`amd.com/gpu`, or per-partition names with mixed resource naming) that a Pod requests in `resources.limits` |
  | `dra` | GPUs are Dynamic Resource Allocation devices published in `ResourceSlice`s; a Pod requests them through a `ResourceClaim` of the `gpu.amd.com` device class. Requires Kubernetes `resource.k8s.io/v1` (k0s v1.34 or later) |

  Changing the mode on a running Platform turns the old mode off before the new one starts, and
  is refused while any Pod still holds an AMD GPU.
- Disallowed meaning: Not GPU partitioning (compute or memory partition profiles) and not a
  scheduling policy such as NUMA alignment.
- Synonyms: None. The dashboard labels are "Device Plugin" and "DRA"; the domain values are
  `device-plugin` and `dra`.
- Deprecated terms: None.
- Examples: "The Platform uses `device-plugin`, so the vLLM Pod asks for `amd.com/gpu: 2`." /
  "After switching to `dra`, each GPU node publishes one ResourceSlice with its devices."
- Related terms: GPU Operator, GPU Operator State.
- Change note: Added with the Swallow-managed AMD GPU Operator (decision 062).
