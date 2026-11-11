# 核心概念

[English](../en/concepts.md) · [文件首頁](README.md)

Shared [glossary](../development/glossaries/README.md) 是權威來源。本頁提供 user
安全使用 swallow 所需的最小 model。

## Site 與 Integration

**Site** 是 swallow 擁有的實體或邏輯 infrastructure scope，例如 datacenter、
colocation cage 或 lab。**Integration** 只會把一個 Site 連到一個外部系統。
Site 通常會有一個 MAAS provisioner 與一個 Prometheus-compatible metrics integration。

Integration credential 是 write-only。Sync metadata 會顯示何時開始 refresh、最後
成功與失敗時間，讓 user 判斷 staleness。

## Server

**Server** 是 provisioner inventory 內 machine 的 projection。Server 由
reconciliation 建立，不存在 create-Server 操作。要加入 Server，就讓 machine 進入
provisioner 的 inventory：從網路開機讓它 enlist、enroll 保留既有 OS 的 host，或從
hypervisor enroll libvirt virtual machine（見
[Servers 與 infrastructure](guides/servers-and-infrastructure.md)）。Server 的 `origin`
記錄它如何加入：`discovered`、`enrolled` 或 `imported`。Opaque `id` 是穩定
identity；hostname、address、serial number、MAC address 都是 observed attributes，
不能當 key。

Server status 有互相獨立的軸：

| Axis | Owner | 例子 |
| --- | --- | --- |
| Provisioning | Provisioner | ready、inspecting、deploying、releasing、failed |
| Membership | Platform runtime | member role 或尚未觀察到 membership |
| Health | Metrics backend，依 swallow 的監控政策 | up、down、unknown 或 not applicable |

Provisioning axis 使用 swallow 定義、與 provider 無關的
[`provisioning.state`](../development/glossaries/terms/os-provisioning-state.md)
值集合。每個 provisioner adapter 都會將自己的 lifecycle 對應到這些值。Provider
自己的用語（例如 MAAS 的 *Commissioning*）只會出現在 display-only
`providerState`；client 不得據此分支。

尚未觀察到的 axis 是 `null`，表示 unknown，不是 unhealthy。每次 observation 都可有自己的 `observedAt`。

Health 是例外：它一定有值，因為 swallow 依自己的監控政策分類它，而不是等觀察結果出現。

- `up` 與 `down`：swallow 讓 Site 的 Prometheus 收集這台 Server 的 Node Exporter，而且最近一次收集成功或失敗。
- `unknown`：swallow 監控這台 Server，但還沒有答案，例如它的 Node Exporter 還沒安裝，或讀不到 Site 的
  Prometheus。請當成警示，而不是故障。
- `not_applicable`：swallow 不監控這台 Server，例如它沒有佈署、被 lock，或所屬 Site 的 automation 關閉。
  這不代表機器有任何問題。

Health 只有在 `up` 與 `down` 時帶 `observedAt`。見 [Server health](guides/monitoring.md#server-health)。

## Desired state 與 observed state

由 swallow 收斂的 resource 有 `spec`（你要求的樣子）與 `status`（swallow 最後觀察到的
樣子，以及它的 `conditions`）：

| Condition | `true` 的意思 |
| --- | --- |
| `realized` | Observed state 符合目前的 spec。 |
| `drifted` | swallow 以外有人改動，已不符合 spec。 |
| `requires_attention` | swallow 已停止重試，需要 user 處理。 |
| `held` | 收斂被暫停，例如 Server 被 lock，或由某個 Workflow 擁有。 |

寫入 spec 後，swallow 存下即回應，變更之後才套用；請以 conditions 與
`status.observedGeneration` 判斷是否已生效。Provisioner 無法實現某個欄位時，
`realized` 為 `false`、reason 為 `capability_missing`，你的 spec 照樣保留。Server 的
tag 與 placement、Zone 與 Pool、SSH key，以及 provisioner 的 HTTP proxy 與 Subnet DNS
都是這樣運作；其他 resource 維持各 guide 描述的行為。

## Zone、Pool、tag 與 lock

Zone／Pool 是 user-defined infrastructure grouping；tag 加上 capability 或 policy
metadata。Server 的手動 tag 與它的 Zone／Pool 屬於 spec，由 provisioner 實現；
provisioner 自行計算或維護的 tag（automatic tag）只是 observed，可以移除或刪除，但不能指派。Provisioner 沒有這個能力時回報
`capability_missing`，swallow 不另外保存替代的副本。

Server lock 會保護 Server 不受 automation 與 destructive action 影響，但不能取代
provider state 或 authorization。

## Platform

**Platform** 是由 swallow 佈署的 Kubernetes 或 Slurm runtime environment，可以是
single-node 或 multi-node。swallow 擁有 deployment policy 與 lifecycle intent；
runtime API 擁有 observed membership 與 live state。既有 third-party runtime
不能註冊成受管理 Platform。

## Workflow、Job、Task 與 Runner

**Workflow** 是 durable user intent，由可重用的 **Job** 與 atomic、idempotent
的 **Task** 組成。**Runner** 透過 provisioner、Ansible 或 internal mechanism 執行
Task。swallow 的 store 記錄每個 Workflow 與 Task 的 state，由 Temporal 執行；負責
派發 Workflow 的 worker 與 Ansible executor 必須保持執行才能前進。

少數 API 欄位名稱仍使用舊的 Operation 與 Step（例如 `operationId`），但公開
canonical terms 是 Workflow 與 Task。

## Managed Software 與 Software Assignment

**Managed Software** 是一個 host-level software，目前包含 Docker CE、Podman、NFS、
Prometheus、Grafana、AMD Device Metrics Exporter、VictoriaMetrics、libvirt (QEMU/KVM) 與 Node Exporter（swallow 會讓它自動化的
每台已佈署 Server 一直保有它）。
**Software Assignment** 記錄一種 software 在一台 Server 的 desired／last-applied
state。它不是 Server status axis，也不同於 multi-component Platform。

## Staleness

多數 view 會結合 cached provider observation 與 live query。Sync 失敗不會抹掉最後
成功資料，但會使它變 stale。對 inventory 或 health 採取動作前，務必檢查 timestamp
與 sync error。
