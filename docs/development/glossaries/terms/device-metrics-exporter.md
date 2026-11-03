# Device Metrics Exporter

- Bounded context: Software Deployment (the Managed Software kind `device-metrics-exporter`) and
  Platform Management (the feature of a swallow-deployed Slurm Platform that composes it); owned by
  `api-server`, displayed by `dashboard` and `cli`.
- Definition: AMD's per-GPU-node metrics exporter (`gpuagent` plus `amd-metrics-exporter`, the
  native `amdgpu-exporter` package). swallow installs it as the Managed Software kind
  `device-metrics-exporter`, natively and pinned, either on its own on a single Server or composed
  into a Slurm deployment, where it also gets Slurm job attribution: the
  `20-device-metrics-exporter` job hook of the Platform's Prolog/Epilog dispatcher records each job
  on the GPUs it was allocated.
- Allowed meaning:
  - **As Managed Software**: one Software Assignment per Server, on the Server's address at port
    `5001`, version recorded in its spec. A standalone install provides the exporter only, without
    job attribution; the Site Prometheus scrapes it once the assignment is installed. It needs
    AMD compute GPUs and Ubuntu 24.04, installs swallow's post-boot amdgpu loader when the Server
    has none, and is refused on a Kubernetes member and on a Server with a Platform Claim, whose
    Platform manages the exporter there.
  - **As a Slurm feature**: the Slurm deployment composes the kind onto every GPU compute node; the
    feature's install method, `container` (default) or `native`, chooses how the Platform's own
    Grafana on the primary controller is installed, exactly like Omnistat's. The Platform records whether the
    feature is enabled, its pinned version, that install method, the observability Server, its
    Device Metrics Exporter State, and the GPU identity a successful install found. The state is
    projected from the Workflow Task that installs it:

  | Value | Meaning |
  | --- | --- |
  | `installing` | the deployment that carries the exporter is accepted or running |
  | `ready` | the install succeeded: every GPU compute node's exporter serves GPU health metrics |
  | `failed` | the install failed, was canceled, or never ran because an earlier Task failed |
  | `absent` | the Platform was uninstalled; swallow manages no exporter on it |

  GPU identity is `distinct` when every GPU node's exporter reports one distinct `gpu_id` per GPU,
  and `collapsed` when any node reports fewer (for example MI308X SR-IOV virtual functions, which
  all report `gpu_id` `0`). Collapsed means the endpoint works but per-GPU job attribution is
  wrong on that node; it never fails the deployment. Only the Workflow recorded as the latest
  change may move the state or the identity. Uninstall removes the exporter from every Server of
  the Platform and marks their Software Assignments `absent`.
- Disallowed meaning: Not the Device Metrics Exporter the GPU Operator runs on a Kubernetes
  Platform: that is the GPU Operator's `metricsExporter` setting, with no Software Assignment and
  no Device Metrics Exporter State, although it is the same exporter on the same port `5001` with
  the same metric names, so the Site Prometheus scrapes it the same way and the same dashboards
  read it. Not a metrics Integration — swallow never queries the exporter — and not the RDC
  exporter (port `5000`), which swallow's RDC scrape job reads on hosts that run one. Not Omnistat: the two are mutually exclusive on
  a Platform, and the exporter keeps jobs sharing nodes while Omnistat needs whole nodes. Not GPU
  GRES, which swallow configures either way.
- Synonyms: DME; AMD Device Metrics Exporter.
- Deprecated terms: None.
- Examples: "Installing `device-metrics-exporter` on one GPU Server gives it per-GPU metrics on
  port `5001`, which the Site Prometheus scrapes." / "The deployment enables the Device Metrics
  Exporter by the `native` method, so the primary controller gets the Platform's Grafana from its
  official package and both GPU compute nodes get the exporter on port `5001`, each with an
  installed Software Assignment." / "The install is `ready`, but the
  GPU identity is `collapsed`: the MI308X virtual functions all report `gpu_id` `0`, so the Grafana
  job dashboard attributes a one-GPU job to every GPU of the node." / "Omnistat and the Device
  Metrics Exporter cannot both be enabled in one deploy request."
- Related terms: Managed Software, Software Assignment, Platform, Platform Claim, Omnistat,
  Monitoring Link, Workflow, Job, Task, GPU Inventory, GPU Operator.
- Change note: Added 2026-10-29 ([decision 066](../../../decisions/066-slurm-job-hooks-and-device-metrics-exporter.md) (slurm job hooks and device metrics exporter)) when swallow started installing AMD's Device
  Metrics Exporter with a Slurm deployment, attributed to jobs through the new Prolog/Epilog
  dispatcher, and recording whether the exporters tell GPUs apart. Updated 2026-10-30
  ([decision 068](../../../decisions/068-device-metrics-exporter-managed-software.md) (device metrics exporter managed software)): it is now
  the Managed Software kind `device-metrics-exporter`, installable on a single Server, and the
  Slurm feature composes that kind instead of carrying its own install. 2026-11-03: swallow does
  not install the RDC exporter on hosts; port `5000` stays reserved for it. 2026-11-03
  ([decision 068](../../../decisions/068-device-metrics-exporter-managed-software.md) (device metrics exporter managed software)): the Site Prometheus scrapes it, no Prometheus is composed
  for it, and the Platform Grafana's own install records the Monitoring Link. 2026-11-03
  ([decision 062](../../../decisions/062-swallow-managed-gpu-operator.md)): the GPU Operator's
  exporter moved to the host network at the same port and metric names, so it is distinguished
  from this term by who installs it, not by how it is scraped.
