# Monitoring

[English](../../../docs/en/guides/monitoring.md) · [文件首頁](../README.md)

swallow 讓你不必離開 swallow 就能看到 Server 的用量。它整合 Site 的
Prometheus-compatible metrics backend，提供基礎監控——Server 的 health，以及 CPU、memory、
disk 與 network 的用量——對象可以是單台 Server、勾選的多台 Server、Pool、Zone 或 Platform。
swallow 不儲存 metrics／alerts，也不提供任意 PromQL proxy；它只執行一組固定的 named
query。Platform 完整的監控（包括 GPU 的 metrics）在它自己的
[Grafana](#platform-自己的-grafana)。對 swallow 本身而言 Grafana 是選用的：需要基本圖表以外的探索時再接上。

> Dashboard 的 alert 仍在開發中。Dashboard release build 會顯示 health、目前的 metrics 與用量圖表，
> 但隱藏 alert 列表與 acknowledge，Overview 的 Alerts 卡片顯示「Not available in this release」，
> 見[開發中的功能](dashboard.md#開發中的功能)。透過 API 與 CLI 使用 alert 依本文運作。

## 從佈署好的 Server 到圖表

1. **Site 的 Prometheus。** [Production installation](../installation.md) 會為它的 Site 執行一套
   Prometheus，並註冊為該 Site 的 metrics Integration（`site-prometheus`）。它從 swallow 自動發現
   該 Site 的 exporter，不需要手動列出主機；它不發佈到主機上，由 swallow 代為讀取。一個 Site 至多
   一個 metrics Integration。
2. **其他 Site。** 這一版只有 installation 的 Site 有 Prometheus。其他 Site 的圖表會說明尚未設定
   metrics backend——不會把它的 Server 回報成 down。
3. **照常佈署 Server。** 每次 OS 佈署都會在佈署的 Server 上安裝 Node Exporter（見
   [Exporters](#exporters)）；幾分鐘後，Server 的 **Monitoring** 分頁就會顯示用量歷史。swallow
   也會讓它自動化的其他已佈署 Server（例如保留原有 OS 納管的 Server）一直保有它，不需要手動安裝。

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
| node-exporter | 9100 | 每台由 swallow 自動化的已佈署 Server（CPU、memory、disk、network），安裝 OS 套件：Debian 與 Ubuntu 為 `prometheus-node-exporter`，RHEL 家族為 `node_exporter`（來自 EPEL） |
| AMD Device Metrics Exporter | 5001 | 有 AMD GPU 的 Server：單台以 Managed Software 安裝、Slurm Platform 的 GPU compute node，或 Kubernetes Platform 開啟 Device Metrics Exporter 時由 AMD GPU Operator 裝在 GPU 節點上；三者的指標名稱相同 |
| Omnistat | 8001 | 作業監控選 Omnistat 的 Slurm Platform 的 compute node |
| [rdc-exporter](https://github.com/maple52046/rdc-exporter) | 5000 | swallow 不在主機上安裝，也不收集：Site 的 Prometheus 只收集 swallow 安裝的 exporter |

每台 Server 只有一個 Node Exporter，不論它屬於哪個 Platform：每次 OS 佈署都會在佈署的 Server
上安裝它，uninstall Platform 會保留它（保留的 Server 若沒有則補裝）。

### Node Exporter 會一直保持安裝

swallow 讓每個啟用 automation 的 Site 中，每台已佈署、未 lock 的 Server 都保有 Node Exporter，包括不是由
swallow 佈署的 Server。worker 大約每十分鐘檢查每台 Server 一次，Server 或它的 Node Exporter 紀錄改變時則
立即檢查：

- Server 沒有 Node Exporter，或它被 uninstall 時，swallow 安裝它。
- 安裝失敗時，swallow 在 15 分鐘、1 小時、6 小時後再試，之後每天一次，所以 OS 沒有這個套件的 Server 不會
  陷入安裝迴圈。
- Site 的 Prometheus 連續 10 分鐘都收集不到它（例如套件被移除或 service 停止）時，swallow 再安裝一次。
  provisioner 回報關機的 Server 不受影響。

這些安裝是一般的 **Install Node Exporter** Workflow，requestedBy 為 `swallow:node-exporter-controller`。
其中一個失敗時，swallow 會以「The Node Exporter install failed; swallow retries it later.」為原因取消它，讓
Server 可以做其他工作；它從不取消別人啟動的 Workflow，有其他工作佔用 Server 時則等待。swallow 從不自行移除
Node Exporter。locked Server 維持原狀：swallow 不在上面安裝也不移除任何東西。要讓某台 Server 不裝 Node
Exporter，請 lock 它；在 swallow 會保持安裝的 Server 上 uninstall，只會維持到下一次檢查。

安裝進行中或失敗之後，這台 Server 不會被收集，所以它的 health 顯示為 Unknown，而不是 Down。

## Server health

每台 Server 的 health 都是四個值之一，每次查看時依 swallow 自己的監控政策算出。Dashboard 顯示為 **Up**、
**Down**、**Unknown** 與 **N/A**；API 的值是 `up`、`down`、`unknown` 與 `not_applicable`。

| Health | 意義 |
| --- | --- |
| **Up** | swallow 讓 Site 的 Prometheus 收集這台 Server 的 Node Exporter，而且最近一次收集成功。 |
| **Down** | 最近一次收集這台 Server 的 Node Exporter 失敗：主機、它的網路或 exporter 沒有回應。這是故障。 |
| **Unknown** | swallow 預期監控這台 Server，但沒有答案：Node Exporter 安裝進行中或失敗、imported Server 還沒裝上，或 Site 的 Prometheus 沒有它的資料、讀取失敗。這是警示，不是故障。 |
| **N/A** | swallow 不監控這台 Server，例如它沒有佈署（ready、releasing）、被 lock、absent，或所屬 Site 的 automation 關閉。這不代表機器有任何問題。 |

- 只有 Node Exporter 決定 health，GPU exporter 不影響。
- swallow release 一台 Server 後，它的 health 立刻變成 N/A，即使 Site 的 Prometheus 還會保留它最後的資料一段時間。
- swallow 已經在收集的 Server 被 lock 後，仍顯示 Up 或 Down。
- Site 沒有 Prometheus 時，受監控的 Server 顯示 Unknown，不會因此顯示為 Down。

## 用量圖表

- **Server detail → Monitoring** 顯示最新數值與該 Server 的用量歷史：CPU、memory、root filesystem
  與 network。
- **Monitoring → Usage charts** 依 scope 繪圖：在 Server metrics 表格勾選 Server，或選擇
  Pool、Zone 或 Platform。Scope 與時間範圍都在 URL 中，分享連結即可重現同一畫面。

時間範圍可選 1 小時、6 小時、24 小時或 7 天；解析度由 swallow 決定（每張圖約 180 點）。
多台 Server 時每張圖是一條合併的線：使用率與佔用率取平均，network throughput 加總。
Pool、Zone 或 Platform 以其**目前**成員繪圖。

swallow 的圖表對每台 Server 都一樣，不論有沒有 GPU；GPU 的 metrics 由 Platform 自己的 Grafana
顯示，不在 swallow 裡。

Backend 沒有該 metric 的資料時，圖表顯示「No data」——例如剛佈署好的 Server 的 Node Exporter
還沒被收集。線條中斷代表該 Server 當時沒有回報。兩者都不會畫成 0。

## Platform 自己的 Grafana

每個 Platform 都有一個它自己的唯讀 Grafana，除非部署時關閉（workspace Features step 的 **Platform Grafana** 開關，
或在 Platform 的 settings 寫 `monitoringDashboards: false`）。CPU-only 的 Platform 也一樣。它和 swallow 讀同一套 Site 的
Prometheus，經過 swallow 讀取，只看得到這個 Platform 自己的 Server。它一律有 **Servers** dashboard（每台 Server 的
CPU、memory、root filesystem、network 與 load），再加上 Platform 的 GPU 或作業監控的 dashboards。Platform 頁面以
**Platform Grafana** 連到它，直接開啟主要的 dashboard：Platform 有 Device Metrics Exporter 或 Omnistat 監控時是
Device Metrics Exporter 的 **Overview** 或 Omnistat 的 **Global - RMS**，否則是 **Servers**。管理網路上的人不必登入
就能檢視。每次部署或重新部署都會給它新的存取憑證，從舊部署複製出來的憑證會失效。

Platform workspace 的 **Monitoring** route 集中這些入口，但不內嵌 dashboard。它連到 Swallow
usage charts、Platform Grafana、其他 Monitoring Links，以及 Omnistat 或 GPU 監控元件記錄的
安裝狀態與 Workflow。Active Platform 的 workspace hero 也提供 **Grafana**，開啟 Platform Grafana。

- **Slurm**：Grafana 在 primary controller 上，port 3000。預設以 container 執行，因此部署會先在 primary controller
  安裝 container engine：部署在 **Container runtime** 要求的那一個，否則是 Docker CE；該 Server 已有 container engine
  時沿用它。也可選擇 native 安裝方法，改由 Grafana 的 apt／yum repository 安裝。
- **Kubernetes**：Grafana 在叢集內，以每個 workload 節點的 port 30300 開放，連結開啟第一個節點上的 Grafana。它不保存自己的資料，
  Pod 換節點也不會遺失任何東西。AMD GPU Operator 執行它的 Device Metrics Exporter 時，Grafana 會加上 exporter 的 GPU
  dashboards。uninstall Platform 時隨叢集一起移除。

之後才開啟 Grafana 會重新部署 Platform；部署後關閉目前還不支援。在這之前部署的 Platform，Grafana 是關閉的：開啟它，
Platform 就會帶著 Grafana 重新部署。在那之前，Slurm Platform 保留它自己的 Prometheus 與原本的 Grafana 連結，Kubernetes
Platform 則沒有 Grafana。仍開啟 Grafana 首頁的 **Platform Grafana** 連結，會在 Platform 下一次部署時改為開啟主要的
dashboard。

## Slurm 的 GPU 與作業監控

在 Slurm workspace 的 Features step 選擇監控 exporter：

- **Omnistat**（預設建議）在每台 compute node 收集主機與 Slurm 作業的 metrics，有 AMD GPU 時也收集 GPU 的
  metrics，所以 CPU-only 的 Platform 也適用。每台 compute node 一次只跑一個作業。compute node 上 Omnistat 的作業報告工具
  `omnistat-query` 經 Platform 的 Grafana 讀取 metrics，要使用它就保留 Grafana（報告需要 GPU 使用率，SR-IOV virtual
  function 不提供，這類 GPU 請改看 job dashboard）。
  - **GPU hardware counters (VALUBusy)** 在 Omnistat 底下，預設勾選：另外收集每張 GPU 的向量 ALU 有多忙，Grafana 的
    job dashboard 會以 hardware counters 顯示。每台 compute node 只在做得到時收集：需要 AMD GPU、swallow 提供 counter
    extension 的 ROCm 版本（ROCm 10.0 與 10.1），以及在它的 GPU 上能成功取樣一次。做不到的節點不收集，其他 metrics
    照常，部署也不會因此失敗。Platform 頁面列出每台 compute node 有沒有收集，或為什麼沒有。收集時，該節點的 exporter
    帶有 `CAP_PERFMON` capability，並在每張 GPU 上保持一個 profiling session（每張約 160 MB 的 GPU 記憶體）；不想要時
    取消勾選。部署後還不能更改。
- **AMD Device Metrics Exporter** 收集每張 GPU 的 metrics，並標上該 GPU 上的作業，作業可繼續共用節點。需要至少一台有
  AMD GPU 的 compute node。
- **None** 兩者都不安裝。

Site 的 Prometheus 像收集其他 exporter 一樣收集選定的 exporter；Platform 頁面顯示它的狀態。在部署檔中以
`omnistat: { enabled: true }` 或 `deviceMetricsExporter: { enabled: true }` 選擇其一；兩者都省略時都不安裝。部署檔以
`omnistat: { enabled: true, hardwareCounters: true }` 要求 hardware counters；和部署精靈不同，省略時不收集。

## 其他監控介面

屬於某個 Platform 的其他監控介面也可以連結：

- 把它登記為 Platform 的 **Monitoring Link**。Platform 頁面會顯示它，旁邊並有前往
  swallow 內該 Platform 用量圖表的捷徑。
- 保留 swallow 的 exporters。Server 圖表在 Platform 建立前、存在期間與移除後都持續可用，
  且所有 Platform 類型都一樣。
- 不要讓 runtime 自己的 collector 綁定 9100、5000、5001 或 8001，這些是 swallow 的 exporters 使用的 port；也不要讓
  Kubernetes 的 Service 使用 node port 30300，那是 Platform Grafana 的 port。

Monitoring Link 透過 Platform update API 設定。使用 CLI：

```yaml
# patch.yaml
monitoringLinks:
  - name: Platform Grafana
    url: http://10.0.0.11:3000/d/edqnz7pce7togc-rms/global-rms
  - name: Runbook
    url: https://wiki.example.internal/research-slurm
```

```bash
swallow platforms update plat1 -f patch.yaml
```

空清單（`monitoringLinks: []`）會移除所有連結。swallow 從不連線 Monitoring Link；它只是
給使用者的捷徑。swallow 自己只在安裝 Platform 的 Grafana 時設定 `Platform Grafana` 這個連結。

## Alert 與 acknowledgement

Alert 會從 Alertmanager live read，並關聯到 swallow resource。Acknowledgement
會建立 Alertmanager silence；swallow 不在 local alert copy 增加 acknowledged field。
Dashboard 的 alert 列表與 acknowledge 仍在開發中，release build 會隱藏；CLI 與 API 在每個
release 都提供。

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
