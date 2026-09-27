# 核心概念

[English](../en/concepts.md) · [文件首頁](README.md)

Shared [glossary](../development/glossaries/README.md) 是權威來源。本頁提供 operator
安全使用 swallow 所需的最小 model。

## Site 與 Integration

**Site** 是 swallow 擁有的實體或邏輯 infrastructure scope，例如 datacenter、
colocation cage 或 lab。**Integration** 只會把一個 Site 連到一個外部系統。
Site 通常會有一個 MAAS provisioner 與一個 Prometheus-compatible metrics integration。

Integration credential 是 write-only。Sync metadata 會顯示何時開始 refresh、最後
成功與失敗時間，讓 operator 判斷 staleness。

## Server

**Server** 是 provisioner inventory 內 machine 的 projection。Server 由
reconciliation 建立，不存在 create-Server 操作。Opaque `id` 是穩定 identity；
hostname、address、serial number、MAC address 都是 observed attributes，不能當 key。

Server status 有互相獨立的軸：

| Axis | Owner | 例子 |
| --- | --- | --- |
| Provisioning | Provisioner | ready、deploying、deployed、failed |
| Membership | Platform runtime | member role 或尚未觀察到 membership |
| Health | Metrics backend | 目前 health 或 unknown |

`null` 表示 unknown，不是 unhealthy。每次 observation 都可有自己的 `observedAt`。

## Zone、Pool、tag 與 lock

Zone／Pool 是 operator-defined infrastructure grouping；tag 加上 capability 或 policy
metadata。Provisioner 支援某個 fact 時仍是 source of truth；不支援時由 swallow
提供 owned fallback。

Server lock 會保護 Server 不受 automation 與 destructive action 影響，但不能取代
provider state 或 authorization。

## Platform

**Platform** 是由 swallow 佈署的 Kubernetes 或 Slurm runtime environment，可以是
single-node 或 multi-node。swallow 擁有 deployment policy 與 lifecycle intent；
runtime API 擁有 observed membership 與 live state。既有 third-party runtime
不能註冊成受管理 Platform。

## Workflow、Job、Task 與 Runner

**Workflow** 是 durable operator intent，由可重用的 **Job** 與 atomic、idempotent
的 **Task** 組成。**Runner** 透過 provisioner、Ansible 或 internal mechanism 執行
Task。Temporal 提供 durability；worker 與 Ansible executor 必須保持執行才能前進。

某些 implementation／compatibility surface 仍使用 `Operation` 與 `Step`，
但公開 canonical terms 是 Workflow 與 Task。

## Managed Software 與 Software Assignment

**Managed Software** 是一個 host-level software，目前包含 Docker CE、Podman 與 NFS。
**Software Assignment** 記錄一種 software 在一台 Server 的 desired／last-applied
state。它不是 Server status axis，也不同於 multi-component Platform。

## Staleness

多數 view 會結合 cached provider observation 與 live query。Sync 失敗不會抹掉最後
成功資料，但會使它變 stale。對 inventory 或 health 採取動作前，務必檢查 timestamp
與 sync error。
