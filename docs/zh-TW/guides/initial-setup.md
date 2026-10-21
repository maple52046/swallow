# 初始設定

[English](../../../docs/en/guides/initial-setup.md) · [文件首頁](../README.md)

初始設定會建立 Site boundary、external Integration，以及 swallow 管理 Server 前
所需的 automation policy。

[Production installation](../installation.md) 已經為 Site `default` 與共置的 MAAS 完成
步驟 1、2、4。本指南可用來檢查該結果、新增其他 Site，或設定 development stack。

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

在 Dashboard 中，**Infrastructure → Integrations → Create integration** 完成註冊後
會開啟該 Integration 的設定頁。每個 Integration 的設定頁（點列表中的名稱或
**Edit**）都有 **General**（名稱、endpoint、啟用）、**Connection**（timeout、TLS、
自動 inspection 與 provider 專屬設定）與 **Credential**（設定或更換 key）分頁。
MAAS provisioner 另有 **HTTP proxy** 與 **Subnet DNS**，會直接套用到 MAAS。每個
分頁都有自己的 URL，以及自己的 Save 或 Apply。

請依 value 的 owner 使用各分頁：

- **General** 與 **Connection** 更新 swallow 保存的 Integration。停用後會停止
  provider read；connection settings 控制 swallow 如何連線。**Inspect newly enrolled
  Servers automatically** 決定新發現且從未檢視的 Server 是否自動開始 inspection Workflow。
- **Credential** 會替換 write-only MAAS API key；保存其他分頁時不會重送或清除它。
- **HTTP proxy** 直接讀寫 live MAAS，不屬於 Integration settings。**None** 不為
  deployed machine 提供 proxy；**Built-in** 直接使用 MAAS proxy；**External** 讓
  MAAS 與 deployed machine 使用指定 proxy；**Peer** 則先使用 MAAS proxy，再由它轉送到
  指定的 upstream proxy。Proxy URL 不可帶 credential。變更只影響之後的 deployment，
  不會改變已佈署 machine。
- **Subnet DNS** 即時讀取 MAAS subnet，並逐一將最多八個 DNS server address 套用到
  subnet。Automatic 與 Static deployment 使用此清單（空清單則使用 MAAS DNS）；
  DHCP deployment 從自己的 DHCP server 取得 DNS。變更也只影響之後的 deployment。

後兩個分頁是 live provider setting；MAAS 無法連線、Integration 沒有 credential，
或 MAAS account 不是 administrator 時，讀寫都可能失敗。

一個 Site 只有一個 provisioner boundary。若 MAAS 跨越彼此獨立管理的地點，應拆成
不同 Site integration。

## 3. 註冊 monitoring

先以 `deploy/third-party/prometheus` 執行 per-Site Prometheus，再建立
`metrics` / `prometheus` Integration，設定其 endpoint，以及選用的 Alertmanager／Grafana
settings。這讓 swallow 能為已佈署的 Server 繪製用量圖表。swallow 只查詢 fixed metric
names、有上限的用量歷史與 current alerts，不提供任意 PromQL passthrough，也不儲存
monitoring data。見 [Monitoring](monitoring.md)。

Prometheus 應使用 swallow HTTP service-discovery endpoint，使 scraped series 帶有
穩定的 `server_id`、`site` 與 Platform labels。

## 4. 設定 Site automation

Site automation 定義：

- SSH port，以及選填的退回用 SSH user（會先使用每個 OS image 的 default user）。
- Mandatory known-host entries。
- 選填的 write-only become password。
- Allowlisted manifest playbook mappings。

Automation 一律以安裝層級的 deployment key 登入；它在安裝時（`swallowctl install`）產生，並由 API
自動註冊到 MAAS，Site 沒有自己的私鑰。見 [SSH key 與 image 登入帳號](ssh-keys.md)。

只有 shipped manifest 內的 playbook 可以執行。Automation 不是 external
Integration，而是 Site-scoped swallow capability。

## 5. 驗證 readiness

- Integration sync 有最近的 `lastSucceededAt`，且沒有未處理 error。
- Reconcile 後出現 Servers；以 **Servers → Add servers** 新增更多（見
  [新增 Server](servers-and-infrastructure.md#新增-server)）。
- Worker 與 Ansible executor healthy。
- Diagnostic Workflow 到達 terminal result。
- Exporter 存在後 Prometheus discovery 會回傳 targets。

精確 payload fields 請使用 active
[Sites and Integrations contract](../../../api-server/docs/development/api-contracts/api-server/sites-integrations.md)
與
[Site automation contract](../../../api-server/docs/development/api-contracts/api-server/site-automation.md)。
