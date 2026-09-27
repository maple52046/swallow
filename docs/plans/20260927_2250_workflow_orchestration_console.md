# Workflow Orchestration Console

## Purpose

Define the long-term direction for the dashboard Workflows route as an
orchestration console for operational triage, live execution monitoring, and
durable history, while preserving the existing backend contract and lifecycle
model.

## Source Scope

This plan consolidates one AI manuscript:

- `docs/plans/manuscripts/20260927-workflow-list-ui-refactor.md`

The source covers the dashboard Workflow list, its shared presentation surface,
adaptive refresh behavior, and the verification required to preserve Platform
list behavior.

## Consolidated Background

The Workflow list must serve both active operations and historical audit use
without becoming a synthetic health dashboard. Its authoritative working set is
the server-paginated Workflow history, ordered newest first. The list should
make state, current Task context, targets, requester, and timing easy to scan,
then route operators to the detail page for execution controls.

The Platform and Workflow lists share a control-plane inventory hierarchy, but
their domain-specific rows, filters, and actions remain separate. A shared
inventory surface should provide consistent chrome without changing the
established Platform presentation.

## Confirmed Decisions

- Keep the default Workflow view newest first and server paginated.
- Store operational lenses, exact kind filtering, and pagination in the URL.
- Preserve Site scope in all Workflow and related-resource links.
- Use native links for list navigation and keep execution controls on the
  Workflow detail page.
- Display Workflow identity, execution state, Task context, targets, requester,
  and timing as independent facts on desktop and mobile.
- Poll only while the current page contains states that can still advance.
- Preserve the last successful result through transient background failures.
- Share the inventory surface between Platform and Workflow lists without
  changing the Platform list appearance.
- Treat `requires_attention` as the exact `Needs attention` filter; keep
  `failed` separate.

## Architecture and Design Principles

- Keep this work in the dashboard presentation layer.
- Treat backend filtering, pagination, ordering, status, and lifecycle data as
  authoritative.
- Use equivalent semantic content in the desktop table and mobile cards.
- Prefer visible text and verifiable facts over synthetic scores or inferred
  progress.
- Use canonical Workflow, Job, Task, and Runner language in new UI and
  documentation while retaining compatibility-oriented Operation names in
  existing code and wire data.
- Reuse shared presentation primitives without coupling Platform and Workflow
  domain behavior.

## Functional Scope

- A refined Workflow inventory with result ranges and current refresh state.
- URL-owned operational lenses and exact kind filtering.
- Distinct empty states for an empty history and a filtered result with no
  matches.
- Workflow identity and Site-scoped detail navigation.
- Task-aware execution context with a safe legacy fallback.
- Platform navigation when a Workflow already includes a Platform identifier.
- Adaptive polling for pending, running, waiting, and canceling Workflows.
- Non-blocking background refresh warnings that retain last-good rows.
- Manual refresh, responsive desktop/mobile layouts, and focused visual states.

## Constraints and Rules

- Do not modify the API, DTOs, repositories, domain types, or lifecycle policy.
- Do not add an overview request or other API fan-out from the list.
- Do not reorder a server-paginated result on the client.
- Do not combine independent statuses into synthetic health or attention
  scores.
- Do not display invented percentages or unverifiable progress.
- `Active` follows the backend non-terminal definition; polling uses the
  narrower changing-state set.
- Cancel, retry, rerun, create, and bulk actions remain outside the list.
- Preserve accessibility, keyboard navigation, reduced-motion behavior,
  responsive parity, and light/dark token use.

## Data Model and Format Notes

- Existing Operation domain and compatibility fields remain unchanged.
- New presentation text uses Workflow and Task terminology.
- Task context is derived only from the existing Workflow projection and must
  fall back safely for legacy records.
- Status semantics remain canonical: `requires_attention` and `failed` are
  distinct, and no lifecycle states are added or rewritten.
- Timestamps, target identifiers, requesters, and Task counts are displayed as
  provided or directly derived from the current payload.

## CLI / API / Config Notes

- No CLI changes are part of this plan.
- No API endpoint, request, response, or repository-port changes are required.
- Existing list filters and server pagination remain the integration boundary.
- No new configuration keys are introduced.

## Implementation Plan

1. Introduce a shared inventory surface and migrate the Platform list to it
   without visual regression.
2. Build the Workflow inventory hierarchy, URL-owned filters, native links, and
   responsive table/card representations.
3. Add presentation-only Task context and status-sensitive navigation labels
   from existing Workflow data.
4. Extend the Workflow list state with refreshed time, manual reload, adaptive
   polling, stale-response protection, and last-good refresh errors.
5. Add focused fixtures, behavioral coverage, and desktop/mobile light/dark
   visual baselines; validate the Platform baselines at the same time.
6. Run lint, production build, relevant Playwright tests, diff checks, and the
   dashboard documentation, accessibility, responsive, reuse, real-data, and
   layer-direction reviews.

## Non-goals

- Backend or wire-format changes.
- A Site-wide Workflow KPI or health overview.
- Client-side sorting of paginated results.
- Inline cancel, retry, rerun, Workflow creation, or bulk operations.
- A broad Operation-to-Workflow codebase rename.
- Invented progress, cross-status scores, or additional Workflow-list API
  requests.

## Open Questions

No unresolved product or architecture questions were recorded in the source
manuscript.

## Future Work

- Keep Workflow visual baselines and the shared Platform inventory baselines in
  the regression suite as the console evolves.
- Revisit broader Operation-to-Workflow compatibility naming only as a separate,
  explicitly scoped migration.
- Consider new list capabilities only when supported by authoritative backend
  contracts rather than client-side inference.
