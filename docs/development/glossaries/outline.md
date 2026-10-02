# Glossary Outline

This file is the bounded-context and term directory for glossary lookup.

For read-only lookup, read this file after [`README.md`](README.md), then open only the relevant term file under [`terms/`](terms).

For glossary authoring or modification, read [`spec.md`](spec.md) before editing term files.

## Terms

### System and Sites

- [swallow](terms/swallow.md): The system this repository builds; the only name for it — not "the platform" and not a `Platform`.
- [Site](terms/site.md): A swallow-owned location that owns its own infrastructure and frames every other managed fact.
- [Integration](terms/integration.md): A swallow-owned registration of an external system (`provisioner` / `metrics` / `cluster`), scoped to one Site.
- [Staleness](terms/staleness.md): The freshness of a mirrored fact — its source and last-observed time — surfaced as part of the API.
- [Provider Data Overlay](terms/provider-data-overlay.md): Swallow-owned fields merged onto a provider-owned fact at read time (e.g. an OS Image name), owned data keyed by provider identity and never written back.

### swallow Delivery

- [Installation](terms/installation.md): Installing and managing the swallow control
  plane (single-VM production, including the co-located MAAS and its official `ubuntu/noble`
  image); never the MAAS-owned deployment of an OS to a managed server.

### Compute Resource

- [Server](terms/server.md): swallow's primary managed compute unit, projected from a provisioner's inventory, identified by a swallow-issued `serverId` (not by hostname or IP, which are observed and non-unique).
- [Server Status](terms/server-status.md): Not a single value but three independent status axes — `provisioning`, `membership`, and `health` — each owned by a different system and absent until observed.
- [Server Lock](terms/server-lock.md): Provider-owned protection that blocks Server, provisioner, and host mutations without hiding the Server or stopping monitoring.
- [Server Type](terms/server-type.md): A derived classification by GPU capability — `cpu`, `amd-gpu`, or (reserved) `nvidia-gpu` — that decides which exporters a host runs; AMD is identified by the MAAS tag `amd-gpu`.
- [Tag](terms/tag.md): An operator-facing label on a Server, provider-owned when the provisioner supports tagging (MAAS) and swallow-owned otherwise; drives Server Type and discovery filters.

### Platform Management

- [Platform](terms/platform.md): A registered or Swallow-deployed Kubernetes or Slurm
  runtime; Uninstall changes original deployment targets while Delete removes only the record.
- [Platform Lifecycle State](terms/platform-lifecycle-state.md): Backend operation-derived
  state from registration through deployment and uninstall, independent of connectivity.
- [Node Role](terms/node-role.md): The part a server plays in a Platform — `control-plane` or `worker` — used both for observed membership and for assigning roles when deploying a Platform.
- [Kubernetes Topology](terms/kubernetes-topology.md): The supported placement and availability
  shape of a Kubernetes deployment — standalone, non-HA multi-node, or high availability.
- [Kubernetes Application](terms/kubernetes-application.md): A live workload grouping (Deployment,
  DaemonSet, StatefulSet, or owner-less Pod) the cluster explorer aggregates on demand; never a
  stored entity.
- [Minimum Resource Requirement](terms/minimum-resource-requirement.md): An optional system-wide deployment eligibility floor for observed Server CPU, memory, and storage; not a reservation or capacity promise.

### OS Provisioning

- [OS Provisioning Provider](terms/os-provisioning-provider.md): External system (MAAS)
  that enumerates hardware and installs operating systems; registered as a `provisioner` Integration.
- [Machine](terms/machine.md): An entry in a provider's inventory — the provider's view;
  a Server is swallow's projection of it.
- [OS Image](terms/os-image.md): Live provider-owned operating system artifact
  available to one provisioner Integration.
- [OS Deployment](terms/os-deployment.md): Asynchronous provider-backed OS
  installation whose progress is observed on each Server provisioning axis.
- [Deploy Target](terms/deploy-target.md): Where an OS Deployment runs — `disk` or `ram` (memory-backed, the fact still called `ephemeral` at the boundary); the canonical deploy-mode vocabulary and the axis a custom OS Image is verified against.
- [Release](terms/release.md): Returning a Machine to the provider's available pool
  (`ready` again) without removing the Server; allowed from deployed, failed, broken, or rescue.
- [Provider Recovery](terms/provider-recovery.md): Swallow-owned policy and the Recover/Release intents that return a `failed`, `broken`, or `rescue` Server to `ready`.
- [Rescue Mode](terms/rescue-mode.md): A provider diagnostic environment; exiting restores the prior state and does not by itself reach `ready`.
- [Deployment Template](terms/deployment-template.md): Reusable,
  integration-scoped Swallow-owned deployment intent without automation content.
- [Network Configuration](terms/network-configuration.md): Typed NIC subnet-link
  and addressing state plus the explicit intent Swallow can apply.
- [IP Binding](terms/ip-binding.md): One explicit static address associated with
  a Server NIC subnet link through its provisioner.
- [Provisioning Task](terms/provisioning-task.md): Durable coordination for
  provider-backed cleanup that spans asynchronous provisioning transitions.

