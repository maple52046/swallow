# Installation 設計與規範

本文件是 swallow [Installation](glossaries/terms/installation.md) 的權威設計文件：單一 VM 的
production installation 拓樸、`swallowctl` lifecycle 的保證、release 發佈物與 image 來源、一行指令
安裝與升級，以及 [Control-Plane NTP Server](glossaries/terms/control-plane-ntp-server.md) 與 OS 佈署後
的時間同步。它收斂 [decision 040](../decisions/040-single-vm-production-installation.md)、
[decision 050](../decisions/050-compose-only-release-artifacts.md)、
[decision 051](../decisions/051-ghcr-holds-only-swallow-images.md)、
[decision 052](../decisions/052-one-command-installation.md)、
[decision 065](../decisions/065-control-plane-ntp-and-post-provision-time-sync.md) 的現行規則。
決策理由與否決方案以 ADR 為準，本文只寫目前有效的行為與不變式。

> **Source of truth**：現行行為以 `deploy/`（`deploy/production` 的 Compose 檔與腳本、
> `deploy/release` 的發佈腳本與 pin 檔）與 `api-server` 的設定與 playbook 為準；本文與它們不一致時，
> 以程式為準並修正本文。程式位置見 §7。

> **用語**：本文的「release」指 swallow 的版本發行物（例如 `0.1.0-rc.5`），與 glossary 的
> [Release](glossaries/terms/release.md)（把 Machine 還給 provider 的可用池）無關。

---

## 1. 目的與範圍

本文涵蓋：

- 支援的安裝拓樸（§2）。
- `swallowctl` 各指令的保證與不變式（§3）；這不是操作手冊。
- release 的內容、image 來源與 digest 固定（§4）。
- `install.sh` 一行指令安裝與升級、`SWALLOW_ADDRESS` 的推導（§5）。
- Control-Plane NTP Server、它的上游，以及每次 OS provision 之後的 `configure-ntp`（§6）。

不在本文範圍：

- Platform 佈署端的時鐘檢查（role `time_sync_check`、deploy 欄位 `timeSyncCheck`）：見
  [platform-deployment.md](platform-deployment.md) §2.2 與 glossary
  [Time Sync Check](glossaries/terms/time-sync-check.md)。
- [Deployment Key](glossaries/terms/deployment-key.md) 的語意與使用；installation 只負責在 install／upgrade
  時執行 `swallow-api deployment-key ensure`。
- Boot Media／Boot ISO、Server Enrollment 下載路由、MAAS rack virsh SSH identity 的用途；installation 只
  推導 `SWALLOW_BOOT_MEDIA_BASE_URL`、提供 Nginx 路由，並建立與記錄 virsh 公鑰。
- MAAS 的 DHCP／PXE 設定：在 installation 之外，由 operator 在 MAAS 設定。
- 多主機拓樸、TLS、離線（air-gapped）安裝與 native（systemd）安裝：目前都不支援（§2.6、§4.5）。

---

## 2. 安裝拓樸

### 2.1 單一 VM

- Production installation 是**一台** Ubuntu 24.04 amd64（x86_64）主機：swallow 跑在 Docker Compose
  （project `swallow-production`），MAAS 3.6 region+rack 以 snap 共置在同一台主機。
- 「Production」指 installation 契約（digest 固定的 image、自動產生的 secrets、lifecycle 工具），不是
  多主機拓樸。`swallowctl install` 只接受 profile `production`。

### 2.2 Compose services 與網路

| Service | Image | 網路 | 對外 |
| --- | --- | --- | --- |
| `mongo` | `SWALLOW_MONGO_IMAGE` | `control` | 無 |
| `postgres` | `SWALLOW_POSTGRES_IMAGE` | `control`、`host-postgres` | `127.0.0.1:${SWALLOW_POSTGRES_HOST_PORT:-5432}`（只給主機上的 MAAS） |
| `temporal` | `SWALLOW_TEMPORAL_SERVER_IMAGE` | `control` | 無 |
| `temporal-ui`（profile `diagnostics`） | `SWALLOW_TEMPORAL_UI_IMAGE`（operator 自設） | `control` | `127.0.0.1:${SWALLOW_TEMPORAL_UI_PORT:-8233}` |
| `api-server` | `SWALLOW_API_IMAGE` | `control`、`egress` | 無（經 Dashboard 代理） |
| `worker` | `SWALLOW_API_IMAGE`（command `worker`） | `control`、`egress` | 無 |
| `ansible-executor` | `SWALLOW_API_IMAGE`（command `ansible-executor`） | `control`、`egress` | 無 |
| `dashboard` | `SWALLOW_DASHBOARD_IMAGE` | `edge`、`control` | `${SWALLOW_HTTP_PORT:-80}` → 容器 8080 |

- `control` 是 `internal` network；MongoDB 與 Temporal 只在這裡。
- `egress` 不是 internal；只有 API、worker、Ansible executor 加入，用來連共置的 MAAS（`SWALLOW_MAAS_URL`，
  §5.4）與它們佈署、設定的 Server。三者另以 `extra_hosts` 把 `host.docker.internal` 對到 host gateway。
- `host-postgres` 只有 PostgreSQL 加入，用於發佈它的 loopback port。
- Dashboard（Nginx）是唯一對外 listener，把 `/api/`、`/healthz`、`/livez`、`/readyz`、`/metrics`、
  `/boot-media/`、`/downloads/` 代理到 API（容器內 `:30051`，不發佈）。
- API、worker、Ansible executor 是同一個 image，以不同 command 啟動。Ansible executor 以 `init: true` 執行：
  Docker 的 init 擔任 PID 1，轉送 signal 並回收 `ansible-runner` 留下的 ssh-agent，否則每次 run 都會留下 zombie。
- Temporal UI 是選用的 diagnostics service：只在 operator 設定 `SWALLOW_TEMPORAL_UI_IMAGE` 後以 profile
  `diagnostics` 啟動，只聽 loopback。

### 2.3 共用的 PostgreSQL instance

