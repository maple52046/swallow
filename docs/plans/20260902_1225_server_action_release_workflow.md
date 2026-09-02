# Server Action Release Workflow

## Purpose

Preserve the completed design and implementation boundaries for Swallow Server actions, with particular emphasis on MAAS Release, operator-visible diagnostics, provider activity, and timely provisioning-state convergence. This document is the long-term reference for maintaining those behaviors without turning synchronous provider actions into durable Operations.

## Source Scope

This consolidation covers one manuscript: `docs/plans/manuscripts/20260901-server-action-diagnostics.md`. It records work completed between 2026-09-01 and 2026-09-02 across `api-server` and `dashboard`, including the original Release failure, Activity follow-up, configurable disk erasure, and targeted projection refresh.

## Consolidated Background

A MAAS-backed Release request failed because Swallow encoded a parameterless POST as an empty multipart body. MAAS expects parameterless operations to have no body and no `Content-Type`, and rejected the request before release began. The Dashboard then reduced the provider failure to an ambiguous summary and offered no reliable place to inspect all per-Server details.

Subsequent work exposed two related operator problems. Action results were transient and difficult to rediscover, while provider state changes were not reflected promptly because the Servers endpoint reads a Mongo projection and the development inventory reconciliation interval is 15 minutes. Repeating the same list read therefore could not observe a live MAAS transition.

The completed workflow now preserves exact provider wire semantics, exposes complete per-target diagnostics, composes a Server Activity view from several honest sources, confirms destructive Release options, and performs bounded targeted provider refreshes while asynchronous state changes are in progress.

## Confirmed Decisions

- Parameterless MAAS operations send a nil body with no `Content-Type`; actions with fields continue to use multipart encoding.
- Common API errors may include a backward-compatible request ID, which the Dashboard retains and backend structured logs use for correlation.
- Bulk and single-Server action failures use direct language and expose every target result, including outcome, error code, HTTP status, request ID, and complete message.
- The latest failed or partial action result remains available in the current browser session and is scoped by Server.
- Server Detail always includes an `Activity` tab. It is not labelled `Audit Log` because Swallow does not own a complete durable audit trail for synchronous provider actions.
- Activity combines browser-session action outcomes, durable Swallow Operations filtered by `serverId`, and read-only provider events. Failure of one source does not hide the others.
- Release always uses a shared PatternFly confirmation flow for detail, row, and bulk actions.
- Release supports optional full, secure, and quick disk-erasure settings plus a provider event comment. The same selected configuration applies to every target in a bulk action.
- Provider-specific Release options are represented as an optional capability; adapters that do not support them must not silently ignore the requested intent.
- Accepted Release targets are refreshed every two seconds until stable, timed out, or unmounted. Refresh calls do not overlap and use at most four concurrent provider requests.
- Targeted refresh reads one provider machine and updates only its provisioning axis. Full reconciliation remains responsible for inventory identity, hardware, creation, and absence detection.

## Architecture and Design Principles

- Preserve provider-owned semantics at the adapter boundary, including the distinction between a no-body operation and a multipart operation.
- Keep synchronous Server actions synchronous. Diagnostics and correlation improve their observability without manufacturing a durable job abstraction.
- Compose Activity from independently available sources and render partial availability explicitly.
- Keep the durable inventory projection authoritative for normal reads, while using a narrow command-like refresh endpoint to observe a known asynchronous transition.
- Restrict targeted refresh writes to the provisioning axis so it cannot compete with full inventory reconciliation ownership.
- Keep action-result presentation reusable across Server Detail, row actions, and bulk actions.
- Make destructive intent explicit before dispatch and keep status tracking independent from result diagnostics.

## Functional Scope

- MAAS action transport for parameterless and field-bearing POST operations.
- Single and bulk Server action result presentation with complete per-target failures.
- Request-ID propagation through API errors, Dashboard errors, and backend logs.
- Server Activity with session outcomes, related Operations, and provider events.
- Configurable Release confirmation and MAAS disk-erasure option mapping.
- Automatic list and detail state convergence after accepted Release actions.
- Visible tracking state while released Servers are being refreshed.
- Browser and backend regression coverage for wire shape, diagnostics, confirmation, provider events, and refresh behavior.

## Constraints and Rules

- Never expose credentials, authorization headers, request bodies, cloud-init, or other secrets in API responses, structured logs, session storage, or diagnostic UI.
- Preserve the existing `/api/v1/servers/{id}/release` status codes and semantics. An empty request body remains valid.
- Do not expose or infer a `force` option for Release.
- Do not automatically retry a real destructive Release during verification.
- Provider-event failure must not suppress Operations or browser-session outcomes.
- Browser-session results are diagnostic convenience, not durable audit data.
- Polling must stop after settlement, a bounded timeout, or component unmount, and it must coalesce overlapping reloads.
- Missing or failed refresh data must not be converted into a successful state transition.

## Data Model and Format Notes

- The common API error envelope may contain an optional `requestId`; existing clients remain compatible.
- Session action outcomes are stored in `sessionStorage`, partitioned by Server, and contain only non-secret result metadata.
- Provider events normalize real provider timestamps before delivery to the Dashboard.
- Release configuration may contain `erase`, `secureErase`, `quickErase`, and `comment` fields. MAAS maps these to `erase`, `secure_erase`, `quick_erase`, and `comment` multipart fields.
- The Server provisioning projection remains persisted in Mongo. Targeted refresh updates only this projection axis.

## CLI / API / Config Notes

- `POST /api/v1/servers/{id}/release` accepts an empty body for compatibility or optional Release configuration fields.
- `POST /api/v1/servers/{id}/refresh` is authenticated, reads exactly one live provider machine, and durably updates its provisioning projection.
- The provider-events API is read-only and exposed through a narrow application use case and optional provider port.
- Dashboard Release tracking bypasses browser caching for follow-up reads and limits targeted provider refresh concurrency to four.
- The development inventory reconcile interval remains independent of the two-second, action-scoped targeted refresh loop.

## Implementation Plan

The source plan is completed. Preserve the delivered behavior through the following maintenance sequence:

1. Keep regression tests for nil-body and multipart MAAS requests, provider error boundaries, request IDs, and Release option mapping.
2. Maintain the shared PatternFly confirmation and result components across all Server action entry points.
3. Keep Activity source adapters independent and render partial failures without collapsing the entire view.
4. Retain bounded targeted projection refresh after accepted Release actions in both list and detail routes.
5. Validate future provider adapters against optional configurable-Release and provider-events capabilities instead of assuming MAAS behavior.
6. Continue verifying Go tests, vet, build, Dashboard lint/build, and end-to-end action flows whenever this workflow changes.

## Non-goals

- Creating Operations or durable job records for synchronous provider actions.
- Claiming that Activity is a complete audit log.
- Persisting complete action results beyond the current browser session.
- Changing full inventory reconciliation ownership or using targeted refresh to update identity and hardware data.
- Adding provider capabilities that cannot be represented or executed by the current backend.
- Automatically dispatching destructive live Release requests during test verification.

## Open Questions

No unresolved product or architecture decisions were recorded in the source manuscript. Any future requirement for durable synchronous-action audit history should be handled as a new design decision rather than inferred from the current Activity implementation.

## Future Work

- Revisit durable action-audit ownership only if operators require history beyond provider events, Operations, and browser-session outcomes.
- Extend configurable Release and provider-event capabilities to additional provisioners when their contracts are known.
- Reassess refresh cadence and concurrency using production telemetry while preserving bounded, non-overlapping behavior.
