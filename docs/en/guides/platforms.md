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

Open **Deploy platform** to choose Kubernetes or Slurm from the runtime catalog. The
selection opens a dedicated workspace: Kubernetes and Slurm share Server discovery
and OS-provisioning controls, but keep their own steps, validation, features, and
review. Optional capabilities use Switches; topology and implementation modes remain
explicit radio choices, and Slurm node roles remain independently selectable.

## Clock check

The platform's install step (**Install k0s Platform** or **Install Slurm Platform**)
first examines every node's clock, before it installs anything. It changes nothing;
time is set when an OS is deployed (see
[Time synchronization](os-provisioning.md#time-synchronization)). A node fails the
check when its clock is not synchronized, when the swallow host's NTP service does
not answer it, or when it is more than a second away from that service.

The **Require synchronized clocks** Switch in the Features step is on by default. A
node that fails the check then stops the install step on every node, and the step's
output names it.
Slurm needs this: MUNGE rejects credentials between nodes whose clocks disagree. Turn
the Switch off to report such nodes and continue anyway.


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

Open the Kubernetes Platform, then use its **Applications**, **Namespaces**, and
**Cluster** tabs:

- **Applications** lists Deployments, DaemonSets, StatefulSets, and bare Pods.
  Filter by namespace; scale Deployments and StatefulSets, restart Deployments,
  DaemonSets, and StatefulSets, read a bounded snapshot of the first Pod's
  logs, or delete a workload. **Apply YAML** supports a server-side dry run
  before applying one or more objects.
- **Namespaces** creates and deletes namespaces. System namespaces are
  identified and cannot be deleted.
- **Cluster** shows version, readiness, schedulable GPU resources, and the
  Server matched to each node. **Cordon** stops new scheduling on a node;
  **Uncordon** makes it schedulable again. Neither action drains existing Pods.

These views query the Kubernetes API live and do not create a durable copy of
in-cluster state in swallow. The Cluster tab also shows the AMD GPUs each node
exposes.

### AMD GPU Operator

The Kubernetes workspace's **Features** step can install the AMD GPU Operator with the
Platform. Turn on **AMD GPU Operator** to see its options:

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
managed node exposes its GPUs. The Switch is unavailable, with a textual reason,
when no selected workload node has an AMD GPU.

After deployment, the **AMD GPU Operator** section of the Platform Overview shows
its state and settings and offers **Install**, **Configure**, and **Uninstall**.
Switching between Device Plugin and DRA, or uninstalling, is refused while a
Pod still uses an AMD GPU; stop that work first. Uninstalling
the operator keeps the Platform running. swallow does not take over an operator
that was installed by hand.

The Device Metrics Exporter is for the cluster's own monitoring; swallow's
GPU charts keep using the host exporter.

## Slurm

The Slurm workspace assigns controller, compute, and login roles independently,
then derives state-server and workload-storage responsibilities from the selected
topology and features. Its Features step uses separate Switches for shared workload
storage, a container runtime, and Omnistat; AMD GPU GRES is included automatically
on eligible compute nodes.

Before a GPU node joins, swallow proves every GPU works by allocating and freeing
memory on it through the HIP runtime, on the node and again through Slurm; a node
that fails is drained and the deployment fails with the reason. The GRES type
defaults to the GPU architecture (for example `gfx942`). Under **Advanced
settings** you can name a different GRES type that existing job scripts expect
(for example `mi308x_vf`) and set amdgpu driver module options. swallow already
applies the options a node's GPU topology needs (for example `pcie_p2p=0` where
ROCm would otherwise crash at exit), so most deployments leave them empty. It
learns them once the driver has loaded and then loads the driver again in place,
without a reboot, so RAM (ephemeral) deployments work too; options you set are
there from the first load. A misspelled option name fails the deployment at once
with the closest real name.

Open **Platforms → Settings** to configure the system-wide Slurm deployment
requirement. Enable the policy and set the minimum **CPU cores**, **Memory GiB**,
and **Storage GB** that every selected Slurm node must report. All three values
are required while it is enabled; disabling it removes the floor. The deploy
wizard shows why an ineligible Server cannot be selected and rechecks the
policy on submission.

The requirement is a hardware eligibility floor. It does not reserve resources,
express Slurm scheduler capacity, or prove that an OS image fits an ephemeral
root filesystem.

Platform detail reads live Slurm state through the Platform API and correlates
nodes back to Server IDs.

## Lifecycle

- **Sync** refreshes observed Platform membership/state.
- **Uninstall** removes the swallow-deployed runtime from its original targets
  and retains the Platform record for diagnosis. When a workflow still holds the
  Platform's servers (a failed deployment waiting for attention, or one still
  running), the dialog lists it and asks you to confirm canceling it; the
  uninstall then starts by itself once it has ended.
- **Delete** removes only the swallow record/projections and must not be
  confused with host-side uninstall.
- Bulk actions remain subject to each Platform's lifecycle gate.

Every deployment or uninstall creates a Workflow. Inspect failed Tasks and
retry at the narrowest supported level rather than recreating the Platform.

Exact payloads and lifecycle states are in the active
[Platforms contract](../../../api-server/docs/development/api-contracts/api-server/platforms.md)
and
[Kubernetes explorer contract](../../../api-server/docs/development/api-contracts/api-server/platforms-kubernetes.md).
