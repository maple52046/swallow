# Managed Software

[English](../../../docs/en/guides/managed-software.md) · [文件首頁](../README.md)

Managed Software 會在已 deployed 的 Server 安裝或移除一個 host-level software，
刻意與 Platform deployment 分開。

## Supported software

- **Docker CE：** 從 configured package source 安裝 Docker runtime。
- **Podman：** 安裝 request 選擇的 Podman variant。
- **NFS：** 依 required storage settings 安裝 server 或 client role。

Shipped automation manifest 是 allowlist。Software kind／variant 沒有出現在 active
API contract 與 manifest 時，就不是 supported capability。

## Eligibility

安裝前：

- 每個 target Server 都是 deployed、manageable state。
- Server 未 lock。
- 沒有 conflicting active Workflow 擁有 target。
- Site automation 能為每台 host resolve SSH user 與 credential。
- Required software-specific settings 已提供。

一個 request 可包含多台 eligible Servers。送出時 API 會再次套用同一組 gates。

## Software Assignment

Software Assignment 以 Server 與 software kind 為 key，記錄 desired state、
last-applied state、related Workflow 與 failure information。它不代表每個 external
package fact 都即時；結果不清楚時請看 associated Workflow 與 host diagnostics。

## Install 與 uninstall

在 **Software** 選擇 kind、variant/settings 與 targets。送出後會透過 registered
playbook mapping 建立 Workflow。Uninstall 是 explicit request，會產生另一個 Workflow；
刪除 Server／Platform 不會默認 uninstall 所有 software。

變更 NFS server/client role 可能影響 mounted storage 與 workload availability，
需要額外檢查。

## Dashboard 與 API

Operator 請使用 Dashboard 的 **Software** 頁面。`swallow` CLI 尚未提供
`software` command group；automation client 應依 active
[Managed Software contract](../../../api-server/docs/development/api-contracts/api-server/software.md)
處理 payload fields、authentication 與 errors。
