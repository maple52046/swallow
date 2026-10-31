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
   `rs0`），執行 `swallow-api migrate`，建立
   Deployment Key（`swallow-api deployment-key ensure`），再啟動整個 stack；
4. 把附帶的 CLI 安裝到 `/usr/local/bin/swallow`（API 也會在 `/downloads/swallow` 提供
   `bin/swallow`，給保留 OS 納管的主機下載）；
5. 執行 [`local-maas.sh`](local-maas.sh)：在 Compose PostgreSQL 建立 `maas` role 與
   `maasdb`、安裝並初始化 MAAS snap、建立 MAAS `admin`、保存 API key、為 MAAS rack 建立
   `virsh` power 用的 SSH key（第一次連線時接受新的 hypervisor host key）並把公鑰存到
   `secrets/maas-virsh-ssh.pub`，再等到官方 `ubuntu/noble` amd64 image complete；
6. 執行 [`local-ntp.sh`](local-ntp.sh) 的 `install` 與 `upstream`（decision 065）。provisioner 是
   共置的 MAAS，所以不需要安裝：MAAS 自己的 chronyd 就是此安裝的 Control-Plane NTP Server
   （`SWALLOW_ADDRESS` 的 UDP 123）。`upstream` 會把 `SWALLOW_NTP_UPSTREAM`（沒有設定時為 default
   `ntp.ubuntu.com`）寫入 MAAS 的 `ntp_servers`，並維持 `ntp_external_only` 關閉。MAAS 的 chrony
   沒有 `makestep`，所以主機時鐘與上游差超過一秒時，script 會讓 chronyd 一次 step 到位，而不是以
   +8.3% 速度 slew 好幾分鐘；
7. 透過已發佈的 API 執行 [`bootstrap.sh`](bootstrap.sh)：建立 Site `default`、MAAS
   provisioner Integration，並把 rack 的 `virsh` 公鑰設為它的 `virshSshPublicKey`（swallow
   納管某台 hypervisor 上的 libvirt 虛擬機時，會把它授權到那台 hypervisor）；Site 尚無
   automation 設定時寫入預設值（啟用、Deployment Key、無 playbook mapping）；再等待 Integration
   同步與 catalog 出現 `ubuntu/noble`；
8. 執行 `doctor`，把已安裝的 release 記錄在 `state/installed`，並印出 Dashboard URL 與密碼
   位置。

它不會建立 SSH Access Key：由 operator 自行在 Dashboard 加入。

## 拓樸

| Component | 位置 | 對外 |
| --- | --- | --- |
| Dashboard（Nginx） | Compose | HTTP `${SWALLOW_HTTP_PORT:-80}`，唯一 public listener |
| API、worker、Ansible executor | Compose | internal `control` 加上 `egress`（連 MAAS 與 Server） |
| MongoDB（single-node replica set `rs0`）、Temporal | Compose | 只在 internal |
| PostgreSQL | Compose | Temporal databases 與 `maasdb`；MAAS 經 `127.0.0.1:${SWALLOW_POSTGRES_HOST_PORT:-5432}` 連入 |
| MAAS 3.6 region+rack | host 上的 snap | `SWALLOW_MAAS_URL`；swallow 的 provisioner Integration 也用這個位址 |
| Control-Plane NTP Server（MAAS 的 chronyd） | host 上的 snap | 所有位址（含 `SWALLOW_ADDRESS`）的 UDP 123 |
| Temporal UI | Compose profile `diagnostics` | `127.0.0.1:${SWALLOW_TEMPORAL_UI_PORT:-8233}`，預設不啟動 |

Installation 提供 plain HTTP。要開放到受信任的管理網路之外前，請在 port 80 前面加上
TLS terminator。佈署出去的 Server 會連 UDP 123 對時，若主機有防火牆，必須對機器網路開放。

swallow provision 的每台 Server 都會在 SSH 就緒後指到這個 NTP server（`configure-ntp` Task）：
Compose 把 `SWALLOW_ADDRESS` 以 `SWALLOW_API_NTP_SERVER` 交給 api-server。各 Platform 的 install
playbook 在安裝前會檢查每個節點的時鐘。網路連不到 `ntp.ubuntu.com` 時，以
`sudo ./swallowctl ntp upstream set 10.0.0.123`（寫成 `server 10.0.0.123 iburst`）給 NTP server
一個可達的上游，或在第一次安裝前設定 `SWALLOW_NTP_UPSTREAM`。上游本身不能從這台主機取得時間（無論是直接對時，或透過
以這台主機時鐘計算時間的 gateway）：這台主機會跟著它走，兩者只會互相追逐。沒有可達的上游時，它會繼續提供本機時鐘，Server 之間
仍保持一致。

## 設定

第一次 `install` 前可在 `.env` 或 environment 設定。`install` 與 `upgrade` 會把 environment
中有設定的值寫入 `.env`，之後執行時會保留：

