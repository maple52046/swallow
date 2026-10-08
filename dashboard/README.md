# swallow dashboard

[繁體中文](README.zh-TW.md) · [Project documentation](../docs/en/README.md)

`dashboard` is the React operator console for swallow. It is a conformist
consumer of the provider-owned HTTP contract and binds only real API adapters.

## Active routes

| Route | Purpose |
| --- | --- |
| `/login` | Authenticate |
| `/` | Site-scoped overview and attention items |
| `/servers`, `/servers/:id/*` | Inventory, actions, activity, monitoring, network, storage, PCI |
| `/provisioning/templates` | Deployment Templates |
| `/provisioning/images` | OS images, upload, delete, and verification |
| `/platforms`, `/platforms/:id` | Kubernetes/Slurm Platform inventory and detail |
| `/platforms/deploy`, `/platforms/settings` | Platform deployment and Slurm requirements |
| `/software`, `/software/:kind` | Managed Software catalog, deployments, and multi-Server installation |
| `/workflows`, `/workflows/:id` | Durable Workflow list, events, logs, and controls |
| `/monitoring` | Alerts, silences, metrics, health, and Grafana |
| `/infrastructure/*` | Sites, Integrations, Zones, and Pools |
| `/account/ssh-keys`, `/account/api-keys` | Your SSH keys and API keys (account menu) |

`/clusters`, `/operations`, and the retired `/provisioning/deploy` page are
compatibility redirects.

Monitoring (`/monitoring`, the Server Monitoring tab, health on shared pages), OS
image upload, and Deployment Templates are still in development. Release builds
(`npm run build`) hide them: their routes show Not Found or redirect, and health
values read "Not available in this release". The dev server shows everything; use
**Account menu → Experimental features** there to switch each one off and preview
the release view. The choice is stored per browser.

## Model rules visible in the UI

- Server provisioning, Platform membership, and health are separate status
  axes; there is no combined status badge.
- `null` means unknown, never implicitly bad.
- Provider sync and every observed axis can be stale and must show its age.
- Opaque IDs are identity; hostnames and addresses are not unique.
- Screens expose only behavior backed by an Active provider contract.

## Development

The recommended path runs the complete stack:

```bash
cd ../deploy/dev
docker compose up -d
```

Open <http://localhost:5173>. For a standalone dev server against an existing
API:

```bash
npm ci
VITE_API_BASE_URL=http://127.0.0.1:30051 npm run dev
```

## Verify

```bash
npm run lint
npm run build
npm run test:e2e
npm run test:e2e:production   # release build via vite preview: hidden features stay hidden
npm run review:visual -- --grep servers
npm run review:visual:themes -- --grep servers
npm run review:visual -- --grep @representative
```

Playwright includes deterministic functional operator journeys. Visual review is
a separate developer workflow: `review:visual` captures dark desktop/mobile
screenshots, while `review:visual:themes` also captures light mode. Open and
inspect the disposable images under `test-results/visual-review/`; they are not
pixel baselines and must not be committed. All browser workflows use synthetic
fixture data—never replace it with captured customer data. The
`@representative` filter selects login, overview, servers, and server detail
for shared layout, navigation, typography, or theme changes.

## Architecture

```text
src/
├── domain/           framework-free concepts and invariants
├── application/      ports and use cases with real logic
├── infrastructure/   HTTP and browser-persistence adapters
├── presentation/     routes, pages, components, hooks, contexts
└── di/               composition root
```

Presentation code must not import infrastructure implementations. API DTOs stay
in infrastructure and are mapped before crossing inward. See [AGENTS.md](AGENTS.md),
the [architecture specification](docs/development/architecture-spec.md), and
[coding style](docs/development/coding-style.md).

## Contracts

The [api-server contract outline](../api-server/docs/development/api-contracts/api-server/outline.md)
defines routes, fields, errors, auth, and compatibility. Domain language comes
from the root [glossary](../docs/development/glossaries/README.md). The Dashboard
must not turn private backend behavior or mocks into a de-facto contract.
