# Local Development Environment

## Purpose

以 Docker Compose 啟動整個 platform 的本機開發環境：MongoDB 加上每個 component，
每個 component 都以 bind mount 掛入自己的原始碼並支援 hot reload。

toolchain（Go、Node）全部封裝在 container 內，host 只需要 Docker，
不需要另外安裝 Go 或 Node。

## Prerequisites

- Docker Engine 與 Compose plugin。
- 目前使用者屬於 `docker` group（`id -nG` 應包含 `docker`；剛加入時需重新登入才生效）。

## Quick Start

```bash
cd deploy/dev
docker compose up -d
```

首次啟動需要下載 Go modules 與 npm 套件，約 1–2 分鐘；之後兩者都會落在
named volume 與 `dashboard/node_modules`，重啟即為秒級。

啟動後：

| Service      | Host port      | 說明                                            |
|--------------|----------------|-------------------------------------------------|
| `dashboard`  | 5173           | Vite dev server，含 HMR                          |
| `api-server` | 30051          | Swallow HTTP API                                 |
| `mongo`      | 27017          | MongoDB 8，保存 domain state 與 query projections |
| `temporal`   | 7233           | Durable Operation workflow server               |
| `temporal-ui` | 8233          | 開發診斷 UI；Dashboard 仍是 operator 入口        |
| `temporal-postgresql` | container | Temporal core 與 visibility databases        |
| `worker`     | container      | 執行 versioned Operation workflows              |
| `ansible-executor` | container | 執行具 idempotency key 的 Ansible attempts     |
| `prometheus` | 9090           | 透過 Swallow http_sd 抓 exporter                 |

預設帶入的 admin 帳號為 `admin` / `admin`，於 api-server 首次啟動時建立。

## 常用指令

```bash
docker compose ps                      # 服務狀態
docker compose logs -f api-server      # 追蹤單一服務日誌
docker compose restart dashboard       # 重啟服務（改動環境變數後需要）
docker compose down                    # 停止並移除 container，保留資料
docker compose down -v                 # 連 MongoDB 資料與快取 volume 一併清除
docker compose build --no-cache        # 重建開發映像（改動 Dockerfile 後）
```

## Configuration

所有設定在 `compose.yaml` 中都帶有開發用預設值，因此**不需要 `.env` 也能直接啟動**。
需要覆寫時才 `cp .env.example .env` 並修改；`.env` 已被 gitignore。

其中兩項較常需要調整：

- `DEV_UID` / `DEV_GID` — container 內執行身分。必須與 component 目錄的擁有者一致，
  否則 container 寫入 bind mount 的檔案（如 `node_modules`）在 host 端會無法編輯。
  預設 `1001:1001`；修改後需重建映像。
- `VITE_API_BASE_URL` — 預設留空，dashboard 透過 Vite 的同源 `/api` proxy 存取
  `api-server:30051`。只有刻意測試 split-origin 時才覆寫。
- `SWALLOW_API_CREDENTIAL_KEY` — 用來加密 integration 憑證的 base64 32-byte 金鑰，**必填**。
  compose 帶了一個開發用預設值；正式 installation 必須自己產生（`openssl rand -base64 32`）。
  換掉這個金鑰會讓既有的已存憑證無法解密，等於要重新輸入所有 integration 憑證。
- `SWALLOW_API_MACHINE_TOKEN` — 給「呼叫者是機器」的端點用的靜態 bearer token：
  Prometheus 抓 metrics/discovery，或外部 Ansible 工具抓 inventory。
  只有那些端點接受它，不是進入其餘 API 的第二條路。

**MAAS 不再是環境變數。** 它是 per-site runtime integration。Ansible 則不是
integration：每個 site 透過 `/sites/{id}/automation` 設定 SSH policy、manifest
playbook mapping 與 write-only credential。

## Hot Reload 行為