- 一個 PostgreSQL instance（service `postgres`、volume `postgres-data`）服務 Temporal（database
  `temporal_core`、`temporal_visibility`）與共置的 MAAS（role `maas`、database `maasdb`）。各自有自己的
  database 與 role，instance 以下不共用任何東西。
- Superuser 是 `temporal`，密碼 secret 是 `temporal-db-password`，因為 Temporal 以此帳號登入。
- `local-maas.sh install` 以冪等 SQL 建立 `maas` role 與 `maasdb`（密碼來自 secret `maas-db-password`），
  並要求 PostgreSQL 14 以上（MAAS 3.6 的需求）。
- 絕不使用 `maas-test-db`。swallow 自己的狀態只在 MongoDB（database `swallow`）。

### 2.3a MongoDB：single-node replica set

swallow 需要 store 支援多文件 transaction 與 change stream（decision 069、
[desired-state-model.md](desired-state-model.md) §10），所以 MongoDB 在每個環境都是 single-node replica set
`rs0`，每個 swallow process 啟動時檢查，不是 replica set 就拒絕啟動。

- Production：開啟認證，replica set 的內部認證使用 secret `mongo-keyfile`（`prepare-secrets.sh` 以
  `openssl rand -base64 756` 產生，只補不換）。`mongo-entrypoint.sh` 是 mongo container 的 entrypoint：它把
  keyFile 複製到 tmpfs 的 `/tmp`、改成 mongodb 擁有的 0400（secret 以 0444 掛載，root filesystem 是唯讀），
  再以 `--replSet rs0 --keyFile` 啟動 mongod。它同時是 healthcheck：以 root 帳密認證，第一次啟動時以成員
  `mongo:27017` 執行 `rs.initiate`，只有這個成員是可寫入的 primary 才算健康。
- 官方 image 第一次啟動時以暫時的 mongod 建立 root 帳號，那個 mongod 不帶 `--replSet` 與 `--keyFile`；真正的
  mongod 才帶，所以 initiate 由 healthcheck 執行。
- `mongo-uri` 不帶 `replicaSet` 參數：driver 經由成員名稱 `mongo:27017` 發現 replica set，所以既有安裝的
  `mongo-uri` 不需要改寫。
- dev stack（`deploy/dev`）沒有認證，只加 `--replSet rs0`，不需要 keyFile；主機上的工具與測試以
  `mongodb://localhost:27017/?directConnection=true` 連線，因為成員名稱 `mongo` 在主機上解析不到。
- 自行提供 MongoDB 時（`SWALLOW_API_MONGO_URI` 指向外部），它必須是 replica set 或 sharded cluster。

### 2.4 共置的 MAAS

- `swallowctl`（經 `local-maas.sh`）自己安裝 MAAS snap（channel 預設 `3.6/stable`，`SWALLOW_MAAS_CHANNEL`）、
  初始化為 `region+rack`、建立 MAAS admin 並把它的 API key 存為 secret `maas-api-key`，也負責停止與
  purge 這個 MAAS。
- 只管理自己安裝的 MAAS：主機已有 MAAS snap 但沒有 `state/local-maas` marker 時拒絕；MAAS 已初始化為
  `region+rack` 以外的模式時拒絕。
- MAAS 以 `SWALLOW_MAAS_URL`（machines 連 MAAS 的位址）初始化，URL 記在 `state/local-maas`；重跑不會把
  MAAS 改指到其他位址。
- 只確保官方 `images.maas.io` boot source 的 `ubuntu/noble` amd64：建立或補齊 selection、觸發 import，
  等到 MAAS 回報該 boot resource 有完整的 resource set（上限 `SWALLOW_MAAS_IMAGE_TIMEOUT`，預設 3600 秒）。
  第三方或 custom image、離線 image media 不屬於 installation。同步 image 不是
  [OS Deployment](glossaries/terms/os-deployment.md)：沒有任何 Server 改變。
- 安裝也為 MAAS rack 建立 virsh power driver 用的 SSH identity，公鑰存為 `secrets/maas-virsh-ssh.pub`，
  由 bootstrap 記到 Integration（用途見 [decision 055](../decisions/055-libvirt-virtual-machine-enrollment.md)）。
- MAAS 的 chronyd 就是 production 主機的 Control-Plane NTP Server（§6.1）。

### 2.5 Installation records 經由已發佈的 API 建立

`bootstrap.sh apply`（冪等）只透過 api-server 已發佈的 HTTP 契約（auth-login、sites-integrations、
site-automation、ssh-keys、provisioning）收斂 swallow 端的紀錄，不直接碰 MongoDB 或 swallow 內部：

1. 以 bootstrap admin 登入。
2. 找或建立 [Site](glossaries/terms/site.md)（名稱 `SWALLOW_SITE_NAME`，預設 `default`）。
3. 把共置 MAAS 註冊為該 Site 的 provisioner [Integration](glossaries/terms/integration.md)
   （`providerKind` `maas`，endpoint 見 §5.4）。已存在時只替換 write-only credential（重新初始化的 MAAS
   會有新的 API key），不改 endpoint，也不重建 Integration 與其 Server 對應。
4. 把 rack 的 virsh 公鑰記為 Integration 的 `settings.virshSshPublicKey`（與既有 settings 合併）。
5. Site 沒有 [Automation Configuration](glossaries/terms/automation-configuration.md) 時才寫入預設：
   啟用、`sshPort` 22、沒有 `playbookMappings`，`knownHosts` 為 `SWALLOW_SSH_KNOWN_HOSTS` 或一行註解
   （executor 每次執行時自行掃描 target 的 host key）；不寫 Site credential（automation 一律使用 Deployment
   Key）。已存在就不動，operator 之後的修改每次重跑都保留。
6. 確認 Deployment Key 存在，等 Integration sync 成功，以及 OS Image catalog 出現完整的 `ubuntu/noble`
   amd64（上限 `SWALLOW_BOOTSTRAP_TIMEOUT`，預設 900 秒）。

- 不建立任何 SSH Access Key：由 operator 自己上傳。
- `bootstrap.sh verify` 是對應的唯讀檢查，供 `doctor` 使用（§3.4）。
- Ownership 不變（[decision 001](../decisions/001-system-ownership-boundaries.md)）：MAAS 仍是擁有
  machine 與 image 事實的 provisioner；共置只改變拓樸。

