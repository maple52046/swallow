# swallow API Contracts

This is the starting point for contract work owned by the `api-server`
component.

## Reading workflow

For any API reading, creation, modification, deprecation, deletion, consumption,
or validation:

1. Read this file.
2. Read [outline.md](outline.md).
3. Open the [api-server outline](api-server/outline.md).
4. Read only the relevant contract.
5. Use only contracts marked Active as implementation-ready.

Do not infer routes, fields, statuses, errors, authentication, authorization, or
semantics from private implementation.

## Ownership

`api-server` owns one HTTP REST surface consumed by `dashboard`, `cli`, and
external integrators. This tree contains no other public protocol.

Contracts live under:

```text
docs/development/api-contracts/
├── README.md
├── outline.md
├── template.md
└── api-server/
    ├── outline.md
    └── <area>.md
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

Update `api-server/outline.md` whenever a contract is added, changed,
deprecated, deleted, renamed, or promoted. Update the top-level outline only
when the set of provider components or outline paths changes.

Contract field names and enums use the repository glossary. Resolve missing or
ambiguous domain language in the glossary before defining the wire behavior.

Provider and consumers update together in this monorepo, but ownership does not
move: the provider contract remains the shared boundary.
