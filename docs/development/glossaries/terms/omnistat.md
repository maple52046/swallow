# Omnistat

- Bounded context: Platform Management, owned by `api-server` as a feature of a Swallow-deployed
  Slurm Platform; displayed by `dashboard` and `cli`.
- Definition: AMD's Slurm-aware GPU monitoring stack, installed by Swallow as an optional part of
  deploying a Slurm Platform. Omnistat itself is the per-compute-node exporter plus the Slurm job
  attribution (the `10-omnistat` job hook of the Platform's Prolog/Epilog dispatcher); its Prometheus and Grafana are Managed Software that the same
  deployment composes onto the primary controller by Omnistat's install method: `container` (the
  default, on the primary controller's container runtime) or `native`.
- Allowed meaning: The Platform records whether Omnistat is enabled, its pinned version, its
  install method, the observability Server (the primary controller that runs Prometheus and
  Grafana), and its Omnistat State, projected from the Workflow Task that installs it:

  | Value | Meaning |
  | --- | --- |
  | `installing` | the deployment that carries Omnistat is accepted or running (including while it is still provisioning or building Slurm) |
  | `ready` | the install succeeded: every compute node serves Omnistat metrics, Prometheus scrapes them, and Grafana has the Omnistat dashboards |
  | `failed` | the install failed, was canceled, or never ran because an earlier Task failed |
  | `absent` | the Platform was uninstalled; Swallow manages no Omnistat on it |

  Only the Workflow recorded as the latest change may move the state. A successful install also
  registers the Monitoring Link `Omnistat Grafana`; uninstall removes that link.
- Disallowed meaning: Not Managed Software (the target is the Slurm Platform and its job
  attribution, not one host's software) and not a metrics Integration — Swallow never queries
  Omnistat's Prometheus. Not the GPU GRES of the Slurm Platform, which Swallow configures whether
  or not Omnistat is enabled. Not the Device Metrics Exporter, which a Platform has instead of
  Omnistat, never together. Not Platform Lifecycle State.
- Synonyms: Omnistat system mode (AMD's name for this deployment shape).
- Deprecated terms: None.
- Examples: "The deployment enables Omnistat by the `container` method, so the primary controller
  gets Docker CE, Prometheus, and Grafana, and both compute nodes get the exporter." / "With the
  `native` method Prometheus and Grafana come from their official release and package repository,
  and the primary controller needs no container runtime." / "Docker CE failed on the primary
  controller, so the Omnistat Task never ran and the Omnistat State is `failed`." / "After the
  uninstall the Omnistat State is `absent` and the `Omnistat Grafana` link is gone, while the
  Grafana Software Assignment stays until it is uninstalled."
- Related terms: Platform, Managed Software, Software Assignment, Monitoring Link, Workflow, Job,
  Task, GPU Inventory.
- Change note: Added 2026-10-21 (decision 063) when Swallow started installing Omnistat with a
  Slurm deployment instead of operators overlaying it by hand. 2026-10-22: added the install
  method (`container` default, `native`). 2026-10-29 (decision 066): its job attribution became a
  hook of the Prolog/Epilog dispatcher, and it is mutually exclusive with the Device Metrics
  Exporter.
