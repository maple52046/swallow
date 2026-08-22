# Swallow API Contract Outline

This file is the project-wide outline for API contracts owned by platform
components in this source project. Use it after [README.md](README.md) to locate
the provider component's API contract outline.

## Component Outlines

| Component | Outline | Purpose |
| --- | --- | --- |
| `api-server` | [api-server/outline.md](api-server/outline.md) | Contracts for the HTTP REST API and the gRPC agent service owned by the API Server component. |

The `agent` component is a consumer of the `api-server` gRPC service and does not
own API contracts, so it has no outline here.

## Rules

- This file only routes readers to component API contract outlines.
- Each component directory owns its own `outline.md`.
- Component `outline.md` files list the API contracts, statuses, paths, and
  purposes for APIs owned by that component.
- When adding, changing, deprecating, or deleting an API contract, update the
  relevant component `outline.md`. Update this file only when the set of
  API-owning components or component outline paths changes.
