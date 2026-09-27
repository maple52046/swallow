# API Contracts

This document defines the root workflow for locating API contracts by provider
component.

API contracts are owned by the component that provides the API. The
component directory only stores the provider component's contract documents.

This root document should only guide agents to the correct provider component.
Provider-specific reading, status, authoring, and maintenance rules live in that
provider component's API contract workflow.

## Provider-First Discovery

When a task creates, modifies, deprecates, deletes, consumes, or validates API
behavior, agents must locate the contract from the provider component:

1. Identify the component that provides the API.
2. Use [`codebase-structure.md`](codebase-structure.md) to map that component to
   its top-level directory.
3. Enter that component directory.
4. Read that component's local `AGENTS.md`.
5. Follow the local API contract workflow and outline to locate the relevant
   provider-owned contract.

If the provider component is unclear, stop and clarify the provider before using
or changing API behavior. Do not infer contract ownership from private
implementation files.

## Current Provider Components

For swallow today, these components provide APIs and own API contracts:

| Provider component | Directory | API contract workflow |
| --- | --- | --- |
| `api-server` | `api-server/` | `api-server/docs/development/api-contracts/README.md` |

`api-server` owns one API surface: the HTTP REST API consumed by `dashboard` and
the `cli` operator client.

The `dashboard` and `cli` components are consumers of the `api-server` HTTP API
and do not own API contracts.

If a future component provides APIs, its contracts should be discovered through
the same component-first workflow and stored under that provider component's own
directory documentation.

## Ownership Rule

API contracts belong to the provider component, not to the consumer component and
not to the source project as an ownership boundary.

For example:

- APIs provided by `api-server` are owned by the `api-server` component.
- The dashboard consumes provider-owned contracts but does not own contracts for
  APIs provided by backend components.
- If the dashboard later provides its own API, those contracts should be owned by
  the dashboard provider component and stored under the `dashboard/` directory.

## Cross-Component Development Rule

When the API provider and API consumer are different components, the contract is
the shared boundary.

The provider component must implement the contract.

The consumer component must integrate according to the contract.

Neither side should rely on private implementation details from the other side.

This rule applies whenever different components call each other, should
such a case arise; today `dashboard` and `cli` are the HTTP consumers of `api-server`.
