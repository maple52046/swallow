# API Contracts

This document defines the root workflow for locating API contracts by provider
component.

API contracts are owned by the platform component that provides the API. The
source project only stores the provider component's contract documents.

This root document should only guide agents to the correct provider component and
source project. Provider-specific reading, status, authoring, and maintenance
rules live in that provider source project's API contract workflow.

## Provider-First Discovery

When a task creates, modifies, deprecates, deletes, consumes, or validates API
behavior, agents must locate the contract from the provider component:

1. Identify the platform component that provides the API.
2. Use [`codebase-structure.md`](codebase-structure.md) to map that component to
   its root component symlink and source project.
3. Enter the component path through the root symlink if it exists; otherwise
   enter the mapped `src/<project>` path.
4. Read that source project's local `AGENTS.md`.
5. Follow the local API contract workflow and outline to locate the relevant
   provider-owned contract.

If the provider component is unclear, stop and clarify the provider before using
or changing API behavior. Do not infer contract ownership from private
implementation files.

## Current Provider Components

For the current platform, these components provide APIs and own API contracts:

| Provider component | Source project | API contract workflow |
| --- | --- | --- |
| `api-server` | `src/swallow` | `src/swallow/docs/development/api-contracts/README.md` |

`api-server` owns two API surfaces: the HTTP REST API consumed by `dashboard`,
and the gRPC service consumed by `agent`.

The `agent` component is a consumer of the `api-server` gRPC service and does not
own API contracts. The `dashboard` component is a consumer of the `api-server`
HTTP API and does not own API contracts.

If a future component provides APIs from another source project, its contracts
should be discovered through the same component-first workflow and stored under
that provider component's source project documentation.

## Ownership Rule

API contracts belong to the provider component, not to the consumer component and
not to the source project as an ownership boundary.

For example:

- APIs provided by `api-server` are owned by the `api-server` component.
- The dashboard consumes provider-owned contracts but does not own contracts for
  APIs provided by backend components.
- The agent consumes the `api-server` gRPC contract but does not own it.
- If the dashboard later provides its own API, those contracts should be owned by
  the dashboard provider component and stored under the dashboard source project.

## Cross-Component Development Rule

When the API provider and API consumer are different components, the contract is
the shared boundary.

The provider component must implement the contract.

The consumer component must integrate according to the contract.

Neither side should rely on private implementation details from the other side.

This rule also applies inside one source project when different platform
components call each other. The gRPC API between `agent` and `api-server` is a
provider-owned contract even though both components' code lives under
`src/swallow`.
