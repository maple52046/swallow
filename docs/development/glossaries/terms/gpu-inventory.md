# GPU Inventory

- Bounded context: Compute Resource inventory, shared by provisioning, Server projections,
  capacity summaries, and monitoring applicability.
- Definition: The provisioner-observed graphics and accelerator devices attached to a Server,
  grouped by vendor, model, and GPU Kind. Each group carries a physical-device count; telemetry
  and workload allocation do not belong to this inventory.
- Allowed meaning: Every inventory group has one of these kinds:

  | Value | Meaning |
  | --- | --- |
  | `compute` | A workload accelerator that contributes Server compute capacity and may produce GPU metrics. |
  | `display` | A local-console or BMC graphics controller, such as ASPEED or Matrox G200, which does not contribute workload accelerator capacity. |

  A provider may expose only a generic GPU hardware class. In that case swallow identifies known
  server-console controllers as `display` and preserves every other reported GPU as `compute` so
  an unnamed accelerator is not silently discarded. The complete inventory retains both kinds.
- Disallowed meaning: Not Server Type, which is the tag-derived exporter-targeting policy; not a
  GPU telemetry stream, status, profile run, or workload allocation; and not a claim that a
  `display` controller can run compute workloads merely because the provisioner calls it a GPU.
- Synonyms: Compute GPU and accelerator are accepted for `compute`; display GPU, graphics
  controller, and management GPU are accepted for `display` when referring to this inventory.
- Deprecated terms: None.
- Examples: "tainan-node01 has eight compute GPUs and one ASPEED display GPU; its accelerator
  capacity is eight." / "A Server with only an onboard Matrox G200 still has GPU Inventory, but
  it has no compute GPU capacity."
- Related terms: Server, Server Type, Machine, BMC.
- Change note: Added after MAAS reported an eight-accelerator Server as nine GPUs because its
  onboard ASPEED graphics controller shared MAAS's generic GPU hardware class. The distinction
  keeps the provider's complete hardware observation without overstating compute capacity.
