# Omnistat

- Bounded context: Platform Management, owned by `api-server` as a feature of a swallow-deployed
  Slurm Platform; displayed by `dashboard` and `cli`.
- Definition: AMD's Slurm-aware GPU monitoring stack, installed by swallow as an optional part of
  deploying a Slurm Platform. Omnistat itself is the per-compute-node exporter plus the Slurm job
  attribution (the `10-omnistat` job hook of the Platform's Prolog/Epilog dispatcher). The Site
  Prometheus scrapes the exporters, and the Platform's own Grafana (Managed Software on the primary
  controller) shows them, installed by Omnistat's install method: `container` (the default, on the
  primary controller's container runtime) or `native`. It suits a CPU-only Platform too, collecting
  host and job metrics without GPUs.
- Allowed meaning: The Platform records whether Omnistat is enabled, its pinned version, its
  install method, the observability Server (the primary controller that hosts the Platform's
  Grafana), and its Omnistat State, projected from the Workflow Task that installs it:

  | Value | Meaning |
  | --- | --- |
  | `installing` | the deployment that carries Omnistat is accepted or running (including while it is still provisioning or building Slurm) |
  | `ready` | the install succeeded: every compute node's exporter serves Omnistat metrics, and the Site Prometheus scrapes them |
  | `failed` | the install failed, was canceled, or never ran because an earlier Task failed |
  | `absent` | the Platform was uninstalled; swallow manages no Omnistat on it |

  Only the Workflow recorded as the latest change may move the state. The exporter answers only
  the node itself and the Site Prometheus.
- Disallowed meaning: Not Managed Software (the target is the Slurm Platform and its job
  attribution, not one host's software) and not a metrics Integration — swallow never queries
  Omnistat. Not the GPU GRES of the Slurm Platform, which swallow configures whether
  or not Omnistat is enabled. Not the Device Metrics Exporter, which a Platform has instead of
  Omnistat, never together. Not Platform Lifecycle State.
- Synonyms: Omnistat system mode (AMD's name for this deployment shape).
- Deprecated terms: None.
- Examples: "The deployment enables Omnistat by the `container` method, so the primary controller
  gets Docker CE and the Platform's Grafana, and both compute nodes get the exporter, which the
  Site Prometheus scrapes." / "With the `native` method Grafana comes from its official package
  repository, and the primary controller needs no container runtime." / "Docker CE failed on the
  primary controller, so the Omnistat Task never ran and the Omnistat State is `failed`." / "After
  the uninstall the Omnistat State is `absent`, while the Grafana Software Assignment stays until
  it is uninstalled."
- Related terms: Platform, Managed Software, Software Assignment, Monitoring Link, Workflow, Job,
  Task, GPU Inventory.
- Change note: Added 2026-10-21 ([decision 063](../../../decisions/063-omnistat-slurm-observability.md) (omnistat slurm observability)) when swallow started installing Omnistat with a
  Slurm deployment instead of users overlaying it by hand. 2026-10-22: added the install
  method (`container` default, `native`). 2026-10-29 ([decision 066](../../../decisions/066-slurm-job-hooks-and-device-metrics-exporter.md) (slurm job hooks and device metrics exporter)): its job attribution became a
  hook of the Prolog/Epilog dispatcher, and it is mutually exclusive with the Device Metrics
  Exporter. 2026-11-03 ([decision 063](../../../decisions/063-omnistat-slurm-observability.md) (omnistat slurm observability)): the Site Prometheus scrapes it, no
  Prometheus is composed for it, its install method chooses only the Platform's Grafana, and the
  Platform Grafana's own install records the Monitoring Link.
