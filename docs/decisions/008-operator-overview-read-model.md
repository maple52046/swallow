# 0008. Provider-owned operator overview read model

- Status: Accepted
- Date: 2026-08-27

## Context

The dashboard overview needs counts and recent state from Server, Site, Integration,
Cluster, Operation, and Monitoring contexts. Having the browser call every resource
endpoint independently makes one page own cross-context aggregation, duplicates scope
rules, and turns one unavailable provider into a full-page failure.

## Decision

The API Server publishes an additive `GET /api/v1/overview` query. It is a read model,
not a new aggregate or source of truth: every field is derived from the context that
already owns it. The dashboard may prioritize the returned sections for presentation,
but the API does not persist or publish a synthetic attention-item entity.

Monitoring is an optional external provider. Its unavailability is represented inside
the monitoring section while the rest of the response remains usable. Failures of the
API Server's durable repositories still fail the request through the common error
contract.

## Alternatives considered

- Browser fan-out was rejected because it makes the consumer coordinate provider-owned
  contexts and produces inconsistent scoping and partial-failure behavior.
- A shared dashboard/backend data model was rejected because component boundaries are
  logical even inside the monorepo.
- Persisting overview rows was rejected because it would duplicate facts already owned
  by the source contexts and introduce a second freshness lifecycle.

## Consequences

The API Server owns one extra query and its compatibility contract. The dashboard gets a
single coherent snapshot and can render useful partial state when monitoring is down.
Adding or changing a section requires contract review because the read model crosses the
provider/consumer boundary.

## Current status

Implemented.
