# Production installation（單一 VM）

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

此目錄是單一 Ubuntu 24.04 amd64 VM 的 production installation：以 Docker Compose 執行
swallow，並在同一台主機上共置 MAAS 3.6 region+rack。這裡的「production」指的是
installation contract（digest-pinned images、產生的 secrets、lifecycle tooling），不是
多機拓樸。swallow 仍在積極開發，請用 release 或 candidate manifest 安裝。

## 安裝

Release 附的 `install.sh` 會下載並核對 bundle、解壓到 `/opt/swallow`，再執行
`swallowctl install`；該目錄已有較早 release 完整裝好時改執行 `swallowctl upgrade`
（[installation guide](../../docs/zh-TW/installation.md)）：

```bash
curl -fsSL https://github.com/maple52046/swallow/releases/download/v<version>/install.sh \
  | sudo bash -s -- --address <address>
```

想先檢查 bundle 內容時，自行從 release 下載 `swallow-compose-<version>.tar.zst` 與
`SHA256SUMS`，再執行：

```bash
sha256sum --ignore-missing -c SHA256SUMS
sudo mkdir -p /opt/swallow
sudo tar --zstd -xf swallow-compose-<version>.tar.zst -C /opt/swallow --no-same-owner
cd /opt/swallow/production
sudo SWALLOW_ADDRESS=<address> ./swallowctl install --release-manifest ../release-manifest.json
```

`install` 可重複執行，依序會：

1. 安裝缺少的 host packages（`curl`、`jq`、`openssl`、`snapd`），並從 Docker apt
   repository 安裝 Docker Engine 與 Compose plugin；
2. 把 manifest 的 version 與 exact image digests 寫入 `.env`，加入 environment 中有設定的
   下列設定（保留 `.env` 其他設定），並用 `prepare-secrets.sh` 在 `secrets/`（權限 0700）
   產生 secrets；
3. 只拉缺少的 images，啟動 MongoDB 與 PostgreSQL 並等兩者 healthy（MongoDB 是 single-node replica set
   `rs0`），在 PostgreSQL 為 Local Repository 建立 `pulp` role 與 database，執行 `swallow-api migrate`，建立
   Deployment Key（`swallow-api deployment-key ensure`），再啟動整個 stack（`pulp-init` 先遷移 Pulp）；
4. 把附帶的 CLI 安裝到 `/usr/local/bin/swallow`（API 也會在 `/downloads/swallow` 提供
   `bin/swallow`，給保留 OS 納管的主機下載）；
5. 執行 [`local-maas.sh`](local-maas.sh)：在 Compose PostgreSQL 建立 `maas` role 與
   `maasdb`、安裝並初始化 MAAS snap、建立 MAAS `admin`、保存 API key、為 MAAS rack 建立
   `virsh` power 用的 SSH key（第一次連線時接受新的 hypervisor host key）並把公鑰存到
   `secrets/maas-virsh-ssh.pub`，再等到官方 `ubuntu/noble` amd64 image complete；
6. 執行 [`local-ntp.sh`](local-ntp.sh) 的 `install` 與 `upstream`（[decision 065](../../docs/decisions/065-control-plane-ntp-and-post-provision-time-sync.md)（control plane ntp and post provision time sync））。provisioner 是
   共置的 MAAS，所以不需要安裝：MAAS 自己的 chronyd 就是此安裝的 Control-Plane NTP Server
   （`SWALLOW_ADDRESS` 的 UDP 123）。`upstream` 會把 `SWALLOW_NTP_UPSTREAM`（沒有設定時為 default
   `ntp.ubuntu.com`）寫入 MAAS 的 `ntp_servers`，並維持 `ntp_external_only` 關閉。MAAS 的 chrony
   沒有 `makestep`，所以主機時鐘與上游差超過一秒時，script 會讓 chronyd 一次 step 到位，而不是以
   +8.3% 速度 slew 好幾分鐘；
