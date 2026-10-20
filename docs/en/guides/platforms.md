# Platforms

[繁體中文](../../zh-TW/guides/platforms.md) · [Documentation home](../README.md)

A Platform is a Kubernetes or Slurm runtime deployed by swallow. swallow does
not register arbitrary existing runtimes, and it never treats itself as a
Platform.

![Platform deployment wizard](../../assets/platform-deployment-wizard.png)

## Before deployment

- Target Servers belong to one Site and have a deployed operating system.
- Server locks and active conflicting Workflows are cleared.
- Site automation, SSH known hosts, credentials, worker, and Ansible executor
  are ready.
- The selected topology meets its minimum resource requirements.
- Required shared state or workload storage is available for the chosen Slurm
  topology.

The wizard revalidates current eligibility when the request is submitted.

## Kubernetes

Choose a supported topology and assign control-plane and worker roles. A
standalone deployment is still one Kubernetes Platform. Deployment can also
request ephemeral OS targets when the selected image/provider combination
supports them.

After deployment, the Platform detail provides a live Kubernetes explorer for:

- namespaces and applications;
- pods, logs, and related resources;
- YAML apply;
- node cordon state.

These views query the Kubernetes API live and do not create a durable copy of
in-cluster state in swallow. The Cluster tab also shows the AMD GPUs each node
exposes.

### AMD GPU Operator

The wizard's **Features** step can install the AMD GPU Operator with the
Platform. Check **Install AMD GPU Operator** to see its options:

- **Allocation mode**: **Device Plugin** (Pods request `amd.com/gpu`) or **DRA**
  (Pods claim GPUs through a ResourceClaim; Kubernetes 1.34 or later).
- **GPU Operator version** and, for DRA, the **DRA driver version**, from the
  releases swallow has verified.
- **Resource naming** for the Device Plugin, **Node Labeller**, and
  **Device Metrics Exporter**.

You do not pick nodes. The operator's Node Feature Discovery finds every node
with a supported AMD GPU, physical or SR-IOV virtual function, and the operator
manages all of them; a node added to the cluster later is included the next
time you change the operator.

The operator uses the GPU driver that the OS image provides, so choose an image
with the `amdgpu` driver. If the image does not load `amdgpu` at boot (some
Instinct and SR-IOV images blacklist it on purpose), Swallow loads it on each
managed node and keeps loading it after every reboot, once SSH is up; Kubernetes
starts after it. The deployment succeeds only when every
managed node exposes its GPUs. The checkbox is unavailable when no selected
workload node has an AMD GPU.

After deployment, the **AMD GPU Operator** section of the Platform Overview shows
its state and settings and offers **Install**, **Configure**, and **Uninstall**.
Switching between Device Plugin and DRA, or uninstalling, is refused while a
Pod still uses an AMD GPU; stop that work first. Uninstalling
the operator keeps the Platform running. swallow does not take over an operator
that was installed by hand.

The Device Metrics Exporter is for the cluster's own monitoring; swallow's
GPU charts keep using the host exporter.

## Slurm

Slurm deployment assigns controller, compute, login, state-server, and workload
storage responsibilities according to the selected topology. Configure and
review the Site's Slurm deployment requirement before submission. Resource
minimums are policy, not suggestions; the API rejects an invalid target set.

Platform detail reads live Slurm state through the Platform API and correlates
nodes back to Server IDs.

## Lifecycle

- **Sync** refreshes observed Platform membership/state.
- **Uninstall** removes the swallow-deployed runtime from its original targets
  and retains the Platform record for diagnosis.
- **Delete** removes only the swallow record/projections and must not be
  confused with host-side uninstall.
- Bulk actions remain subject to each Platform's lifecycle gate.

Every deployment or uninstall creates a Workflow. Inspect failed Tasks and
retry at the narrowest supported level rather than recreating the Platform.

Exact payloads and lifecycle states are in the active
[Platforms contract](../../../api-server/docs/development/api-contracts/api-server/platforms.md)
and
[Kubernetes explorer contract](../../../api-server/docs/development/api-contracts/api-server/platforms-kubernetes.md).