### 2.6 Plain HTTP

- Dashboard 與 API 走 plain HTTP（`SWALLOW_HTTP_PORT`，預設 80）。需要 TLS 時，由 installation 前面的
  terminator 終結；Nginx 原樣轉送前端帶來的 `X-Forwarded-Proto`（沒有時用自己的 scheme）。
- 沒有 TLS terminator 時，admin 登入在網路上是明文。

### 不變式

- 一台主機、一個 Compose project、一個由 swallowctl 安裝的 MAAS。
- MAAS 依賴 Compose 的 PostgreSQL：停止 stack 或 uninstall 會停止 MAAS，重建 PostgreSQL 的 upgrade 會短暫
  中斷它，backup／restore 包含 `maasdb`（§3.6）。
- MongoDB 與 Temporal 不加入 `egress`；對外連線只來自 API、worker 與 Ansible executor。
- 主機是 swallow 與其 provisioner 共同的單一 failure domain。
- 安裝需要對外連線：GHCR 與 Docker Hub（image）、`images.maas.io`（`ubuntu/noble`）、snap store（MAAS），
  以及主機缺 Docker 時的 Docker apt repository。

### 設計理由

單一 VM、MAAS 共用 Compose 的 PostgreSQL instance（而非第二個 PostgreSQL 或 host PostgreSQL）、不用
`maas-test-db`、紀錄經已發佈 API 建立、先以 plain HTTP 提供：見
[decision 040](../decisions/040-single-vm-production-installation.md)。

---

## 3. `swallowctl` lifecycle

`swallowctl` 是 installation 唯一的 lifecycle 工具。它在 Compose bundle 的 `production/` 目錄（或 checkout
的 `deploy/production`）中執行，操作的是它所在的目錄。

### 3.1 共通保證

- **需要 root**：`install`、`upgrade`、`ensure`（production）、`backup`、`restore`、`uninstall`、
  `ntp upstream set|default`。它們安裝主機套件、管理 MAAS snap，而 `secrets/` 由 root 擁有。
- **`.env` 是 installation 的宣告設定**。release manifest 提供 `SWALLOW_RELEASE_VERSION` 與五個 image
  參照（`SWALLOW_API_IMAGE`、`SWALLOW_DASHBOARD_IMAGE`、`SWALLOW_MONGO_IMAGE`、`SWALLOW_POSTGRES_IMAGE`、
  `SWALLOW_TEMPORAL_SERVER_IMAGE`）：從 manifest 寫入時一律覆寫，且優先於 shell 中殘留的同名值；其餘
  operator 設定保留。設定的持久化規則見 §5.3。
- 腳本讀取 `.env` 的優先順序與 Compose 相同：shell environment 優先於 `.env`。
- **Secrets 只補不換**：`prepare-secrets.sh` 只產生缺少的 secret（隨機值），從不替換既有的（例如替換
  credential key 會讓已封存的資料無法解開）。`secrets/` 必須是 mode 0700。
- **Image 一律以 digest 參照**：preflight 要求每個必要 image 都是 `@sha256:<64 hex>`，
  `SWALLOW_TEMPORAL_UI_IMAGE` 有設定時也是；installer 從不改用 tag。只 pull 本機沒有的 image。
- **Installed marker**：`state/installed` 記錄最後一次通過 `doctor` 的 `install` 或 `upgrade` 的 release
  （`SWALLOW_RELEASE_VERSION=<version>`）。它只在 `doctor` 通過後寫入，`uninstall` 會刪除它；`install.sh`
  依它選擇 `install` 或 `upgrade`（§5.2）。

### 3.2 `install`

`install` 把主機收斂成一個 installation，可安全重跑；第一次安裝、重跑、中途停止後的續跑都走這條路。
順序：

1. 主機套件：缺少時安裝 `curl`、`jq`、`openssl`、`snapd`、`iproute2`；沒有 Docker Engine 與 Compose
   plugin 時，從 Docker 的簽章 apt repository 安裝（不用 convenience script）。
2. 從 `--release-manifest` 寫入 `.env`；未傳入且還沒有 `.env` 時，使用 bundle 的 `release-manifest.json`。
   既沒有 manifest 也沒有 `.env` 即失敗。
3. 持久化設定並推導位址（§5.3），補齊 secrets。
4. 第一次安裝（stack 未執行）時，確認 HTTP port、PostgreSQL host port，以及 MAAS 尚未安裝時的 5240 沒有被
   佔用。
5. preflight：x86_64、Ubuntu 24.04、Docker 與 Compose、必要工具、SemVer 版本、digest 參照、secrets 齊全
   與目錄權限、Compose 設定有效。
6. 啟動 MongoDB 與 PostgreSQL 並等兩者 healthy（MongoDB 要等 replica set 有 primary，§2.3a；最多 300 秒），
   執行 `swallow-api migrate` 與 `swallow-api deployment-key ensure`（Deployment Key 只在這裡建立，既有的
   保留），啟動整個 stack，等 API 回應 `/readyz`。
7. 把 bundle 的 CLI 安裝為 `/usr/local/bin/swallow`。
8. 共置 MAAS：`local-maas.sh install` 與 `ensure-image`（§2.4）。
9. 時間：`local-ntp.sh install` 與 `upstream`（§6.1、§6.2）。
10. `bootstrap.sh apply`（§2.5）。
11. `doctor`；通過才寫入 `state/installed`。

### 3.3 `upgrade`

- 有未到終態的 Workflow（包含停在 `requires_attention` 等待 operator 的）時拒絕，除非帶 `--force`。
- 先執行 `backup`，再寫入新 manifest、持久化設定、補齊 secrets、preflight、pull。
- 啟動 MongoDB 與 PostgreSQL 並等兩者 healthy。replica set 之前的 installation 在這一步轉換：補上的
  `mongo-keyfile` 與新的 mongo 設定讓 container 重建，healthcheck 在既有資料上執行 `rs.initiate`（資料保留）。
- `swallow-api migrate --backup-confirmed`（確認已有備份，允許破壞性的 schema rename）、
  `deployment-key ensure`、以新 image 啟動 stack、等 `/readyz`、更新 CLI。
