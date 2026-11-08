# Glossary Outline

This file is the bounded-context and term directory for glossary lookup.

For read-only lookup, read this file after [`README.md`](README.md), then open only the relevant term file under [`terms/`](terms).

Each summary gives a term's meaning in one line. Who owns a fact and how it is written or stored are stated only in the term file, so a summary never goes stale when that changes.

For glossary authoring or modification, read [`spec.md`](spec.md) before editing term files.

## Terms

### System and Sites

- [swallow](terms/swallow.md): The system this repository builds — a generic core delivered, with providers and plugins, as an out-of-the-box distribution; the only name for it — not "the platform" and not a `Platform`.
- [Site](terms/site.md): A location with its own infrastructure that frames every other managed fact.
- [Integration](terms/integration.md): One registration of a Provider Adapter at one Site (`provisioner` / `metrics` / `platform` / `repository`).
- [Staleness](terms/staleness.md): The freshness of a mirrored fact (Observed State) — its source and last-observed time — surfaced as part of the API.
- [Provider Data Overlay](terms/provider-data-overlay.md): Retired: fields once merged onto a provider fact at read time (the OS Image overlay, the Server tag fallback); both became Desired State ([decision 071](../../decisions/071-provisioner-realized-desired-state.md) (provisioner realized desired state)).

### Resource Ownership and Convergence

- [Desired State](terms/desired-state.md): What a user wants a managed resource to be (its spec), the single source of truth that providers realize.
- [Observed State](terms/observed-state.md): What a provider reports a resource actually is, with source and observation time; never authoritative for a write.
- [Condition](terms/condition.md): A named judgement in a resource's status — shared types `realized`, `drifted`, `requires_attention`, `held` — written only by its controller.
- [Drift](terms/drift.md): Observed State that departed from Desired State without swallow asking; reported, not corrected, by default.
- [Convergence](terms/convergence.md): The controller's continuous, level-triggered process of bringing Observed State to Desired State; not provider reconcile.
- [Change Plan](terms/change-plan.md): The user-approved preview of a Destructive Change (approved with its submission or afterwards), applied as a Workflow — `pending_approval | approved | applied | rejected | superseded | expired`.
- [Destructive Change](terms/destructive-change.md): A change whose realization erases data, takes a Server out of service or a Platform, interrupts workloads, or cannot be undone by reapplying the old spec.
- [Controller](terms/controller.md): The level-triggered convergence loop for one resource kind; one active instance (store lease), outside the API process; the only writer of that resource's Conditions.
- [Hold](terms/hold.md): A pause on a resource's convergence — Server Lock, Rescue Mode, or a running Workflow holding the resource — shown as the `held` Condition; observation and Drift reporting continue.

### Core and Extensions

- [Provider Interface](terms/provider-interface.md): A core-defined port, in core vocabulary, for one kind of external system (OS provisioning, Workflow executor, host automation, store, metrics and alerts, BMC and hypervisor, Local Repository).
- [Provider Adapter](terms/provider-adapter.md): The replaceable implementation of a Provider Interface for one system (MAAS, Temporal, Ansible, MongoDB, Pulp, …), translating its vocabulary; never part of the core.
- [Extension Module](terms/extension-module.md): A unit adding something swallow manages — a Platform Module, Accelerator Module, or Software Module — through the module contract; also called a plugin.
- [Platform Module](terms/platform-module.md): The module that makes one kind of Platform runtime manageable (Kubernetes, Slurm).
- [Accelerator Module](terms/accelerator-module.md): The module for one GPU vendor (AMD, later NVIDIA), an axis separate from Platforms.
- [Software Module](terms/software-module.md): The module that provides one Managed Software kind.
- [Module Manifest](terms/module-manifest.md): The data declaration, with a contract version, by which the core loads a module.
- [Module Package](terms/module-package.md): A planned bundle of one module installed or updated without upgrading swallow — stage 2 for project-built modules, stage 3 (signed, no Go hooks) for modules maintained outside the project.

### swallow Delivery

- [Installation](terms/installation.md): Installing and managing the swallow control
  plane (single-VM production, including the co-located MAAS and its official `ubuntu/noble`
  image); never the provisioner's deployment of an OS to a managed server.
- [Control-Plane NTP Server](terms/control-plane-ntp-server.md): The one host NTP service an
  Installation provides at `SWALLOW_ADDRESS`, which every provisioned Server follows (the
  co-located MAAS chronyd, or distribution chrony with another provisioner); its upstream is set
  separately with `swallowctl ntp upstream`; never a container.
