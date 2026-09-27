# MongoDB 8 media

[English](README.md) · [Third-party overview](../README.zh-TW.md)

Compose path 使用 `release-manifest.json` 記錄的 exact MongoDB OCI digest。
以 `docker load` 載入 core OCI archive；production-style installation 不應
implicit pull。

Native path 使用 separately checksummed Ubuntu 24.04 amd64 packages，放在
`third-party/packages/`。Native installer 啟動 localhost-only MongoDB、建立
configured admin user、啟用 authorization，並在 migration 前驗證 authenticated URI。

Daily backup 包含 MongoDB、credential encryption key 與 runner artifacts。
Retention 是七份 daily、四份 weekly；restore drill 是 release acceptance gate。

Official references:

- <https://www.mongodb.com/docs/v8.0/tutorial/install-mongodb-on-ubuntu/>
- <https://www.mongodb.com/docs/manual/core/backups/>