- 重新套用冪等步驟：`local-ntp.sh install` 與 `upstream`；有共置 MAAS 時另有
  `local-maas.sh ensure-virsh-identity` 與 `bootstrap.sh apply`。
- `doctor`；通過才更新 `state/installed`。
- 宣告設定的變更（`compose.yaml`、`.env` 的 digest、`secrets/`）只經由 `upgrade` 套用，因為它先備份、
  再 migrate。

### 3.4 `doctor`：installation 的 desired state

`doctor` 只回報偏離，不修改也不佈署任何東西；任一項失敗即以非零結束。以下 desired state 也是 `ensure`
收斂的目標：

1. 每個宣告的 Compose service 都在執行。
2. MongoDB 是 replica set `rs0` 的可寫入 primary（§2.3a）。
3. Temporal cluster healthy。
4. worker 在 `swallow-operations` task queue 上同時有 workflow 與 activity poller。
5. Ansible executor process 在執行、在 `swallow-ansible` task queue 有 activity poller，且能以
   `ansible-runner` 跑隨附的 `diagnostic-ping` playbook。
6. Dashboard 與 API 在 HTTP port 回應 `/readyz`。
7. 僅 production：共置 MAAS 回應、有完整的 `ubuntu/noble` amd64 與 virsh SSH identity（`local-maas.sh check`）；
   Control-Plane NTP Server 在 UDP 123 服務且上游為設定值（`local-ntp.sh check`，§6.1）；installation records
   存在（`bootstrap.sh verify`：admin 登入、Deployment Key、可執行的 Site automation、MAAS Integration sync
   無錯誤、catalog 有 `ubuntu/noble` amd64、Integration 記有 virsh 公鑰）。

- 未到終態的 Workflow 數只是資訊：忙碌的 installation 仍是健康的。
- `--profile dev` 讓 `doctor` 與 `ensure` 改看 source checkout 的開發 stack（`deploy/dev`），不需要 root，
  並略過第 7 項；release bundle 不含 `deploy/dev`，所以只有 checkout 能用。

### 3.5 `ensure`

- 把 stack 收斂回 §3.4 的 desired state，最後以 `doctor` 的判定為結果；可安全重跑，已符合契約的 stack
  不被動到。production 沒有 `.env`（沒有安裝）時拒絕。
- 啟動未執行的宣告 service，但**從不 recreate 執行中的 container**：設定與 image 的變更屬於 `upgrade`。
- 每個條件先等一段時間（ensure 剛啟動的 service 給 240 秒的啟動視窗，原本就在跑的給 20 秒重查）；仍
  失敗就重啟擁有它的 service 一次，再等一個啟動視窗；仍失敗則印出那些 service 的近期 log。
- 共置 MAAS：API 不回應時 `snap restart maas`；image 或 virsh identity 檢查失敗時重跑 `local-maas.sh` 的
  冪等 ensure 步驟。**從不重裝或重新初始化 MAAS。**
- NTP 檢查失敗時重跑 `local-ntp.sh install` 與 `upstream`；排在 MAAS 條件之後，因為 MAAS 的 chronyd 就是
  server。
- installation records 的 verify 失敗時重跑 `bootstrap.sh apply`。
- **從不取消或重試 Workflow**：那是 operator 的決定。

### 3.6 `backup` 與 `restore`

- `backup` 在 `backups/`（`SWALLOW_BACKUP_DIR` 可改）建立 `daily-<UTC 時間戳>`，內容為 MongoDB `swallow`
  database 的 archive、job artifacts、用來解開封存 secrets 的 credential key、有共置 MAAS 時的 `maasdb`
  dump，以及 `SHA256SUMS`。MAAS boot images 不備份：restore 後從 `images.maas.io` 重新同步。
- 週日的備份另以 hard link 複製為 `weekly-<時間戳>`；保留最近 7 份 daily 與 4 份 weekly。
- Compose installation 不自帶備份排程；`upgrade` 一律先備份。
- `restore BACKUP_DIRECTORY` 先驗證 `SHA256SUMS`，再還原 MongoDB（`--drop`）、job artifacts、credential
  key，以及（備份含 dump 且有共置 MAAS 時，期間停止 MAAS snap）`maasdb`，最後 recreate API 與 Dashboard。

### 3.7 `uninstall`

- 先刪除 `state/installed`。
- 預設：停止 MAAS snap（它的資料庫在 swallow 的 PostgreSQL 中，會隨 stack 停止）並 `docker compose down`；
  保留 named volumes、`secrets/`、`backups/` 與已停止的 MAAS snap。
- `--purge-data`：另外 purge MAAS snap（連同 `state/local-maas`、MAAS API key 與 virsh 公鑰 secret）、刪除
  named volumes 與 `backups/`。`.env` 與其餘 `secrets/` 不刪除。

### 3.8 `ntp` 與 `preflight`

- `ntp upstream [show | set SERVER... | default]`：見 §6.2。
- `preflight`：單獨執行 §3.2 第 5 步的檢查。

### 設計理由

- `swallowctl` 是唯一的 lifecycle 工具、installed marker 只在 `doctor` 通過後寫入：見
  [decision 052](../decisions/052-one-command-installation.md)。
- upgrade 在 operation 進行中拒絕、除非 operator 明確強制：沿用
  [decision 006](../decisions/006-embedded-ansible-execution.md) 的 Consequences。
- backup／restore 包含 `maasdb`，因為 MAAS 依賴 Compose 的 PostgreSQL：見
  [decision 040](../decisions/040-single-vm-production-installation.md)。

---

## 4. Release artifacts 與 image provenance

### 4.1 Release 的內容

一個 release 的版本是不帶 `v` 的 SemVer（GitHub Release tag 為 `v<version>`），只發佈 Compose installation：