- [Support Matrix](terms/support-matrix.md): The per-release record of included adapters and
  modules and the validated combinations; only those may be claimed as supported.

### Compute Resource

- [Server](terms/server.md): swallow's primary managed compute unit, projected from a provisioner's inventory, identified by a swallow-issued `serverId` (not by hostname or IP, which are observed and non-unique).
- [Server Status](terms/server-status.md): Not a single value but three independent status axes — `provisioning`, `membership`, and `health` — each reported by a different system; `provisioning` and `membership` are absent until observed, while `health` (`up | down | unknown | not_applicable`) is always classified by swallow's monitoring policy.
- [Server Lock](terms/server-lock.md): A provider-side protection that blocks Server, provisioner, and host mutations without hiding the Server or stopping monitoring.
- [Server Origin](terms/server-origin.md): How a Server arrived — `discovered`, `enrolled`, or `imported`; display only.
- [Server Default User](terms/server-default-user.md): The OS account automation logs in as with the Deployment Key — set on the Server (verified, optionally installing the key with a one-time password) or else the deployed OS Image's default user; Docker CE adds it to `docker`.
- [Server Type](terms/server-type.md): A derived classification by GPU capability — `cpu`, `amd-gpu`, or (reserved) `nvidia-gpu` — that decides which scrape jobs target a host; AMD is identified by the MAAS tag `amd-gpu`.
- [GPU Inventory](terms/gpu-inventory.md): Provider-observed GPU groups classified as workload `compute` accelerators or non-capacity `display` controllers.
- [Tag](terms/tag.md): A user-facing label on a Server — manual tags set in the Server spec, automatic tags computed by the provisioner; observed tags drive Server Type and discovery filters.
- [BMC](terms/bmc.md): A physical Server's out-of-band management controller: its connection facts from the provisioner and swallow's probe of its Redfish capability (virtual media, boot override); independent of the provisioner's power driver.
- [Power Configuration](terms/power-configuration.md): The power driver and parameters that let the provisioner switch and read a Server's power — family `bmc` (`ipmi`, `redfish`) or `virsh`, control `none`/`manual`/`automatic`; the password is write-only.
- [Boot Media](terms/boot-media.md): A Server's setting that a chosen Boot ISO is attached and booted first to reach the provisioner on externally served networks — method `redfish` (BMC virtual media) or `libvirt` (a VM's CD-ROM on its Hypervisor); applied as a preflight and re-ensured before every inspection and OS Deployment.
- [Hypervisor](terms/hypervisor.md): A deployed Server that runs libvirt virtual machines swallow can enroll by name and give Boot Media, reached over SSH with the Deployment Key; the provisioner still switches their power.
- [Boot ISO](terms/boot-iso.md): A boot ISO swallow builds for one provisioner Integration that takes a site DHCP lease and boots through its MAAS rack — iPXE on BIOS, the ISO's own GRUB on UEFI; built from fixed templates, served by swallow, selected by Boot Media.

### Platform Management

- [Platform](terms/platform.md): A Kubernetes or Slurm runtime that swallow deploys and manages from its
  spec; Uninstall changes its deployment targets while Delete removes only the record.
- [Platform Lifecycle State](terms/platform-lifecycle-state.md): Where a Platform stands — `planned`
  through deployment and uninstall — derived from its Workflows, independent of connectivity.
- [Platform Claim](terms/platform-claim.md): The record on a Server that an accepted
  Platform deployment targets it; released by that Platform's successful uninstall or deletion,
  and the reason a claimed Server cannot be released directly.
- [Node Role](terms/node-role.md): The part a server plays in a Platform — `control-plane` or `worker` — used both for observed membership and for assigning roles when deploying a Platform.
- [Kubernetes Topology](terms/kubernetes-topology.md): The supported placement and availability
  shape of a Kubernetes deployment — standalone, non-HA multi-node, or high availability.
- [Kubernetes Application](terms/kubernetes-application.md): A live workload grouping (Deployment,
  DaemonSet, StatefulSet, or owner-less Pod) the cluster explorer aggregates on demand.
