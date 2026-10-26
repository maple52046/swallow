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

1. **為 Site 執行一個 Prometheus。** 使用
   [`deploy/third-party/prometheus`](../../../deploy/third-party/prometheus/README.zh-TW.md)
   的 per-Site 範本。它從 swallow 自動發現 target，不需要手動列出主機。
2. **把它註冊為該 Site 的 metrics Integration**（見下方）。在 metrics Integration 存在之前，
   圖表會說明尚未設定 metrics backend——不會把你的 Server 回報成 down。
3. **照常佈署 Server。** Server 進入 `deployed` 後，swallow 會自動安裝 exporters（見
   [Exporters](#exporters)）；幾分鐘後，Server 的 **Monitoring** 分頁就會顯示用量歷史。

## 設定 integration

註冊 Site-scoped Prometheus-compatible metrics Integration。可選擇設定 Alertmanager
endpoint（alerts 與 silences 使用）以及 Grafana URL（選用的「Explore in Grafana」連結）。
Integration sync state 會顯示 upstream reachability 與 staleness。

沒有設定 Alertmanager 時，Overview 的 Alerts 卡片顯示「Not configured」，這是正常狀態，不是故障。
只有已設定的 Alertmanager 讀取失敗時，Overview 才會顯示「Alerts unavailable」警告。

## Service discovery 與 labels

Prometheus 使用：

```text
GET /api/v1/discovery/prometheus
```

Machine-to-machine access 請使用 configured machine bearer token。Target 會帶有
穩定 `server_id`、`site` 與 canonical Platform labels；不要用 hostname
或 address join 取代。

## Exporters

swallow 使用固定 port，因此 Server 的 scrape target 不會因為由誰安裝 exporters 而改變：

| Exporter | Port | 安裝對象 |
| --- | --- | --- |
| node-exporter | 9100 | 每台受管 Server（CPU、memory、disk、network） |
| [rdc-exporter](https://github.com/maple52046/rdc-exporter) | 5000 | AMD GPU Server，以 tag `amd-gpu` 辨識 |

每台 Server 的 exporters 只會有一個 owner 負責安裝：

- 不屬於任何 Platform 的 Server 由 swallow 負責，在 OS 佈署後安裝 exporters；
- Kubernetes Platform 的成員依該 Platform 的 `exporterOwner`（`ansible`——由 swallow
  安裝——或 `k8s`——在相同 port 上以 DaemonSet 執行）；
- locked Server 為 unmanaged：swallow 不安裝也不移除任何東西。

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
加上 `omnistat: { enabled: true }`）：primary controller 會裝 Prometheus 與帶有 Omnistat job
dashboards 的唯讀 Grafana，每台 compute node 裝 exporter，Platform 頁面會顯示 Omnistat 狀態與
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
給操作者的捷徑。

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
