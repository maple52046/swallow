# Swallow API Contracts

This is the starting point for API contract work in this codebase.

API contracts are owned by the component that provides the API. Callers must use
the provider's contract and must not infer behavior from provider implementation
details.

## Reading Workflow

For any API reading, creation, modification, deprecation, deletion, consumption,
or validation:

1. Read this `README.md`.
2. Read `outline.md`.
3. Follow the index path from `outline.md` to the relevant API contract.
4. Use only active contracts as implementation-ready sources of truth.

Do not infer routes, fields, status codes, error formats, authentication,
authorization, or behavior semantics from code.

## Contract Status

- Active: the contract document exists and is the current source of truth.
- Planned: the API area is expected, but the contract document does not yet
  exist or is not authoritative.
- Deprecated: the contract remains only for compatibility or migration.

Only active contracts are implementation-ready. A planned API must be created or
promoted to an active contract before implementation or integration depends on
that API behavior.

## Maintenance Workflow

For any API addition, behavior change, deprecation, or deletion, update the
provider-owned API contract before or together with implementation.

API contracts are stored by provider component:

```text
docs/development/api-contracts/
  README.md
  outline.md
  template.md
  {component}/
    outline.md
    {api}.md
```

The only API-owning component in this source project is `api-server`. It owns two
surfaces: the HTTP REST API consumed by `dashboard`, and the gRPC service
consumed by `agent`. The `agent` component is a consumer and does not own API
contracts.

When writing a new API contract:

1. Start from `template.md`.
2. Place the new contract under
   `docs/development/api-contracts/{component}/{api}.md`.
3. Use kebab-case for `{api}.md`.
4. Update the component `outline.md`.
5. Update the top-level `outline.md` only if this introduces a new API-owning
   component or changes a component outline path.

When maintaining existing contracts:

- Update the component `outline.md` whenever an API contract is added, changed,
  deprecated, deleted, renamed, promoted from planned to active, or moved.
- Update the top-level `outline.md` only when the set of API-owning components or
  component outline paths changes.

Each API contract should define:

1. API owner component
2. API consumers
3. API purpose
4. Endpoint or RPC definition
5. Request schema
6. Response schema
7. Error schema
8. Authentication and authorization requirements
9. Versioning or compatibility notes
10. Related glossary terms
11. Implementation notes, if needed

Implementation must not define new API behavior that is absent from the
provider-owned contract.

## Domain Language

Field names, enum values, and resource names in a contract must match the
platform ubiquitous language owned by the superproject glossary. A contract must
not introduce a domain term or status value that the glossary does not define. If
a needed term is missing or ambiguous, resolve it in the glossary first, then
write the contract.

## Cross-Component API Consumption

When one component calls another component's API, the caller must follow the
provider's API contract. The gRPC API between `agent` and `api-server` is a
provider-owned contract even though both components' code lives in this source
project.

Consumers outside this source project — currently `dashboard` — discover these
contracts through the superproject's API contract workflow and must integrate
according to the contract, not according to this project's implementation.