| Artifact | 內容 |
| --- | --- |
| `swallow-compose-<version>.tar.zst` | Compose bundle：`production/`（`compose.yaml`、`.env.example`、`env.sh`、`prepare-secrets.sh`、`mongo-entrypoint.sh`、`swallowctl`、`local-maas.sh`、`local-ntp.sh`、`bootstrap.sh`、`bin/swallow` CLI）、`testing/` 與 `release-manifest.json` |
| `install.sh` | 一行指令的 installer 與 upgrader，預設版本就是這個 release（§5） |
| `swallow-linux-amd64` | CLI binary，從已推送的 `cli` image 以 `crane export` 取出 |
| `release-manifest.json` | image digest 與 release metadata（§4.3） |
| `offline-media-manifest.json` | 第三方離線 media 的目錄格式描述；installer 不讀取 |
| `SHA256SUMS` | 以上五個檔案的 SHA-256 |

- release 只帶 SHA-256 checksum，沒有簽章或 SBOM。
- 不發佈 native bundle、離線 OCI image archive 與 Temporal UI image（§4.5）。

### 4.2 Image 的來源

| Image | 來源 | manifest 中的參照 |
| --- | --- | --- |
| `api`、`dashboard`、`cli` | 由本 repository 建置（linux/amd64），推送到 GHCR package `IMAGE_REPO`（預設 `ghcr.io/maple52046/swallow`），tag 為 `<component>-<version>` | `<IMAGE_REPO>@sha256:<推送後的 digest>` |
| MongoDB、PostgreSQL、Temporal Server | Docker Hub 官方 image `docker.io/library/mongo`、`docker.io/library/postgres`、`docker.io/temporalio/auto-setup`，原樣使用 | `deploy/release/runtime-images.env` 中的上游 index digest，原樣寫入 |

- GHCR 只放 swallow 建置的 image。第三方 image 不鏡像到 GHCR；installation 直接從 Docker Hub pull。
- `runtime-images.env` 的每一項都必須是 `<registry>/<name>@sha256:<digest>`；發佈前確認每一項都能解析出
  linux/amd64 image。升版是刻意的審查動作；PostgreSQL 必須維持 14 以上，因為它同時承載 MAAS 的資料庫。
- installation 從不使用 tag。
- GHCR package 必須是 public，否則每台 installation 都要先 `docker login ghcr.io`。

### 4.3 Release manifest

- `schemaVersion` 為 1，帶 `version` 與 `commit`。`images` 正好有六個 key：`api`、`dashboard`、`cli`、
  `mongo`、`postgres`、`temporalServer`，全部是 digest 參照。另有 `platforms`、`schemaVersions`、
  `compatibility`（host `ubuntu-24.04-amd64`、MAAS `3.6`、MongoDB `8.0` 等）、`artifacts`（`composeBundle`、
  `cliBinary`、`thirdPartyMediaManifest`）與 `upgrade`。
- manifest 不含 Temporal UI image、OCI archive 或 native bundle 的欄位。
- `swallowctl` 只讀 `version` 與 `images` 中的 `api`、`dashboard`、`mongo`、`postgres`、`temporalServer`，
  缺任一項即拒絕；其餘欄位目前沒有 consumer。`cli` image 只用於組裝 release 時取出 CLI。
- `deploy/release/release-manifest.example.json` 是格式範例；`validate.sh` 檢查它的 `images` key 集合與
  registry 前綴（swallow image 在 GHCR、runtime image 在 `docker.io`）。

### 4.4 發佈

- 專案沒有 CI：release 由 workstation 以 `deploy/release/publish.sh` 發佈。
- 發佈機必須是 x86_64：以一般的 `docker build`／`docker push` 原生建置 linux/amd64（Docker Engine，或
  BuildKit 執行中的 nerdctl），不用 buildx 或 emulation，並關閉 buildx 的預設 attestation，讓每個 component
  只有一個 linux/amd64 manifest。另需 `git`、`jq`、`zstd`、`python3` 與 `PATH` 上的 `crane`（沒有以 container
  代跑的 fallback）；`--github-release` 另需 `gh`。
- 發佈前的 gate：`validate.sh`（manifest 契約、runtime image 固定、Python 依賴精確 pin、`install.sh` 的版本
  placeholder、腳本語法）與文件連結檢查；預設拒絕有未 commit 變更的 working tree（`--allow-dirty` 只供演練）。
- 已發佈的版本不覆寫：任一 `<component>-<version>` tag 已存在即拒絕。
- `assemble-release.sh` 只從已推送的 image 組裝 artifacts，不建置任何東西，並以 `crane` 讀 registry，
  不依賴本機 container engine。

### 4.5 延後的發佈物

- **Native bundle**：native Temporal 與 PostgreSQL packaging 完成前不發佈；範本保留在
  `deploy/production/native/`。
- **離線 image archive**：支援離線安裝（含 MAAS images）之前不發佈。離線安裝屆時自帶 media，不以把第三方
  image 放進 GHCR 實現。
- **Temporal UI**：不鏡像，installer 也不寫入 `.env`；`SWALLOW_TEMPORAL_UI_IMAGE` 由 operator 自行設定，且
  必須是 digest。
- 因此 air-gapped 站點目前無法從 release 安裝。

### 設計理由

- release 只發佈 Compose installation、延後 native bundle 與離線 archive、Temporal UI 交給 operator、發佈機
  必須自備 crane：見 [decision 050](../decisions/050-compose-only-release-artifacts.md)。這延後而非放棄
  [decision 006](../decisions/006-embedded-ansible-execution.md) 與
  [decision 016](../decisions/016-temporal-operation-orchestration.md) 的 air-gap 目標。
- GHCR 只放 swallow 建置的 image、官方 image 以上游 digest 原樣使用、共用 PostgreSQL 的中性命名：見
  [decision 051](../decisions/051-ghcr-holds-only-swallow-images.md)。

---

## 5. One-command installation

### 5.1 `install.sh`

- 以 `curl -fsSL <release>/install.sh | sudo bash -s -- [--address ADDRESS] [--dir DIRECTORY] [--version VERSION]`
  執行；升級是同一個指令搭配較新的 release。每個選項也可用 environment 設定：`SWALLOW_ADDRESS`、
  `SWALLOW_INSTALL_DIR`（預設 `/opt/swallow`）、`SWALLOW_VERSION`（預設為腳本隨附的 release，可帶前置
  `v`）。`SWALLOW_RELEASE_URL` 可把 GitHub 下載位置換成 mirror。
