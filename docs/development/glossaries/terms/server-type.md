# Server Type

- Bounded context: Platform-wide, spanning provisioning and observability; used by
  `api-server` (exporter targeting and discovery filters), `dashboard` (metric display), and `cli` (Server and monitoring output).
- Definition: A coarse classification of a managed server by GPU capability, used to
  decide which scrape jobs target a host and which GPU metrics to display. It is a
  **derived** classification, not a stored server field.
- Allowed meaning: The classification values are:

  | Value | Meaning | How it is decided |
  | --- | --- | --- |
  | `cpu` | A server with no GPU workload role; only host-level metrics apply | Not carrying a GPU tag |
  | `amd-gpu` | An AMD GPU server; the RDC exporter scrape job targets it in addition to node-exporter | MAAS tag `amd-gpu` (mirrored into `observed.tags`) |
  | `nvidia-gpu` | An NVIDIA GPU server; runs the DCGM exporter | Reserved; not yet implemented |

  Only `cpu` and `amd-gpu` are implemented. The type decides scrape targeting: every server
  is a node-exporter target; an `amd-gpu` server is also a target of the RDC exporter job,
  which returns GPU series only where an RDC exporter runs. swallow does not install the RDC
  exporter on hosts; the Kubernetes exporter DaemonSet runs it on a Platform whose exporters
  Kubernetes owns.
- Disallowed meaning: Not a hardware inventory of GPUs (that is the server's observed
  `gpus`), and not a status. A machine that physically has a GPU but is not tagged is
  still treated as `cpu` for exporter targeting, because targeting follows the tag, not a
  probe. Do not persist server type as an authoritative field; recompute it from the tag.
  Not a statement that an exporter is installed.
- Synonyms: "GPU server" refers to `amd-gpu` or `nvidia-gpu`; "CPU server" is `cpu`.
- Deprecated terms: None.
- Examples: "gpu-node-07.maas carries the `amd-gpu` tag, so it is an AMD GPU server and the
  RDC exporter job targets it alongside node-exporter." / "A `lab-` VM has no GPU tag, so it
  is a CPU server and only the node-exporter job targets it."
- Related terms: Server, Tag, Exporter Ownership, Server Status.
- Change note: Added for the Prometheus monitoring integration, which installs node-exporter
  on every server and the AMD RDC exporter on `amd-gpu`-tagged servers. NVIDIA is listed as
  reserved because the first iteration implements CPU and AMD GPU only. 2026-11-03: swallow
  does not install the RDC exporter on hosts, so `amd-gpu` selects the RDC scrape job rather
  than an exporter swallow runs.