- [Minimum Resource Requirement](terms/minimum-resource-requirement.md): An optional system-wide deployment eligibility floor for observed Server CPU, memory, and storage; not a reservation or capacity promise.
- [GPU Operator](terms/gpu-operator.md): The in-cluster operator that makes a Platform's GPUs schedulable; swallow installs and configures the AMD GPU Operator on a Kubernetes Platform swallow deployed, using the OS image's driver, and runs its Device Metrics Exporter on the host network where the Site Prometheus scrapes it.
- [GPU Allocation Mode](terms/gpu-allocation-mode.md): How the GPU Operator exposes GPUs — `device-plugin` (extended resource) or `dra` (ResourceSlice devices); one at a time.
- [GPU Operator State](terms/gpu-operator-state.md): Where the latest swallow change to a Platform's GPU Operator stands — `installing | ready | failed | uninstalling | absent`.
- [Time Sync Check](terms/time-sync-check.md): How a Platform deployment treats a target whose clock is not in sync — `enforce` (default, stop before installing) or `warn` (report and continue); each Platform checks as the first part of its own installation, never sets time.
- [Omnistat](terms/omnistat.md): AMD's Slurm-aware GPU monitoring that swallow optionally installs with a Slurm deployment — exporter and job attribution on the Platform, Prometheus and Grafana composed as Managed Software; Omnistat State `installing | ready | failed | absent`.
- [Omnistat Hardware Counters](terms/omnistat-hardware-counters.md): The GPU hardware counters (today `VALUBusy`) Omnistat samples when a Slurm Platform asks for them, at deployment or later; each compute node's outcome `collecting | unavailable`, with the reason a node does not collect.
- [Device Metrics Exporter](terms/device-metrics-exporter.md): AMD's per-GPU exporter, the Managed Software kind `device-metrics-exporter` installable on a single Server, and the Slurm feature (instead of Omnistat) that composes it onto the GPU compute nodes with job attribution; Platform state `installing | ready | failed | absent` and GPU identity `distinct | collapsed`.

### OS Provisioning

- [OS Provisioning Provider](terms/os-provisioning-provider.md): External system (MAAS, the
  current Provider Adapter of the OS provisioning interface) that enumerates hardware and installs
  operating systems; registered as a `provisioner` Integration.
- [Machine](terms/machine.md): An entry in a provider's inventory — the provider's view;
  a Server is swallow's projection of it.
- [Server Enrollment](terms/server-enrollment.md): Putting a machine into the provisioner's inventory —
  network boot (then automatic inspection to `ready`, asking for a Power Configuration when the
  enrollment power-off cannot be observed), a libvirt virtual machine named on its Hypervisor, or a
  host keeping its OS (`deployed`); never creating a Server record.
- [OS Provisioning State](terms/os-provisioning-state.md): The swallow-defined `provisioning.state`
  value set (`new | inspecting | ready | … | releasing | …`) that every provider adapter maps onto.
- [OS Image](terms/os-image.md): An operating system artifact available to one
  provisioner Integration.
- [OS Deployment](terms/os-deployment.md): Asynchronous provider-backed OS
  installation whose progress is observed on each Server provisioning axis.
- [Deploy Target](terms/deploy-target.md): Where an OS Deployment runs — `disk` or `ram` (memory-backed, the fact still called `ephemeral` at the boundary); the canonical deploy-mode vocabulary and the axis a custom OS Image is verified against.
- [Release](terms/release.md): Returning a Machine to the provider's available pool
  (`ready` again) without removing the Server; allowed from deployed, allocated, failed, broken, or rescue.
- [Provider Recovery](terms/provider-recovery.md): The policy and the Recover/Release intents that return a `failed`, `broken`, `rescue`, or `allocated` Server to `ready`.
- [Rescue Mode](terms/rescue-mode.md): A provider diagnostic environment; exiting restores the prior state and does not by itself reach `ready`.
- [Deployment Template](terms/deployment-template.md): Reusable,
  integration-scoped deployment intent without automation content.
- [Network Configuration](terms/network-configuration.md): Typed NIC subnet-link
  and addressing state plus the explicit intent swallow can apply.
- [IP Binding](terms/ip-binding.md): One explicit static address associated with
  a Server NIC subnet link through its provisioner.
- [Provisioning Task](terms/provisioning-task.md): Durable coordination for
  provider-backed cleanup that spans asynchronous provisioning transitions.
- [Provisioner HTTP Proxy](terms/provisioner-http-proxy.md): The proxy setting a provisioner
  hands to deployed machines — mode `none | builtin | external | peer` plus a URL — one per
  provisioner Integration; not an Integration setting.
