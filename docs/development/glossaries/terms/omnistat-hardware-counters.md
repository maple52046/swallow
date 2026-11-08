# Omnistat Hardware Counters

- Bounded context: Platform Management, owned by `api-server` as part of a Slurm Platform's Omnistat;
  displayed by `dashboard` and `cli`.
- Definition: The GPU hardware performance counters Omnistat's exporter samples through
  ROCProfiler-SDK on a compute node, when a Slurm Platform with Omnistat asks for them: at its
  deployment, or later by turning them on or off on the deployed Platform. Today the only counter is
  `VALUBusy`, the share of time a GPU's vector ALUs are busy. Whether the Platform asks for them is
  the user's intent; whether each compute node collects them is that node's outcome, reported by the
  Workflow Task that installs or reconfigures Omnistat.
- Allowed meaning: A compute node's outcome is one state, with a reason when it does not collect:

  | State | Meaning |
  | --- | --- |
  | `collecting` | the node's exporter samples the counters and serves them with its other metrics |
  | `unavailable` | the node does not collect them; its exporter runs and serves everything else |

  | Reason (state `unavailable`) | Meaning |
  | --- | --- |
  | `no_gpu` | the node has no AMD compute GPU |
  | `rocm_unsupported` | swallow publishes no extension for the node's ROCm minor, or the node's ROCm version cannot be read |
  | `glibc_too_old` | the node's glibc is older than the glibc the extension was built against |
  | `artifact_unavailable` | the source the Site chose could not provide the extension |
  | `probe_failed` | the extension did not load, or did not sample the counters, in a probe isolated from the exporter |
  | `exporter_failed` | with the counters on, the exporter did not serve metrics in time, so they were turned off again |

  A node that is `unavailable` never fails the deployment or the change, and its Omnistat State is
  the Platform's as usual. The outcome is the one the latest Omnistat install recorded; swallow does
  not watch it change afterwards. Turning the counters off records no outcome: every compute node
  runs Omnistat without them.
- Disallowed meaning: Not the Omnistat State, which a Platform has whether or not its nodes collect
  counters. Not the Device Metrics Exporter's profiler, which a Platform has instead of Omnistat. Not
  counters a user collects inside a job (Omnistat's user mode or `rocprofv3`).
- Synonyms: rocprof counters.
- Deprecated terms: None.
- Examples: "The deployment asked for hardware counters, so both MI308X compute nodes probe the
  extension and are `collecting`; the CPU-only compute node is `unavailable` with `no_gpu`." / "The
  node runs a ROCm minor swallow publishes no extension for, so it is `unavailable` with
  `rocm_unsupported`, and its exporter still serves AMD SMI and job metrics." / "The Site does not
  use its Local Repository and the release URL could not be reached, so every GPU node is
  `unavailable` with `artifact_unavailable`."
- Related terms: Omnistat, Platform, Workflow, Task, GPU Inventory, Local Repository, Device Metrics
  Exporter.
- Change note: Added 2026-11-08 ([decision 063](../../../decisions/063-omnistat-slurm-observability.md)
  (omnistat slurm observability)) when swallow started collecting `VALUBusy` with Omnistat, degrading
  per node instead of failing the deployment; the extension is built and published by swallow's
  release ([decision 079](../../../decisions/079-release-built-host-artifacts.md) (release-built host
  artifacts)). Changed 2026-11-09: a deployed Platform may turn them on or off, so the intent is the
  Platform's, no longer only its deployment's.
