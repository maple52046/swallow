# Third-party services

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

Third-party media 與 swallow core bundle 分開 version。

| Component | Supported production topology |
| --- | --- |
| Docker CE | Host prerequisite；缺少時 `swallowctl install` 會從 Docker Ubuntu apt repository 安裝，不用 convenience script |
| MongoDB 8 | 由 swallow Compose 或 native bundle 管理 |
| PostgreSQL | 單一 Compose instance：Temporal databases、Local Repository 的 `pulp` database 與共置 MAAS 的 `maasdb`，各自獨立 role |
| Ubuntu MAAS 3.6 | 與 swallow 共置在同一台 production VM，由 `swallowctl` 安裝與管理（region+rack snap）；禁止 `maas-test-db` |
| Ansible | 位於 execution environment 或 native offline venv，不是 service |
| Prometheus | 每個 Site 一套，隨 production installation 為它的 Site 部署（Compose service `prometheus`，digest 固定的 image），並註冊為該 Site 的 metrics Integration；它從 swallow 發現該 Site 的 exporter |
| prom-label-proxy | 每個 Site Prometheus 前面一個（Compose service `prom-label-proxy`，digest 固定的 image，只在 internal network），即 metrics Integration 的 `labelProxyUrl`；它把每個查詢限制在一個 Platform 的 `platform_id` |
| Pulp 3 | Site 的 Local Repository，每個 Site 一套，隨 production installation 部署（Compose services `pulp-init`、`pulp-api`、`pulp-content`、`pulp-worker` 與 `pulp-web`；digest 固定的 `pulp-minimal` 與 `pulp-web` image），使用共用的 PostgreSQL，並註冊為該 Site 的 `repository` Integration。Site 打開它之後，部署出去的 Server 從 `pulp-web` 的 port 取得每個套件、檔案、container image 與 Python 套件。以未修改的形式只經 HTTP API 使用；release 只以 digest 參照它 GPLv2-or-later 的 image，從不附帶 |

Installation 會從 GHCR 拉 swallow 自己的映像、從 Docker Hub 拉官方 MongoDB、PostgreSQL、
Temporal 與 Prometheus 映像以及 Pulp 專案的 `pulp-minimal` 與 `pulp-web` 映像、從 Quay 拉 prom-label-proxy
映像，並從 `images.maas.io` 同步官方 `ubuntu/noble` boot resource，因此需要能連到這四者。部署時需要連到
swallow 的模組宣告的來源、檔案、container registry（Docker Hub、Quay、`registry.k8s.io`）與 PyPI。Site 使用
Local Repository 時由 Pulp 連到它們與 Site 的來源（預設是 Ubuntu archive `archive.ubuntu.com`），部署出去的
Server 只從 Local Repository 取得 artifact，不連網際網路；否則由部署出去的 Server 自己連到它們與自己的 OS 套件來源。
DNS、DHCP/PXE/BMC network 與
target hardware 是 site-provided inputs，不由 swallow 管理。`offline-media-manifest.json` 描述 air-gap
site 需要 mirror 的 media；MAAS image 的離線安裝尚未自動化，Local Repository 也還不能離線匯入內容。

詳細資料請見各 component 子目錄；Local Repository 的設計見
[local-repository.md](../../docs/development/local-repository.md)。
