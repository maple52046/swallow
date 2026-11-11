# 初始設定

[English](../../../docs/en/guides/initial-setup.md) · [文件首頁](../README.md)

初始設定會建立 Site boundary、external Integration，以及 swallow 管理 Server 前
所需的 automation policy。

[Production installation](../installation.md) 已經為 Site `default`、共置的 MAAS、它的
Prometheus 與它的 local repository（部署還不使用它，步驟 4 選擇怎麼使用）完成步驟 1 到 5。本指南可用來檢查該結果、新增其他
Site，或設定 development stack。

## 1. 建立 Site

在 **Infrastructure → Sites** 建立簡短的 user-facing name 與 optional
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

Production installation 會為它的 Site 執行一套 Prometheus，並註冊為該 Site 的
`metrics` / `prometheus` Integration（`site-prometheus`，endpoint `http://prometheus:9090`）；
有 Alertmanager／Grafana 時，把它們的 settings 加到這個 Integration。一個 Site 至多一個 metrics
Integration。這讓 swallow 能為已佈署的 Server 繪製用量圖表。swallow 只查詢 fixed metric
names、有上限的用量歷史與 current alerts，不提供任意 PromQL passthrough，也不儲存
monitoring data。見 [Monitoring](monitoring.md)。

Site 的 Prometheus 透過 swallow HTTP service-discovery endpoint 發現 swallow 安裝在該 Site 的
exporter，使 scraped series 帶有穩定的 `server_id`、`site`、`platform_id` 與 `exporter` labels。
這一版其他 Site 沒有 Prometheus。

## 4. 選擇部署從哪裡取得

這一步是選用的。部署安裝的套件、container image、Python 套件與檔案，都是 swallow 的模組以固定版本宣告的，
每個 Site 選擇它們從哪裡取得：

- **上游來源**（預設）：每台 Server 從模組宣告的來源取得每個 artifact。檔案以宣告的 checksum 驗證，套件庫
  以宣告的簽章金鑰驗證，Server 保留自己的 OS 套件來源。
- **Cache**：Site 的 local repository，也就是和 swallow 執行在一起的套件庫（Pulp，在同一個 installation 中），
  每個 artifact 只從上游取得一次，再提供給該 Site 的每台 Server。Server 從它取得它已經提供的部分，其餘從上游
  來源取得，所以部署不必等它同步，Server 也仍要連得到上游。
- **Offline**：Server 只從 local repository 取得每個 artifact，不連網際網路。它提供部署可能取得的每一項之後，
  部署才會被接受。local repository 本身在套件第一次被要求時仍向上游取得，所以填入內容的期間 swallow 的主機要能
  連網際網路。

兩種使用 local repository 的模式中，OS 套件索引都以 Site 自己的金鑰簽章，檔案以宣告的 checksum 驗證。

Production installation 會把 local repository 註冊為 Site 的 `repository` / `pulp` Integration，把 Ubuntu
archive 加為 Site 的 `ubuntu` 來源，並讓 Site 維持 **Upstream sources**。一個 Site 至多一個。要使用它，在
**Infrastructure → Local repository** 把 **Artifact source for deployments** 選為 **Cache** 或 **Offline**
（API 的 `mode`）：local repository 開始取得內容。cache 模式下部署立即被接受；offline 模式下 ready 之後部署才會
被接受，在那之前部署會被拒絕，訊息會指出要等待或修正什麼。改回 **Upstream sources** 之後，之後的部署再從上游來源
取得，local repository 已有的內容會保留。已佈署的 Server 在下一次有部署在它上面執行時，跟著 Site 的選擇改變。

除了 `ubuntu` 來源，local repository 的其他內容由 swallow 自己的模組宣告，例如 Docker CE 的套件庫、k0s
binary，以及 Platform 執行的 container image，所以只有 swallow 沒有宣告的東西才需要自己加來源。Development
stack 要自己註冊：endpoint 是 swallow 連到 Pulp API 的位址，`contentOrigin` setting 是 Server 連到它的位址，
credential 是 Pulp admin 的密碼。

**Infrastructure → Local repository** 顯示來源與同步狀態，並可依格式瀏覽內容，標出宣告每一項的模組。
API 請見 [local repository contract](../../../api-server/docs/development/api-contracts/api-server/local-repository.md)。

## 5. 設定 Site automation

Site automation 定義：

- SSH port，以及選填的退回用 SSH user（會先使用每個 OS image 的 default user）。
- Mandatory known-host entries。
- 選填的 write-only become password。
- Allowlisted manifest playbook mappings。

Automation 一律以安裝層級的 deployment key 登入；它在安裝時（`swallowctl install`）產生，並由 API
自動註冊到 MAAS，Site 沒有自己的私鑰。見 [SSH key 與 image 登入帳號](ssh-keys.md)。

只有 shipped manifest 內的 playbook 可以執行。Automation 不是 external
Integration，而是 Site-scoped swallow capability。

## 6. 驗證 readiness

- Integration sync 有最近的 `lastSucceededAt`，且沒有未處理 error。
- Site 使用 local repository 時，**Infrastructure → Local repository** 顯示 deployment 會被接受：每個來源、
  file 與宣告的 image 都是 **Ready**。
- Reconcile 後出現 Servers；以 **Servers → Add servers** 新增更多（見
  [新增 Server](servers-and-infrastructure.md#新增-server)）。
- Worker 與 Ansible executor healthy。
- Diagnostic Workflow 到達 terminal result。
- Exporter 存在後 Prometheus discovery 會回傳 targets。

精確 payload fields 請使用 active
[Sites and Integrations contract](../../../api-server/docs/development/api-contracts/api-server/sites-integrations.md)
與
[Site automation contract](../../../api-server/docs/development/api-contracts/api-server/site-automation.md)。
