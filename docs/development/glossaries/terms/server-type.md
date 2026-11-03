# Server Type

- Bounded context: Platform-wide, spanning provisioning and observability; used by
  `api-server` (the `tag` filter of the port-based discovery route) and `cli` (Server output).
- Definition: A coarse classification of a managed server by GPU capability, from its provisioner
  tag. It is a **derived** classification, not a stored server field. It does not decide what the
  Site Prometheus scrapes: that follows the exporters swallow knows are installed.
- Allowed meaning: The classification values are:

  | Value | Meaning | How it is decided |
  | --- | --- | --- |
  | `cpu` | A server with no GPU workload role; only host-level metrics apply | Not carrying a GPU tag |
  | `amd-gpu` | An AMD GPU server | MAAS tag `amd-gpu` (mirrored into `observed.tags`) |
  | `nvidia-gpu` | An NVIDIA GPU server; runs the DCGM exporter | Reserved; not yet implemented |

  Only `cpu` and `amd-gpu` are implemented. A Prometheus outside swallow may select one type
  through the port-based discovery route's `tag` filter.
- Disallowed meaning: Not a hardware inventory of GPUs (that is the server's observed
  `gpus`), and not a status. A machine that physically has a GPU but is not tagged is
  still `cpu`, because the type follows the tag, not a probe. Do not persist server type as an
  authoritative field; recompute it from the tag. Not a statement that an exporter is installed,
  and not what decides GPU monitoring (that is the GPU exporter installed in its context).
- Synonyms: "GPU server" refers to `amd-gpu` or `nvidia-gpu`; "CPU server" is `cpu`.
- Deprecated terms: None.
- Examples: "gpu-node-07.maas carries the `amd-gpu` tag, so it is an AMD GPU server." / "A
  `lab-` VM has no GPU tag, so it is a CPU server; an external scrape job that passes
  `tag=amd-gpu` does not target it."
- Related terms: Server, Tag, Server Status.
- Change note: Added for the Prometheus monitoring integration, which installs node-exporter
  on every server and the AMD RDC exporter on `amd-gpu`-tagged servers. NVIDIA is listed as
  reserved because the first iteration implements CPU and AMD GPU only. 2026-11-03: swallow
  does not install the RDC exporter on hosts, so `amd-gpu` selects the RDC scrape job rather
  than an exporter swallow runs. Same day: the Kubernetes exporter DaemonSet that could run it on a
  Platform was retired with the per-Platform exporter owner. Same day: swallow's metrics API and
  dashboard offer basic monitoring only, so the type no longer decides which GPU metrics are shown. Same day: the Site Prometheus scrapes the exporters swallow knows are installed, so the type
  no longer decides scrape targeting; only the port-based discovery route's `tag` filter uses it.
