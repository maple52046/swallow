# 初始設定

[English](../../../docs/en/guides/initial-setup.md) · [文件首頁](../README.md)

初始設定會建立 Site boundary、external Integration，以及 swallow 管理 Server 前
所需的 automation policy。

## 1. 建立 Site

在 **Infrastructure → Sites** 建立簡短的 operator-facing name 與 optional
description。Site 刻意不是 rack hierarchy；inventory 出現後請用 Zone 與 Pool
組織 Server。

## 2. 註冊 MAAS

建立包含下列資料的 Integration：

- kind 為 `provisioner`。
- provider kind 為 `maas`。
- 指向 MAAS API base 的 endpoint。
- MAAS API credential。
- Target Site。

Credential 會使用 API credential key 加密，而且永遠不會回傳。建立後檢查
`hasCredential` 與 integration sync status。Reconciliation 成功後，MAAS machines
會投影成 Servers。

一個 Site 只有一個 provisioner boundary。若 MAAS 跨越彼此獨立管理的地點，應拆成
不同 Site integration。

## 3. 註冊 monitoring

建立 `metrics` / `prometheus` Integration，設定 Prometheus endpoint，以及
contract 要求的 Alertmanager／Grafana settings。swallow 只查詢 fixed metric names
與 current alerts，不提供任意 PromQL passthrough，也不儲存 monitoring data。

Prometheus 應使用 swallow HTTP service-discovery endpoint，使 scraped series 帶有
穩定的 `server_id`、`site` 與 Platform labels。

## 4. 設定 Site automation

Site automation 定義：

- SSH port，以及選填的退回用 SSH user（會先使用每個 OS image 的 default user）。
- Mandatory known-host entries。
- Write-only credentials：選填、可覆寫安裝層級 deployment key 的私鑰，以及 become password。
- Allowlisted manifest playbook mappings。

Deployment key 會在安裝時（`swallowctl install`）產生，並由 API 自動註冊到 MAAS，因此大多數 Site 不需要自己的私鑰。
見 [SSH key 與 image 登入帳號](ssh-keys.md)。

只有 shipped manifest 內的 playbook 可以執行。Automation 不是 external
Integration，而是 Site-scoped swallow capability。

## 5. 驗證 readiness

- Integration sync 有最近的 `lastSucceededAt`，且沒有未處理 error。
- Reconcile 後出現 Servers。
- Worker 與 Ansible executor healthy。
- Diagnostic Workflow 到達 terminal result。
- Exporter 存在後 Prometheus discovery 會回傳 targets。

精確 payload fields 請使用 active
[Sites and Integrations contract](../../../api-server/docs/development/api-contracts/api-server/sites-integrations.md)
與
[Site automation contract](../../../api-server/docs/development/api-contracts/api-server/site-automation.md)。
