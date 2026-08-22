# swallow

Backend control plane for the GDCM platform.

## What this service is

swallow owns **intent, policy, and identity mapping**. It owns no facts about the
physical or runtime world.

Every fact about hardware, operating systems, execution, metrics, or cluster state is
owned by an external system that swallow integrates with. The one thing swallow keeps
that nothing else can is the mapping between them: this server, at this site, provisioned
by that MAAS, currently a worker in that Kubernetes cluster, emitting these metrics, last
touched by that AWX job.

That correlation is the product. Everything else is someone else's job.

Read [`docs/decisions/001-system-ownership-boundaries.md`](../../docs/decisions/001-system-ownership-boundaries.md)
in the platform repository before adding anything that stores state. Most feature ideas
for this service are already ruled out there, because something else owns the facts.

## Integrations

What swallow talks to is **registered at runtime, not configured**: a fleet has one
provisioner per site and they change without a redeploy.

| Kind | Product | Used for |
|------|---------|----------|
| `provisioner` | Ubuntu MAAS | Machine inventory, OS deploy and release |
| `automation` | AWX | Long-running operations, their status and logs |
| `metrics` | Prometheus-compatible store | Metric queries; Alertmanager for alerts |
| `cluster` | Kubernetes API, Slurm (slurmrestd) | Live cluster state and membership |

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
| `membership` | The cluster's own API |
| `health` | The metrics store, resolved at query time and never stored |

An axis that has never been observed is `null`. That matters: "we do not know" must never
be presentable as "we know it is bad".

## Operations

Long-running work is executed by AWX. An operation is swallow's intent plus a reference to
the job doing it — swallow stores no logs, implements no retry, and enforces no
idempotency.

Targeting flows the opposite way from what you might expect: **swallow does not push
inventory into AWX**. AWX pulls from `GET /api/v1/discovery/ansible`, so there is one
source of targeting truth and nothing to keep in sync. Hosts are keyed by `serverId`, so
`--limit` takes server IDs directly.

Operation kinds map to AWX job template names through settings on the automation
integration, e.g. `template.install-gpu-driver`. Playbooks live in git; swallow stores no
automation content.

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
| `SWALLOW_API_OPERATION_POLL_INTERVAL` | `api.operationPollInterval` | `15s` |

`credentialKey` has no default on purpose: a shipped default encryption key looks like
protection and is not. Starting without one would defer the failure to the first operator
who tries to register an integration.

`machineToken` is a static bearer token for callers that are other systems — Prometheus
pulling scrape targets, AWX posting job notifications. It is accepted only on those
endpoints and is not a second way into the rest of the API.

## Background loops

Two, and both are periodic readers of external state. swallow executes nothing itself.

- **Reconciler** — polls each enabled provisioner and each registered cluster.
- **Operation poller** — re-reads unfinished operations from their controller. The
  correctness backstop behind AWX webhooks, which are lossy.

## Building

```bash
go build -o bin/swallow ./cmd/swallow
```

## Further reading

- [`docs/apis.md`](docs/apis.md) — implemented endpoints.
- [`docs/config-example.yaml`](docs/config-example.yaml) — annotated config.
- [`docs/decisions/`](../../docs/decisions) in the platform repository — the binding
  decisions this service implements, including what was rejected and why.
