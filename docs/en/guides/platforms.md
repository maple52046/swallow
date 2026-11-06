# Platforms

[繁體中文](../../zh-TW/guides/platforms.md) · [Documentation home](../README.md)

A Platform is a Kubernetes or Slurm runtime deployed by swallow. swallow does
not register arbitrary existing runtimes, and it never treats itself as a
Platform.

![Platform deployment wizard](../../assets/platform-deployment-wizard.png)

## Before deployment

- Target Servers belong to one Site and are `ready` or `deployed`; a `ready`
  Server gets its operating system deployed first (the OS preparation step).
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

Submitting the wizard creates the Platform and its install as a
[Change Plan](workflows.md#change-plans) approved by you, so the install starts at once and
the wizard opens the new Platform. When the
[safety switch](workflows.md#change-plans) is on, the install waits on **Change Plans**
instead, and the wizard opens that page and says why. Until its install starts the Platform
reads **Planned**; deleting a Platform in that state withdraws the plan, and no host was
changed.

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

Choose a supported topology and assign control-plane and worker roles. The
workspace starts with **Multi-node (non-HA)**; **High availability** needs an odd
number of at least three control planes and an unused API virtual IP. A
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

### Cluster credentials

A Kubernetes Platform's deployment records two credentials for its cluster, and
swallow never shows either. Membership sync and every read in these views use a
read-only token, which cannot read Secrets. The changes you make here (cordon,
namespace and workload changes, Pod deletion, and **Apply YAML**, dry run
included) and the Secret list use a separate cluster-admin token, only when an
admin asks for them.

A Platform last deployed before swallow separated the two still has one
cluster-admin token for both reads and writes. Its Overview says so under
**Cluster access**: *Cluster credentials are not separated yet*. The explorer
keeps working. The next deployment Workflow on the Platform that succeeds
narrows that token to read-only and records the separate cluster-admin token.
If a deployment fails after narrowing the token, changes are refused until a
deployment succeeds; reads keep working.

### AMD GPU Operator

The Kubernetes workspace's **Features** step can install the AMD GPU Operator with the
Platform. When a selected workload node has an AMD GPU, **AMD GPU Operator** starts on,
with its **Device Metrics Exporter** on as the Platform's GPU monitoring. Turn it off to
deploy without it. Once you change the switch or any option, your choice stays when you
change the selected Servers later; until then it follows them, and turns off when no workload
node has an AMD GPU. While it is on, it shows its options:

- **Allocation mode**: **Device Plugin** (Pods request `amd.com/gpu`) or **DRA**
  (Pods claim GPUs through a ResourceClaim; Kubernetes 1.34 or later).
- **GPU Operator version** and, for DRA, the **DRA driver version**, from the
  releases swallow has verified.
- **Resource naming** for the Device Plugin, **Node Labeller** (on), and
  **Device Metrics Exporter** (on).

The **Review** step lists the operator's version and allocation mode, its resource naming
(or DRA driver), whether the Node Labeller is on, and its **GPU monitoring**.

You do not pick nodes. The operator's Node Feature Discovery finds every node
with a supported AMD GPU, physical or SR-IOV virtual function, and the operator
manages all of them; a node added to the cluster later is included the next
time you change the operator.

The operator uses the GPU driver that the OS image provides, so choose an image
with the `amdgpu` driver. If the image does not load `amdgpu` at boot (some
Instinct and SR-IOV images blacklist it on purpose), swallow loads it on each
managed node and keeps loading it after every reboot, once SSH is up; Kubernetes
starts after it. The deployment succeeds only when every
managed node exposes its GPUs. The Switch is unavailable, with a textual reason,
when no selected workload node has an AMD GPU.

After deployment, the **AMD GPU Operator** section of the Platform Overview shows
its state and settings and offers **Install**, **Configure**, and **Uninstall**.
**Install** starts from the same options as the workspace, Device Metrics Exporter on;
**Configure** starts from the operator's current settings. Installing starts at once. Configuring or uninstalling a running operator restarts GPU
workloads, so it is a Change Plan; confirming the dialog approves it and the change starts,
unless the safety switch makes it wait on **Change Plans**. Switching between Device
Plugin and DRA, or uninstalling, is refused while a Pod still uses an AMD GPU; stop that
work first. Uninstalling the operator keeps the Platform running. swallow does not take over an operator
that was installed by hand.

With **Device Metrics Exporter** on, the operator runs AMD's exporter on each GPU
node's host network at port 5001, the same port and metric names as every other
Device Metrics Exporter swallow manages. The Site Prometheus collects it, and the
Platform Grafana adds its GPU dashboards. A GPU node that already runs AMD's
standalone exporter package is refused, since both need that port.

### Platform Grafana

The Kubernetes workspace's **Features** step also has the **Platform Grafana**
switch, on by default, CPU-only Platforms included: a read-only Grafana in the
cluster, reached at port 30300 of every workload node (see
[Monitoring](monitoring.md#a-platforms-own-grafana)).

## Slurm

The Slurm workspace assigns controller, compute, and login roles independently,
then derives state-server and workload-storage responsibilities from the selected
topology and features. Its Features step uses separate Switches for shared workload
storage, a container runtime, job monitoring (Omnistat by default, the AMD Device Metrics
Exporter, or none), and the Platform Grafana (on by default; see
[Monitoring](monitoring.md#a-platforms-own-grafana)); AMD GPU GRES is included automatically
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

## Platform runtime workspace

Opening `/platforms/<id>` redirects to the Platform's **Overview** while preserving the
selected Site. Each tab under the header, an icon beside its name as on a Server's detail
page, has its own URL, so refresh, browser history, and shared URLs keep the current tab;
the arrow keys move between tabs.

- **Overview** is summary-first: lifecycle, topology, member matching, applied generation,
  sync freshness, runtime access, and the capabilities supplied by the Platform module.
  It does not repeat the complete OS, network, driver, or node configuration.
- **Configuration** shows the applied specification, desired generation and drift, Conditions,
  member roles, OS preparation, Integration, module settings, and accelerator contributions.
  It is read-only except for module actions that already have a supported Workflow, such as
  AMD GPU Operator management.
- **Monitoring** links to Swallow usage charts, the Platform Grafana, other Monitoring Links,
  and the recorded state and Workflow of Omnistat or GPU monitoring components. It does not
  embed those dashboards.
- **Activity** lists this Platform's Workflows newest first. Page number is part of the URL;
  the rows selector offers 5, 10, 15, or 20 Workflows.
- Kubernetes adds **Applications**, **Namespaces**, and **Cluster**. Slurm adds **Nodes**
  (login access, controllers, and compute nodes) and **Partitions**.

Changing or failed lifecycle states lead with **View workflow** or **Review workflow**.
A Change Plan waiting for approval links to **Change Plans**. An active Platform with its
own Grafana offers **Open Platform Grafana**; **Sync**, uninstall, and record deletion remain
available without turning the Overview into an action wall.

## Lifecycle

- **Sync** refreshes observed Platform membership/state.
- **Uninstall** removes the swallow-deployed runtime from its original targets through a
  Change Plan, which confirming the dialog approves: the uninstall starts at once and the
  Platform page shows it running (with the safety switch on, it waits on **Change Plans**
  instead, and the Platform page says so with a link there). swallow deletes the Platform
  record once the uninstall succeeds; a failed uninstall keeps the record for diagnosis.
  When a workflow still holds the Platform's servers (a failed deployment waiting
  for attention, or one still running), the dialog lists it and asks you to confirm
  that swallow cancels it before the uninstall starts. A Platform whose deployment is
  running or failed is uninstalled too, because the deployment may already have changed
  its servers; only a Platform whose install never started is deleted at once.
- If the uninstall's Change Plan was rejected or expired, or the uninstall failed, press
  **Uninstall** again: it proposes a new uninstall with the options you choose this time.
  Choosing different options while the uninstall still waits for approval replaces the
  waiting plan.
- **Delete only** removes only the swallow record/projections and must not be
  confused with host-side uninstall.
- Bulk actions remain subject to each Platform's lifecycle gate.

Every approved install or uninstall creates a Workflow. Inspect failed Tasks and
retry at the narrowest supported level rather than recreating the Platform.

Exact payloads and lifecycle states are in the active
[Platforms contract](../../../api-server/docs/development/api-contracts/api-server/platforms.md)
and
[Kubernetes explorer contract](../../../api-server/docs/development/api-contracts/api-server/platforms-kubernetes.md).
