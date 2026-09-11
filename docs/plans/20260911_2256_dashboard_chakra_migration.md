# Dashboard Chakra UI v3 Migration — Consolidated Plan

## 1. Purpose

Capture the long-term record of migrating the `dashboard` component from PatternFly 6
to Chakra UI v3. The migration is a single big-bang replacement of the UI library
that preserves full functional parity while delivering a fresh, modern visual
language. This document is the durable reference for what was decided, what was
built, the constraints that hold, and what remains open — so future UI work stays
consistent with the migration's architecture and design intent.

## 2. Source Scope

- Consolidated from the single manuscript
  `docs/plans/manuscripts/20260911-dashboard-chakra-migration.md`
  ("Dashboard: Migrate PatternFly 6 to Chakra UI v3"), status: complete.
- Also records the final palette decisions confirmed with the user during
  implementation (accent/background/logo), which refine — and are supported by — the
  manuscript's design-language section.
- Scope is the `dashboard/` component only. `api-server/`, provider-owned API
  contracts, glossary, and shared `docs/` model are out of scope and unchanged.

## 3. Consolidated Background

The dashboard was previously built on PatternFly 6 (`@patternfly/react-core`,
`react-table`, `react-icons`, `react-log-viewer`) spread across ~60 files (~18.7k
LOC), with ~950 lines of `index.css` using `--pf-t--*` tokens, `.pf-v6-*` overrides,
and ~117 `.sw-*` structural classes. Project docs had drifted (they referenced
"Radix Themes" while the code used PatternFly, and ESLint restricted imports to
"PatternFly 6 only"). The visual language read as an old-style enterprise console.

The migration replaces PatternFly with Chakra UI v3 in one pass, keeps Lucide icons
and the hand-drawn `SwallowLogo`, and re-tunes the look through a custom Chakra theme
plus component swaps, without changing domain/application layers or API contracts.

## 4. Confirmed Decisions

- **Library**: `@chakra-ui/react` v3 (3.35+) with peer `@emotion/react`; color mode
  via `next-themes`. Node 20+ (env is Node 24).
- **Strategy**: big-bang migration in one deliverable. PatternFly stayed installed
  during the work so intermediate `tsc -b` could run, and was removed at the tooling
  step. End state: zero `@patternfly` / `pf-v6-` / `--pf-t--` in `src`; deps pruned.
- **Icons / logo**: keep Lucide (`lucide-react`) for all icons; keep the hand-drawn
  `SwallowLogo`.
- **Final palette (user-confirmed this session)**:
  - Default color mode is **dark**; the dark canvas is near-black
    (`gray.950 = #0a0a0b`) with panels lifted one step (`gray.900 = #171719`).
  - Neutrals are a **neutral cool-grey** (zinc-like) scale so nothing clashes with
    the accent (earlier cool-slate and green-tinted "sage" variants were rejected).
  - Accent (`brand`) is a **blue** scale; `brand.solid` = blue.600 (light) / blue.500
    (dark, brighter so it pops on black). Used only for primary interaction,
    selection, focus, links, and key status.
  - The `SwallowLogo` uses a **fixed brand blue `#0066cc`**, intentionally decoupled
    from the theme accent (it must not change when the accent changes).
- **Theme source of truth**: `src/presentation/app/theme/system.ts` via
  `createSystem(defaultConfig, defineConfig(...))` — brand scale, `gray` override,
  semantic tokens (`bg`/`bg.subtle`/`bg.panel`/`border`/`brand.*`), radii, fonts, and
  `globalCss`.
- **Structural CSS retained**: the `.sw-*` layout classes stayed in `index.css` but
  were retuned to Chakra CSS variables (`--chakra-*`) rather than fully rewritten as
  components; all `.pf-v6-*` and `--pf-t--*` were removed; `SwallowLogo` was made
  self-contained (its `.sw-swallow-logo*` classes dropped). Soft elevation shadows
  were added to framed surfaces for depth.