### Infrastructure Grouping

- [Zone](terms/zone.md): A swallow-owned, Site-scoped grouping of Servers for availability/fault/organization, realized in the provisioner (MAAS physical zone) when it is grouping-capable.
- [Pool](terms/pool.md): A swallow-owned, Site-scoped resource pool for allocation grouping, realized in the provisioner (MAAS resource pool) when it is grouping-capable; not the "available pool" of Release.

### Access

- [SSH Key](terms/ssh-key.md): A swallow-owned public key realized into key-capable provisioners, purpose `deployment` (the Deployment Key) or `access` (a User's public-key-only Access Key).
- [Session](terms/session.md): One password sign-in of a User, held by a short-lived access token and a rotating refresh token; ends at logout, idle expiry, or maximum age.
- [API Key](terms/api-key.md): A named, long-lived secret a User creates so non-interactive clients (CLI, scripts) call the API as that User; shown once, stored hashed, optional expiry, revoked by deletion.
- [Deployment Key](terms/deployment-key.md): The single system-owned key pair, created as ed25519 by the installation step (not at API start), whose sealed private key swallow uses for every Site's SSH readiness and Ansible (no Site override); OS and Platform deployment require it.

### Tenancy

- [Allocation State](terms/allocation-state.md): A Server's tenancy assignment —
  `free | team | user` — derived from ownership; swallow-owned policy.

### Observability

- [Exporter Ownership](terms/exporter-ownership.md): Which subsystem installs a host's Prometheus exporters — `ansible`, `k8s`, or `unmanaged` — resolved per host so exactly one owner exists and exporters never contend for a fixed port.

### Software Deployment

- [Managed Software](terms/managed-software.md): A single piece of host software and its variants that Swallow installs on / uninstalls from already-deployed Servers; the deploy target is one software, not a multi-component platform.
- [Software Assignment](terms/software-assignment.md): A swallow-owned durable record, keyed by `(serverId, kind)`, of the desired and last-applied state of one Managed Software on one Server; not a fourth Server status axis.

### Automation

- [Workflow](terms/workflow.md): An operator intent as a desired end-state for a set of resources, executed durably and convergently as a composition of Jobs and Tasks (canonical; compatibility code and wire fields still contain "Operation").
- [Job](terms/job.md): A reusable, convergent unit of Tasks that brings resources to a sub-goal (e.g. `ensure-os`), composable into Workflows and runnable on its own.
- [Task](terms/task.md): The atomic, idempotent ("ensure") unit of work run by exactly one Runner (code/API still say "Step").
- [Runner](terms/runner.md): The mechanism that executes a Task — `provisioner` / `ansible` / `internal` (named by purpose; code still says "executor", and `maas` → `provisioner`).
- [Automation Configuration](terms/automation-configuration.md): Site-scoped settings for Swallow-owned Ansible execution.

> Deprecated aliases kept for the [decision 017](../../decisions/017-workflow-job-task-runner-model.md) rename window: [Operation](terms/operation.md) → Workflow, [Operation Step](terms/operation-step.md) → Task.

## Pending Terms

以下術語已被既有文件或程式碼使用，但**尚未定義**。依 [`README.md`](README.md) 的 Pending Terms 規則：任務用到其中任何一個時，必須先補上 term 或與使用者確認語意，不得沿用推測的定義。term 檔建立後，把它從本節移到上方 Terms。

### Ownership and Access

- `Owner` — Server 的擁有關係（team 或 user）。由 `Server` 的 ownership 規則引用。
- `Team` — Server 的可能擁有者之一。
- `User` — swallow 身分；現有用法包含 `admin | owner | user` 三種角色，角色語意尚未定義。Session、API Key 與 Access Key 都屬於某個 User（目前只有 bootstrap admin）。
- `SSH Connection`、`BMC` — Server 的遠端存取路徑（`SSH Key` 已定義於上方 Access）。

### Physical Topology

- `Datacenter`、`Room`、`Rack` — Server 的實體層級；`Location` 為指向此層級的複合參照。

### Workload

- `Workload` — 涵蓋 container 與未來 workload 種類的抽象概念。
- `Container` — 可在 Server 上執行的 workload 種類；現有狀態值集為 `running | stopped | exited | created | restarting`。

### Observability

- `Alert` 及其 severity / status / category 值集。
- `GPU Device`、`GPU Metrics`、`GPU Status`、`GPU Profile`。

### Provisioning and Management Planes

- ~~`Plane`（Kubernetes / Slurm 管理平面）~~：已由 [decision 032](../../decisions/032-self-deployed-platform-management.md) supersede。管理平面不再是可註冊的外部物件；受管 runtime 一律是 Swallow 自部的 `Platform`，Kubernetes 的叢集內管理走 Platform explorer，不需要獨立的 `Plane` term。

> `Agent`（節點端 runtime）與其 `Node ID` / `Inventory` / `Agent Info` 概念已依
> [decision 001](../../decisions/001-system-ownership-boundaries.md) 退場，不再是 pending term。
