# Per-Site Prometheus

[English](README.md) · [Third-party overview](../README.zh-TW.md)

每個 Site 建立獨立 Compose project 或 native systemd installation。替換 swallow
URL，建立 permission `0700` 的 `secrets/`，並安裝 `secrets/machine-token`
（即 installation 的 `secrets/machine-token`，permission `0444`）。Secret file 可由
non-root container read，directory traversal 保持 root-only。

範本以 plain HTTP 抓取 swallow，與 production installation 一致。若 swallow 前面有
TLS terminator，請把 URL 改為 `https`，並加上帶 CA 的 `tls_config`。

`PROMETHEUS_IMAGE` 使用 exact OCI digest。Default retention 是 30 days。Exporter
installation 由 versioned swallow playbook 與 exporter-owner policy 控制。未來 central
TSDB 可使用 `remote_write`，不更改 discovery labels。

Prometheus 應使用 swallow HTTP service discovery，以穩定 `server_id`、`site`
與 canonical Platform labels scrape targets。
