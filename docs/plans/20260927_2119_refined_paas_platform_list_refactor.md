# Refined PaaS Platform List Refactor

## Purpose

Turn the Platforms route into a refined PaaS control plane. Each Platform is
treated as an operable runtime, while its deployment workflow is presented as
current operational context. The refactor must preserve existing API and
lifecycle behavior.

## Source Scope

This plan consolidates one manuscript:

- `docs/plans/manuscripts/20260927-platform-list-ui-refactor.md`

The source covers the Platform inventory information architecture, responsive
presentation, filtering, workflow context, compatibility constraints, and
verification expectations.

## Consolidated Background

The current Platforms route needs a clearer control-plane identity. The page
should prioritize runtime identity and operational state rather than resemble a
generic KPI dashboard. Lifecycle, connectivity, membership, and sync freshness
are separate facts, and deployment or uninstall workflows provide relevant
context when work is active or has failed.

## Confirmed Decisions

- Use one layered fleet overview rather than a wall of KPI cards.
- Keep the overview scoped to the complete current Site, independent of list
  filters.
- Store shareable name, type, and operational-status filters in the URL.
- Keep selection local and clear it when the visible working set changes.
- Provide equivalent information and actions in the desktop table and mobile
  cards.
- Promote existing lifecycle workflows for in-progress and failed deployment or
  uninstall states without loading additional workflow data.
- Preserve selection, bulk uninstall, bulk delete, settings, and deploy flows.

## Architecture and Design Principles

- Platform remains the Platform Management aggregate defined by the root
  glossary.
- Lifecycle and connectivity remain independent state axes.
- The page consumes existing data and shared dashboard primitives; it does not
  introduce new cross-layer dependencies.
- The visual hierarchy should be refined and operational: balanced spacing,
  restrained status treatment, responsive parity, and readable state meaning
  without dependence on color.
- Interactive surfaces must remain keyboard accessible.

## Functional Scope

- Rework the route header and fleet overview.
- Add URL-owned filters for Platform name, type, and operational status.
- Show lifecycle, connectivity, membership, and sync freshness independently.
- Surface lifecycle workflow context for active and failed deployment or
  uninstall work.
- Retain row selection and bulk uninstall or delete actions.
- Retain settings, deployment, and Platform navigation flows.
- Provide filtered-result and empty-result messaging.
- Implement desktop and mobile presentations with equivalent capabilities.
- Add focused Playwright behavior coverage and desktop/mobile, light/dark visual
  baselines.

## Constraints and Rules

- Do not change the API, DTOs, repositories, domain model, or lifecycle policy.
- Fleet overview facts always describe the full current Site, not the filtered
  subset.
- A Platform needs attention when its lifecycle has failed, a non-uninstalled
  runtime is unreachable, its sync has failed, or it has unmatched members.
- Selection must be cleared when filters change the visible working set.
- Status meaning must be understandable without color.
- Desktop and mobile renderers must expose equivalent data and actions.

## Data Model and Format Notes

- Platform is the canonical domain term and aggregate.
- Existing lifecycle, connectivity, membership, and sync data remain distinct;
  no composite health score is introduced.
- Existing lifecycle workflow information is reused as operational context.
- URL query parameters own the shareable list view; selection remains ephemeral
  page state.

## CLI / API / Config Notes

- No CLI behavior is added or changed.
- No API endpoints, DTOs, or additional workflow requests are introduced.
- No configuration changes are required.
- The dashboard continues to use the current Site scope and existing Platform
  and lifecycle data.

## Implementation Plan

1. Rework the Platforms route header and introduce the layered Site-wide fleet
   overview.
2. Add URL-owned name, type, and operational-status filtering, including result
   counts and a clear-filter empty state.
3. Build the modern desktop inventory table and information-equivalent mobile
   runtime cards.
4. Present runtime identity, lifecycle, connectivity, membership, and sync
   freshness as independent facts.
5. Prioritize runtime and workflow actions according to active, in-progress, and
   failed states.
6. Preserve selection and bulk operations, and reset selection when the filter
   signature changes.
7. Add responsive, light/dark, reduced-motion, hover, focus, selection, and
   attention styling with existing Chakra tokens and shared primitives.
8. Add Playwright behavior coverage and four Platform-list visual baselines.
9. Run dashboard lint and production build, targeted behavior tests, visual
   regression tests, and the manual JSDoc, accessibility, responsive-state,
   real-data, reuse, and layer-direction review.

## Non-goals

- Changing Platform lifecycle policy or domain semantics.
- Combining lifecycle and connectivity into a single state or health score.
- Adding API fan-out to load workflow data.
- Replacing existing selection, bulk actions, settings, deployment, or detail
  navigation flows.
- Expanding the work to CLI or configuration behavior.

## Open Questions

No open questions were recorded in the source manuscript.

## Future Work

No future work beyond the stated implementation and verification scope was
recorded in the source manuscript.