| Variable | Default | 用途 |
| --- | --- | --- |
| `SWALLOW_ADDRESS` | primary IPv4 | 機器與 BMC 連到這台主機的位址（`install.sh --address`） |
| `SWALLOW_HTTP_PORT` | `80` | Dashboard 與 API port |
| `SWALLOW_POSTGRES_HOST_PORT` | `5432` | MAAS 連 PostgreSQL 的 loopback port |
| `SWALLOW_MAAS_URL` | `http://<SWALLOW_ADDRESS>:5240/MAAS` | 機器連 MAAS 的位址；第一次安裝後固定 |
| `SWALLOW_BOOT_MEDIA_BASE_URL` | `http://<SWALLOW_ADDRESS>` | BMC 下載 Boot ISO 的位址 |
| `SWALLOW_MAAS_IMAGE_TIMEOUT` | `3600` | 等待 `ubuntu/noble` 的秒數 |
| `SWALLOW_SITE_NAME` | `default` | Bootstrap 建立的 Site |
| `SWALLOW_SSH_KNOWN_HOSTS` | 每次執行時掃描 | Site automation 的 static known_hosts |
| `SWALLOW_NTP_UPSTREAM` | `ntp.ubuntu.com` | Control-Plane NTP Server 的上游，以逗號或空白分隔；位址寫成 `server <ip> iburst`，名稱寫成 `pool <name> iburst`。之後以 `swallowctl ntp upstream` 修改 |
| `SWALLOW_NTP_ALLOW` | 所有 client | 僅限沒有共置 MAAS：host chrony 允許的 client subnet |

## 檢查

```bash
sudo ./swallowctl doctor
```

`doctor` 會檢查 Dashboard 與 API、Temporal health、worker 是否 poll
`swallow-operations` task queue、Ansible executor 是否執行、是否 poll `swallow-ansible` task queue，
且 `ansible-runner` 能跑附帶的 `diagnostic-ping` playbook、MAAS API 與 `ubuntu/noble` boot resource、Control-Plane NTP
Server（UDP 123 有服務、MAAS 的上游等於 `SWALLOW_NTP_UPSTREAM` 或 default，以及是否跟隨該上游）、
admin 登入、Deployment Key、Site automation、MAAS Integration 同步，以及 OS Image catalog 的
`ubuntu/noble`，並列出尚未進入 terminal status
的 Workflow 數量。它不會佈署任何東西。

## 恢復

安裝的 desired state 就是 `doctor` 檢查的契約：

1. 宣告的 Compose services 都在執行；
2. Temporal cluster healthy；
3. worker 在 `swallow-operations` task queue 同時有 workflow 與 activity poller；
4. Ansible executor 進程在執行、在 `swallow-ansible` task queue 有 activity poller，且能跑附帶的
   `diagnostic-ping` playbook；
5. Dashboard 與 API 在 HTTP port 回應 `/readyz`；
6. 共置 MAAS 的 API 回應、有 complete 的 `ubuntu/noble` 與 virsh SSH identity，
   Control-Plane NTP Server 以設定的上游提供 UDP 123，且安裝紀錄（admin 登入、Deployment
   Key、Site automation、MAAS Integration 同步、OS Image catalog）存在。

`doctor` 只回報與此狀態的差距。斷電、主機 reboot 或出現任何 `FAIL` 之後，執行一個指令：

```bash
sudo ./swallowctl ensure
```

`ensure` 會啟動尚未執行的宣告 services，再依 dependency 順序維持每個條件：條件不符時重啟
擁有它的 services（MAAS API 則重啟 MAAS snap）；`local-maas.sh`、`local-ntp.sh` 或
`bootstrap.sh` 的檢查失敗時，執行對應的冪等 ensure 步驟、`local-ntp.sh install` 與 `upstream` 或
`bootstrap.sh apply`；最後執行 `doctor`。仍不符的條件
會連同該 services 最近的 log 一起印出。`ensure` 可以重複執行；它不會重建執行中的容器、
不會套用變更過的 `.env`（那是 `upgrade` 的工作），也不會 cancel 或 retry Workflow。

## 維運

- `sudo ./swallowctl upgrade [--release-manifest PATH] [--force]` 會先 backup；有 active
  Workflow 時拒絕，除非 `--force`；完成 migrate、確保 Control-Plane NTP Server 與它的上游、MAAS
  rack 的 `virsh` SSH key 並記錄到 Integration 後，重新執行 `doctor`。
- `sudo ./swallowctl ntp upstream [show | set SERVER... | default]` 顯示、設定或重設 NTP server
  的上游。`set` 與 `default` 會先套用，成功後才寫入 `.env`。MAAS 的 `ntp_servers` 由 swallowctl
  管理：在 MAAS UI 設定的值，會在下次 install、upgrade 或 ensure 時被取代。
- `sudo ./swallowctl backup` 把 MongoDB、job artifacts、credential key 與 MAAS database
  寫到 `backups/`。請每日排程；retention 是七份 daily 與四份 Sunday weekly。MAAS boot
  images 會從 `images.maas.io` 重新同步。
- `sudo ./swallowctl restore BACKUP_DIRECTORY` 驗證 checksum，還原 MongoDB、artifacts、
  credential key 與 MAAS database，再重建 API 與 Dashboard。MAAS 重啟期間的 SSH key 登錄
  會以退避重試，並在下一次 controller resync 再試；`swallow ssh-keys sync`（或 **Sync to
  provisioners**）可立即重試。
- `sudo ./swallowctl uninstall` 停止 stack 與 MAAS，保留 volumes、secrets 與 backups；
  `--purge-data` 會一併移除 MAAS snap、volumes 與 backups。

`secrets/` 不得進 source control；請透過 lifecycle tool 備份。安裝需要能連到 registry：
release 不附離線 image 封存檔，MAAS image 的離線匯入也尚未自動化。

## Diagnostics

```bash
docker compose logs -f api-server worker ansible-executor
sudo ./local-maas.sh check
sudo ./local-ntp.sh check
```

Temporal UI 是選配，不屬於 release。要使用時，在 `.env` 把 `SWALLOW_TEMPORAL_UI_IMAGE` 設成
exact digest（`.env.example` 有建議值），再執行
`docker compose --profile diagnostics up -d temporal-ui`；它只聽 loopback。

[Native path](native/README.zh-TW.md) 仍是 incomplete preview，請使用此 Compose topology。
