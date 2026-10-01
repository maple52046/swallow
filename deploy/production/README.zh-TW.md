# Production installation（單一 VM）

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

此目錄是單一 Ubuntu 24.04 amd64 VM 的 production installation：以 Docker Compose 執行
swallow，並在同一台主機上共置 MAAS 3.6 region+rack。這裡的「production」指的是
installation contract（digest-pinned images、產生的 secrets、lifecycle tooling），不是
多機拓樸。swallow 仍在積極開發，請用 release 或 candidate manifest 安裝。

## 安裝

```bash
sudo ./swallowctl install --profile production --release-manifest ../release-manifest.json
```

`install` 可重複執行，依序會：

1. 安裝缺少的 host packages（`curl`、`jq`、`openssl`、`snapd`），並從 Docker apt
   repository 安裝 Docker Engine 與 Compose plugin；
2. 把 manifest 的 version 與 exact image digests 寫入 `.env`（保留 `.env` 其他設定），
   並用 `prepare-secrets.sh` 在 `secrets/`（權限 0700）產生 secrets；
3. 只拉缺少的 images，啟動 MongoDB 與 PostgreSQL，執行 `swallow-api migrate`，建立
   Deployment Key（`swallow-api deployment-key ensure`），再啟動整個 stack；
4. 把附帶的 CLI 安裝到 `/usr/local/bin/swallow`；
5. 執行 [`local-maas.sh`](local-maas.sh)：在 Compose PostgreSQL 建立 `maas` role 與
   `maasdb`、安裝並初始化 MAAS snap、建立 MAAS `admin`、保存 API key，並等到官方
   `ubuntu/noble` amd64 image complete；
6. 透過已發佈的 API 執行 [`bootstrap.sh`](bootstrap.sh)：建立 Site `default`、MAAS
   provisioner Integration；Site 尚無 automation 設定時寫入預設值（啟用、Deployment Key、
   無 playbook mapping）；再等待 Integration 同步與 catalog 出現 `ubuntu/noble`；
7. 執行 `doctor`，並印出 Dashboard URL 與密碼位置。

它不會建立 SSH Access Key：由 operator 自行在 Dashboard 加入。

## 拓樸

| Component | 位置 | 對外 |
| --- | --- | --- |
| Dashboard（Nginx） | Compose | HTTP `${SWALLOW_HTTP_PORT:-80}`，唯一 public listener |
| API、worker、Ansible executor | Compose | internal `control` 加上 `egress`（連 MAAS 與 Server） |
| MongoDB、Temporal | Compose | 只在 internal |
| PostgreSQL | Compose | Temporal databases 與 `maasdb`；MAAS 經 `127.0.0.1:${SWALLOW_POSTGRES_HOST_PORT:-5432}` 連入 |
| MAAS 3.6 region+rack | host 上的 snap | `http://<primary IPv4>:5240/MAAS`；swallow 經 `http://host.docker.internal:5240/MAAS` 連入 |
| Temporal UI | Compose profile `diagnostics` | `127.0.0.1:${SWALLOW_TEMPORAL_UI_PORT:-8233}`，預設不啟動 |

Installation 提供 plain HTTP。要開放到受信任的管理網路之外前，請在 port 80 前面加上
TLS terminator。

## 設定

第一次 `install` 前可在 `.env` 或 environment 設定：

| Variable | Default | 用途 |
| --- | --- | --- |
| `SWALLOW_HTTP_PORT` | `80` | Dashboard 與 API port |
| `SWALLOW_POSTGRES_HOST_PORT` | `5432` | MAAS 連 PostgreSQL 的 loopback port |
| `SWALLOW_MAAS_URL` | `http://<primary IPv4>:5240/MAAS` | 機器連 MAAS 的位址 |
| `SWALLOW_MAAS_IMAGE_TIMEOUT` | `3600` | 等待 `ubuntu/noble` 的秒數 |
| `SWALLOW_SITE_NAME` | `default` | Bootstrap 建立的 Site |
| `SWALLOW_SSH_KNOWN_HOSTS` | 每次執行時掃描 | Site automation 的 static known_hosts |

## 檢查

```bash
sudo ./swallowctl doctor
```

`doctor` 會檢查 Dashboard 與 API、Temporal health、worker 是否 poll
`swallow-operations` task queue、Ansible executor 是否執行且 `ansible-runner` 能跑附帶的
`diagnostic-ping` playbook、MAAS API 與 `ubuntu/noble` boot resource、admin 登入、
Deployment Key、Site automation、MAAS Integration 同步，以及 OS Image catalog 的
`ubuntu/noble`。它不會佈署任何東西。

## 維運

- `sudo ./swallowctl upgrade [--release-manifest PATH] [--force]` 會先 backup；有 active
  Workflow 時拒絕，除非 `--force`；完成 migrate 後重新執行 `doctor`。
- `sudo ./swallowctl backup` 把 MongoDB、job artifacts、credential key 與 MAAS database
  寫到 `backups/`。請每日排程；retention 是七份 daily 與四份 Sunday weekly。MAAS boot
  images 會從 `images.maas.io` 重新同步。
- `sudo ./swallowctl restore BACKUP_DIRECTORY` 驗證 checksum，還原 MongoDB、artifacts、
  credential key 與 MAAS database，再重建 API 與 Dashboard。MAAS 重啟期間的 SSH key 同步
  會顯示 `failed`，直到下一次 sync interval；`swallow ssh-keys sync`（或 **Sync to
  provisioners**）可立即重試。
- `sudo ./swallowctl uninstall` 停止 stack 與 MAAS，保留 volumes、secrets 與 backups；
  `--purge-data` 會一併移除 MAAS snap、volumes 與 backups。

`secrets/` 不得進 source control；請透過 lifecycle tool 備份。Air-gap host 請先
`docker load` release OCI archive：`install` 只會拉缺少的 images。MAAS image 的離線匯入
尚未自動化。

## Diagnostics

```bash
docker compose --profile diagnostics up -d temporal-ui   # 需要 SWALLOW_TEMPORAL_UI_IMAGE
docker compose logs -f api-server worker ansible-executor
sudo ./local-maas.sh check
```

[Native path](native/README.zh-TW.md) 仍是 incomplete preview，請使用此 Compose topology。
