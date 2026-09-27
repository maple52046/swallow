# Docker CE prerequisite

[English](README.md) · [Third-party overview](../README.zh-TW.md)

Docker CE 是 Compose installation 的 host prerequisite，不是 swallow service。
Connected Ubuntu 24.04 host 應從 Docker signed apt repository 安裝 Docker Engine
與 Compose plugin，不使用 convenience script。

Disconnected host 需 mirror `../offline-media-manifest.json` 指定的 exact packages，
保留 repository metadata／signing key、驗證 `SHA256SUMS`，再從 local repository
安裝。執行 `deploy/production/swallowctl preflight` 前先確認
`docker version` 與 `docker compose version`。

Official procedure: <https://docs.docker.com/engine/install/ubuntu/>