- **No generic `DataTable`**: PatternFly `react-table` was swapped near 1:1 to Chakra
  `Table.*` per page (wrapped in the shared `StickyTableFrame`) to preserve each
  table's bespoke behaviour (sticky columns, group rows, sortable headers,
  selection). This supersedes the manuscript's earlier idea of a generic `DataTable`.
- **Shared wizard**: `components/Wizard.tsx` on Chakra `Steps`, supporting per-step
  validity gating and an async Next gate (used by the OS wizard's `checkTargets`).
- **`components/ui/*` snippets**: hand-authored Chakra compositions (provider,
  color-mode, toaster, tooltip, modal, checkbox, alert, native-select, search-input,
  description-list) that intentionally export helper hooks/stores alongside a
  component; `react-refresh/only-export-components` is disabled for that directory.

## 5. Architecture and Design Principles

- **Clean Architecture unchanged**: only `src/presentation/**` (plus `main.tsx`,
  `di/AppProvider`, `index.css`, tooling, docs) changed. `domain/` and `application/`
  do not import React/Chakra/Emotion/router/HTTP/storage; `presentation/` must not
  import `@/infrastructure/**` (still ESLint-enforced).
- **UI may be redesigned freely** (layout, composition, visual language) — no need to
  mirror the PatternFly layout — but **functional parity is mandatory**.
- **Design language**: steady, low-chroma neutral base; depth via subtle elevation +
  borders, not dense gridlines; one restrained accent used sparingly; modern sans
  typography, slightly larger radii, generous spacing; steady ≠ rigid (tasteful
  emphasis, soft elevation, and motion are allowed).
- **Accessibility**: status is never conveyed by colour alone (badges/indicators
  always carry text or an `aria-label`); form fields have labels; icon-only controls
  have accessible names + tooltips; a skip-to-content link exists; Chakra Dialog/Drawer
  own focus trapping.
- **DRY**: one shared, composable component per shared feature (states, badges,
  dialogs via a shared `Modal`, selects, toaster, log viewer, wizard).

## 6. Functional Scope

- All routed screens migrated with parity: overview, servers (list + detail tabs +
  all dialogs: delete/release/power/lock, action menu, action-result, deployment
  failure), platforms (list/detail, deploy wizard, lifecycle/bulk actions,
  Kubernetes/Slurm views), provisioning (OS deploy wizard, templates, OS images),
  operations (list/detail, durable operation detail, log/event workspaces),
  infrastructure (sites/integrations + dialogs), monitoring, auth (login), errors
  (403/404/500).
- App shell rebuilt in Chakra: `OperatorShell` / `OperatorHeader` / `OperatorSideNav`
  / `OperatorLayout` with a Flex/Grid layout, `Menu`-based site/appearance/account
  menus, a `Drawer` mobile nav, and a skip link.
- In-house replacements: a lightweight `LogViewer` (mono view + search highlight +
  match navigation + copy/download + stdout/stderr) replaces
  `@patternfly/react-log-viewer`; wizards rebuilt on Chakra `Steps`.

## 7. Constraints and Rules

- Preserve every route, action, state (loading/empty/error/success/permission-denied),
  filter/sort/pagination/column toggle, dialog, wizard step + validation, toast, and
  permission visibility, with unchanged domain semantics (e.g. status glossary values
  still flow through filters/payloads/persistence; a label may differ from the value).
- Domain/application layers and provider-owned API contracts do not change.
- Coding-style completion gate applies: JSDoc on exports and important boundaries,
  accessibility, real-data behaviour, and layer direction must hold; `npm run lint`
  and `npm run build` must be green.
- ESLint bans `@patternfly/*` imports (regression guard) and keeps the
  presentation → `@/infrastructure/*` ban.

## 8. Data Model and Format Notes

- **Appearance storage**: color mode persists under `localStorage` key
  `swallow.appearance` (unchanged key). The stored value format changed from a JSON
  string to a plain `next-themes` string (`system` | `light` | `dark`) — an accepted
  one-time format change. Default is now `dark`.
- **Theme tokens**: brand scale (blue 50–950) + overridden `gray` (neutral cool-grey
  50–950, near-black deep end); semantic tokens map `brand.solid/contrast/fg/muted/
  subtle/emphasized/focusRing` and `bg.*` / `border.*` for both modes; consumed in CSS
  via `--chakra-*` variables and in components via Chakra style props.
- No domain data model or API DTO shapes were changed by the migration.

## 9. CLI / API / Config Notes

- **package.json**: added `@chakra-ui/react`, `@emotion/react`, `next-themes`; removed
  all `@patternfly/*`; lockfile pruned. Scripts unchanged (`dev`/`build`/`lint`/
  `test:e2e`/`preview`).
- **vite.config.ts**: added `server.host: true` and `server.allowedHosts: true` so the
  dev server works behind the Cursor preview proxy / container hostname (Vite 7 blocks
  non-localhost hosts by default). Existing `/api`, `/healthz`, `/livez`, `/readyz`
  proxies and the `@` → `src` alias are unchanged.
- **eslint.config.js**: `no-restricted-imports` bans `@patternfly/*` globally and the
  presentation layer additionally bans `@/infrastructure/*`; an override disables
  `react-refresh/only-export-components` for `src/presentation/components/ui/**`.
- **Docs/README**: `dashboard/docs/development/architecture-spec.md`,
  `coding-style.md`, `commit-spec.md`, and `README.md` were re-pointed from
  Radix/PatternFly to Chakra UI v3.

## 10. Implementation Plan

Executed (and the canonical order for any similar future migration):

1. Dependencies + manuscript (add Chakra; keep PatternFly installed during migration).
2. Theme system (`theme/system.ts`).
3. Provider + color mode (`main.tsx`, `components/ui/{provider,color-mode,toaster}`,
   appearance hook).
4. App shell (`OperatorShell`/`Header`/`SideNav`/`Layout`).
5. Shared component library + new shared abstractions (`Modal`, `LogViewer`, `Wizard`,
   `SingleSelect`, states/badges/primitives, `ui/*` snippets).
6. Pages: servers.
7. Pages: platforms + provisioning (two wizards on `Steps`).
8. Pages: operations + infrastructure + monitoring + overview + auth + errors.
9. Global CSS rewrite (drop pf tokens/selectors; retune `.sw-*` to `--chakra-*`; add
   elevation).
10. Tooling (ESLint ban `@patternfly/*`, remove PF deps) + docs + README.
11. Verify: lint + build green; zero pf residue; coding-style / a11y / JSDoc gate;
    functional parity check; headless-browser render smoke of `/login` (light + dark).

## 11. Non-goals

- Changing domain/application logic, provider-owned API contracts, glossary terms, or
  data models.
- Rewriting the `.sw-*` structural layout into Chakra components (intentionally
  retained, retuned to Chakra variables).
- Introducing a generic `DataTable` abstraction.
- Adding a charting library (monitoring is table-based; none was needed).
- Bundle-size optimization / code-splitting (the single ~1.1 MB JS chunk is a known
  Vite warning, not a migration goal).

## 12. Open Questions

- Whether to force dark-only or keep the tri-state appearance control with a dark
  default (currently: dark default, light/system still selectable).
- Whether the large single JS bundle warrants `manualChunks` / lazy route splitting.
- Whether any residual `.sw-*` structural styles should eventually become first-class
  Chakra components/recipes for stronger theme cohesion.
- Status "success = green" vs. a green/blue accent overlap is a latent concern if the
  accent ever returns to green (mitigated because status badges always carry text).

## 13. Future Work

- Optional: convert remaining `.sw-*` structural classes into Chakra recipes/slot
  recipes for full token-driven theming.
- Optional: route-level code splitting to shrink the initial bundle.
- Optional: add a unit/e2e test runner (Playwright is installed; no script yet) and
  cover the shared components (states, badges, `Modal`, `Wizard`, `LogViewer`).
- Keep docs and ESLint aligned with Chakra as the UI evolves; never reintroduce
  PatternFly.
