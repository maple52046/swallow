# Monitoring

[English](../../../docs/en/guides/monitoring.md) · [文件首頁](../README.md)

swallow 讓你不必離開 swallow 就能看到 Server 的用量。它整合 Site 的
Prometheus-compatible metrics backend，並自行繪製基本用量圖表——CPU、memory、disk、
network 與 GPU——對象可以是單台 Server、勾選的多台 Server、Pool、Zone 或 Platform。
swallow 不儲存 metrics／alerts，也不提供任意 PromQL proxy；它只執行一組固定的 named
query。Grafana 是選用的：需要基本圖表以外的探索時再接上。

> Dashboard 的 monitoring 畫面仍在開發中。Dashboard release build 會隱藏它們，health 顯示
> 「Not available in this release」，見[開發中的功能](dashboard.md#開發中的功能)。註冊 Integration、
> Monitoring Link、API 與 CLI 依本文運作。

## 從佈署好的 Server 到圖表

1. **Site 的 Prometheus。** [Production installation](../installation.md) 會為它的 Site 執行一套
   Prometheus，並註冊為該 Site 的 metrics Integration（`site-prometheus`）。它從 swallow 自動發現
   該 Site 的 exporter，不需要手動列出主機；它不發佈到主機上，由 swallow 代為讀取。一個 Site 至多
   一個 metrics Integration。
2. **其他 Site。** 這一版只有 installation 的 Site 有 Prometheus。其他 Site 的圖表會說明尚未設定
   metrics backend——不會把它的 Server 回報成 down。
3. **照常佈署 Server。** 每次 OS 佈署都會在佈署的 Server 上安裝 Node Exporter（見
   [Exporters](#exporters)）；幾分鐘後，Server 的 **Monitoring** 分頁就會顯示用量歷史。不是由
   swallow 佈署的 Server（例如保留原有 OS 納管的 Server），請以 Managed Software 安裝
   **Node Exporter**。

## 設定 integration

Installation 的 Site 已經有它的 metrics Integration。可選擇在上面設定 Alertmanager
endpoint（alerts 與 silences 使用）以及 Grafana URL（選用的「Explore in Grafana」連結）；重跑
installation 會保留它們，只把 endpoint 改回 Site 的 Prometheus。
Integration sync state 會顯示 upstream reachability 與 staleness。

沒有設定 Alertmanager 時，Overview 的 Alerts 卡片顯示「Not configured」，這是正常狀態，不是故障。
只有已設定的 Alertmanager 讀取失敗時，Overview 才會顯示「Alerts unavailable」警告。

## Service discovery 與 labels

Site 的 Prometheus 使用：

```text
GET /api/v1/discovery/prometheus/exporters?siteId=<site>
```

它只列出 swallow 確知已安裝在該 Site 的 exporter，所以沒有 exporter 的 Server 不會成為 down
的 target。Machine-to-machine access 請使用 configured machine bearer token。Target 會帶有
穩定的 `server_id`、`site`、`platform_id`（Server 屬於某個 Platform 時）與 `exporter` labels；
不要用 hostname 或 address join 取代。

## Exporters

swallow 使用固定 port，因此 Server 的 scrape target 不會因為 exporters 的安裝方式而改變：

| Exporter | Port | 安裝對象 |
| --- | --- | --- |
| node-exporter | 9100 | 每台由 swallow 佈署的 Server（CPU、memory、disk、network），安裝 OS 套件：Debian 與 Ubuntu 為 `prometheus-node-exporter`，RHEL 家族為 `node_exporter`（來自 EPEL） |
| [rdc-exporter](https://github.com/maple52046/rdc-exporter) | 5000 | swallow 不在主機上安裝，也不收集：Site 的 Prometheus 只收集 swallow 安裝的 exporter |

每台 Server 只有一個 Node Exporter，不論它屬於哪個 Platform：每次 OS 佈署都會在佈署的 Server
上安裝它，uninstall Platform 會保留它（保留的 Server 若沒有則補裝），不是由 swallow 佈署的 Server
可以用 Managed Software 安裝。locked Server 維持原狀：swallow 不在上面安裝也不移除任何東西。

## 用量圖表

- **Server detail → Monitoring** 顯示最新數值與該 Server 的用量歷史；GPU Server 另有 GPU 圖表。
- **Monitoring → Usage charts** 依 scope 繪圖：在 Server metrics 表格勾選 Server，或選擇
  Pool、Zone 或 Platform。Scope 與時間範圍都在 URL 中，分享連結即可重現同一畫面。

時間範圍可選 1 小時、6 小時、24 小時或 7 天；解析度由 swallow 決定（每張圖約 180 點）。
多台 Server 時每張圖是一條合併的線：使用率與佔用率取平均，network throughput 與 GPU power
加總，GPU temperature 取最熱的那台。Pool、Zone 或 Platform 以其**目前**成員繪圖。

Backend 沒有該 metric 的資料時，圖表顯示「No data」——例如沒有 GPU exporter 的 Server 的
GPU 圖表。線條中斷代表該 Server 當時沒有回報。兩者都不會畫成 0。

## 自帶監控的 Platform

有些 runtime 自帶完整監控方案——例如 Slurm Platform 上的
[Omnistat](https://github.com/ROCm/omnistat)，有自己的 Prometheus 與 Grafana。部署 Slurm Platform 時
可以讓 swallow 代為安裝 Omnistat（在 Slurm workspace 開啟 **Features → Omnistat GPU monitoring** Switch，或在部署檔
加上 `omnistat: { enabled: true }`）：primary controller 會裝 Prometheus（API 沒有登入機制，因此只在
該主機的 loopback 位址回應）與帶有 Omnistat job dashboards 的唯讀 Grafana，每台 compute node 裝
exporter 與 Omnistat 的作業報告工具 `omnistat-query`（透過該 Grafana 查詢 Prometheus；報告需要 GPU 使用率，
SR-IOV virtual function 不提供，這類 GPU 請改看 Grafana 的 job dashboard），Platform 頁面會顯示 Omnistat 狀態與
`Omnistat Grafana` 連結。Prometheus 與 Grafana 預設以 container 執行；也可選擇 native 安裝方法，改由
官方 release 與 apt／yum repository 安裝。swallow 不會 scrape 或查詢這類方案，而是：

- 把它的 UI 登記為 Platform 的 **Monitoring Link**。Platform 頁面會顯示它，旁邊並有前往
  swallow 內該 Platform 用量圖表的捷徑。
- 保留 swallow 的 exporters。Server 圖表在 Platform 建立前、存在期間與移除後都持續可用，
  且所有 Platform 類型都一樣。
- 不要讓 runtime 自己的 collector 綁定 9100 或 5000。Omnistat 的 collector 預設使用自己的
  port；請維持如此，避免與 swallow 的 exporters 衝突。

Monitoring Link 透過 Platform update API 設定。使用 CLI：

```yaml
# patch.yaml
monitoringLinks:
  - name: Omnistat Grafana
    url: https://omnistat.example.internal/grafana
```

```bash
swallow platforms update plat1 -f patch.yaml
```

空清單（`monitoringLinks: []`）會移除所有連結。swallow 從不連線 Monitoring Link；它只是
給使用者的捷徑。

## Alert 與 acknowledgement

Alert 會從 Alertmanager live read，並關聯到 swallow resource。Acknowledgement
會建立 Alertmanager silence；swallow 不在 local alert copy 增加 acknowledged field。

Silence 前檢查 matcher、duration、Site scope 與 affected Servers。遇到
provider-unavailable error 時先診斷 Integration，不要把它當 fleet health result。

## CLI

```bash
swallow monitoring alerts list --site-id site1
swallow monitoring metrics names
swallow monitoring metrics get --server server1
swallow monitoring metrics range --server server1 --window 6h
swallow monitoring metrics range --pool pool1 --window 24h
swallow discovery prometheus --machine-token "$SWALLOW_MACHINE_TOKEN"
```

請見 active [metrics contract](../../../api-server/docs/development/api-contracts/api-server/server-metrics.md)、
[alerts contract](../../../api-server/docs/development/api-contracts/api-server/monitoring-alerts.md)
與 [metrics label decision](../../decisions/003-metrics-label-contract.md)。