- 整支腳本在 `main` 中執行，讓 `curl | bash` 先讀完全部內容，再執行任何指令。
- 檢查 root、x86_64 與 Ubuntu 24.04；缺少時安裝 `curl` 與 `zstd`。
- 下載 `swallow-compose-<version>.tar.zst` 與 `SHA256SUMS`，以 `sha256sum --strict` 核對，不符即停止。
- 解壓到安裝目錄，取代 bundle 內的檔案；`.env`、`secrets/`、`backups/`、`state/` 不在 bundle 中，因此保留。
- 有 `--address` 時以 `SWALLOW_ADDRESS` 傳給 `swallowctl`，執行
  `production/swallowctl <install|upgrade> --release-manifest <dir>/release-manifest.json`。

### 5.2 install 或 upgrade

- `production/state/installed` 記錄的版本非空、且與要安裝的版本不同 → `upgrade`。
- 否則 → `install`：第一次安裝、同版本重跑，或中途停止的安裝（例如 MAAS 匯入 image 時中斷）都走 `install`
  並收斂。
- 判斷只看 installed marker，不看 `.env` 中的版本。
- 沒有 marker 的既有 installation（早於此機制）第一次執行 `install.sh` 時走 `install`，收斂後寫入 marker。

### 5.3 設定持久化與位址推導

- `install` 與 `upgrade` 把 environment 中有設定的 operator settings 寫入 `.env`（environment 優先，與
  Compose 相同），讓之後的 `upgrade` 或直接的 `docker compose up` 看到相同的值：`SWALLOW_ADDRESS`、
  `SWALLOW_MAAS_URL`、`SWALLOW_BOOT_MEDIA_BASE_URL`、`SWALLOW_HTTP_PORT`、`SWALLOW_POSTGRES_HOST_PORT`、
  `SWALLOW_MAAS_IMAGE_TIMEOUT`、`SWALLOW_SITE_NAME`、`SWALLOW_TEMPORAL_UI_IMAGE`、`SWALLOW_TEMPORAL_UI_PORT`、
  `SWALLOW_NTP_UPSTREAM`、`SWALLOW_NTP_ALLOW`。多行的值（例如 `SWALLOW_SSH_KNOWN_HOSTS`）不寫入 `.env`。
- 接著只補 `.env` 缺少的位址衍生值，不改已有的值：

| 設定 | `.env` 沒有時的值 |
| --- | --- |
| `SWALLOW_ADDRESS` | `SWALLOW_MAAS_URL`（`.env` 中的值，或 MAAS 初始化時記在 `state/local-maas` 的 URL）的 host；都沒有時為主機的 primary IPv4（預設路由的來源位址）；仍無法判斷即失敗，要求設定 |
| `SWALLOW_MAAS_URL` | MAAS 初始化時記錄的 URL，否則 `http://<SWALLOW_ADDRESS>:5240/MAAS` |
| `SWALLOW_BOOT_MEDIA_BASE_URL` | `http://<SWALLOW_ADDRESS>`；`SWALLOW_HTTP_PORT` 不是 80 時加上 `:<port>` |

- `SWALLOW_ADDRESS` 是 machines 與 BMC 連到這台主機的位址。Compose 以它作為 api-server 與 worker 的
  `SWALLOW_API_NTP_SERVER`（§6.3）。
- 第一次安裝後**不支援變更位址**：MAAS 保留第一次初始化時的 URL。

### 5.4 共置 MAAS 的 Integration endpoint

- Bootstrap 以 `SWALLOW_MAAS_URL`（主機自己的位址）註冊共置 MAAS，而不是 `host.docker.internal`；API
  containers 經 `egress` 連到主機位址。`SWALLOW_MAAS_ENDPOINT` 可覆寫，但不持久化。
- Dashboard 的 Build Boot ISO 對話框以 provisioner Integration endpoint 的 host 預填 rack address，因此預填值
  是 BMC 開機的 machine 能連到的位址。
- 在此規則之前註冊的 Integration 保留原本的 endpoint：bootstrap 重跑只替換 credential（§2.5）。

### 設計理由

`install.sh` 作為 bootstrapper 而 `swallowctl` 仍是唯一 lifecycle 工具、以 installed marker 而非 `.env` 版本選擇
upgrade、以 Integration endpoint 而非 Dashboard 修正 rack 預填：見
[decision 052](../decisions/052-one-command-installation.md)。

---

## 6. 時間同步

### 6.1 Control-Plane NTP Server

- Installation 在 swallow 主機提供唯一一個 NTP service：[Control-Plane NTP Server](glossaries/terms/control-plane-ntp-server.md)，
  UDP 123，Servers 以 `SWALLOW_ADDRESS` 連到它。它是 host service，從不是 container。主機防火牆必須允許
  machine networks 連 UDP 123。
- 提供方式依 provisioner（`local-ntp.sh install`，冪等）：
  - 主機上有 MAAS snap（production installation 一定有）：什麼都不裝。MAAS 自己的 chronyd 已佔用 UDP 123
    並允許所有 client，它就是 server；不在旁邊另裝第二個 chronyd。
  - 主機上沒有 MAAS snap：安裝發行版 `chrony`，停用主機的 `systemd-timesyncd`，並擁有
    `/etc/chrony/chrony.conf`：上游（§6.2）、`allow`（所有 client，或 `SWALLOW_NTP_ALLOW` 列出的 subnets）、
    `local stratum 10`（所有上游都連不到時仍以本機時鐘服務，讓 Servers 彼此一致）與 `makestep 1.0 3`。
- `local-ntp.sh check`（唯讀，`doctor` 使用）：UDP 123 有服務；MAAS 時 `ntp_servers` 等於設定的上游且
  `ntp_external_only=false`；host chrony 時服務在執行且設定檔與渲染結果一致。是否已同步到上游**只是資訊**：
  以本機時鐘服務的 server 仍讓所有 Servers 時間一致。

### 6.2 上游

