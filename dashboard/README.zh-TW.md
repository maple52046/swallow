# swallow dashboard

[English](README.md) · [專案文件](../docs/zh-TW/README.md)

`dashboard` 是 swallow React operator console，也是 provider-owned HTTP contract
的 conformist consumer，只綁定 real API adapter。

## Active routes

| Route | 用途 |
| --- | --- |
| `/login` | Authenticate |
| `/` | Site-scoped overview 與 attention items |
| `/servers`、`/servers/:id/*` | Inventory、action、activity、monitoring、network、storage、PCI |
| `/provisioning/templates` | Deployment Template |
| `/provisioning/images` | OS image、upload、delete、verification |
| `/platforms`、`/platforms/:id` | Kubernetes／Slurm Platform inventory 與 detail |
| `/platforms/deploy`、`/platforms/settings` | Platform deployment 與 Slurm requirement |
| `/software` | Managed Software Assignment 與 action |
| `/workflows`、`/workflows/:id` | Durable Workflow list、event、log 與 control |
| `/monitoring` | Alert、silence、metric、health 與 Grafana |
| `/infrastructure/*` | Site、Integration、Zone、Pool |
| `/account/ssh-keys`、`/account/api-keys` | 你的 SSH key 與 API key（account menu） |

`/clusters`、`/operations` 與已退役的 `/provisioning/deploy` 頁面是
compatibility redirect。

Monitoring（`/monitoring`、Server Monitoring tab、共用頁面上的 health）、OS image upload 與
Deployment Template 仍在開發中。Release build（`npm run build`）會隱藏它們：route 顯示 Not Found
或 redirect，health 顯示「Not available in this release」。Dev server 顯示全部功能；可在
**Account menu → Experimental features** 逐項關閉以預覽 release 畫面，選擇保存在各瀏覽器。

## UI model rules

- Server provisioning、Platform membership、health 是獨立 status axis，沒有
  combined status badge。
- `null` 表示 unknown，不會被默認成 bad。
- Provider sync 與每個 observed axis 都可能 stale，必須顯示 age。
- Opaque ID 才是 identity；hostname／address 不保證唯一。
- Screen 只呈現 Active provider contract 支援的 behavior。

## Development

建議執行完整 stack：

```bash
cd ../deploy/dev
docker compose up -d
```

開啟 <http://localhost:5173>。要對既有 API 單獨啟動 dev server：

```bash
npm ci
VITE_API_BASE_URL=http://127.0.0.1:30051 npm run dev
```

## 驗證

```bash
npm run lint
npm run build
npm run test:e2e
npm run test:e2e:production   # 以 vite preview 驗證 release build 隱藏開發中功能
npm run review:visual -- --grep servers
npm run review:visual:themes -- --grep servers
npm run review:visual -- --grep @representative
```

Playwright 包含 deterministic functional operator journey。Visual review 是獨立
developer workflow：`review:visual` 產生 dark desktop/mobile screenshots，
`review:visual:themes` 另加 light mode。必須實際開啟並檢查
`test-results/visual-review/` 下的 disposable images；它們不是 pixel baseline，
也不得 commit。所有 browser workflow 都使用 synthetic fixture data，不得替換成
customer data capture。`@representative` filter 會選取 login、overview、servers 與 server detail，供
shared layout、navigation、typography 或 theme change 使用。

## Architecture

```text
src/
├── domain/           framework-free concept 與 invariant
├── application/      port 與具有 real logic 的 use case
├── infrastructure/   HTTP 與 browser-persistence adapter
├── presentation/     route、page、component、hook、context
└── di/               composition root
```

Presentation code 不得 import infrastructure implementation。API DTO 留在
infrastructure，向內穿越 boundary 前完成 mapping。請見 [AGENTS.md](AGENTS.md)、
[architecture specification](docs/development/architecture-spec.md) 與
[coding style](docs/development/coding-style.md)。

## Contracts

[api-server contract outline](../api-server/docs/development/api-contracts/api-server/outline.md)
定義 route、field、error、auth 與 compatibility。Domain language 來自 root
[glossary](../docs/development/glossaries/README.md)。Dashboard 不得把 private
backend behavior 或 mock 變成 de-facto contract。
