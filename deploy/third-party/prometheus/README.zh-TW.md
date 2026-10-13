# Per-Site Prometheus

[English](README.md) · [Third-party overview](../README.zh-TW.md)

swallow 的用量圖表讀取的就是這個 Prometheus：它執行並註冊為該 Site 的 `metrics`
Integration 後，每台已佈署 Server 的 Monitoring 分頁就會顯示 CPU、memory、disk、network
與 GPU 的歷史。Grafana 是選用的。

每個 Site 建立獨立 Compose project 或 native systemd installation。替換 swallow
URL，建立 permission `0700` 的 `secrets/`，並安裝 `secrets/machine-token`
（即 installation 的 `secrets/machine-token`，permission `0444`）。Secret file 可由
non-root container read，directory traversal 保持 root-only。

範本以 plain HTTP 抓取 swallow，與 production installation 一致。若 swallow 前面有
TLS terminator，請把 URL 改為 `https`，並加上帶 CA 的 `tls_config`。

`PROMETHEUS_IMAGE` 使用 exact OCI digest。Default retention 是 30 days，這也限制了 swallow
能繪製的最長用量歷史（最大 window 為 7 天）。`node_exporter`（port 9100）以及 `amd-gpu`
Server 上的 RDC exporter（port 5000）由 versioned swallow playbook 依 exporter-owner policy
安裝；範本中的兩個 scrape job 對應這兩個 port。未來 central TSDB 可使用 `remote_write`，
不更改 discovery labels。

Prometheus 應使用 swallow HTTP service discovery，以穩定 `server_id`、`site`
與 canonical Platform labels scrape targets。
