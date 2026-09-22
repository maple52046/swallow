# swallow CLI

`swallow` is the operator command-line client for the Swallow GPU datacenter
platform. It talks to the `api-server` HTTP API and covers the full active
contract surface: auth, sites, integrations, servers, provisioning,
infrastructure, platforms (including the live Kubernetes explorer and Slurm
views), workflows, monitoring, and discovery.

This is the `cli` component of the swallow monorepo — an HTTP consumer of the
`api-server` contract. It is a separate Go module and builds a binary named
`swallow`, distinct from the `api-server` service binary `swallow-api`.

> **Full usage manual:** [`docs/usage.md`](docs/usage.md) — configuration,
> authentication, every command group with examples, request-body files, output
> formats, and scripting recipes. This README is a quick overview.

## Build

```bash
go build -o bin/swallow ./cmd/swallow
```

## Configure and log in

The CLI reads a profile from `$SWALLOW_CONFIG` or, by default,
`<user-config-dir>/swallow/config.yaml` (on Linux, `~/.config/swallow/config.yaml`).
Environment variables (`SWALLOW_ENDPOINT`, `SWALLOW_TOKEN`, `SWALLOW_SITE`,
`SWALLOW_MACHINE_TOKEN`, `SWALLOW_INSECURE`) override the file, and global flags
override those.

```bash
# First login writes the endpoint and access token to the profile.
swallow --endpoint https://swallow.example login -u admin --password-stdin < pw.txt

swallow auth me
swallow logout   # clears the stored token locally
```

## Global flags

| Flag | Meaning |
| --- | --- |
| `--endpoint` | api-server base URL (overrides the profile) |
| `--token` | access token for this invocation |
| `--site` | default Site scope for commands that accept `siteId` |
| `--machine-token` | machine bearer token for discovery endpoints |
| `-o, --output` | `table` (default), `json`, or `yaml` |
| `--insecure` | skip TLS verification (lab endpoints only) |
| `--request-timeout` | per-request timeout; `0` disables it |
| `--config` | profile file path |

## Command groups

Run `swallow <group> --help` for the full verb list.

- `auth`, `login`, `logout` — session
- `overview` — site-scoped operational summary
- `sites`, `integrations` — Site identity, provider integrations, site automation
- `servers` — inventory list/stream, detail, lifecycle actions, network, placement
- `provisioning` — OS images, templates, tags, deploy/release/recover operations,
  image verification, network inspection, provisioning tasks
- `infrastructure` — Zones and Pools
- `platforms` — Kubernetes/Slurm deploy, lifecycle, sync, Slurm state and
  requirements, and the live `kubernetes` explorer
- `workflows` — durable Workflow create/observe/control and per-Task diagnostics
- `monitoring` — alerts and server metrics
- `discovery` — Prometheus service discovery (machine auth)

## Request bodies

Read commands use flags for path and query parameters. Create/update/action
commands that carry a structured payload accept `-f/--file <path>` (a JSON or
YAML document, or `-` for stdin) so the body tracks the API contract exactly.
Common simple cases also have convenience flags (for example
`provisioning release --server ... --erase`).

## Output

`table` renders a human-readable table for lists (flattening one level of nested
fields into dotted columns) and a key/value table for single objects; `json` and
`yaml` are the lossless formats. `servers watch` streams SSE frames as JSON lines
regardless of `--output`.
