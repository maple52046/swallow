# Glossary Outline

This file is the bounded-context and term directory for glossary lookup.

For read-only lookup, read this file after [`README.md`](README.md), then open only the relevant term file under [`terms/`](terms).

For glossary authoring or modification, read [`spec.md`](spec.md) before editing term files.

## Terms

### Platform Delivery

- [Installation](terms/installation.md): Installing and managing the Swallow control
  plane; never the MAAS-owned deployment of an OS to a managed server.

### Compute Resource

- [Server](terms/server.md): The platform's primary managed compute unit, projected from a provisioner's inventory, identified by a swallow-issued `serverId` (not by hostname or IP, which are observed and non-unique).
- [Server Status](terms/server-status.md): Not a single value but three independent status axes — `provisioning`, `membership`, and `health` — each owned by a different system and absent until observed.
- [Server Lock](terms/server-lock.md): Provider-owned protection that blocks Server, provisioner, and host mutations without hiding the Server or stopping monitoring.
- [Server Type](terms/server-type.md): A derived classification by GPU capability — `cpu`, `amd-gpu`, or (reserved) `nvidia-gpu` — that decides which exporters a host runs; AMD is identified by the MAAS tag `amd-gpu`.
- [Cluster](terms/cluster.md): A registered or Swallow-deployed Kubernetes or Slurm
  cluster; Uninstall changes original deployment targets while Delete removes only the record.
- [Cluster Lifecycle State](terms/cluster-lifecycle-state.md): Backend operation-derived
  state from registration through deployment and uninstall, independent of connectivity.
- [Node Role](terms/node-role.md): The part a server plays in a cluster — `control-plane` or `worker` — used both for observed membership and for assigning roles when deploying a cluster.
- [Cluster Topology](terms/cluster-topology.md): The supported placement and availability
  shape of a Kubernetes deployment — standalone, non-HA multi-node, or high availability.

### OS Provisioning

- [OS Image](terms/os-image.md): Live provider-owned operating system artifact
  available to one provisioner Integration.
- [OS Deployment](terms/os-deployment.md): Asynchronous provider-backed OS
  installation whose progress is observed on each Server provisioning axis.
- [Deployment Template](terms/deployment-template.md): Reusable,
  integration-scoped Swallow-owned deployment intent without automation content.
- [Network Configuration](terms/network-configuration.md): Typed NIC subnet-link
  and addressing state plus the explicit intent Swallow can apply.
- [IP Binding](terms/ip-binding.md): One explicit static address associated with
  a Server NIC subnet link through its provisioner.
- [Provisioning Task](terms/provisioning-task.md): Durable coordination for
  provider-backed cleanup that spans asynchronous provisioning transitions.

## Pending Terms

以下術語已被既有文件或程式碼使用，但**尚未定義**。依 [`README.md`](README.md) 的 Pending Terms 規則：任務用到其中任何一個時，必須先補上 term 或與使用者確認語意，不得沿用推測的定義。term 檔建立後，把它從本節移到上方 Terms。

### Ownership and Access

- `Owner` — Server 的擁有關係（team 或 user）。由 `Server` 的 ownership 規則引用。
- `Allocation State` — 由 ownership 推導的配置狀態。**現有用法互相矛盾，補定義前必須先收斂**：平台 API contract 記為 `free | team | user`，dashboard 的 `ServerRepository` port 記為 `free | assigned`。
- `Team` — Server 的可能擁有者之一。
- `User` — 平台身分；現有用法包含 `admin | owner | user` 三種角色，角色語意尚未定義。
- `SSH Key`、`SSH Connection`、`BMC` — Server 的遠端存取路徑。

### Physical Topology

- `Datacenter`、`Room`、`Rack` — Server 的實體層級；`Location` 為指向此層級的複合參照。

### Workload

- `Workload` — 涵蓋 container 與未來 workload 種類的抽象概念。
- `Container` — 可在 Server 上執行的 workload 種類；現有狀態值集為 `running | stopped | exited | created | restarting`。

### Observability

- [Exporter Ownership](terms/exporter-ownership.md): Which subsystem installs a host's Prometheus exporters — `ansible`, `k8s`, or `unmanaged` — resolved per host so exactly one owner exists and exporters never contend for a fixed port.
- `Alert` 及其 severity / status / category 值集。
- `GPU Device`、`GPU Metrics`、`GPU Status`、`GPU Profile`。

### Automation

- [Automation Configuration](terms/automation-configuration.md): Site-scoped settings for embedded Ansible execution.
- [Operation](terms/operation.md): A durable operator intent and its locally owned execution lifecycle.

### Provisioning and Management Planes

- `Plane`（Kubernetes / Slurm 管理平面）。

### Agent

- `Agent` — 節點端 runtime component；與 `api-server` 之間的 gRPC 契約由 provider 擁有。
- `Node ID`、`Inventory`、`Agent Info` — agent 回報身分與環境探測結果時使用的概念。
