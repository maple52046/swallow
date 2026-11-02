# Contributing to swallow

[繁體中文](CONTRIBUTING.zh-TW.md) · [Project README](README.md)

swallow is a monorepo with strict component and contract boundaries. A useful
contribution changes the smallest owning component, keeps shared language and
provider contracts accurate, and includes verification proportional to risk.

swallow's core is generic: composed with providers, which talk to external
systems, and plugins, which add what swallow manages, it becomes out-of-the-box
datacenter management. A new capability is a plugin or a provider; the core
stays free of vendor, product, Platform, and GPU-vendor knowledge. The
[design principles](docs/development/architecture-spec.md#swallow-的設計主張)
state this idea, the current composition, and the principles every change
follows.

## Before you start

1. Read the [system overview](docs/development/system-overview.md): what swallow
   does, which processes run and what they do in the background, how a Platform
   deployment flows, where data lives and who writes it, and where the code is.
2. Read the root [codebase structure](docs/development/codebase-structure.md) and
   the design principles at the top of the root
   [architecture specification](docs/development/architecture-spec.md).
3. Identify the owning component: `api-server`, `dashboard`, or `cli`, and read
   that component's `AGENTS.md` and architecture specification.
4. Read the specification for the area you change:
   - HTTP API behavior: the provider-owned
     [API contract](api-server/docs/development/api-contracts/README.md), updated
     before or with the code.
   - OS deployment, OS Images, Deployment Templates, Zones and Pools:
     [OS provisioning](docs/development/os-provisioning.md).
   - Enrollment, hardware inspection, Power Configuration, Boot Media:
     [Server enrollment and boot media](docs/development/server-enrollment-and-boot-media.md).
   - Platforms (Kubernetes, Slurm): [platform deployment](docs/development/platform-deployment.md).
   - Managed Software: [software deployment](docs/development/software-deployment.md).
   - How Workflows run, retry, and cancel: [Workflow execution](docs/development/workflow-execution.md).
   - SSH keys, login users, API keys: [access and credentials](docs/development/access-and-credentials.md).
   - Playbooks, roles, or a plugin under `api-server/modules/`: the
     [module authoring guide](docs/development/module-authoring.md).
5. When the change adds a managed resource or field, changes who owns a fact,
   or adds a provider or plugin, also read the
   [desired-state model](docs/development/desired-state-model.md), the
   [core and extensions design](docs/development/core-and-extensions.md), and
   the [glossary workflow](docs/development/glossaries/README.md).

Not every change needs a written plan. If you write one, keep it under the
repository root `docs/plans/manuscripts/`, not in a component or only in your
tool's own directory.

## Development environment

The supported contributor golden path uses Docker Compose:

```bash
cd deploy/dev
docker compose up -d
docker compose ps
```

See the [development environment guide](deploy/dev/README.md) for hot reload,
configuration, seeding, remote access, and troubleshooting. The
[development and testing guide](docs/development/development-and-testing.md)
explains where each kind of test lives, the shared fakes and fixtures, and the
test environment variables.

Component checks (run on the host; they need Go, Node 24, and Python 3):

```bash
# API
cd api-server
gofmt -l .
go vet ./...
SWALLOW_TEST_MONGO_URI='mongodb://localhost:27017/?directConnection=true' go test ./...
go build ./...
# after changing an OpenAPI fragment
UPDATE_OPENAPI=1 go test ./internal/shared/apicontract

# CLI
cd ../cli
gofmt -l .
go vet ./...
go test ./...
go build ./...

# Dashboard
cd ../dashboard
npm ci
npx playwright install chromium   # once, for the e2e browsers
npm run contract:check            # after an OpenAPI change, run contract:generate first
npm run lint
npm run build
npm run test:e2e
```

The Mongo integration tests need the development stack's replica set and skip
when `SWALLOW_TEST_MONGO_URI` is unset. Run only the checks relevant while
iterating, then run the complete required set before submitting.

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
- [ ] No secret, credential, customer data, or local artifact (build output, test results) is committed; generated contract files (`openapi.yaml`, the dashboard's `openapi.ts`) are committed with their sources.