- 上游是 installation 設定 `SWALLOW_NTP_UPSTREAM`：IP 位址或 host name，以逗號或空白分隔、依序使用；未設定
  即預設 `ntp.ubuntu.com`（MAAS 自己的預設）。每一項必須是 IP 位址或 host name，`SWALLOW_NTP_ALLOW` 的每一項
  必須是位址或 subnet，否則拒絕；因此設定值無法注入 chrony directive 或 MAAS CLI 參數。
- 渲染方式與 MAAS 相同：IP 位址為 `server <ip> iburst`，名稱為 `pool <name> iburst`。
- `local-ntp.sh upstream`（冪等）：
  - **MAAS**：與 MAAS 的 `ntp_servers` 不同時寫入；強制 `ntp_external_only=false`，讓 MAAS 繼續把本主機交給
    它佈署的 machines 當 NTP server。接著最多等 180 秒，直到 MAAS 的 chrony 設定列出所有指定來源、chronyd
    選定其中一個上游；此時主機時鐘與上游差超過 1 秒就以 `chronyc makestep` step 一次，而不是讓 chronyd
    以最大速率 slew（MAAS 的 chrony 設定沒有 `makestep`）。
  - **host chrony**：重寫設定檔，有變更才重啟（重啟會重新套用 `makestep`）。
- swallowctl 擁有 MAAS 的 `ntp_servers`：在 MAAS UI 設定的上游，會在下一次 `install`、`upgrade` 或 `ensure`
  被 `SWALLOW_NTP_UPSTREAM`（未設定時為預設值）取代。
- `swallowctl ntp upstream`：
  - `show`（預設）：顯示設定的上游並執行 `local-ntp.sh check`。
  - `set SERVER...`：先套用，成功才寫入 `.env`；被拒絕的值不會進 `.env`。
  - `default`：先套用預設上游，再從 `.env` 移除 `SWALLOW_NTP_UPSTREAM`。
- `install` 與 `upgrade` 一律執行 `install` 與 `upstream`；`ensure` 在 check 失敗時執行。

**不變式：上游不得從 swallow 主機取得時間**——不論是直接取得，或經由以本主機時鐘計算時間的 gateway。
本主機跟隨上游，兩者會互相追逐。

### 6.3 OS provision 之後的 `configure-ntp`

- 每個 `provision-<serverId>` Task 之後都有自己的 `configure-ntp-<serverId>`，只依賴該 Server 的
  `provision-<serverId>`，所以一台佈署失敗不會擋住同批其他 Server：
  - **Deploy OS Workflow**：Task 不帶 Job（與 `provision-os` Task 相同）。
  - **Platform deployment 的 `ensure-os` Job**（`provision_os` 模式）：`install-platform` 依賴它，見
    [platform-deployment.md](platform-deployment.md) §2.1。
- 它是 `ansible` Runner 的 Task，執行 release manifest 中的 playbook `configure-ntp`；launcher 直接指名
  playbook，不需要 Site `playbookMappings`。Workflow kind `configure-ntp` 只用來標示這個 Task，從不是獨立的
  Workflow。
- NTP server 來自 api-server 設定 `SWALLOW_API_NTP_SERVER`（production Compose 帶入 `SWALLOW_ADDRESS`），在
  受理時凍結為 trusted variable `swallow_ntp_servers`（單一元素的 list），operator 無法偽造。
- 不會被重新設定的 Server：Platform deployment 中沿用的 `deployed` Server，以及 `existing_os` 模式的所有
  targets。已佈署的 Server 保留原本的時間設定，直到再次被 provision。OS Image verification 不設定時間
  （它最後會把 Server 歸還）。
- Server 的 `deployment` 結果只反映它的 `provision-os` Task。
- 時間在 SSH 起來之後由 Ansible 設定；swallow 不合成 cloud-init 的 `ntp:` 段落。

`configure-ntp` playbook（role `ntp_client`）的行為：

1. 再次驗證 `swallow_ntp_servers` 只含 IP 位址或 host name（否則 `[ntp_servers_invalid]`）；只支援 Debian 與
   RHEL 家族。
2. 偵測 OS 已有的時間 client，依序選第一個存在的：`systemd-timesyncd` → `chrony` → `ntpd`（classic ntp 或
   ntpsec；binary、unit 與設定檔都要存在）。都沒有才安裝 chrony。`openntpd` 從不被選用，只會被停止。
3. 停止並停用其他已安裝的 client，讓只有一個 client 調整時鐘。
4. 寫入 swallow 擁有、只指向這些 server 的設定（`systemd-timesyncd` 為 drop-in
   `/etc/systemd/timesyncd.conf.d/swallow.conf`），啟用並啟動 client，設定變更時重啟。
5. 有界等待 client 收到回應（24 × 5 秒＝2 分鐘）；沒有同步就以 `[ntp_not_synchronized]` 失敗，Task 結果為
   `ansible_failed`。
6. 冪等：已收斂的主機重跑沒有變更。

`systemd-timesyncd` 仍優先使用 DHCP lease 提供的 server；在 MAAS 服務的 subnet 上，那是 MAAS rack，也就是
swallow 主機。

### 6.4 受理條件

- `SWALLOW_API_NTP_SERVER`（config `api.ntpServer`）不是 API process 啟動的必要條件；有設定時必須正好是一個
  IP 位址或 DNS name（去除前後空白，不得有 port、scheme 或選項），否則 process 啟動失敗。
- installation 沒有 NTP server 時，任何需要 provision OS 的請求——Deploy OS，以及有 Server 需要佈署 OS 的
  `provision_os` Platform deployment——在受理時、寫入 provider 之前以 `409 conflict` 拒絕，訊息指出
  `SWALLOW_API_NTP_SERVER`。
- `configure-ntp` 是 Ansible Task，所以 Site automation 不可執行時，同樣的請求在受理時以
  `503 provider_unavailable` 拒絕（可執行的條件見
  [site-automation](../../api-server/docs/development/api-contracts/api-server/site-automation.md) 契約）。
- 對外行為以契約為準：[provisioning](../../api-server/docs/development/api-contracts/api-server/provisioning.md)
  的 Time synchronization 與 [platforms](../../api-server/docs/development/api-contracts/api-server/platforms.md)
  的 Time sync check。

