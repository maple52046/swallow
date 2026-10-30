# 062. Swallow installs and configures the AMD GPU Operator on Kubernetes Platforms

- Status: Accepted
- Date: 2026-10-20
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §2.3.1 選用功能：AMD GPU Operator（decision 062）

## Context

`gpuStackOwner=gpu-operator` ([decision 003](003-metrics-label-contract.md)) said a Platform's GPU
operator owns its GPU stack, but Swallow never installed one: operators followed hand-written
runbooks to install the AMD GPU Operator chart, create a `DeviceConfig`, and work around k0s
paths for DRA. Operators want the operator as an optional part of deploying a Kubernetes Platform,
with a choice between the device plugin and DRA, and want to change those settings later.

## Decision

- **A Platform deployment Job, not Managed Software.** The deploy target is the Platform, so the
  operator is installed by a `configure-gpu-operator` Job: inside the `deploy-kubernetes` Workflow
  after `validate-platform`, or in its own `configure-gpu-operator` Workflow when changed later.
  Both run one idempotent playbook.
- **Swallow owns the settings and the state; the cluster owns the operator.** The Platform records
  the release, GPU Allocation Mode (`device-plugin` or `dra`), resource naming, node labeller, and
  Device Metrics Exporter, plus the GPU Operator State projected from the applying Task. Only the
  latest Workflow may move the state.
- **The OS image's driver only.** The chart runs with KMM and remediation disabled, so no
  cert-manager or driver registry is needed, and a node whose image has no `amdgpu` module is
  refused. An image may keep `amdgpu` out of the boot path (`modprobe.blacklist=amdgpu`, AMD's
  guidance for Instinct hosts and SR-IOV guests), so every managed node gets a Swallow unit that
  loads the driver after SSH is up, with k0s ordered after it, so the GPUs are visible after a
  reboot. The unit is removed when the node stops being managed; a loaded driver is never unloaded.
- **Helm on the initial controller, with a pinned binary and a bundled chart.** Supported releases
  (chart, DRA driver, and pinned operand images) are a Swallow catalog.
- **The operator's NFD decides which nodes are managed; nobody lists them.** No API, CLI, or
  dashboard surface names the nodes or Servers to manage, because a cluster may mix thousands of
  CPU-only and GPU nodes. The chart's Node Feature Discovery marks AMD GPU nodes by PCI id
  (`amd-gpu` for a physical GPU, `amd-vgpu` for an SR-IOV virtual function); Swallow mirrors that
  result into its own label, and the single `DeviceConfig` selects it, because a `DeviceConfig`
  selector is an AND-only label map that cannot match either NFD label. If per-node control is
  ever needed, it is an exclusion of named Servers from the NFD result, never a list of nodes to
  include.
- **One allocation mode at a time, changed safely.** A change turns the old mode off before
  enabling the new one, and is refused while GPUs are in use. DRA on k0s gets two exact symlinks to
  the k0s kubelet directories, removed again when DRA is off.
- **`gpuStackOwner` follows the feature.** Enabling the operator sets `gpu-operator`; while
  Swallow manages one, the policy cannot be set back to `provisioning`. A `gpu-operator` Platform
  without a Swallow-managed operator still means an operator managed outside Swallow.
- **Monitoring is unchanged.** The operator's Device Metrics Exporter is optional, never uses host
  networking, and is not scraped by Swallow; the host RDC exporter keeps the fixed port 5000
  (decision 003, 2026-10-12 amendment).

## Alternatives considered

- **k0s Helm extension (`Chart` resources).** Verified on k0s v1.36.3: it installs from a local
  chart, but its uninstall gives Helm a zero timeout, so this chart's pre-delete hook Job fails at
  once and the `Chart` stays deleting forever. Rejected for that reason.
- **Managed Software kind.** Rejected: the target is a Platform, and per-Server assignments cannot
  express one cluster-wide release.
- **Selecting nodes by the NFD `amd-gpu` label directly.** Rejected: SR-IOV virtual functions get
  `amd-vgpu` instead, and a selector cannot express either label, so a Platform mixing both would
  need two DeviceConfigs, a setup the lab (virtual functions only) cannot verify.
- **Letting a person choose the managed Servers** (a `serverIds` setting with every eligible
  Server preselected). Removed before release: it asks a person to check every node of a large
  mixed cluster, duplicates what NFD already detects, and decides from Swallow's inventory rather
  than from the cluster.
- **KMM-managed drivers in the first release.** Deferred: it needs cert-manager, a driver image
  registry, and an exact kernel match.
- **Refusing a node whose image defers loading `amdgpu`.** Rejected after the lab image
  (`modprobe.blacklist=amdgpu`, "the operator loads amdgpu manually") made every deploy fail
  preflight. Loading it once without a post-boot unit was also rejected: a rebooted node would
  expose no GPUs until the operator re-applied the settings.

## Consequences

- A GPU Operator install is part of the deployment's success: a failure leaves the deployment in
  `requires_attention` with the GPU Task retryable.
- The chart archive and the Helm checksums ship in the release; container images are still pulled
  from Docker Hub at pinned tags, and the Helm binary is downloaded at run time.
- Adding a release means adding a catalog entry, its chart archive, and a lab verification.
- Swallow does not adopt an operator it did not install; preflight refuses one.
- Node-level checks need NFD, so a node whose image has no `amdgpu`, or whose driver does not load,
  is refused after the release is installed and before the `DeviceConfig` is applied.