7. 透過已發佈的 API 執行 [`bootstrap.sh`](bootstrap.sh)：建立 Site `default`、MAAS
   provisioner Integration，並把 rack 的 `virsh` 公鑰設為它的 `virshSshPublicKey`（swallow
   納管某台 hypervisor 上的 libvirt 虛擬機時，會把它授權到那台 hypervisor）；把 Site 的 Prometheus
   註冊為它的 metrics Integration（`site-prometheus`、`http://prometheus:9090`，setting `labelProxyUrl` 為
   `http://prom-label-proxy:8080`；已有 metrics Integration 時改指到這裡並保留它的其他 settings，從不建立第二個）；把
   installation 的 Pulp 註冊為它的 `repository` Integration（`local-repository`、`http://pulp-web:8080`，setting
   `contentOrigin` 為 `http://<SWALLOW_ADDRESS>:<SWALLOW_PULP_PORT>`，credential 為 Pulp admin 密碼）；Local Repository
   的 spec 從未寫入過時寫入第一份 spec：預設來源 `ubuntu`（Ubuntu archive 的 `noble`、`noble-updates` 與
   `noble-security`），模式為 `upstream`，所以在你為 Local Repository 選擇其他模式之前 Site 從上游佈署，之後的修改
   （切換模式、刪光所有來源）也會保留；Site 尚無
   automation 設定時寫入預設值（啟用、Deployment Key、無 playbook mapping）；再等待 Integration 同步與 catalog 出現
   `ubuntu/noble`；
8. 為 Site 渲染 Prometheus 的 exporter discovery：以 Site 的 id 從
   [`prometheus-exporters.yml.template`](prometheus-exporters.yml.template) 產生 `swallow-exporters` job，
   寫到 `state/prometheus/swallow-exporters.yml`；只有 Prometheus 尚未載入這個檔案時才重啟它；
9. 執行 `doctor`，把已安裝的 release 記錄在 `state/installed`，並印出 Dashboard URL 與密碼
   位置。

它不會建立 SSH Access Key：由 user 自行在 Dashboard 加入。

## 拓樸

| Component | 位置 | 對外 |
| --- | --- | --- |
| Dashboard（Nginx） | Compose | HTTP `${SWALLOW_HTTP_PORT:-80}`，給 user 與 API client 的 public listener |
| Local Repository（`pulp-web`，Pulp 的 Nginx） | Compose | HTTP `${SWALLOW_PULP_PORT:-8080}`，給佈署出去的 Server 的 public listener；API 以 `http://pulp-web:8080` 呼叫它 |
| Pulp 的 API、content app 與兩個 worker | Compose | internal `control` 加上 `egress`（從 Local Repository 鏡像的上游取得內容） |
| API、worker、Ansible executor | Compose | internal `control` 加上 `egress`（連 MAAS 與 Server） |
| MongoDB（single-node replica set `rs0`）、Temporal | Compose | 只在 internal |
| PostgreSQL | Compose | Temporal databases、`pulp` 與 `maasdb`；MAAS 經 `127.0.0.1:${SWALLOW_POSTGRES_HOST_PORT:-5432}` 連入 |
| Prometheus（Site 的 metrics） | Compose | internal `control` 加上 `egress`（收集 Server 上的 exporter）；不發佈，API 以 `http://prometheus:9090` 讀取 |
| prom-label-proxy（一個 Platform 看到的 metrics） | Compose | 只在 internal `control`；不發佈，API 以 `http://prom-label-proxy:8080` 讀取 |
| MAAS 3.6 region+rack | host 上的 snap | `SWALLOW_MAAS_URL`；swallow 的 provisioner Integration 也用這個位址 |
| Control-Plane NTP Server（MAAS 的 chronyd） | host 上的 snap | 所有位址（含 `SWALLOW_ADDRESS`）的 UDP 123 |
| Temporal UI | Compose profile `diagnostics` | `127.0.0.1:${SWALLOW_TEMPORAL_UI_PORT:-8233}`，預設不啟動 |

Installation 提供 plain HTTP。要開放到受信任的管理網路之外前，請在 port 80 前面加上
TLS terminator。佈署出去的 Server 會連 UDP 123 對時；Site 使用 Local Repository 時，也會連 `SWALLOW_PULP_PORT` 的
Local Repository，若主機有防火牆，兩者都必須對機器網路開放。

Local Repository 是 Site 自己的來源，可以提供部署安裝的每樣東西：OS 套件（Ubuntu archive 與廠商的
archive，以這個 installation 的金鑰簽章）、container image（每個 registry 一個 pull-through cache）、Python 套件
（PyPI 的 pull-through cache），以及以宣告的 sha256 驗證的檔案。Site 一開始不使用它：部署從 swallow 的模組宣告的
上游取得每個 artifact（檔案以 sha256 驗證，廠商的套件庫以宣告的簽章金鑰驗證），Server 保留自己的 OS 套件來源。
Dashboard 的 Local repository 頁上 Site 的 **Artifact source for deployments**（API 的 `mode`）可以改變這點：選
**Cache** 時，佈署出去的 Server 從 `http://<SWALLOW_ADDRESS>:<SWALLOW_PULP_PORT>` 取得 Local Repository 已經提供的
部分，其餘從上游，不等它同步；選 **Offline** 時只從它取得，它提供每一項之前部署會被拒絕（Pulp 本身在第一次被要求
時仍要連網際網路取得套件）。Pulp 只從上游取得一次，把取得的內容保留在
volume `pulp-data`，所以磁碟要留出這份 cache 的空間。Dashboard 的 Local repository 頁顯示它的內容與同步狀態。同一個
port 上的 Pulp REST API 需要 Pulp admin 密碼（`secrets/pulp-admin-password`）。