- **api-server** — 由 [air](https://github.com/air-verse/air) 監看 `api-server`，
  `.go` / `.yaml` 變更即重新編譯並重啟。編譯產物寫在 container 的 `/tmp/air`，
  不會弄髒 working tree。設定見 [`air.toml`](air.toml)。
- **dashboard** — Vite HMR，`dashboard` 的變更立即反映在瀏覽器。
  `package-lock.json` 較 `node_modules` 新時，entrypoint 會自動重跑 `npm ci`。
- **worker / ansible-executor** — 各自使用獨立 Air 設定監看 Go、manifest 與 playbook
  變更。API 不在 process 內執行新的 durable Operations；必須同時保持這兩個服務與
  Temporal 可用。

api-server 的 Go 版本刻意固定在 `api-server/go.mod` 宣告的 1.25；
air 因為需要較新的 compiler，改由獨立的 build stage 編譯後複製進來。

## 從其他機器連入

Vite 與 API 都綁在 `0.0.0.0`，直接用這台的 IP 開 `http://<host-ip>:5173` 即可，
不需要改任何設定：Vite 會以同源方式代理 API。

透過 SSH tunnel 只需轉發 dashboard port：

```bash
ssh -L 5173:localhost:5173 <user>@<host>
```

Vite 預設只信任以 IP 或 localhost 形式送來的 Host header；
若要用網域名稱存取，需在 `dashboard/vite.config.ts` 設定 `server.allowedHosts`。

## 註冊 integration

api-server 啟動後本身不知道任何外部系統，要用 API 註冊。以 MAAS 為例：

```bash
API=http://127.0.0.1:30051/api/v1
TOKEN=$(curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | jq -r .accessToken)

SITE=$(curl -s -X POST $API/sites -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"dc-east"}' | jq -r .id)

curl -s -X POST $API/integrations -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d "{
    \"siteId\":       \"$SITE\",
    \"kind\":         \"provisioner\",
    \"providerKind\": \"maas\",
    \"name\":         \"maas-east\",
    \"endpoint\":     \"http://10.0.0.5:5240/MAAS\",
    \"credential\":   \"<consumer>:<token>:<secret>\"
  }"
```

註冊之後 reconciler 會在下一輪（預設 60 秒）把 MAAS 的 machine 投影成 server，
或用 `POST $API/provisioning/reconcile` 立刻跑一次。

憑證寫進去之後**讀不回來**，任何回應都不會包含它；`hasCredential` 只告訴你有沒有存。
`GET $API/integrations` 的 `sync` 欄位會顯示上次同步的時間與錯誤，連不上 MAAS 時
`lastSucceededAt` 會保持舊值而 `lastError` 有內容——這樣看得出資料有多舊。

其他 external kind 同樣方式註冊：`metrics`/`prometheus`、
`platform`/`kubernetes`、`platform`/`slurm`。Automation 使用 site-scoped API，
不是 external integration。

## Prometheus 監控 demo

compose 內含一個 `prometheus` 服務，透過 swallow 的 http_sd（`/api/v1/discovery/prometheus`）
抓每台 server 的 node-exporter（:9100）與 AMD GPU server 的 rdc-exporter（:5000，tag `amd-gpu`），
scrape 到的序列自帶 `server_id`、`site` 與 canonical `platform_id` 標籤；deprecated
`cluster` 標籤保留一個版本供既有查詢遷移。設定檔為 [`prometheus.yml`](prometheus.yml)。

一鍵種子（登入、建立 site、註冊指向 in-compose Prometheus 的 metrics 整合、設定 automation
與 exporter playbook 對應、可選註冊 MAAS）：

```bash
cp .env.example .env          # 填入 MAAS URL/key 與 SSH key（見檔內註解）
docker compose up -d
bash seed.sh
```

`seed.sh` 會冪等執行；未提供 MAAS 或 SSH 值時會略過對應步驟並印出提示。OS 佈署完成
（reconcile 偵測到 `deployed`）後，平台會自動對該機建立 `install-exporters` operation；
被鎖定的機器視為 `unmanaged`，不會被自動安裝。

## Troubleshooting

- **`permission denied ... /var/run/docker.sock`** — 目前 shell 尚未套用 `docker`
  group。重新登入，或以 `sg docker -c "docker compose ps"` 暫時取得群組身分。
- **`npm warn allow-scripts ... esbuild`** — 可忽略。esbuild 透過
  optionalDependencies 取得平台 binary，被擋下的 postinstall 只是備援路徑。
  若要執行 Playwright 測試，需另外在容器內安裝瀏覽器。
- **改了環境變數但沒生效** — Vite 的 `import.meta.env` 在 dev server 啟動時就已固定，
  請 `docker compose restart dashboard`。
- **api-server 一直重啟** — 多半是編譯錯誤，`docker compose logs api-server`
  會直接顯示 air 的 build 輸出。
