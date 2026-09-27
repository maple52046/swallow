# Per-Site Prometheus

[English](README.md) · [Third-party overview](../README.zh-TW.md)

每個 Site 建立獨立 Compose project 或 native systemd installation。替換 swallow
URL，建立 permission `0700` 的 `secrets/`，並安裝 `secrets/machine-token`
與 issuing CA certificate `secrets/swallow-ca.crt`。Secret files 可由 non-root
container read，directory traversal 保持 root-only。

`PROMETHEUS_IMAGE` 使用 release manifest exact OCI digest。Default retention
是 30 days。Exporter installation 由 versioned swallow playbook 與 exporter-owner
policy 控制。未來 central TSDB 可使用 `remote_write`，不更改 discovery labels。

Prometheus 應使用 swallow HTTP service discovery，以穩定 `server_id`、`site`
與 canonical Platform labels scrape targets。
