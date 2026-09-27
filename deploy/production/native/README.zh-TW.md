# Native Ubuntu 24.04 amd64 installation preview

[English](README.md) · [Installation 選擇](../../../docs/zh-TW/installation.md)

Release workflow 會將此 template 組成 self-contained native bundle，包含
`bin/swallow-api`、Dashboard assets、automation、offline Python wheelhouse、
systemd／Nginx files，以及 separately checksummed MongoDB/Nginx packages。

將 checksummed MongoDB 與 native runtime media 合併為 `third-party/mongodb/`
與 `third-party/native-runtime/`。兩者都必須有 `SHA256SUMS` 與完整
`packages/` dependency closure；installation 使用 `apt-get --no-download`。

```bash
sudo ./prepare-secrets.sh
sudo install -m 0600 customer.crt /etc/swallow/tls/tls.crt
sudo install -m 0600 customer.key /etc/swallow/tls/tls.key
sudo ./swallowctl preflight
sudo ./swallowctl install
sudo ./swallowctl doctor
```

## 重要限制：Temporal native packaging 尚未完成

Temporal 是 swallow 唯一 execution engine，不存在 embedded fallback。完整 native
installation 必須同時執行 Temporal Server、PostgreSQL、`swallow-api worker`
與 `swallow-api ansible-executor`。

在 native Temporal／PostgreSQL systemd packaging 完成前，本路徑是 incomplete
preview。沒有完整 topology 時任何 Workflow 都無法執行；請使用 parent directory
的 Compose topology 驗證完整產品。

`upgrade` 預設拒絕 active Workflow 並先 backup；`uninstall` 保留 data，
只有 `--purge-data` 刪除。Installer 初始化 authenticated localhost-only MongoDB，
執行 explicit migration，並設定 backup retention。
