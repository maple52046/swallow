# Local development environment

[繁體中文](README.zh-TW.md) · [Getting started](../../docs/en/getting-started.md)

The development Compose project starts the complete swallow topology from source
with hot reload. The host needs Docker Engine and the Compose plugin; Go, Node,
MongoDB, Temporal, Python, and Ansible tooling run in containers.

## Quick start

```bash
cd deploy/dev
docker compose up -d
docker compose ps
```

Open <http://localhost:5173>. The API is on <http://localhost:30051>.
The local-only bootstrap account is `admin` / `admin`.

## Services

| Service | Host port | Purpose |
| --- | ---: | --- |
| `dashboard` | 5173 | Vite dev server and same-origin API proxy |
| `api-server` | 30051 | swallow HTTP API |
| `mongo` | 27017 | swallow-owned state and projections |
| `temporal` | 7233 | durable orchestration |
| `temporal-ui` | 8233 | development diagnostics |
| `temporal-postgresql` | internal | Temporal databases |
| `worker` | internal | Workflow and activity execution |
| `ansible-executor` | internal | Idempotent Ansible attempts and events |
| `prometheus` | 9090 | local monitoring demo using swallow discovery |

## Common commands

```bash
docker compose logs -f api-server worker ansible-executor
docker compose restart dashboard
docker compose down
docker compose down -v       # destructive: removes local data and caches
docker compose build --no-cache
```

## Configuration

The stack starts without a `.env`. Copy `.env.example` only when overriding
defaults:

```bash
cp .env.example .env
```

Important values:

- `DEV_UID` / `DEV_GID` must match the source-tree owner when containers
  write bind mounts.
- `VITE_API_BASE_URL` is empty by default so Vite proxies same-origin
  `/api` requests. Restart Dashboard after changing it.
- `SWALLOW_API_CREDENTIAL_KEY` encrypts write-only credentials. The bundled
  value is local-only; changing it makes existing local credentials unreadable.
- `SWALLOW_API_MACHINE_TOKEN` authenticates Prometheus discovery/metrics and
  other machine endpoints, not the operator API.

All bundled passwords, tokens, TLS material, and keys are development-only.

## Hot reload

- API, worker, and Ansible executor use separate Air processes. Go, manifest,
  and playbook changes rebuild the owning process.
- Dashboard uses Vite HMR. If `package-lock.json` changes, the entrypoint
  refreshes dependencies.
- Compiled artifacts and caches remain in container/named-volume paths rather
  than polluting tracked source.

Temporal, worker, and Ansible executor must all remain available for Workflows;
there is no API-process execution fallback.

## Access from another machine

Dashboard and API listen on all interfaces for development. Open
`http://<host-ip>:5173`, or tunnel only the Dashboard port:

```bash
ssh -L 5173:localhost:5173 user@host
```

Vite trusts IP/localhost host headers by default. Add a deliberate
`server.allowedHosts` entry before using a development DNS name.

## Boot Media lab

To try Boot Media (Redfish virtual media iPXE boot, decision 047) against a real
BMC, put the iPXE ISO at `boot-media/swallow-ipxe.iso` (gitignored) and set in
`.env` the address the BMC network reaches this host at:

```bash
SWALLOW_API_BOOT_MEDIA_BASE_URL=http://10.0.0.5
BOOT_MEDIA_HTTP_PUBLISH=10.0.0.5:80
docker compose up -d api-server
```

The API is then also published on that address's port 80, where BMCs mount
`/boot-media/ipxe/swallow-ipxe.iso`. Many BMCs accept only plain HTTP on port 80.
Editing api-server Go code restarts the worker (Air), which fences a running
deployment's lease; avoid it while a lab deployment is in flight.

## Seed a demonstration Site

The idempotent seed script creates the deployment key (`swallow-api deployment-key
ensure`, the installation step; the API does not create it on start), logs in, creates a Site, registers the in-Compose
Prometheus Integration, configures automation/playbook mappings, and optionally
registers MAAS when values are present:

```bash
cp .env.example .env
docker compose up -d
bash seed.sh
```

Without MAAS or SSH inputs, those steps are skipped with guidance. After a
provisioned Server is observed, exporter installation can run automatically
according to ownership policy; locked Servers remain unmanaged.

For manual setup, follow the [initial setup guide](../../docs/en/guides/initial-setup.md).

## Troubleshooting

- **Docker socket permission denied:** log in again after joining the
  `docker` group, or use an explicitly authorized session.
- **Dashboard environment change not visible:** restart `dashboard`; Vite
  reads environment values at process start.
- **API keeps restarting:** inspect `docker compose logs api-server` for Air
  build/config errors.
- **Workflow does not progress:** inspect Temporal, worker, and
  `ansible-executor` health and logs.
- **Integration data is old:** read its last success/error, validate the
  external endpoint and credential, then reconcile.
