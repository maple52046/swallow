# swallow

Backend control plane for swallow.

## What this service is

swallow owns **intent, policy, and identity mapping**. It owns no facts about the
physical or runtime world.

Facts about hardware, operating systems, metrics, and platform state are owned by external
systems. Swallow owns automation intent and embedded execution. The unique value it keeps
that nothing else can is the mapping between them: this server, at this site, provisioned
by that MAAS, currently a worker in that Kubernetes platform, emitting these metrics, last
touched by this versioned operation.

That correlation is the product. Everything else is someone else's job.

Read [`docs/decisions/001-system-ownership-boundaries.md`](../../docs/decisions/001-system-ownership-boundaries.md)
in the swallow repository before adding anything that stores state. Most feature ideas
for this service are already ruled out there, because something else owns the facts.

## Integrations

What swallow talks to is **registered at runtime, not configured**: a fleet has one
provisioner per site and they change without a redeploy.

| Kind | Product | Used for |
|------|---------|----------|
| `provisioner` | Ubuntu MAAS | Machine inventory, OS deploy and release |
| `metrics` | Prometheus-compatible store | Metric queries; Alertmanager for alerts |
| `platform` | Kubernetes API, Slurm (slurmrestd) | Live platform state and membership |

Registering a provisioner:

```bash
curl -X POST $API/api/v1/sites -H "$AUTH" \
  -d '{"name": "dc-east"}'

curl -X POST $API/api/v1/integrations -H "$AUTH" -d '{
  "siteId":       "<site-id>",
  "kind":         "provisioner",
  "providerKind": "maas",
  "name":         "maas-east",
  "endpoint":     "http://10.0.0.5:5240/MAAS",
  "credential":   "<consumer>:<token>:<secret>"
}'
```

A credential is **write-only**. No endpoint returns one, in any form, including redacted;
`hasCredential` tells you whether one is stored. Credentials are sealed with AES-256-GCM
using `api.credentialKey`.

## Servers

A server is a **projection** of a machine in a provisioner's inventory. Operators do not
create servers: a reconciler polls each enabled provisioner and produces them, which is
why there is no `POST /servers`.

Identity has three layers, because each answers a question the others cannot:

- `serverId` — swallow-issued, stable for the machine's life in the platform. Every
  reference uses this.
- `source` — `(siteId, integrationId, providerMachineId)`. Unique. How the reconciler
  finds a record.
- `hardware` — system UUID, serial, MACs. Recognises the same machine after re-enrollment.

`hostname` and IP addresses are **observed attributes**: nullable, mutable, and not
unique. Two sites may both have `gpu-node-01` at `10.0.1.10`.

Status is **three independent axes**, each with its own owner and `observedAt`:

| Axis | Owner |
|------|-------|
| `provisioning` | The provisioner |
| `membership` | The platform's own API |
| `health` | The metrics store, resolved at query time and never stored |

An axis that has never been observed is `null`. That matters: "we do not know" must never
be presentable as "we know it is bad".

## Operations

Long-running work is executed by the embedded dispatcher through pinned
`ansible-runner`. Intent is stored as `pending` before execution. A MongoDB lease
enforces one job per site while allowing different sites to run concurrently.

Each site owns SSH settings, mandatory known-hosts, write-only encrypted credentials, and
operation-kind mappings. Only playbooks registered in the release manifest can run.
Temporary credentials are removed after the job; persistent local artifacts serve the
logs API. An expired run becomes `indeterminate` and is never retried automatically.

The dynamic Ansible inventory remains available for diagnostics and external tools. The
embedded runner calls the same use case directly rather than making an HTTP round trip.

## Monitoring

swallow stores no metrics and no alerts. It serves the scrape target list
(`GET /api/v1/discovery/prometheus`, Prometheus `http_sd` format) with `server_id` and
`site` labels attached, which is what makes the metrics join key impossible to drift.

Alerts are read from Alertmanager on demand; acknowledging an alert creates a silence
there rather than setting a field here.

Metric queries are a **fixed, named set** rather than a PromQL passthrough. Exploration
belongs in Grafana, which swallow deep-links to.

## Running

```bash
export SWALLOW_API_MONGO_URI="mongodb://localhost:27017"
export SWALLOW_API_JWT_SECRET="your-secret"
export SWALLOW_API_CREDENTIAL_KEY="$(openssl rand -base64 32)"

swallow api
```

Or with a config file:

```bash
cp docs/config-example.yaml swallow.yaml
swallow api --config swallow.yaml
```

## Configuration priority (highest → lowest)

1. Environment variables (`SWALLOW_API_*`)
2. CLI flags (`--addr`, `--mongo-uri`, …)
3. Config file (`--config path/to/swallow.yaml`)
4. Default values

## Environment variables

| Variable | Config field | Default |
|----------|--------------|---------|
| `SWALLOW_API_ADDR` | `api.addr` | `:30051` |
| `SWALLOW_API_MONGO_URI` | `api.mongoUri` | `mongodb://localhost:27017` |
| `SWALLOW_API_MONGO_DB` | `api.mongoDb` | `swallow` |
| `SWALLOW_API_JWT_SECRET` | `api.jwtSecret` | *(required in prod)* |
| `SWALLOW_API_JWT_EXPIRY_HOURS` | `api.jwtExpiryHours` | `24` |
| `SWALLOW_API_BOOTSTRAP_ADMIN_USERNAME` | `api.bootstrapAdminUsername` | `admin` |
| `SWALLOW_API_BOOTSTRAP_ADMIN_PASSWORD` | `api.bootstrapAdminPassword` | `admin` |
| `SWALLOW_API_CREDENTIAL_KEY` | `api.credentialKey` | **required, no default** |
| `SWALLOW_API_MACHINE_TOKEN` | `api.machineToken` | *(unset: those endpoints need an admin JWT)* |
| `SWALLOW_API_RECONCILE_INTERVAL` | `api.reconcileInterval` | `60s` |
| `SWALLOW_API_INVENTORY_INTERVAL` | `api.inventoryInterval` | `15m` |
| `SWALLOW_API_OPERATION_DISPATCH_INTERVAL` | `api.operationDispatchInterval` | `1s` |
| `SWALLOW_API_OPERATION_LEASE_DURATION` | `api.operationLeaseDuration` | `90s` |
| `SWALLOW_API_PLAYBOOK_MANIFEST` | `api.playbookManifest` | `automation/manifest.json` |
| `SWALLOW_API_JOB_ARTIFACT_DIR` | `api.jobArtifactDir` | `./var/jobs` |

`credentialKey` has no default on purpose: a shipped default encryption key looks like
protection and is not. Starting without one would defer the failure to the first operator
who tries to register an integration.

`machineToken` is a static bearer token for Prometheus metrics/discovery and external
Ansible inventory diagnostics. It is not a second way into the operator API.

## Background loops

- **Reconciler** — polls each enabled provisioner and each registered platform.
- **Inventory sweep** — refreshes expensive attached-hardware observations.
- **Embedded dispatcher** — claims durable pending operations, renews site leases, and
  records a terminal or indeterminate result.

## Building

```bash
go build -o bin/swallow ./cmd/swallow
```

## Further reading

- [`docs/apis.md`](docs/apis.md) — implemented endpoints.
- [`docs/config-example.yaml`](docs/config-example.yaml) — annotated config.
- [`docs/decisions/`](../../docs/decisions) in the swallow repository — the binding
  decisions this service implements, including what was rejected and why.
