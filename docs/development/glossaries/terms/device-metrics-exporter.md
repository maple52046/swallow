# Device Metrics Exporter

- Bounded context: Software Deployment (the Managed Software kind `device-metrics-exporter`) and
  Platform Management (the feature of a Swallow-deployed Slurm Platform that composes it); owned by
  `api-server`, displayed by `dashboard` and `cli`.
- Definition: AMD's per-GPU-node metrics exporter (`gpuagent` plus `amd-metrics-exporter`, the
  native `amdgpu-exporter` package). Swallow installs it as the Managed Software kind
  `device-metrics-exporter`, natively and pinned, either on its own on a single Server or composed
  into a Slurm deployment, where it also gets Slurm job attribution: the
  `20-device-metrics-exporter` job hook of the Platform's Prolog/Epilog dispatcher records each job
  on the GPUs it was allocated.
- Allowed meaning:
  - **As Managed Software**: one Software Assignment per Server, on the Server's address at port
    `5001`, version recorded in its spec. A standalone install provides the exporter only, without
    job attribution; Prometheus and Grafana are separate kinds an operator points at it. It needs
    AMD compute GPUs and Ubuntu 24.04, installs Swallow's post-boot amdgpu loader when the Server
    has none, and is refused on a Kubernetes member and on a Server with a Platform Claim, whose
    Platform manages the exporter there.
  - **As a Slurm feature**: the Slurm deployment composes the kind onto every GPU compute node,
    together with Prometheus and Grafana on the primary controller by the feature's install method,
    `container` (default) or `native`, exactly like Omnistat's. The Platform records whether the
    feature is enabled, its pinned version, that install method, the observability Server, its
    Device Metrics Exporter State, and the GPU identity a successful install found. The state is
    projected from the Workflow Task that installs it:

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
  `Device Metrics Exporter Grafana`; uninstall removes the exporter from every Server of the
  Platform, marks their Software Assignments `absent`, and removes the link.
- Disallowed meaning: Not the Device Metrics Exporter the GPU Operator runs on a Kubernetes
  Platform (that is the GPU Operator's `metricsExporter` setting, which Swallow never scrapes). Not
  a metrics Integration — Swallow never queries the Prometheus that scrapes it — and not the host
  GPU exporter (RDC, port `5000`) Swallow scrapes. Not Omnistat: the two are mutually exclusive on
  a Platform, and the exporter keeps jobs sharing nodes while Omnistat needs whole nodes. Not GPU
  GRES, which Swallow configures either way.
- Synonyms: DME; AMD Device Metrics Exporter.
- Deprecated terms: None.
- Examples: "Installing `device-metrics-exporter` on one GPU Server gives it per-GPU metrics on
  port `5001`; a Prometheus on another Server scrapes it by `scrapeServerIds`." / "The deployment
  enables the Device Metrics Exporter by the `native` method, so the primary controller gets
  Prometheus and Grafana from their official packages and both GPU compute nodes get the exporter
  on port `5001`, each with an installed Software Assignment." / "The install is `ready`, but the
  GPU identity is `collapsed`: the MI308X virtual functions all report `gpu_id` `0`, so the Grafana
  job dashboard attributes a one-GPU job to every GPU of the node." / "Omnistat and the Device
  Metrics Exporter cannot both be enabled in one deploy request."
- Related terms: Managed Software, Software Assignment, Platform, Platform Claim, Omnistat,
  Monitoring Link, Workflow, Job, Task, GPU Inventory, GPU Operator.
- Change note: Added 2026-10-29 (decision 066) when Swallow started installing AMD's Device
  Metrics Exporter with a Slurm deployment, attributed to jobs through the new Prolog/Epilog
  dispatcher, and recording whether the exporters tell GPUs apart. Updated 2026-10-30
  ([decision 068](../../../decisions/068-device-metrics-exporter-managed-software.md)): it is now
  the Managed Software kind `device-metrics-exporter`, installable on a single Server, and the
  Slurm feature composes that kind instead of carrying its own install.