### 6.5 Platform 端的時鐘檢查

Platform deployment 不設定時間：各 Platform 的 install playbook 開頭以 role `time_sync_check` 檢查所有
targets，量測與 Control-Plane NTP Server 的 offset（上限 1 秒），由 deploy 欄位 `timeSyncCheck`（`enforce`
預設、`warn`）決定結果。完整規則見 [platform-deployment.md](platform-deployment.md) §2.2 與 glossary
[Time Sync Check](glossaries/terms/time-sync-check.md)。

### 設計理由

由 installation 提供 host 上的 NTP server、安裝與上游分開、每次 OS provision 後以 Ansible 設定 OS 自己的
client、Platform 只檢查不設定、cloud-init 合成列為備選：見
[decision 065](../decisions/065-control-plane-ntp-and-post-provision-time-sync.md)（含兩次修訂移除的 Site NTP
Configuration、共用的 `check-time-sync` Task 與一律安裝 chrony，以及促成「上游不得從本主機取得時間」的
`root123-intel` 事故）。

---

## 7. 程式與資產位置對照

### Installation 資產（`deploy/`）

| 職責 | 路徑 |
| --- | --- |
| Compose 拓樸（services、networks、volumes、secrets） | `deploy/production/compose.yaml` |
| Lifecycle 工具（install、upgrade、doctor、ensure、backup、restore、uninstall、ntp） | `deploy/production/swallowctl` |
| `.env` 讀取（environment 優先） | `deploy/production/env.sh` |
| Secrets 產生（只補不換） | `deploy/production/prepare-secrets.sh` |
| MongoDB replica set（keyFile、啟動、initiate 與 healthcheck） | `deploy/production/mongo-entrypoint.sh` |
| 共置 MAAS（role／database、snap、init、admin、virsh identity、`ubuntu/noble`） | `deploy/production/local-maas.sh` |
| Control-Plane NTP Server（install、upstream、check） | `deploy/production/local-ntp.sh` |
| Installation records（Site、Integration、automation 預設） | `deploy/production/bootstrap.sh` |
| Dashboard Nginx 設定 | `deploy/production/nginx.conf`（由 `deploy/production/dashboard.Dockerfile` 放入 image） |
| Image 建置 | `deploy/production/api-server.Dockerfile`、`dashboard.Dockerfile`、`cli.Dockerfile` |
| Native 範本（未發佈） | `deploy/production/native/` |
| 一行指令 installer | `deploy/release/install.sh` |
| 發佈、組裝、發佈前檢查 | `deploy/release/publish.sh`、`assemble-release.sh`、`validate.sh` |
| 官方 runtime image 的 digest pin | `deploy/release/runtime-images.env` |
| Release manifest 範例 | `deploy/release/release-manifest.example.json` |
| 第三方離線 media manifest | `deploy/third-party/offline-media-manifest.json` |

### 時間同步（`api-server`）

| 職責 | 路徑 |
| --- | --- |
| `SWALLOW_API_NTP_SERVER`（`api.ntpServer`）與驗證 | `config/config.go`、`config/env.go`、`config/validate.go` |
| `configure-ntp-<serverId>` 組裝與 Platform 時鐘檢查 vars | `internal/app/ntp_planner.go` |
| Deploy OS 的呼叫點 | `internal/app/provisioning_operation_adapter.go` |
| Platform `ensure-os` 的呼叫點 | `internal/app/platform_deployment_adapter.go` |
| `ErrNTPServerMissing` 與其 409 對應 | `internal/core/provisioning/domain/machine.go`、`internal/core/provisioning/delivery/handler.go`、`internal/core/platform/delivery/handler.go` |
| Workflow kind `configure-ntp` | `internal/core/workflow/domain/operation.go` |
| Playbook 與 role | `modules/shared/playbooks/configure-ntp.yml`、`modules/shared/roles/ntp_client`（client 順序在 `vars/`、等待預算在 `defaults/`、設定 template 在 `templates/`） |
| Schema migration（`swallow-api migrate`） | `cmd/swallow-api/migrate.go`、`internal/adapters/store/mongodb/migration/` |

---

## 8. 相關文件

- 決策：[decision 040](../decisions/040-single-vm-production-installation.md)、
  [decision 050](../decisions/050-compose-only-release-artifacts.md)、
  [decision 051](../decisions/051-ghcr-holds-only-swallow-images.md)、
  [decision 052](../decisions/052-one-command-installation.md)、
  [decision 065](../decisions/065-control-plane-ntp-and-post-provision-time-sync.md)；背景：
  [decision 001](../decisions/001-system-ownership-boundaries.md)、
  [decision 006](../decisions/006-embedded-ansible-execution.md)、
  [decision 016](../decisions/016-temporal-operation-orchestration.md)、
  [decision 058](../decisions/058-deployment-dhcp-and-subnet-dns.md)（swallow 不合成 cloud-init）。
- 語言：glossary [Installation](glossaries/terms/installation.md)、
  [Control-Plane NTP Server](glossaries/terms/control-plane-ntp-server.md)、
  [Time Sync Check](glossaries/terms/time-sync-check.md)、[Site](glossaries/terms/site.md)、
  [Integration](glossaries/terms/integration.md)、[OS Image](glossaries/terms/os-image.md)、
  [OS Deployment](glossaries/terms/os-deployment.md)、[Deployment Key](glossaries/terms/deployment-key.md)、
  [Automation Configuration](glossaries/terms/automation-configuration.md)、
  [Workflow](glossaries/terms/workflow.md)、[Task](glossaries/terms/task.md)。
- 規格：[platform-deployment.md](platform-deployment.md)（`ensure-os` 的 Task 組成與 platform 端時鐘檢查）。
- API 契約（`api-server` 擁有）：
  [provisioning](../../api-server/docs/development/api-contracts/api-server/provisioning.md)、
  [platforms](../../api-server/docs/development/api-contracts/api-server/platforms.md)、
  [site-automation](../../api-server/docs/development/api-contracts/api-server/site-automation.md)、
  [sites-integrations](../../api-server/docs/development/api-contracts/api-server/sites-integrations.md)。
