# Dashboard UI Refresh and Agent Commit Workflow

## 1. Purpose

Preserve the completed dashboard presentation refresh, the unified Chakra Select
behavior, the follow-up design corrections, and the repository rule for adding an
agent `Co-authored-by` trailer to generated commits.

## 2. Source Scope

This plan consolidates three completed manuscripts:

- `20260911-dashboard-select-unification.md`
- `20260912-dashboard-ui-refresh.md`
- `20260912-git-commit-coauthor-trailer.md`

The sources cover dashboard presentation and copy, shared select behavior,
responsive and accessible operator workflows, visual regression coverage, and
the repository-wide Git commit skill.

## 3. Consolidated Background

The PatternFly 6 to Chakra UI 3 migration left the dashboard functional but
visually inconsistent, overly verbose, and dependent on two select
implementations. Native selects used OS chrome and could render at the top-left
in embedded previews; Chakra selects could also lose positioning when their
generated trigger ID was overridden or when a portalled positioner was used
inside a focus-trapped dialog.

The dashboard refresh established a modern operator-console presentation while
preserving the existing blue palette and operational density. Review feedback
then identified four areas requiring correction: Provisioning and Infrastructure
tabs, both deployment wizard bodies, compressed Monitoring alerts, and Platform
detail navigation and metadata placement.

The repository's commit workflow also needed to identify the agent that created
a commit without replacing the human author identity.

## 4. Confirmed Decisions

- Chakra UI 3 remains the sole dashboard UI system; no additional UI or charting
  library is introduced.
- Visible product copy remains English and is reduced to decision context,
  failure reasons, or the next action.
- Provisioning, Infrastructure, and Platform detail use Chakra Tabs' default
  presentation without custom enclosed styling or decorative icons.
- Platform detail tabs appear directly below the page header. Lifecycle notices,
  type-specific operations, and configuration belong inside Overview; no
  metadata table sits above the tabs.
- Desktop keeps dense, sticky tables. Narrow screens render equivalent actionable
  cards and retain the same data, selection state, and operations.
- Monitoring presents alerts as spaced, alert-first items rather than compressed
  table rows, and historical trends are not invented from current-value data.
- Every commit created by the Git commit skill ends with exactly one trailer for
  the invoking agent. Cursor uses `Cursor <cursoragent@cursor.com>` and Codex uses
  `Codex <noreply@openai.com>`.

## 5. Architecture and Design Principles

- Keep all dashboard changes in the presentation layer and compose pages from
  shared metric, section, responsive collection, selection, state, tab, modal,
  and wizard primitives.
- Use one shared Chakra `Select` wrapper throughout the dashboard. Pass custom
  trigger IDs through `Select.Root` machine IDs instead of overriding the trigger
  element ID.
- Portal select positioners outside dialogs so they escape clipping; render them
  inline inside focus-trapped dialogs so positioning and focus remain correct.
- Express visual hierarchy through theme tokens for surfaces, spacing,
  typography, radius, shadow, focus, and short motion rather than hardcoded
  colors or page-specific composed duplicates.
- Preserve keyboard behavior, screen-reader labels, focus restoration, and status
  meaning. Unknown and stale states must remain explicit and may not rely on
  color alone.
- Keep the canonical Git workflow in `skills/git-commit/SKILL.md` and synchronize
  the Codex-discovered copy under `.agents/skills/git-commit/`.

## 6. Functional Scope

- Authenticated app shell, responsive navigation drawer, Site scope, appearance,
  account controls, login, forbidden, not-found, and unexpected-error states.
- Overview KPIs, attention items, recent workflows, and integration health using
  existing aggregates only.
- Servers, Platforms, Workflows, Sites, Integrations, OS Images, and Templates,
  including filters, sorting, column preferences, selection, bulk actions,
  desktop tables, and mobile cards.
- Server, Platform, and Workflow detail workspaces, including local tabs,
  operational actions, timelines, logs, metrics, membership, and configuration.
- Deploy OS and Deploy Platform workflows with progress navigation, grouped form
  surfaces, gated navigation, and sticky actions.
