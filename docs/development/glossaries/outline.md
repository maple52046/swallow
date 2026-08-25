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

- `Alert` 及其 severity / status / category 值集。
- `GPU Device`、`GPU Metrics`、`GPU Status`、`GPU Profile`。

### Automation

- [Automation Configuration](terms/automation-configuration.md): Site-scoped settings for embedded Ansible execution.
- [Operation](terms/operation.md): A durable operator intent and its locally owned execution lifecycle.

### Provisioning and Management Planes

- `Provisioning Image`、`Provisioning Profile`、`Provisioning Job`。
- `Plane`（Kubernetes / Slurm 管理平面）。

### Agent

- `Agent` — 節點端 runtime component；與 `api-server` 之間的 gRPC 契約由 provider 擁有。
- `Node ID`、`Inventory`、`Agent Info` — agent 回報身分與環境探測結果時使用的概念。
