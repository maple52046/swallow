# Third-party services

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

Third-party media 與 swallow core bundle 分開 version。

| Component | Supported production-style topology |
| --- | --- |
| Docker CE | Host prerequisite；使用 Docker Ubuntu signed apt repository 或 verified offline deb media，不用 convenience script |
| MongoDB 8 | 由 swallow Compose 或 native bundle 管理 |
| Ubuntu MAAS 3.6 | Dedicated Ubuntu 24.04 host/VM，使用 production PostgreSQL 與 region+rack controller |
| Ansible | 位於 execution environment 或 native offline venv，不是 service |
| Prometheus | 每個 Site 一個獨立 project，使用 persistent storage 與 swallow HTTP discovery |

Connected 與 air-gap procedure 都必須使用 `offline-media-manifest.json` 的 version
與 checksum。Site 斷網前需同步 MAAS boot resources。DNS、DHCP/PXE、BMC network、
CA certificate 與 target hardware 是 site-provided inputs，不由 swallow 管理。

詳細資料請見各 component 子目錄。
