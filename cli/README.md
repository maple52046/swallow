# swallow CLI

[繁體中文](README.zh-TW.md) · [Full usage manual](docs/usage.md) ·
[Project documentation](../docs/en/README.md)

`swallow` is the operator command-line client for the swallow system. It is a
separate Go module from `api-server` and consumes the published HTTP API. It
covers the operator surfaces listed below; Managed Software currently uses the
Dashboard or HTTP API.

## Build and verify

No stable packaged release is currently published.

```bash
go build -o bin/swallow ./cmd/swallow
gofmt -l .
go vet ./...
go test ./...
```

## Configure and log in

```bash
printf '%s\n' "$PASSWORD" |
  bin/swallow --endpoint https://swallow.example \
  login -u admin --password-stdin

bin/swallow auth me
```

The profile lives under the user configuration directory unless
`$SWALLOW_CONFIG` or `--config` selects another path. Resolution order is
profile, `SWALLOW_*` environment variables, then global flags.

## Output and bodies

- `table` is the human-readable default.
- `-o json` and `-o yaml` preserve the full response for scripts.
- `servers watch` emits Server-Sent Events as JSON lines.
- Structured mutations accept `-f/--file` with JSON, YAML, or `-` for stdin.

Request files intentionally track the API contract without copied CLI structs.

## Command groups

- `auth`, `login`, `logout` — session.
- `overview` — Site-scoped operational summary.
- `sites`, `integrations` — infrastructure identity and Site automation.
- `servers` — inventory, stream, detail, protection, provider actions.
- `provisioning` — images, verification, templates, tags, deploy/release/recovery.
- `infrastructure` — Zones and Pools.
- `platforms` — Kubernetes/Slurm deployment, lifecycle, settings, and live views.
- `workflows` — durable Workflow observation and control.
- `monitoring` — alerts and fixed Server metrics.
- `discovery` — Prometheus HTTP service discovery using machine auth.

Run `swallow <group> --help` or read the
[usage manual](docs/usage.md) for every verb, flag, exit code, and recipe.

## Compatibility and safety

The CLI targets canonical Active routes and does not expose deprecated
`/operations`, `/clusters`, or legacy single-Server provisioning aliases.
Tokens are never logged and the profile is owner-only. `--insecure` is for
controlled labs only.

See [AGENTS.md](AGENTS.md), the
[architecture specification](docs/development/architecture-spec.md), and
[coding style](docs/development/coding-style.md) before changing the component.
