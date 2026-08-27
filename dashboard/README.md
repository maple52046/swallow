# dashboard

Frontend for the Swallow operator console. React, TypeScript, Vite, and PatternFly 6
provide one shared design system across every domain workflow.

## What is here

Only screens backed by a real endpoint:

| Route | Shows |
|-------|-------|
| `/login` | Authentication |
| `/` | Fleet health, attention items, integrations, clusters, and recent operations |
| `/servers` | NetBox-style inventory filters and saved views with MAAS-style actions |
| `/servers/:id/*` | Cockpit-style machine summary, monitoring, network, storage, and PCI views |
| `/clusters` | Multi-cluster readiness and membership issues |
| `/clusters/:id` | Cluster members, roles, issues, and related operations |
| `/clusters/deploy` | Validated four-step cluster deployment wizard |
| `/operations` | Compact jobs list with URL-owned filters |
| `/operations/:id` | Stdout-first job detail, events, search, download, and retry |
| `/monitoring` | Current alerts, acknowledgement, fleet health, metrics, and Grafana link |
| `/sites`, `/integrations` | Scoped resource management |

Screens expose only behavior backed by the provider-owned API contracts. PatternFly owns
the visual language; Cockpit, NetBox, MAAS, Headlamp, Rancher, AWX, and Grafana inform the
information architecture without contributing their CSS, components, assets, or branding.

There are no mock repositories and no flag to switch to them. Every binding in
[`src/di/container.ts`](src/di/container.ts) is a real HTTP implementation.

## Three things to get right

**Null means unknown.** A server's status is three independent axes — `provisioning`,
`membership`, `health` — each owned by a different system and each `null` until observed.
Rendering `null` as a negative state is the most common way to get this model wrong: a
provisioner being unreachable does not make its servers unhealthy, and a server with no
metrics is not down.

For the same reason there is no combined status badge. A server that is deployed, in no
cluster, and not reporting metrics is either a spare awaiting allocation or a broken host,
and no rule can tell which.

**Show staleness.** The backend caches provisioner inventory and mirrors external state.
Integrations carry a `sync` object and every status axis carries `observedAt`. When a
sync has failed, say so and say how old the data is — the components already do this, and
new screens should too.

**`id` is the only identifier.** `hostname` and addresses are observed, mutable, and not
unique: two sites may both have `gpu-node-01` at `10.0.1.10`. Never key on them.

## Layout

```
src/
├── domain/           entities and their invariants; no framework imports
├── application/
│   └── ports/        repository interfaces the UI depends on
├── infrastructure/
│   ├── api/          HTTP implementations of the ports
│   └── persistence/  browser storage, for UI preferences only
├── presentation/     pages, components, layout, contexts
└── di/               container wiring the ports to implementations
```

Repositories are exposed from the container directly rather than behind pass-through use
cases. The boundary that matters is the port interface; a use case earns its own type when
it has logic of its own.

## Development

The dashboard runs as part of the platform dev stack rather than on its own, so that it
has an API to talk to:

```bash
cd ../../deploy/dev
docker compose up -d
```

Then open <http://localhost:5173>. See
[`deploy/dev/README.md`](../../deploy/dev/README.md).

Standalone, against an API you are running yourself:

```bash
npm install
VITE_API_BASE_URL=http://127.0.0.1:30051 npm run dev
```

```bash
npm run build    # tsc -b && vite build
npm run lint
```

## Contract

Request and response shapes are defined in
[`docs/api-contracts/README.md`](../../docs/api-contracts/README.md), and section 14 of
that document lists what changed from the previous model. The concepts behind the shapes
are in [`docs/glossaries/`](../../docs/glossaries), and the reasoning is in
[`docs/decisions/`](../../docs/decisions).

Read the decisions before adding a screen that stores or derives state. Several obvious
features — an alert acknowledged flag, stored metrics, a provisioning profile editor — are
ruled out there because another system owns them.
