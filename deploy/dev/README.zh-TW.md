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
| `mongo` | 27017 | swallow-owned state 與 projection |
| `temporal` | 7233 | durable orchestration |
| `temporal-ui` | 8233 | development diagnostics |
| `temporal-postgresql` | internal | Temporal databases |
| `worker` | internal | Workflow／activity execution |
| `ansible-executor` | internal | Idempotent Ansible attempt 與 event |
| `prometheus` | 9090 | 使用 swallow discovery 的 local monitoring demo |

## 常用指令

```bash
docker compose logs -f api-server worker ansible-executor
docker compose restart dashboard
docker compose down
docker compose down -v       # destructive：移除 local data 與 cache
docker compose build --no-cache
```

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
  endpoint，不是 operator API 的替代 login。

所有 bundled password、token、TLS material、key 都只能用於 development。

## Hot reload

- API、worker、Ansible executor 使用獨立 Air process。Go、manifest、playbook
  變更只 rebuild owning process。
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

要對實際 BMC 試用 Boot Media（Redfish virtual media iPXE 開機，decision 047），把 iPXE ISO 放到
`boot-media/swallow-ipxe.iso`（已被 git 忽略），並在 `.env` 設定 BMC 網段連到這台主機的位址：

```bash
SWALLOW_API_BOOT_MEDIA_BASE_URL=http://10.0.0.5
BOOT_MEDIA_HTTP_PUBLISH=10.0.0.5:80
docker compose up -d api-server
```

API 也會發布到該位址的 port 80，BMC 由此掛載 `/boot-media/ipxe/swallow-ipxe.iso`。許多 BMC 只接受 port 80
的 plain HTTP。修改 api-server 的 Go 程式會讓 worker 重啟（Air），使進行中佈署的 lease 失效；lab 佈署
進行時請避免修改。

## Seed demonstration Site

Idempotent seed script 會建立 deployment key（`swallow-api deployment-key ensure`，即安裝步驟；API 啟動時不會建立）、login、建立 Site、註冊 in-Compose Prometheus Integration、
設定 automation/playbook mapping，並在有提供資料時註冊 MAAS：

```bash
cp .env.example .env
docker compose up -d
bash seed.sh
```

未提供 MAAS／SSH input 時會略過並顯示提示。觀察到 deployed Server 後，可依
ownership policy 自動安裝 exporter；locked Server 保持 unmanaged。

手動設定請依[初始設定指南](../../docs/zh-TW/guides/initial-setup.md)。

## Troubleshooting

- **Docker socket permission denied：** 加入 `docker` group 後重新登入。
- **Dashboard environment change 未生效：** Restart `dashboard`。
- **API 持續 restart：** 查看 `docker compose logs api-server` 的 Air build/config error。
- **Workflow 沒有前進：** 檢查 Temporal、worker、`ansible-executor` health/log。
- **Integration data 過舊：** 查看 last success/error、驗證 endpoint／credential，再 reconcile。
