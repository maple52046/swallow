# Monitoring

[English](../../../docs/en/guides/monitoring.md) · [文件首頁](../README.md)

swallow 將 monitoring data 與穩定 resource identity 關聯，但不儲存 metrics／alerts，
也不提供任意 PromQL proxy。

> Dashboard 的 monitoring 畫面仍在開發中。Dashboard release build 會隱藏它們，health 顯示
> 「Not available in this release」，見[開發中的功能](dashboard.md#開發中的功能)。註冊 Integration、
> API 與 CLI 依本文運作。

## 設定 integration

註冊 Site-scoped Prometheus-compatible metrics Integration，設定 deployment 需要的
Alertmanager endpoint 與 Grafana URL。Integration sync state 會顯示 upstream
reachability 與 staleness。

## Service discovery 與 labels

Prometheus 使用：

```text
GET /api/v1/discovery/prometheus
```

Machine-to-machine access 請使用 configured machine bearer token。Target 會帶有
穩定 `server_id`、`site` 與 canonical Platform labels；不要用 hostname
或 address join 取代。

Exporter installation 會 per Server resolve，確保每個 fixed-port exporter 只有一個
owner。Locked Server 不會由 automatic exporter installation 管理。

## Metrics

Dashboard 只對 selected Servers 查詢 fixed named metric set。Unknown／missing data
保持 unknown，不代表 Server down。探索式 query 與 long-range analysis 仍由 Grafana
負責。

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
swallow discovery prometheus --machine-token "$SWALLOW_MACHINE_TOKEN"
```

請見 active [monitoring contracts](../../../api-server/docs/development/api-contracts/api-server/monitoring-alerts.md)
與 [metrics label decision](../../decisions/003-metrics-label-contract.md)。