Platform Grafana（Slurm 的在 controller 上，Kubernetes 的在叢集內）經 `http://<SWALLOW_ADDRESS>:<SWALLOW_HTTP_PORT>` 的 API 讀取 Site 的
Prometheus（Compose 以 `SWALLOW_API_PUBLIC_URL` 交給 api-server），Omnistat 只允許從 `SWALLOW_ADDRESS`
收集的 Prometheus（`SWALLOW_API_MONITORING_SCRAPE_ADDRESSES`），所以 machine 網路也要連得到這台主機的 HTTP port。

swallow provision 的每台 Server 都會在 SSH 就緒後指到這個 NTP server（`configure-ntp` Task）：
Compose 把 `SWALLOW_ADDRESS` 以 `SWALLOW_API_NTP_SERVER` 交給 api-server。各 Platform 的 install
playbook 在安裝前會檢查每個節點的時鐘。網路連不到 `ntp.ubuntu.com` 時，以
`sudo ./swallowctl ntp upstream set 10.0.0.123`（寫成 `server 10.0.0.123 iburst`）給 NTP server
一個可達的上游，或在第一次安裝前設定 `SWALLOW_NTP_UPSTREAM`。上游本身不能從這台主機取得時間（無論是直接對時，或透過
以這台主機時鐘計算時間的 gateway）：這台主機會跟著它走，兩者只會互相追逐。沒有可達的上游時，它會繼續提供本機時鐘，Server 之間
仍保持一致。

Site 的 Prometheus 每 30 秒收集 swallow 自己的 metrics（`swallow-api`），以及 swallow 確知已安裝在這個
Site 的 Server 上的每個 exporter（`swallow-exporters`，以 machine token 從 API 發現）。
[`prometheus.yml`](prometheus.yml) 隨 bundle 提供；exporter 的 job 渲染在 `state/` 下，新的 release 不會覆蓋它。
Metrics 保留 `SWALLOW_PROMETHEUS_RETENTION`，且從不備份：遺失只少了用量歷史，不影響 swallow 的狀態。

prom-label-proxy 在這個 Prometheus 前面，把每個查詢（含 label API）限制在一個 Platform 的 series：
`X-Swallow-Platform-Id` header 中的 `platform_id`。沒有這個 header 的請求會被拒絕。它不驗證身分，所以只能從
internal 的 `control` network 連到它。

## 設定

第一次 `install` 前可在 `.env` 或 environment 設定。`install` 與 `upgrade` 會把 environment
中有設定的值寫入 `.env`，之後執行時會保留：

| Variable | Default | 用途 |
| --- | --- | --- |
| `SWALLOW_ADDRESS` | `.env` 中已有的值；沒有時為共置 MAAS 首次初始化時的主機；再沒有時為 primary IPv4 | 機器與 BMC 連到這台主機的位址（`install.sh --address`） |
| `SWALLOW_HTTP_PORT` | `80` | Dashboard 與 API port |
| `SWALLOW_PULP_PORT` | `8080` | 佈署出去的 Server 取得內容的 Local Repository port，聽所有介面 |
| `SWALLOW_POSTGRES_HOST_PORT` | `5432` | MAAS 連 PostgreSQL 的 loopback port |
| `SWALLOW_MAAS_URL` | `http://<SWALLOW_ADDRESS>:5240/MAAS` | 機器連 MAAS 的位址；第一次安裝後固定 |
| `SWALLOW_BOOT_MEDIA_BASE_URL` | `http://<SWALLOW_ADDRESS>` | BMC 下載 Boot ISO 的位址 |
| `SWALLOW_MAAS_IMAGE_TIMEOUT` | `3600` | 等待 `ubuntu/noble` 的秒數 |
| `SWALLOW_SITE_NAME` | `default` | Bootstrap 建立的 Site |
| `SWALLOW_SSH_KNOWN_HOSTS` | 每次執行時掃描 | Site automation 的 static known_hosts |
| `SWALLOW_NTP_UPSTREAM` | `ntp.ubuntu.com` | Control-Plane NTP Server 的上游，以逗號或空白分隔；位址寫成 `server <ip> iburst`，名稱寫成 `pool <name> iburst`。之後以 `swallowctl ntp upstream` 修改 |
| `SWALLOW_NTP_ALLOW` | 所有 client | 僅限沒有共置 MAAS：host chrony 允許的 client subnet |
| `SWALLOW_PROMETHEUS_RETENTION` | `30d` | Site 的 Prometheus 保留 metrics 多久（Prometheus duration）；也是 swallow 能繪製的最長用量歷史 |

