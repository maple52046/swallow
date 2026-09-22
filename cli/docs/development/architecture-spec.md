# CLI Architecture Spec

This document is the architecture constitution for the `cli` component (the
`swallow` operator command-line client). It refines, and never contradicts, the
repository root architecture spec (`../../../docs/development/architecture-spec.md`).
Where both apply, both must be satisfied.

## Component Role

`cli` is a **consumer** bounded context: an HTTP client of the `api-server`
published contract, peer to `dashboard`. It owns no API contract and no domain
model. Its ubiquitous language is swallow's, defined by the repository glossary
and the api-server contracts; the CLI translates that language into commands.

Relationship to the provider (context map): `cli` is a **conformist** consumer.
It follows the api-server contract as published rather than building an
anticorruption layer, because the CLI's job is to expose the contract faithfully.
The consequence is a hard rule: the CLI must not import api-server internals and
must not encode payload field sets it would have to keep in sync by hand — it
passes structured request bodies through as operator-supplied JSON/YAML so a
contract change does not silently diverge from a copied struct.

## Clean Architecture Layers

The component uses Clean Architecture, kept deliberately thin because the CLI has
little domain logic of its own — its logic lives on the server.

```text
cmd/swallow            entry point: signal-to-context wiring, delegates to command
internal/command       Cobra command tree; one file per resource group
internal/client        the only HTTP adapter: base path, auth, error envelope, SSE, upload
internal/output        rendering (table/json/yaml) of opaque decoded payloads
internal/config        profile resolution and persistence (file, env, flags)
```

Dependency direction:

- `command` depends on `client`, `output`, and `config`.
- `client`, `output`, and `config` do not depend on `command` and do not depend
  on each other (they are independent leaf packages).
- No package imports `api-server`. No package reaches outside these boundaries
  for HTTP, credentials, or rendering — those responsibilities live in exactly
  one package each (single source of truth).

### Layer responsibilities

- **command** — parses operator input, maps it to a contract request, and prints
  the result. It contains no HTTP or transport detail beyond choosing the path,
  method, query, body, and auth mode. Cross-cutting request/response mechanics
  live in shared helpers (`helpers.go`) so a new command is a small, uniform
  addition rather than a fresh copy of request handling.
- **client** — the single place that knows how to reach api-server: the
  `/api/v1` base path, bearer/machine/none authentication, the shared JSON error
  envelope decoded into a typed `APIError`, Server-Sent Events framing, and
  multipart upload. Payloads pass through as opaque values.
- **output** — renders decoded JSON values (maps, slices, scalars) as a table,
  JSON, or YAML. It knows nothing about specific resources, so it works for every
  endpoint; table rendering is a best-effort convenience and JSON/YAML are the
  lossless formats.
- **config** — resolves the connection profile from file, then `SWALLOW_*`
  environment, then flags, and persists the token on login. It is the only
  durable client-side state.

## Design Constraints

- The CLI targets the **Active** contract surface only. Deprecated compatibility
  aliases (`/operations`, `/clusters`, single-server `deploy`/`release`) are not
  exposed as first-class commands.
- Credentials (access token, machine token) are never logged; the profile file
  is written owner-only.
- A command must not invent API behavior. If an endpoint needs a body shape the
  CLI cannot express with simple flags, it accepts a `--file` document rather
  than guessing fields.
- Reuse first: request/response plumbing, query building, and body reading are
  shared helpers; a resource command should not re-implement them.
