# CLI reference

[繁體中文](../../zh-TW/reference/cli.md) · [Documentation home](../README.md)

The `swallow` binary is the operator command-line client. It is a separate Go
module from `swallow-api` and communicates only through the published HTTP API.

## Install

The production installation installs the CLI at `/usr/local/bin/swallow`. On another
Linux amd64 host, download the `swallow-linux-amd64` release asset (listed in the release
`SHA256SUMS`) or run the `cli-<version>` image from `ghcr.io/<owner>/swallow`:

```bash
sudo install -m 0755 swallow-linux-amd64 /usr/local/bin/swallow
swallow --help
```

To build the active-development binary from the repository instead:

```bash
cd cli
go build -o bin/swallow ./cmd/swallow
bin/swallow --help
```

## Configure and authenticate

```bash
printf '%s\n' "$PASSWORD" |
  swallow --endpoint http://swallow.example \
  login -u admin --password-stdin
swallow auth me
```

The profile is stored in the user configuration directory with owner-only
permissions. Environment variables override the profile and flags override
environment variables. A password login keeps a session that the CLI renews
automatically; for scripts and CI, create an API key (`swallow api-keys create`)
and store it with `login --api-key-stdin` or pass it in `SWALLOW_API_KEY`. Use
`logout` to end the session and remove stored credentials. See
[API keys](../guides/api-keys.md) for creation, storage, expiry, and revocation.

## Output and request bodies

- `table` is the interactive default.
- `-o json` and `-o yaml` are lossless scripting formats.
- `servers watch` emits SSE frames as JSON lines.
- Structured mutations accept `--file` with JSON, YAML, or `-` for stdin.

Prefer explicit `--site-id` in scripts. A configured global Site is convenient
for interactive work but can make automation ambiguous.

## Command groups

This index names the available verbs without duplicating their flags or payload
examples. Run `swallow <group> --help` for the installed binary's exact surface,
and follow each link into the complete usage manual.

- [`login`, `logout`, `auth me`](../../../cli/docs/usage.md#auth--session) —
  create, inspect, renew, and end a password Session or save an API key profile.
- [`api-keys`](../../../cli/docs/usage.md#api-keys) — `list`, `create`, and
  `delete` (`revoke` alias) non-interactive credentials.
- [`overview`](../../../cli/docs/usage.md#overview) — read the swallow-wide or
  Site-scoped operator aggregate.
- [`sites`](../../../cli/docs/usage.md#sites) — `list`, `get`, `create`,
  `update`, and `delete` Sites; `automation get|set` and
  `automation credential set` manage Site automation.
- [`integrations`](../../../cli/docs/usage.md#integrations) — `list`, `get`,
  `create`, `update`, `delete`, and `credential set`; `enroll-bundle` prepares
  the existing-OS Server enrollment command.
- [`servers`](../../../cli/docs/usage.md#servers) — `list`, `get`, `watch`,
  `refresh`, `delete`, provider details, events, power state, and provisioning
  tasks; lifecycle verbs cover power, inspection, tests, abort, lock, provider
  recovery states, and rescue mode. Nested commands manage
  `power-configuration`, `network`, `placement`, `boot-media`, Redfish probing,
  libvirt `virtual-machines` (`vms`), and host-side `enroll`.
- [`provisioning`](../../../cli/docs/usage.md#provisioning) — manage `images`
  and their metadata, `templates` and user data, `boot-isos`, and `tags`; run
  `preflight`, `deploy`, `release`, `recover`, `verify-image`, network
  inspection, and task reads/retries.
- [`ssh-keys`](../../../cli/docs/usage.md#ssh-keys) — `list`, `get`, `import`,
  `generate`, `delete`, and `sync` Access Keys; show, regenerate, or replace the
  installation Deployment Key.
- [`infrastructure`](../../../cli/docs/usage.md#infrastructure-zones-and-pools)
  — `zones` and `pools` expose `list`, `get`, `create`, `update`, and `delete`.
- [`platforms`](../../../cli/docs/usage.md#platforms) — `list`, `get`, `deploy`,
  `update`, `delete`, `uninstall`, `sync`, and `sync-all`; read live Slurm,
  manage `slurm-requirements` and `gpu-operator`, and use the nested
  `kubernetes` explorer for nodes, namespaces, applications, Pods, related
  resources, logs, and YAML apply.
- [`workflows`](../../../cli/docs/usage.md#workflows) — `list`, `get`, `create`,
  `cancel`, `rerun`, and `timeline`; `task retry|logs|stderr|events|artifacts`
  diagnoses and controls individual Tasks.
- [`monitoring`](../../../cli/docs/usage.md#monitoring) — list and acknowledge
  alerts; `metrics get|range|names` reads current values and bounded history.
- [`discovery`](../../../cli/docs/usage.md#discovery-machine-auth) —
  `prometheus` returns machine-authenticated service-discovery targets.

Managed Software currently uses the Dashboard or HTTP API. Deprecated aliases
and Planned endpoints are intentionally absent. The
[complete CLI usage manual](../../../cli/docs/usage.md) also defines global
flags, request files, exit codes, and recipes.