## 檢查

```bash
sudo ./swallowctl doctor
```

`doctor` 會檢查 Dashboard 與 API、Temporal health、worker 是否 poll
`swallow-operations` task queue、Ansible executor 是否執行、是否 poll `swallow-ansible` task queue，
且 `ansible-runner` 能跑附帶的 `diagnostic-ping` playbook、MAAS API 與 `ubuntu/noble` boot resource、Control-Plane NTP
Server（UDP 123 有服務、MAAS 的上游等於 `SWALLOW_NTP_UPSTREAM` 或 default，以及是否跟隨該上游）、
admin 登入、Deployment Key、Site automation、指向 Site Prometheus 與它的 prom-label-proxy 的 metrics
Integration、指向 installation 的 Pulp 的 repository Integration、MAAS Integration 同步、OS Image catalog 的
`ubuntu/noble`、Prometheus 是否 ready 並執行這個 Site 的 `swallow-exporters` job、prom-label-proxy 是否把查詢限制在
指定的 `platform_id`，以及 Pulp 是否經 `pulp-web` 在 `SWALLOW_PULP_PORT` 回應（資料庫已連線，API app、content app
與 worker 都在線），並列出尚未進入 terminal status 的 Workflow 數量。它不會佈署任何東西。Local Repository 的來源
是否同步完成由 Dashboard 顯示，不在這裡檢查。

## 恢復

安裝的 desired state 就是 `doctor` 檢查的契約：

1. 宣告的 Compose services 都在執行；
2. Temporal cluster healthy；
3. worker 在 `swallow-operations` task queue 同時有 workflow 與 activity poller；
4. Ansible executor 進程在執行、在 `swallow-ansible` task queue 有 activity poller，且能跑附帶的
   `diagnostic-ping` playbook；
5. Dashboard 與 API 在 HTTP port 回應 `/readyz`；
6. Prometheus ready，且執行 `swallow-exporters` job；prom-label-proxy 把每個查詢限制在指定的
   `platform_id`；
7. Pulp 經 `pulp-web` 在 `SWALLOW_PULP_PORT` 回應，資料庫已連線，API app、content app 與 worker 都在線；
8. 共置 MAAS 的 API 回應、有 complete 的 `ubuntu/noble` 與 virsh SSH identity，
   Control-Plane NTP Server 以設定的上游提供 UDP 123，安裝紀錄（admin 登入、Deployment
   Key、Site automation、metrics Integration、repository Integration、MAAS Integration 同步、OS Image
   catalog）存在，且 Prometheus 發現 installation Site 的 exporter。

`doctor` 只回報與此狀態的差距。斷電、主機 reboot 或出現任何 `FAIL` 之後，執行一個指令：

```bash
sudo ./swallowctl ensure
```

`ensure` 會先確保 PostgreSQL 在執行且有 Pulp 的 role 與 database，再啟動尚未執行的宣告 services，然後依
dependency 順序維持每個條件：條件不符時重啟
擁有它的 services（MAAS API 則重啟 MAAS snap）；`local-maas.sh`、`local-ntp.sh`、
`bootstrap.sh` 或 Prometheus exporter discovery 的檢查失敗時，執行對應的冪等 ensure 步驟、
`local-ntp.sh install` 與 `upstream`、`bootstrap.sh apply` 或重新渲染 exporter discovery；最後執行 `doctor`。仍不符的條件
會連同該 services 最近的 log 一起印出。`ensure` 可以重複執行；它不會重建執行中的容器、
不會套用變更過的 `.env`（那是 `upgrade` 的工作），也不會 cancel 或 retry Workflow。

## 維運

