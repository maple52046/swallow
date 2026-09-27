# swallow api-server

[繁體中文](README.zh-TW.md) · [Project documentation](../docs/en/README.md)

`api-server` is swallow's API-owning backend component. It builds the
`swallow-api` binary and owns HTTP contracts, intent, policy, stable identity
mapping, reconciliation, durable Workflow activities, and embedded Ansible
content.

## Runtime processes

The same binary provides separate processes:

| Command | Responsibility |
| --- | --- |
| `swallow-api api` | HTTP API, authentication, queries, reconciliation, and Workflow start/control |
| `swallow-api worker` | Temporal Workflow and activity worker |
| `swallow-api ansible-executor` | Executes idempotent Ansible attempts and publishes task events |
| `swallow-api migrate` | Explicit schema/data migration before service startup |

Temporal is the sole durable orchestration engine. A runnable topology also
requires Temporal Server/PostgreSQL and MongoDB; there is no in-process
dispatcher fallback.

## External systems and ownership

Integrations are registered per Site at runtime:

| Kind | Current provider | Used for |
| --- | --- | --- |
| `provisioner` | Ubuntu MAAS | Machine inventory, power, OS images, deploy/release, grouping, tags |
| `metrics` | Prometheus/Alertmanager/Grafana | Fixed metric queries, alerts/silences, links |
| Platform runtime | Kubernetes, Slurm | Live membership and runtime management for self-deployed Platforms |

swallow stores intent and identity relationships. It does not copy hardware
facts, live Platform state, metrics, or alerts into a competing source of truth.
See the [ownership decision](../docs/decisions/001-system-ownership-boundaries.md).

## Build and verify

Go 1.25 is declared by `go.mod`.

```bash
go build -o bin/swallow-api ./cmd/swallow-api
gofmt -l .
go vet ./...
go test ./...
```

For the complete runnable development topology, use
[deploy/dev](../deploy/dev/README.md) rather than starting only the API.

## Run with explicit dependencies

```bash
export SWALLOW_API_MONGO_URI=mongodb://localhost:27017
export SWALLOW_API_JWT_SECRET='replace-me'
export SWALLOW_API_CREDENTIAL_KEY="$(openssl rand -base64 32)"
export SWALLOW_API_TEMPORAL_ADDRESS=localhost:7233

bin/swallow-api migrate
bin/swallow-api api
```

In separate processes, start:

```bash
bin/swallow-api worker
bin/swallow-api ansible-executor
```

The commands need the automation manifest, playbook directory, Ansible runner
environment, MongoDB, and Temporal topology configured for the current working
directory. The Compose stack supplies those details.

## Configuration

Precedence, highest first:

1. `SWALLOW_API_*` environment variables.
2. command flags;
3. `--config` YAML file;
4. defaults.

Use the annotated [config example](docs/config-example.yaml) as the complete
field reference. Important groups include:

- API address, MongoDB, JWT, bootstrap admin, credential encryption, and
  machine authentication;
- reconcile and attached-inventory intervals;
- Temporal address, namespace, task queue, start polling, and parallelism;
- Ansible runner command, manifest/playbook directories, runtime/artifact
  storage, retention, and upload limits.

`api.credentialKey` is required and must decode to 32 bytes. It encrypts
write-only integration and automation credentials. Back it up with MongoDB;
changing it makes existing ciphertext unreadable.

Development defaults such as `admin` / `admin` and
`changeme-in-production` are unsafe outside local development.

## API contracts

The provider-owned [contract outline](docs/development/api-contracts/api-server/outline.md)
lists every Active, Deprecated, and Planned area. Only Active contracts are
implementation-ready.

Shared behavior:

- base path `/api/v1`;
- opaque bearer tokens and role-based authorization;
- one JSON error envelope with a request ID;
- ISO 8601 UTC timestamps;
- consistent pagination for large collections.

`dashboard` and `cli` are conformist consumers. They must not infer behavior
from this component's private code.

## Reconciliation and execution

- Provisioner reconciliation creates and updates Server projections.
- Inventory sweeps refresh expensive attached-hardware observations.
- Platform sync reads runtime-owned membership.
- Temporal Workflows execute versioned Jobs and Tasks.
- Ansible execution is restricted to playbooks in
  [automation/manifest.json](automation/manifest.json).

An expired execution may become `indeterminate` and is not retried
automatically because its external side effect cannot be proven.

## Development architecture

Before changing code, read [AGENTS.md](AGENTS.md), the component
[architecture specification](docs/development/architecture-spec.md), and
[coding style](docs/development/coding-style.md). Domain/application packages
must remain independent of Fiber, MongoDB, Temporal, Ansible, and provider SDK
implementations.
