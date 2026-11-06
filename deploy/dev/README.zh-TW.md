# 本機 development environment

[English](README.md) · [快速入門](../../docs/zh-TW/getting-started.md)

Development Compose project 會從 source 啟動完整 swallow topology，並提供 hot
reload。Host 只需要 Docker Engine 與 Compose plugin；Go、Node、MongoDB、
Temporal、Python 與 Ansible toolchain 都在 container 內。

## Quick start

```bash
cd deploy/dev
docker compose up -d
docker compose ps
```

開啟 <http://localhost:5173>；API 位於 <http://localhost:30051>。
Local-only bootstrap account 是 `admin` / `admin`。

## Services

| Service | Host port | 用途 |
| --- | ---: | --- |
| `dashboard` | 5173 | Vite dev server 與 same-origin API proxy |
| `api-server` | 30051 | swallow HTTP API |
| `mongo` | 27017 | swallow-owned state 與 projection；single-node replica set `rs0`（從 host 連線用 `mongodb://localhost:27017/?directConnection=true`） |
| `temporal` | 7233 | durable orchestration |
| `temporal-ui` | 8233 | development diagnostics |
| `temporal-postgresql` | internal | Temporal databases |
| `worker` | internal | Workflow／activity execution |
| `ansible-executor` | internal | Idempotent Ansible attempt 與 event |
| `prometheus` | 9090 | Site 的 Prometheus：swallow 自己的 metrics（`swallow-api`）與 swallow 確知已安裝的 exporter（`swallow-exporters`，來自 swallow discovery） |
| `prom-label-proxy` | 9091（只在 loopback） | 只看得到一個 Platform 的 series 的 Site Prometheus：`X-Swallow-Platform-Id` header 中的 `platform_id`；API 以 `http://prom-label-proxy:8080` 連到它 |
| `pulp-db`、`pulp-init` | 無（一次性） | 每次 `up` 都執行：在 `temporal-postgresql` 建立 Pulp 的 role 與 database `pulp`，再補上 Pulp 的金鑰、遷移 schema、設定 admin 密碼，並加上 deb 簽章服務 |
| `pulp-api`、`pulp-content`、`pulp-worker` | internal | 開發 Site 的 [Local Repository](../../docs/development/local-repository.md)（Pulp 3）：API、content app 與兩個 worker |
| `pulp-web` | 8080（`PULP_HTTP_PORT`，每個介面） | Local Repository 唯一的位址：content、registry（`/v2/`）、Python index（`/pypi/`）與 Pulp 的 API；lab 的 Server 從這裡取得每個 artifact，API 以 `http://pulp-web:8080` 連到它 |

## 常用指令

```bash
docker compose logs -f api-server worker ansible-executor
docker compose restart dashboard
docker compose down
docker compose down -v       # destructive：移除 local data 與 cache
docker compose build --no-cache
```

## 檢查與恢復

`swallowctl` 以與 production installation 相同的健康契約（不含共置 MAAS、安裝紀錄，以及
installation Site 的 exporter discovery；這裡的 `prometheus.yml` 改以一個涵蓋所有 Site 的 job 取代）
檢查並收斂這個 stack：

```bash
../production/swallowctl doctor --profile dev
../production/swallowctl ensure --profile dev
```

主機 reboot 後所有容器會同時重啟，Air 可能讓容器維持運作、但它監管的進程已經退出，例如在
Temporal 就緒前啟動的 worker。`ensure` 會啟動沒在執行的 services、重啟檢查失敗的 services，
最後執行 `doctor`；無法修復的檢查會連同該 service 最近的 log 一起印出。它不需要 root。

## Configuration

沒有 `.env` 也能啟動。只有覆寫 default 時才建立：

```bash
cp .env.example .env
```

重要設定：

- `DEV_UID` / `DEV_GID` 應與 source-tree owner 相同。
- `VITE_API_BASE_URL` 預設為空，Vite 以 same-origin proxy 轉送 `/api`；
  修改後需 restart Dashboard。
- `SWALLOW_API_CREDENTIAL_KEY` 加密 write-only credential。Bundled value
  只適合本機；更換後既有 credential 無法解密。
- `SWALLOW_API_MACHINE_TOKEN` 只用於 Prometheus discovery/metrics 等 machine
  endpoint，不是 user API 的替代 login。

所有 bundled password、token、TLS material、key 都只能用於 development。

## Hot reload

- API、worker、Ansible executor 使用獨立 Air process。每個都監看 `api-server/` 的 `.go`、`.yaml`、`.yml`
  變更（不含測試與文件），所以這類變更會 rebuild 並重啟三個 process；重啟 Ansible executor 會中斷執行中的
  playbook。`modules/manifest.json` 不在監看範圍內，修改後要手動重啟這三個 service。
- Dashboard 使用 Vite HMR；`package-lock.json` 變更時 entrypoint 會 refresh dependencies。
- Build artifact／cache 留在 container 或 named volume，不污染 tracked source。

Workflow 需要 Temporal、worker、Ansible executor 同時 available；API process
沒有 execution fallback。