- [Subnet DNS](terms/subnet-dns.md): The DNS servers of one subnet that the provisioner hands to
  `automatic` and `static` deployments on it.

### Infrastructure Grouping

- [Zone](terms/zone.md): A Site-scoped grouping of Servers for availability/fault/organization, held by every grouping-capable provisioner of the Site (MAAS physical zone); a Server's placement in it is part of the Server's spec.
- [Pool](terms/pool.md): A Site-scoped resource pool for allocation grouping, held by every grouping-capable provisioner of the Site (MAAS resource pool); not the "available pool" of Release.

### Access

- [SSH Key](terms/ssh-key.md): A public key realized into key-capable provisioners, purpose `deployment` (the Deployment Key) or `access` (a User's public-key-only Access Key).
- [Session](terms/session.md): One password sign-in of a User, held by a short-lived access token and a rotating refresh token; ends at logout, idle expiry, or maximum age.
- [API Key](terms/api-key.md): A named, long-lived secret a User creates so non-interactive clients (CLI, scripts) call the API as that User; shown once, kept only as a hash, optional expiry, revoked by deletion.
- [Deployment Key](terms/deployment-key.md): The installation's single key pair, created as ed25519 by the installation step (not at API start), whose sealed private key swallow uses for every Site's SSH readiness and Ansible (no Site override); OS and Platform deployment require it.

### Tenancy

- [Allocation State](terms/allocation-state.md): A Server's tenancy assignment —
  `free | team | user` — derived from whom the Server is assigned to.

### Observability

- [Monitoring Link](terms/monitoring-link.md): A name and URL from a Platform to its own monitoring UI (e.g. the Platform's Grafana that swallow installs); display only, never queried, not a metrics Integration.

### Software Deployment

- [Managed Software](terms/managed-software.md): A single piece of host software and its variants that swallow installs on / uninstalls from already-deployed Servers; the deploy target is one software, not a multi-component platform.
- [Software Assignment](terms/software-assignment.md): A durable record, keyed by `(serverId, kind)`, of the desired and last-applied state of one Managed Software on one Server; not a fourth Server status axis.
- [Docker Host Explorer](terms/docker-host-explorer.md): The live, api-server-mediated management of the Docker Engine on a Server where swallow installed Docker CE with `enableApi`; images, containers, volumes, and networks stay in the Docker Engine.
- [Registry Credential](terms/registry-credential.md): A sealed, installation-wide credential for one image registry, attached to an explorer pull whose image reference resolves to that registry.

### Local Repository

- [Local Repository](terms/local-repository.md): A Site's own source of every artifact a deployment installs — `deb`, `rpm` (designed in), `oci`, `python`, `file` — kept by a provider (Pulp) next to the control plane; used when the Site chooses it (`serveDeployments`), otherwise deployments fetch from the declared upstreams; never a fallback in either direction.
- [Repository Source](terms/repository-source.md): One upstream a Local Repository mirrors (a declared package source) or caches (a pull-through registry host or PyPI) — `pending | syncing | ready | failed`.

### Automation

- [Workflow](terms/workflow.md): A user intent as a desired end-state for a set of resources, executed durably and convergently as a composition of Jobs and Tasks (canonical; compatibility code and wire fields still contain "Operation").
- [Job](terms/job.md): A reusable, convergent unit of Tasks that brings resources to a sub-goal (e.g. `ensure-os`), composable into Workflows and runnable on its own.
- [Task](terms/task.md): The atomic, idempotent ("ensure") unit of work run by exactly one Runner (code/API still say "Step").
- [Task Diagnostic](terms/task-diagnostic.md): The durable, redacted, two-layer explanation of a failed Task attempt — what happened, who is affected, what to do next, and the evidence.
- [Failure Class](terms/failure-class.md): Whether doing the same thing again can succeed — `transient | permanent | unknown`.
- [Runner](terms/runner.md): The mechanism that executes a Task — `provisioner` / `ansible` (host automation) / `internal` (named by the Provider Interface it drives; code still says "executor", and `maas` → `provisioner`).
- [Automation Configuration](terms/automation-configuration.md): Site-scoped settings for swallow's Ansible execution.

> Retired names that survive only in code types and unrenamed wire fields ([decision 072](../../decisions/072-store-driven-workflow-execution.md) (store driven workflow execution), [decision 074](../../decisions/074-retire-schema-v2-and-deprecated-aliases.md) (retire schema v2 and deprecated aliases)): [Operation](terms/operation.md) → Workflow, [Operation Step](terms/operation-step.md) → Task.

## Old Names

Names a reader still meets in older documents, code, or payloads, and the term to use instead. The
term file of the current term has the details.

| Old name | Use | Where the old name survives |
| --- | --- | --- |
| Cluster | [Platform](terms/platform.md) | Kubernetes vocabulary inside the Kubernetes context only; the public aliases were removed |
| Operation | [Workflow](terms/workflow.md) | Code types; payload fields `operationId`, `retryOfOperationId`, `lifecycleOperationId`; the Temporal workflow ID `swallow-operation/<id>` |
| Operation Step, Step | [Task](terms/task.md) | Code types; the `stepId` payload field |
| `executor` | [Runner](terms/runner.md) | The Task's `executor` payload field |
| `maas` (as a Runner) | The provisioner [Runner](terms/runner.md) | The persisted Runner value `maas` |
| Provisioning Job | [Provisioning Task](terms/provisioning-task.md), [OS Deployment](terms/os-deployment.md) | — |
| Provisioning Profile | [Deployment Template](terms/deployment-template.md) | — |
| Provisioning Image | [OS Image](terms/os-image.md) | — |
| Provider Data Overlay | OS Image metadata and the Server's manual tags, both [Desired State](terms/desired-state.md) | Migration code only |
| Host | [Server](terms/server.md) | Parts of the dashboard |
| Ephemeral | [Deploy Target](terms/deploy-target.md) `ram` | The `ephemeral` field in responses, the store, and the MAAS adapter |
| `commissioning` | `inspecting` ([OS Provisioning State](terms/os-provisioning-state.md)) | The MAAS adapter |
| Automation key, Site private key | [Deployment Key](terms/deployment-key.md) | — |
| `master` | The control-plane [Node Role](terms/node-role.md) | — |
| Server import | [Server Enrollment](terms/server-enrollment.md); a Server registered with its OS kept has the `imported` [Server Origin](terms/server-origin.md) | — |
| Artifact Repository | [Local Repository](terms/local-repository.md) | — |

Two names that look related are not: the schema-v2 Operation records were retired and dropped
(decision 074), while `swallow.workflow.v2` is the name of the current Temporal driver workflow.

## Pending Terms

以下術語已被既有文件或程式碼使用，但**尚未定義**。依 [`README.md`](README.md) 的 Pending Terms 規則：任務用到其中任何一個時，必須先補上 term 或與使用者確認語意，不得沿用推測的定義。term 檔建立後，把它從本節移到上方 Terms。

### Ownership and Access

- `Owner` — Server 的擁有關係（team 或 user）。由 `Server` 的 ownership 規則引用。
- `Team` — Server 的可能擁有者之一。
- `User` — swallow 身分；現有用法包含 `admin | owner | user` 三種角色，角色語意尚未定義。Session、API Key 與 Access Key 都屬於某個 User（目前只有 bootstrap admin）。
- `SSH Connection` — Server 的遠端存取路徑（`SSH Key` 已定義於上方 Access；`BMC` 已定義於上方 Compute Resource）。

### Physical Topology

- `Datacenter`、`Room`、`Rack` — Server 的實體層級；`Location` 為指向此層級的複合參照。

### Workload

- `Workload` — 涵蓋 container 與未來 workload 種類的抽象概念。
- `Container` — 可在 Server 上執行的 workload 種類；現有狀態值集為 `running | stopped | exited | created | restarting`。

### Observability

- `Alert` 及其 severity / status / category 值集。
- `GPU Device`、`GPU Metrics`、`GPU Status`、`GPU Profile`。

### Provisioning and Management Planes

- ~~`Plane`（Kubernetes / Slurm 管理平面）~~：已由 [decision 032](../../decisions/032-self-deployed-platform-management.md)（self deployed platform management） supersede。管理平面不再是可註冊的外部物件；受管 runtime 一律是 swallow 自部的 `Platform`，Kubernetes 的叢集內管理走 Platform explorer，不需要獨立的 `Plane` term。

> `Agent`（節點端 runtime）與其 `Node ID` / `Inventory` / `Agent Info` 概念已依
> [decision 069](../../decisions/069-swallow-owns-desired-state.md)（swallow owns desired state） 退場，不再是 pending term。
