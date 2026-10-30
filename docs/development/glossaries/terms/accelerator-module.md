# Accelerator Module

- Bounded context: swallow-wide (core and extensions).
- Definition: The Extension Module for one GPU vendor — today AMD, later NVIDIA — on an axis separate from Platform Modules, so a vendor is supported once rather than once per Platform.
- Allowed meaning: Provides GPU classification (workload `compute` or `display`) and the basis for Server Type, driver handling and availability checks, monitoring exporters, and hooks a Platform Module calls (for example Slurm GRES configuration or the Kubernetes GPU Operator).
- Disallowed meaning: Not a Platform, and not a Managed Software kind (although it may rely on one, such as the Device Metrics Exporter). Not hardware inventory, which is Observed State from the provisioner.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "Adding NVIDIA means adding an Accelerator Module, not changing the Kubernetes and Slurm modules."
- Related terms: Extension Module, Platform Module, GPU Inventory, Server Type, GPU Operator, Device Metrics Exporter.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md), which makes the GPU vendor a separate axis.
