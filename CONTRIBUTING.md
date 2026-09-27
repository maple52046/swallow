# Contributing to swallow

[繁體中文](CONTRIBUTING.zh-TW.md) · [Project README](README.md)

swallow is a monorepo with strict component and contract boundaries. A useful
contribution changes the smallest owning component, keeps shared language and
provider contracts accurate, and includes verification proportional to risk.

## Before you start

1. Read the root [codebase structure](docs/development/codebase-structure.md).
2. Identify the owning component: `api-server`, `dashboard`, or `cli`.
3. Read that component's `AGENTS.md` and architecture specification.
4. For domain behavior, data models, core flows, or cross-component work, read
   the root [architecture specification](docs/development/architecture-spec.md)
   and follow the [glossary workflow](docs/development/glossaries/README.md).
5. For API work, locate and update the provider-owned
   [API contract](docs/development/api-contracts.md) before or with the code.

Plans for implementation work belong under the repository root
`docs/plans/manuscripts/`; do not create component-local plan trees.

## Development environment

The supported contributor golden path uses Docker Compose:

```bash
cd deploy/dev
docker compose up -d
docker compose ps
```

See the [development environment guide](deploy/dev/README.md) for hot reload,
configuration, seeding, remote access, and troubleshooting.

Component checks:

```bash
# API
cd api-server
gofmt -l .
go vet ./...
go test ./...
go build ./...

# CLI
cd ../cli
gofmt -l .
go vet ./...
go test ./...
go build ./...

# Dashboard
cd ../dashboard
npm ci
npm run lint
npm run build
npm run test:e2e
```

Run only the checks relevant while iterating, then run the complete required set
before submitting.

## Component boundaries

- `api-server` owns the HTTP API, intent, policy, identity mapping,
  reconciliation, and durable execution adapters.
- `dashboard` and `cli` consume the published HTTP contract; they do not
  import or infer private api-server implementation.
- Cross-component behavior travels through provider-owned contracts and the
  shared glossary, not shared internal packages.
- The same behavior should have one canonical implementation inside its owning
  component.

## API and domain changes

- Only Active contracts are implementation-ready.
- Update contract status, request/response shape, errors, auth, and compatibility
  notes whenever behavior changes.
- Never derive a consumer model from a private provider struct.
- Define or update domain language before using a new term, enum, or state.
- A breaking public-contract change requires a migration note and a
  `BREAKING CHANGE` commit footer.

## Documentation

- Public documentation is English-first with an equivalent Traditional Chinese
  counterpart.
- Cross-project public pages under `docs/en/` and `docs/zh-TW/` must have
  matching relative paths.
- Colocated public references use `README.md` plus `README.zh-TW.md`, or
  `name.md` plus `name.zh-TW.md`.
- Public documentation may link to development sources of truth. AGENTS,
  development specs, contracts, ADRs, glossaries, and skills must not link back
  to public guides.
- Update screenshots only from deterministic Playwright fixtures; never publish
  real endpoints, credentials, customer names, or inventory.

Run:

```bash
python3 scripts/check-docs.py
```

## Commit and pull-request checklist

Follow the [commit specification](docs/development/commit-spec.md). Commit
messages are English Conventional Commits.

Before requesting review:

- [ ] The change has one clear owning component or documented cross-component boundary.
- [ ] Domain terms and API contracts are current.
- [ ] Tests cover success, failure, and compatibility behavior.
- [ ] Go comments or TypeScript/JSDoc satisfy the component completion gate.
- [ ] Public documentation and both languages are updated where behavior is user-visible.
- [ ] Local documentation links and directionality pass.
- [ ] No secret, credential, customer data, or generated local artifact is committed.
