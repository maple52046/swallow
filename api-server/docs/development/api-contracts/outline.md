# swallow API Contract Outline

This file routes readers to provider-owned contract outlines in the swallow
monorepo. Read it after [README.md](README.md).

## Provider outlines

| Provider component | Outline | Surface |
| --- | --- | --- |
| `api-server` | [api-server/outline.md](api-server/outline.md) | HTTP REST API consumed by Dashboard, CLI, and external integrators |

## Rules

- Contracts remain under their provider component's development documentation.
- The provider outline lists status, route area, purpose, and consumers.
- Update this file only when provider ownership or outline paths change.
- Do not add consumer-owned copies of provider contracts.
