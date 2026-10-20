# GPU Operator

- Bounded context: Platform Management, owned by `api-server`; configured from `dashboard` and
  `cli`.
- Definition: The in-cluster Kubernetes operator that makes a Platform's GPUs schedulable — it
  runs the device plugin or the DRA driver, and optionally the node labeller and the Device
  Metrics Exporter, on the nodes it manages. A **Swallow-managed GPU Operator** is the AMD GPU Operator
  Swallow installs into a Swallow-deployed Kubernetes Platform, either as an optional feature of
  the deployment or later from the Platform. Swallow owns the desired settings (release, GPU
  Allocation Mode, resource naming, node labeller, and Device Metrics Exporter) and the GPU
  Operator State; the cluster owns the running operator.
- Allowed meaning: The operator and the settings Swallow applies to it. The first release uses
  the GPU driver already installed by the OS image; the operator does not build or install a
  driver. When the image keeps that driver out of the boot path, Swallow loads it on the managed
  nodes after boot. The managed nodes are the ones the operator's Node Feature Discovery marks as
  AMD GPU nodes (physical GPUs and SR-IOV virtual functions alike); nobody lists them, and Swallow
  only mirrors that result into its own node label. A Platform whose `gpuStackOwner` is
  `gpu-operator` but that has no Swallow-managed GPU Operator has an operator managed outside
  Swallow.
- Disallowed meaning: Not Managed Software — the deploy target is the Platform, not one Server,
  so it is a Platform deployment Job rather than a software kind. Not a GPU driver installation.
  Not a metrics Integration: the operator's Device Metrics Exporter is never scraped by Swallow,
  whose charts keep reading the host RDC exporter.
- Synonyms: "AMD GPU Operator" when the vendor matters.
- Deprecated terms: None.
- Examples: "Deploy the Platform with the AMD GPU Operator in Device Plugin mode on its three GPU
  nodes." / "Switching the GPU Operator to DRA is refused while a Pod still uses an AMD GPU." /
  "Uninstalling the GPU Operator leaves the Platform active and its nodes Ready."
- Related terms: Platform, GPU Allocation Mode, GPU Operator State, GPU Inventory, Exporter
  Ownership, Workflow.
- Change note: Added when Swallow started installing the AMD GPU Operator (decision 062). Before
  it, `gpuStackOwner=gpu-operator` was only a policy flag and Swallow installed no operator.
