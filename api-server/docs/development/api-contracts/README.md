# swallow API Contracts

This is the starting point for contract work owned by the `api-server`
component.

## When this workflow applies

Follow it whenever a task adds, changes, deprecates, removes, or validates an HTTP
route, query parameter, field, status, or error. Other documents refer to this definition instead of
restating it.

A task that only consumes or explains existing API behavior reads the contract
itself: open it from a spec's link, or find it in the
[api-server outline](api-server/outline.md), and read the sections for the routes
involved. The Markdown contract is enough for a consumer; open the OpenAPI fragment
only for the exact wire shape (the dashboard's types are generated from it).

## Reading workflow

1. Read this file.
2. Open the [api-server outline](api-server/outline.md), unless you already know the contract file.
3. Read only the relevant contract, and in it the sections for the routes involved.
4. Use only contracts marked Active as implementation-ready.

Do not infer routes, fields, statuses, errors, authentication, authorization, or
semantics from private implementation.

## Ownership

`api-server` is the only component that provides an API: one HTTP REST surface,
consumed by `dashboard` and `cli`. Contracts are also written for external
integrators, though no known integration outside this repository depends on them yet. This tree contains no
other public protocol, and no consumer keeps a copy of a provider contract.

Contracts live under:

```text
docs/development/api-contracts/
├── README.md
├── template.md
├── api-server/
│   ├── outline.md
│   └── <area>.md
└── openapi/
    ├── areas/<area>.yaml
    └── openapi.yaml       # generated bundle
```

## Status

- **Active:** current source of truth and implementation-ready.
- **Deprecated:** compatibility/migration surface only.
- **Planned:** design direction without an implementation-ready contract.

Planned behavior must be promoted into a complete Active contract before
implementation or consumer integration.

## Authoring and maintenance

Start new contracts from [template.md](template.md). Every contract defines
owner, consumers, purpose, endpoints, request/response, errors, auth,
compatibility, glossary terms, and relevant implementation constraints.

Update `api-server/outline.md` whenever a contract is added, deprecated,
deleted, renamed, or promoted, or a change makes its one-line summary wrong; a
new consumer changes only the contract's Consumer Components. Its one-line summaries state what a
contract is for and where it lives; they do not restate ownership or behavior
(for example whether a setting is stored or written through), which only the
contract says.

A contract's **Consumer Components** are the components that call it today. A
component that could call it but does not is not listed; add it in the commit
that starts calling the contract.

Contract field names and enums use the repository glossary. Resolve missing or
ambiguous domain language in the glossary before defining the wire behavior.

Provider and consumers update together in this monorepo, but ownership does not
move: the provider contract remains the shared boundary.

## OpenAPI: the machine-checked wire shape

Every Active and Deprecated HTTP route is also described in OpenAPI 3.0.3 under
[`openapi/`](openapi). The two forms split the work:

- **OpenAPI** is authoritative for the wire shape: paths, methods, parameters,
  request and response schemas, status codes, error codes, and authentication.
  It is written by hand, contract-first; it is never generated from code.
- **Markdown** (`api-server/<area>.md`) is authoritative for behavior: semantics,
  state transitions, authorization rules, side effects, and compatibility. Where a
  Markdown contract still lists fields, the list must agree with OpenAPI.

```text
openapi/
├── common.yaml        # document metadata, security schemes, conventions.md components
├── areas/<area>.yaml  # the paths of one area and the components it owns
└── openapi.yaml       # generated bundle; never edit by hand
```

Areas follow route prefixes; the area table is the source of which fragment owns
a route: `system` (`/livez`, `/readyz`, `/healthz`, `/metrics`), `auth`
(`/api/v1/auth/*`, `/api/v1/api-keys*`, `/api/v1/ssh-keys*`), `local-repository`
(`/api/v1/sites/{siteId}/local-repository*`), `sites`
(the rest of `/api/v1/sites*`, `/api/v1/integrations*`, `/api/v1/infrastructure/*`,
`PUT /api/v1/servers/{id}/placement`, `/api/v1/provisioning/integrations/{id}/http-proxy`,
`/api/v1/provisioning/integrations/{id}/subnets*`), `servers` (`/api/v1/servers`,
`/api/v1/servers/stream`, `/api/v1/servers/{id}` and the routes under it except
`/docker*` and `/placement`, `/api/v1/provisioning/tags*`), `provisioning` (the rest
of `/api/v1/provisioning/*`, the Boot ISO file, `/downloads/*`), `platforms`
(`/api/v1/platforms*`), `change-plans` (`/api/v1/change-plans*`, `/api/v1/change-plan-policy`),
`workflows` (`/api/v1/workflows*`), `software` (`/api/v1/software/*`, `/api/v1/servers/{id}/docker*`),
`monitoring` (`/api/v1/overview`, `/api/v1/monitoring/*`, `/api/v1/discovery/*`), `modules`
(`/api/v1/modules`), and `validation` (`/api/v1/validation/*`).

### Authoring rules

- A fragment contains only `tags`, `paths`, and `components`. Each operation has
  its area's tag, a document-unique camelCase `operationId`, and a `summary`.
- Paths are full (including `/api/v1`), without a trailing slash, with Fiber's
  parameter names in braces.
- Every operation lists each success status with its content type, each error
  status its Markdown contract names, and `default: {$ref: '#/components/responses/Error'}`.
  Security is inherited (bearer); unauthenticated routes set `security: []`.
- Schemas describe exactly what is emitted. Tests close every object schema to
  undocumented properties, so list every property, mark always-present ones
  `required`, mark values that can be JSON `null` `nullable: true`, and set
  `additionalProperties` explicitly only for genuinely free-form maps.
- `allOf` may have only one member (the nullable `$ref` idiom). Write objects out
  instead of composing them.
- A component is defined once, by the area whose Markdown contract owns the
  resource (for example `Workflow` in `workflows`); other areas reference it.
- Deprecated routes carry `deprecated: true` and reuse the canonical schemas.

### Verification

`api-server` tests enforce the contract (`internal/shared/apicontract`):

- `TestContractIsValid` and `TestBundledFileIsCurrent` — the fragments form valid
  OpenAPI and `openapi.yaml` is their current bundle. Regenerate it with
  `UPDATE_OPENAPI=1 go test ./internal/shared/apicontract`.
- `TestRegisteredRoutesMatchTheContract` (`internal/app`) — every route the API
  registers is documented, and every documented operation is served.
- `apicontract.ResponseValidator(t)` — Fiber middleware installed on HTTP test apps
  that fails the test when a response's status, Content-Type, or JSON body does not
  match the contract. Install it first on any new test app that serves real routes.
- `APICONTRACT_AREAS=<area>[,<area>]` loads only those fragments, for working on one
  area while others are incomplete. List the areas whose components the listed areas
  reference too (for example `software,workflows`, because `software` returns the
  `workflows` area's `WorkflowAccepted`). The suite must pass with it unset.

The dashboard generates its types from `openapi.yaml` into
`dashboard/src/infrastructure/api/generated/openapi.ts` (`npm run contract:generate` in `dashboard/`,
checked by `npm run contract:check`). A wire-shape
change therefore updates the fragment, regenerates `openapi.yaml`, regenerates the
dashboard types, and updates the Markdown contract when behavior changes.