- Monitoring meters and alert-first content with section-local filters.
- Shared Select usage in toolbars, forms, dialogs, popovers, tables, and wizards,
  preserving empty values, placeholders, disabled state, and change semantics.
- Semantic Playwright assertions and deterministic visual baselines for nine core
  screens at desktop and mobile sizes in light and dark appearances.

## 7. Constraints and Rules

- Backend APIs, domain types, authentication, routes, redirects, URL/query
  parameters, Site scope, permissions, and local-storage preference keys remain
  unchanged.
- Destructive confirmations, security and permission explanations, unknown data,
  and stale-data warnings may not be shortened away.
- Server's three status axes and their unknown/staleness semantics remain intact.
- Layouts must avoid page-level horizontal overflow from 320px upward.
- Selects retain an accessible name through `aria-label` or a label associated
  with the machine-managed trigger ID.
- The Git commit skill does not modify Git configuration, bypass hooks, change
  staging authorization, create empty commits, push without `--push`, or use the
  human author's identity for the agent trailer.
- The current agent trailer is the final footer and is deduplicated without
  removing trailers for other co-authors.

## 8. Data Model and Format Notes

- No backend field, endpoint, historical series, trend value, or operational
  capability is added by the dashboard work.
- Compact meters represent current monitoring values only; Grafana remains the
  historical-data workspace.
- The shared Select consumes explicit option objects and preserves existing
  string values, enum casts, empty-string choices, placeholders, required state,
  sizing, and width behavior.
- Commit messages continue to follow Conventional Commits 1.0.0. The agent
  trailer is a standard final footer in the form
  `Co-authored-by: <agent name> <agent email>`.

## 9. CLI / API / Config Notes

- Dashboard verification commands are `npm run lint`, `npm run build`, and
  `npm run test:e2e` from `dashboard/`.
- The completed visual regression matrix contains 36 baselines: nine screens,
  two viewports, and light/dark appearances.
- The Git skill keeps `/git-commit` and `$git-commit` arguments for staged-only,
  `--auto-add`, `--all`, `--push`, and `--date` workflows.
- Backdated commits set both `--date` and `GIT_COMMITTER_DATE` to the same value.
  Every commit path, including backdated commits, includes the agent trailer.
- No API or runtime configuration change is required.

## 10. Implementation Plan

The planned work is complete:

1. Unified native and Chakra select sites on one shared, positioned Chakra
   Select and removed the superseded implementations.
2. Retuned the theme and shell, introduced shared presentation primitives, and
   rebuilt overview, collection, detail, monitoring, form, and wizard layouts.
3. Removed obsolete Vite, PatternFly, CSS, i18n, and presentation aliases.
4. Applied review feedback to tabs, deployment workflows, Monitoring alerts, and
   Platform detail content order.
5. Migrated E2E selectors to semantic Chakra behavior and expanded deterministic
   desktop/mobile light/dark baselines.
6. Updated canonical and Codex Git commit skills to add, deduplicate, and verify
   the invoking agent's final co-author trailer.

Completed dashboard verification recorded by the source manuscripts: lint and
build passed, the final E2E suite passed with 95 tests, `git diff --check` passed,
and manual light-mode review covered Platform detail on desktop and mobile while
the deterministic suite covered both appearances.

## 11. Non-goals

- Backend, API-contract, domain-model, authentication, routing, or operator-flow
  changes.
- Rebranding, changing the established color family, or adding invented
  historical monitoring data.
- New UI frameworks, chart libraries, backend fields, endpoints, or operational
  actions.
- Changing Git author configuration, push defaults, hook policy, or staging
  permissions.

## 12. Open Questions

- No unresolved dashboard requirement remains in the source manuscripts.
- If another agent harness is added, define its stable co-author name and email
  before enabling the shared commit workflow for that harness.

## 13. Future Work

- Keep semantic E2E coverage and the 36 visual baselines synchronized with future
  presentation changes.
- Preserve the shared Select positioning invariants when Chakra internals or
  dialog behavior changes.
- Continue applying concise copy and responsive table/card parity to future
  dashboard routes.
- Add a stable trailer identity to the Git skill when support for another agent
  harness is introduced.
