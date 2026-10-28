# Device Metrics Exporter

- Bounded context: Platform Management, owned by `api-server` as a feature of a Swallow-deployed
  Slurm Platform; displayed by `dashboard` and `cli`.
- Definition: AMD's per-GPU-node metrics exporter (`gpuagent` plus `amd-metrics-exporter`, the
  native `amdgpu-exporter` package), installed by Swallow as an optional part of deploying a Slurm
  Platform, together with its Slurm job attribution: the `20-device-metrics-exporter` job hook of
  the Platform's Prolog/Epilog dispatcher records each job on the GPUs it was allocated. Its
  Prometheus and Grafana are Managed Software that the same deployment composes onto the primary
  controller by its install method, `container` (default) or `native`, exactly like Omnistat's.
- Allowed meaning: The Platform records whether the exporter is enabled, its pinned version, its
  install method, the observability Server, its Device Metrics Exporter State, and the GPU identity
  a successful install found. The state is projected from the Workflow Task that installs it:

  | Value | Meaning |
  | --- | --- |
  | `installing` | the deployment that carries the exporter is accepted or running |
  | `ready` | the install succeeded: every GPU compute node serves GPU metrics, Prometheus scrapes them, and Grafana has the exporter's dashboards |
  | `failed` | the install failed, was canceled, or never ran because an earlier Task failed |
  | `absent` | the Platform was uninstalled; Swallow manages no exporter on it |

  GPU identity is `distinct` when every GPU node's exporter reports one distinct `gpu_id` per GPU,
  and `collapsed` when any node reports fewer (for example MI308X SR-IOV virtual functions, which
  all report `gpu_id` `0`). Collapsed means the endpoint works but per-GPU job attribution is
  wrong on that node; it never fails the deployment. Only the Workflow recorded as the latest
  change may move the state or the identity. A successful install registers the Monitoring Link
  `Device Metrics Exporter Grafana`; uninstall removes it.
- Disallowed meaning: Not the Device Metrics Exporter the GPU Operator runs on a Kubernetes
  Platform (that is the GPU Operator's `metricsExporter` setting, which Swallow never scrapes). Not
  Managed Software and not a metrics Integration — Swallow never queries its Prometheus, and it is
  not the host GPU exporter (RDC, port `5000`) Swallow scrapes. Not Omnistat: the two are mutually
  exclusive on a Platform, and the exporter keeps jobs sharing nodes while Omnistat needs whole
  nodes. Not GPU GRES, which Swallow configures either way.
- Synonyms: DME; AMD Device Metrics Exporter.
- Deprecated terms: None.
- Examples: "The deployment enables the Device Metrics Exporter by the `native` method, so the
  primary controller gets Prometheus and Grafana from their official packages and both GPU compute
  nodes get the exporter on port `5001`." / "The install is `ready`, but the GPU identity is
  `collapsed`: the MI308X virtual functions all report `gpu_id` `0`, so the Grafana job dashboard
  attributes a one-GPU job to every GPU of the node." / "Omnistat and the Device Metrics Exporter
  cannot both be enabled in one deploy request."
- Related terms: Platform, Omnistat, Managed Software, Software Assignment, Monitoring Link,
  Workflow, Job, Task, GPU Inventory, GPU Operator.
- Change note: Added 2026-10-29 (decision 066) when Swallow started installing AMD's Device
  Metrics Exporter with a Slurm deployment, attributed to jobs through the new Prolog/Epilog
  dispatcher, and recording whether the exporters tell GPUs apart.