## 從其他機器存取

Development 的 Dashboard／API listen all interfaces。可開啟
`http://<host-ip>:5173`，或只 tunnel Dashboard：

```bash
ssh -L 5173:localhost:5173 user@host
```

Vite 預設信任 IP／localhost host header。使用 development DNS name 前，需明確
設定 `server.allowedHosts`。

## Boot Media lab

要對實際 BMC 試用 Boot Media（Redfish virtual media iPXE 開機，[decision 047](../../docs/decisions/047-redfish-boot-media.md)（redfish boot media）），請在 `.env`
設定 BMC 網段連到這台主機的位址：

```bash
SWALLOW_API_BOOT_MEDIA_BASE_URL=http://10.0.0.5
BOOT_MEDIA_HTTP_PUBLISH=10.0.0.5:80
docker compose up -d api-server
```

開啟 **Provisioning → Boot ISOs**，為 provisioner 建置 Boot ISO；不再需要把 ISO 放進此目錄。
建置完成的 ISO 會存放於 git 已忽略的 `boot-media/<isoId>/` 目錄，並透過
`<SWALLOW_API_BOOT_MEDIA_BASE_URL>/boot-media/ipxe/<isoId>/swallow-ipxe.iso`
提供。API 也會發布到所設位址的 port 80，因為許多 BMC 只接受 port 80 的 plain HTTP。

修改 api-server 的 Go 程式會讓 worker 重啟（Air），使進行中佈署的 lease 失效；lab 佈署進行時請避免修改。

## Control-plane NTP

每次 OS provisioning 都會在 SSH 就緒後，把新的 Server 指到這台主機的 NTP server
（`configure-ntp` Task，[decision 065](../../docs/decisions/065-control-plane-ntp-and-post-provision-time-sync.md)（control plane ntp and post provision time sync））；各 Platform 的 install playbook 在安裝前會檢查各節點的時鐘。
`.env` 的 `SWALLOW_API_NTP_SERVER` 是 Server 連到這台主機 NTP server 的位址（不是它的上游）；dev
環境無法自動推得，所以它是空的時候，所有 OS 佈署都會被拒絕。

主機上的 NTP server 就是 Control-Plane NTP Server。有本機 MAAS snap 時是 MAAS 自己的
chronyd，不另外安裝；否則由 `local-ntp.sh install` 安裝 host chrony。上游另外以
`local-ntp.sh upstream` 設定（production installation 使用 `swallowctl ntp upstream`），沒有設定
就是 `ntp.ubuntu.com`。網路連不到公開的 NTP 時，把上游指到它連得到的時間來源（寫成
`server <address> iburst`），並把 Server 連到這台主機的位址交給 Server。上游在 `192.0.2.3`、這台主機在
`192.0.2.20` 時：

```bash
sudo SWALLOW_NTP_UPSTREAM=192.0.2.3 ../production/local-ntp.sh upstream
sudo SWALLOW_NTP_UPSTREAM=192.0.2.3 ../production/local-ntp.sh check
echo SWALLOW_API_NTP_SERVER=192.0.2.20 >> .env && docker compose up -d api-server worker
```

在 installation 之外，`local-ntp.sh` 以 `SWALLOW_MAAS_API_KEY`，或 MAAS 使用者
`SWALLOW_MAAS_ADMIN_USER`（預設 `admin`）的 key 設定 MAAS。沒有設定 `SWALLOW_NTP_UPSTREAM` 就執行
`local-ntp.sh upstream`，會把 MAAS 改回 default。

上游不能從這台主機取得時間。提供「所在主機的時鐘加上修正量」的 gateway（例如 NTP-over-HTTP
gateway）不能跑在這台主機上：MAAS 的 chronyd 會追一個用它自己在調整的時鐘算出來的目標，兩者一起偏離真實
時間。`local-ntp.sh upstream`
也會在與上游差超過一秒時先 step 一次，所以請用這支 script 設定上游，不要手動改 MAAS。

## Platform 監控位址

Slurm Platform 的 Grafana 經 API 的 Platform metrics 閘道讀取 Site Prometheus，而 Omnistat 只允許那套
Prometheus 收集時的來源位址。兩者都在 `.env` 設定：`SWALLOW_API_PUBLIC_URL` 是 Servers 連到這個 API 的 URL
（API 要發布在那裡，例如用 `BOOT_MEDIA_HTTP_PUBLISH`），`SWALLOW_API_MONITORING_SCRAPE_ADDRESSES` 列出
Servers 看到的這台主機的位址。任一個是空的時候，需要它的 Slurm 部署會在規劃時被拒絕。lab 的設定：

```bash
printf 'SWALLOW_API_PUBLIC_URL=http://10.170.168.20\nSWALLOW_API_MONITORING_SCRAPE_ADDRESSES=10.170.168.20,192.168.100.1\n' >> .env
docker compose up -d api-server worker
```

## Local Repository

