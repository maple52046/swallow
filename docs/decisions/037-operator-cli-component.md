# 037. Operator CLI component and binary naming

- Status: Accepted
- Date: 2026-09-23

## Context

swallow had no operator-facing command-line client for its HTTP API. The only
"CLI" surfaces were the `api-server` service binary (a Cobra process entry point
with the `api`, `worker`, `migrate`, and `ansible-executor` service subcommands)
and `swallowctl`, a bash lifecycle installer under `deploy/production/`. Neither
is a client of the Data Center API: `dashboard` was the only consumer of the
`api-server` HTTP contract.

We want a scriptable operator client that covers the full Active API surface
(auth, sites, integrations, servers, provisioning, infrastructure, platforms
including the live Kubernetes explorer and Slurm views, workflows, monitoring,
and discovery) in Go.

Two boundary questions had to be settled:

1. Where does the client live in the monorepo?
2. The desired client binary name is `swallow`, which the `api-server` service
   binary already used.

## Decision

Add a new top-level component `cli/` — an operator command-line client and a
second HTTP consumer of the `api-server` contract, peer to `dashboard`. It is a
separate Go module (`github.com/maple52046/swallow/cli`) and builds a binary
named `swallow`.

Rename the `api-server` service binary from `swallow` to `swallow-api` to free
the `swallow` name for the client. This changes only the binary, the Cobra root
`Use`, and install/build paths (Dockerfile, systemd unit `ExecStart`, release
assembly, native `swallowctl`, air dev configs, and docs). The `api-server`
component directory, module path (`github.com/maple52046/swallow`), service
subcommands, environment variables (`SWALLOW_API_*`), install directories
(`/opt/swallow`, `/etc/swallow`), and the `swallow-api.service` unit name are
unchanged.

Component directory names remain role-based (`api-server/`, `cli/`); `swallow`
and `swallow-api` are binary names produced by their respective components, never
directory names.

The CLI is a conformist consumer: it depends only on the provider-owned API
contract, never on `api-server` internals, and passes complex request bodies
through as operator-supplied JSON/YAML so payloads track the contract rather than
a hand-copied struct. It targets the Active surface only and does not expose the
deprecated `/operations` and `/clusters` aliases or the single-server
`deploy`/`release` routes as first-class commands.

## Alternatives considered

- **Add resource subcommands to the existing `swallow` service binary.**
  Rejected: it mixes the API provider's process entry point with an operator
  client, blurring the provider/consumer boundary, and would ship client code
  inside the backend module and container image.
- **Extend `swallowctl` into an API client.** Rejected: `swallowctl` is a
  deployment lifecycle tool (install/upgrade/backup/restore) with a different
  audience and language; overloading it would confuse two distinct concerns and
  reuse a name already meaning "installer".
- **Keep the service binary named `swallow` and name the client differently
  (e.g. `swallowctl`, `sw`).** Rejected per explicit product preference: the
  operator client is the primary `swallow` command; the backend service is the
  supporting `swallow-api` binary.
- **Rename the `api-server/` directory to `swallow-api/`.** Rejected: directory
  names denote component roles, not binaries (see codebase-structure); renaming
  would churn docs, ADRs, and AGENTS files for no boundary benefit.

## Consequences

- A second consumer now depends on the `api-server` contract, reinforcing that
  the contract — not any component's internals — is the shared boundary.
  Contract changes must consider both `dashboard` and `cli`.
- Operators, docs, and any automation that invoked `swallow <service-subcommand>`
  must use `swallow-api` for the backend service; `swallow` now means the client.
- The CLI is intentionally scoped to the Active surface, so a Planned endpoint
  gets a command only after its contract is promoted to Active.

## Current status

Implemented. `cli/` builds the `swallow` client covering the Active surface; the
`api-server` service binary and all deploy/build references were renamed to
`swallow-api`. Shared docs (codebase-structure, api-contracts, conventions,
outline, root AGENTS) register `cli` as a component and api-server consumer.
