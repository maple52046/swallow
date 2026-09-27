# swallow installation assets

[English](README.md) · [Installation 指南](../docs/zh-TW/installation.md)

本目錄包含安裝與執行 swallow 所需的 environment orchestration、release tooling
與 third-party media contracts。

`deploy/` 是為 repository 相容性保留的內部 path，不是產品術語。安裝、啟動、
升級、restore 或 uninstall swallow 稱為 **Installation**；**Deployment** 專指
要求 MAAS 等 provisioner 在 managed Server 安裝 OS。

- `dev/` — source-based development golden path。
- `testing/` — digest-pinned candidate installation 與 isolated test data。
- `production/` — Compose installation lifecycle 與 native preview。
- `release/` — candidate assembly 與 SemVer promotion tooling。
- `third-party/` — 分開 version 的 prerequisite 與 offline media contract。

swallow 正在積極開發，尚無 stable SemVer release。Native path 在 Temporal／
PostgreSQL systemd packaging 完成前仍是 incomplete preview。