- `sudo ./swallowctl upgrade [--release-manifest PATH] [--force]` 拒絕比已安裝版本舊的
  release，也拒絕 database schema 比該 release 支援的還新（不支援降級）；有 active Workflow 時
  拒絕，除非 `--force`。它先 pull 新 image，停止 API、worker 與 Ansible executor，備份到
  `backups/pre-upgrade-<已安裝版本>-<時間>`（保留到你手動刪除），再 migrate、啟動 stack、確保
  Control-Plane NTP Server 與它的上游、MAAS rack 的 `virsh` SSH key 並記錄到 Integration、metrics
  Integration 與 Prometheus 的 exporter discovery、repository Integration 與 Local Repository 的第一份 spec，最後重新
  執行 `doctor`。Local Repository 之前的 installation 在這裡得到 Pulp 的 database、services 與 Integration；它的 Site
  的 spec 模式為 `upstream`，所以在你選擇 cache 或 offline 之前部署仍從上游取得。新的 Pulp image 在 stack 啟動時
  遷移它的 database，在備份之後。`install.sh` 在解壓任何東西之前就拒絕降級。
- `sudo ./swallowctl ntp upstream [show | set SERVER... | default]` 顯示、設定或重設 NTP server
  的上游。`set` 與 `default` 會先套用，成功後才寫入 `.env`。MAAS 的 `ntp_servers` 由 swallowctl
  管理：在 MAAS UI 設定的值，會在下次 install、upgrade 或 ensure 時被取代。
- `sudo ./swallowctl backup` 把 MongoDB、job artifacts、credential key、Local Repository 的 Pulp database 與它的
  金鑰（volume `pulp-certs`：資料庫欄位加密金鑰、registry token 金鑰與 deb 簽章金鑰）、MAAS database 與
  `backup.json`（取得時的 release 與 database schema）寫到 `backups/`，checksum 以備份內的相對
  檔名記錄。請每日排程；retention 是七份 daily 與四份 Sunday weekly。MAAS boot images 會從
  `images.maas.io` 重新同步。Temporal、Prometheus 的 metrics（volume `prometheus-data`）與 Pulp 的 artifact（volume
  `pulp-data`，都來自上游）不備份。
- `sudo ./swallowctl restore BACKUP_DIRECTORY --yes` 驗證 checksum、拒絕較新 release 的備份，停止
  API、worker 與 Ansible executor，還原 MongoDB、artifacts（取代現有的）、credential key、Pulp 的 database 與金鑰、
  MAAS database，migrate，終止 Temporal 中所有 swallow 的 run，再啟動 stack，請 Pulp 重新下載還原的 database 列出但
  `pulp-data` 沒有的 artifact，並執行 `doctor`。還原後的
  database 就是真相：每個未結束的 Workflow 依它重新派發，所以 backup 之後記錄的工作會遺失，
  backup 之後才完成的 Task 會再跑一次。沒有 `--yes` 時只說明將取代什麼。MAAS 重啟期間的 SSH key
  登錄會以退避重試，並在下一次 controller resync 再試；`swallow ssh-keys sync`（或 **Sync to
  provisioners**）可立即重試。
- `sudo ./swallowctl uninstall` 停止 stack 與 MAAS，保留 volumes（含 Pulp 的）、secrets 與 backups；
  `--purge-data` 會一併移除 MAAS snap、volumes 與 backups。

`secrets/` 不得進 source control；請透過 lifecycle tool 備份。安裝需要能連到 registry：
release 不附離線 image 封存檔，MAAS image 的離線匯入也尚未自動化。

## Diagnostics

```bash
docker compose logs -f api-server worker ansible-executor
sudo ./local-maas.sh check
sudo ./local-ntp.sh check
docker compose exec prometheus wget -qO- http://127.0.0.1:9090/api/v1/targets   # Prometheus 不發佈到主機
docker compose exec prometheus wget -qO- --header 'X-Swallow-Platform-Id: <platform id>' \
  'http://prom-label-proxy:8080/api/v1/query?query=up'                          # 一個 Platform 看到的資料
docker compose logs -f pulp-init pulp-api pulp-content pulp-worker pulp-web
curl -s http://127.0.0.1:8080/pulp/api/v3/status/ | jq                           # Server 看到的 Pulp
```

Temporal UI 是選配，不屬於 release。要使用時，在 `.env` 把 `SWALLOW_TEMPORAL_UI_IMAGE` 設成
exact digest（`.env.example` 有建議值），再執行
`docker compose --profile diagnostics up -d temporal-ui`；它只聽 loopback。

[Native path](native/README.zh-TW.md) 仍是 incomplete preview，請使用此 Compose topology。
