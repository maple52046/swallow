# Third-party services

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

Third-party media 與 swallow core bundle 分開 version。

| Component | Supported production topology |
| --- | --- |
| Docker CE | Host prerequisite；缺少時 `swallowctl install` 會從 Docker Ubuntu apt repository 安裝，不用 convenience script |
| MongoDB 8 | 由 swallow Compose 或 native bundle 管理 |
| PostgreSQL | 單一 Compose instance：Temporal databases 加上共置 MAAS 的 `maasdb`，各自獨立 role |
| Ubuntu MAAS 3.6 | 與 swallow 共置在同一台 production VM，由 `swallowctl` 安裝與管理（region+rack snap）；禁止 `maas-test-db` |
| Ansible | 位於 execution environment 或 native offline venv，不是 service |
| Prometheus | 每個 Site 一個獨立 project，使用 persistent storage 與 swallow HTTP discovery |

Installation 會從 GHCR 拉映像，並從 `images.maas.io` 同步官方 `ubuntu/noble` boot resource，
因此需要能連到兩者。DNS、DHCP/PXE/BMC network 與 target hardware 是 site-provided inputs，
不由 swallow 管理。`offline-media-manifest.json` 描述 air-gap site 需要 mirror 的 media；
MAAS image 的離線安裝尚未自動化。

詳細資料請見各 component 子目錄。
