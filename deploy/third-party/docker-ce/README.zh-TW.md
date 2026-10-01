# Docker CE prerequisite

[English](README.md) · [Third-party overview](../README.zh-TW.md)

Docker CE 是 Compose installation 的 host prerequisite，不是 swallow service。
連網的 Ubuntu 24.04 host 若缺少 Docker Engine 與 Compose plugin，`swallowctl install`
會從 Docker signed apt repository 安裝，不使用 convenience script；已安裝 Docker 的
host 保留原本的安裝。

Disconnected host 需 mirror `../offline-media-manifest.json` 指定的 exact packages，
保留 repository metadata／signing key、驗證 `SHA256SUMS`，並在執行
`swallowctl install` 前從 local repository 安裝。

Official procedure: <https://docs.docker.com/engine/install/ubuntu/>