開發站預設使用 Site 的 Local Repository（開發環境的 Pulp）：部署從它取得套件、檔案、container image 與 Python
套件，Local Repository 還沒 ready 時，這類部署在受理時就被拒絕。production 的 Site 則一開始從上游取得。
Pulp 以它的 origin 寫出絕對 URL，lab 的 Server 也從這個位址連到它，
所以要在 `.env` 把 `PULP_ORIGIN` 設成 Server 看到的、這台主機在 `PULP_HTTP_PORT` 的位址，並重建 Pulp 的 services；
預設的 `http://localhost:8080` 只有這台主機自己連得到。lab 的設定：

```bash
echo PULP_ORIGIN=http://10.170.168.20:8080 >> .env && docker compose up -d && bash seed.sh
```

設了 `PULP_ORIGIN` 時，`seed.sh` 會把 Pulp 登錄為 Site 的 `repository` Integration（provider kind `pulp`、endpoint
`http://pulp-web:8080`、setting `contentOrigin` 等於 `PULP_ORIGIN`，credential 是 Pulp 的 admin 密碼
`PULP_ADMIN_PASSWORD`，預設 `admin`），並在從未寫入過的 Local Repository spec 寫入：比照 production installation 的
Site `ubuntu` 來源（Ubuntu archive），以及 `serveDeployments` `true`（production installation 寫入 `false`）；見
[local-repository.md](../../docs/development/local-repository.md) §2.1、§4.1 與 §5.2。Ubuntu archive 的第一次同步要
一段時間，進度在 Infrastructure 的 Local Repository 頁面。要改從上游部署，在該頁把 **Artifact source for
deployments** 選為 **Upstream sources**；重跑
`seed.sh` 會保留你的選擇。沒有設 `PULP_ORIGIN` 時，Site 沒有 `repository` Integration，從上游部署。

## Node Exporter controller

在 production，worker 會讓每個啟用 automation 的 Site 中每台已部署、未 lock 的 Server 都保有 Node Exporter：
它自己啟動 `configure-node-exporter` Workflow，requestedBy 是 `swallow:node-exporter-controller`。這個
stack 與 lab 共用機器，所以這裡預設關閉（`compose.yaml` 中 `SWALLOW_WORKER_NODE_EXPORTER_ENSURE` 的預設是
`false`），worker 的 log 會說明它沒有註冊。只有在某次 lab 測試確實要讓它在那些機器上安裝時才打開，並重建
worker（它在 container 啟動時讀取這個變數）：

```bash
echo SWALLOW_WORKER_NODE_EXPORTER_ENSURE=true >> .env && docker compose up -d worker
```

## Seed demonstration Site

Idempotent seed script 會建立 deployment key（`swallow-api deployment-key ensure`，即安裝步驟；API 啟動時不會建立）、login、建立 Site、
把 in-Compose Prometheus 註冊為它的 metrics Integration，並以 in-Compose prom-label-proxy 為它的 `labelProxyUrl`
setting（與 production bootstrap 相同，從不建立第二個）、
設定 site automation，並在有提供資料時註冊 MAAS：

```bash
cp .env.example .env
docker compose up -d
bash seed.sh
```

未提供 MAAS／SSH input 時會略過並顯示提示。每次 OS 佈署都會在佈署的 Server 上安裝
Node Exporter；[Node Exporter controller](#node-exporter-controller) 關閉時，不是由 swallow 佈署的 Server
請以 Managed Software 安裝。swallow 不會在 locked
Server 上安裝任何東西。

若要以本機 MAAS snap 納管 libvirt 虛擬機，先給它的 rack 與 production 安裝相同的 SSH
身分（已安裝的主機上 `local-maas.sh ensure-virsh-identity` 做的就是這件事），再 seed 它的公鑰：

```bash
sudo install -d -m 0700 /var/snap/maas/current/root/.ssh
sudo test -s /var/snap/maas/current/root/.ssh/id_ed25519 ||
  sudo ssh-keygen -q -t ed25519 -N '' -f /var/snap/maas/current/root/.ssh/id_ed25519
printf '\n# BEGIN swallow virsh\nHost *\n  StrictHostKeyChecking accept-new\n# END swallow virsh\n' |
  sudo tee -a /var/snap/maas/current/root/.ssh/config >/dev/null
sudo cat /var/snap/maas/current/root/.ssh/id_ed25519.pub >/tmp/maas-virsh.pub
SWALLOW_MAAS_VIRSH_SSH_PUBLIC_KEY_FILE=/tmp/maas-virsh.pub bash seed.sh
```

手動設定請依[初始設定指南](../../docs/zh-TW/guides/initial-setup.md)。

## Troubleshooting

- **Docker socket permission denied：** 加入 `docker` group 後重新登入。
- **Dashboard environment change 未生效：** Restart `dashboard`。
- **API 持續 restart：** 查看 `docker compose logs api-server` 的 Air build/config error。
- **Workflow 沒有前進：** 執行 `../production/swallowctl ensure --profile dev`；它會收斂
  Temporal、worker 與 `ansible-executor`，並印出無法修復者的 log。
- **Integration data 過舊：** 查看 last success/error、驗證 endpoint／credential，再 reconcile。
