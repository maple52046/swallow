# Server Type

- Bounded context: Platform-wide, spanning provisioning and observability; used by
  `api-server` (exporter targeting, discovery filters) and `dashboard` (metric display).
- Definition: A coarse classification of a managed server by GPU capability, used to
  decide which exporters a host should run and which GPU metrics to display. It is a
  **derived** classification, not a stored server field.
- Allowed meaning: The classification values are:

  | Value | Meaning | How it is decided |
  | --- | --- | --- |
  | `cpu` | A server with no GPU workload role; only host-level metrics apply | Not carrying a GPU tag |
  | `amd-gpu` | An AMD GPU server; runs the RDC exporter in addition to node-exporter | MAAS tag `amd-gpu` (mirrored into `observed.tags`) |
  | `nvidia-gpu` | An NVIDIA GPU server; runs the DCGM exporter | Reserved; not yet implemented |

  Only `cpu` and `amd-gpu` are implemented. The type decides exporter set: every server
  runs node-exporter; an `amd-gpu` server additionally runs the RDC exporter.
- Disallowed meaning: Not a hardware inventory of GPUs (that is the server's observed
  `gpus`), and not a status. A machine that physically has a GPU but is not tagged is
  still treated as `cpu` for exporter targeting, because targeting follows the tag, not a
  probe. Do not persist server type as an authoritative field; recompute it from the tag.
- Synonyms: "GPU server" refers to `amd-gpu` or `nvidia-gpu`; "CPU server" is `cpu`.
- Deprecated terms: None.
- Examples: "tainan-ci.maas carries the `amd-gpu` tag, so it is an AMD GPU server and runs
  both node-exporter and the RDC exporter." / "A `lab-` VM has no GPU tag, so it is a CPU
  server and runs node-exporter only."
- Related terms: Server, Tag, Exporter Ownership, Server Status.
- Change note: Added for the Prometheus monitoring integration, which installs node-exporter
  on every server and the AMD RDC exporter on `amd-gpu`-tagged servers. NVIDIA is listed as
  reserved because the first iteration implements CPU and AMD GPU only.
