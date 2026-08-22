# Dashboard Radix Migration and MAAS UI Port

## 1. Purpose

Deliver two ordered dashboard workstreams:

1. Migrate the whole dashboard UI library from Mantine 8 to Radix
   (`@radix-ui/themes`) — provider, theme, and every Mantine file — and remove
   the Mantine dependency entirely.
2. Port MAAS UI's machines list and machine summary UX onto Radix, built over
   the existing swallow server API.

## 2. Source Scope

- Consolidated from a single source manuscript:
  `docs/plans/manuscripts/20260821-maas-ui-port.md`.
- `docs/plans/manuscripts/README.md` is the consolidation spec and is not a
  source plan.
- Because there is one source, this document restructures that plan into the
  standard long-term sections without inventing designs the source does not
  support.

## 3. Consolidated Background

The dashboard originally used Mantine 8 as its component library, with
`@tabler/icons-react` for icons and `@mantine/notifications` for toasts. The
target UX reference is Ubuntu MAAS UI (`/home/bill/maas-ui`, React 19 + Redux +
Canonical Vanilla / `@canonical/react-components`), specifically its machines
list and machine summary screens. The intent is to adopt MAAS's layout and UX
while implementing it with our own stack, and in the same effort replace Mantine
with Radix so all new and existing UI share one foundation.

## 4. Confirmed Decisions

- MAAS UI reference source: `/home/bill/maas-ui`.
- Fidelity: adopt MAAS's layout/UX, implement with our stack's component
  library.
- UI library: switch to `@radix-ui/themes`. Scope is a **full migration**
  (replace Mantine everywhere and remove the dependency), not new-pages-only.
- Component gaps: it is acceptable to add `@radix-ui/react-icons` and Radix
  Primitives for what Radix Themes lacks (toast, accordion). Icons move to
  `@radix-ui/react-icons`.
- Feature scope for the ported pages: implement the full MAAS feature set now,
  trim later.

## 5. Architecture and Design Principles

- Clean Architecture layering holds. The UI-library swap touches only
  `src/presentation/**` and `src/main.tsx`. `src/domain/**`,
  `src/application/**`, and `src/infrastructure/**` must not gain any Radix
  import.
- Pure grouping/filter/sort helpers live in `src/domain/server/` with no React
  and no Radix dependency.
- Data access stays behind the `ServerRepository` port. Bulk actions fan out
  over the existing per-server `runServerAction`.
- Shared cards/rows are a single composable component reused by both the list
  and the detail views.

## 6. Functional Scope

Ported MAAS surfaces:

- **Machines list**: server-side coarse filter (`keyword`, `provisioningState`,
  `includeAbsent`) plus client-side grouping, multi-dimension filtering
  (zone/pool/tags/GPU), sortable/configurable columns, pagination, tri-state
  selection, and bulk actions with partial-failure feedback.
- **Machine summary**: detail page with nested-route tabs
  (Summary / Network / Storage / PCI) and a shared serverSummary card set.

Radix gap kit built under `src/presentation/components/radix/`:

- Pagination (custom), Toast (`@radix-ui/react-toast`, replacing
  `@mantine/notifications`), searchable Select/Combobox (Themes Popover +
  TextField + list), Accordion (`@radix-ui/react-accordion`) for the filter
  panel, the AppShell layout shell, and NavLink (react-router `NavLink` + Themes
  styling).

## 7. Constraints and Rules

- Coding-style gate: intent JSDoc on every new export and important boundary;
  status is never conveyed by colour only; shared cards/rows stay a single
  composable component reused across list and detail.
- No `@mantine` imports may remain after migration.
- Verification must pass tsc + lint + the coding-style gate + a browser check.

## 8. Data Model and Format Notes

MAAS-to-swallow field mapping:

- **Kept (via the projection)**: FQDN + MAC, Power, Status (+ ephemeral), Tags,
  Resource pool, Zone, Cores + Arch, RAM, Storage, GPUs; plus swallow-only axes
  Cluster and Health.
- **Dropped (no swallow data)**: Owner/tenancy, Fabric/VLAN/Spaces, physical disk
  count in the list, description/note, workload annotations, kernel crash dump.
- **Client-side only (no server-side API support)**: grouping, multi-dimension
  filtering (zone/pool/tags/GPU), and bulk actions. The list loads a working set
  by looping `listServers` pages; server-side `keyword` / `provisioningState` /
  `includeAbsent` remain the coarse filter.

Radix component mapping:

- **Direct Themes equivalents**: Text, Heading, Button, Card, Badge, Tooltip,
  Table, TextField, TextArea, Switch, Checkbox, Separator, ScrollArea, Avatar,
  IconButton, Spinner, Callout, DropdownMenu, Select (non-search), Flex (for
  Group/Stack/Center), Grid, Tabs, Popover, Dialog.
- **Not in Themes (built in the gap kit)**: Pagination, Toast, searchable
  Select/Combobox, Accordion, AppShell, NavLink.
- **Icons**: `@tabler/icons-react` → `@radix-ui/react-icons`.
- **Removed**: `@mantine/charts` (only a CSS import, no chart usage) and
  `@mantine/hooks` (unused).

## 9. CLI / API / Config Notes

- No backend or API changes. Everything runs against the current swallow server
  API.
- Light/dark persistence moves to Radix `<Theme appearance>` while keeping the
  existing `color-scheme` localStorage key.
- `<Theme>` in `main.tsx` replaces `MantineProvider` / `ColorSchemeScript` /
  `ModalsProvider` / `Notifications`.

## 10. Implementation Plan

1. Manuscript + read AGENTS / architecture / coding-style.
2. Radix foundation: install deps; mount `<Theme>` in `main.tsx` replacing the
   Mantine provider stack; port light/dark persistence to `<Theme appearance>`
   keeping the `color-scheme` localStorage key.
3. Radix gap kit: Pagination, Toast, searchable Select, Accordion, AppShell,
   NavLink.
4. Migrate existing UI: shared components, layout, auth/error/overview pages;
   remove Mantine deps and theme; update the spec docs' Mantine references.
5. Domain helpers: grouping/filter/sort/columns/display for servers.
6. Working-set loader over the `ServerRepository` port.
7. Shared table components (DoubleRow, SortableTh, GroupHeaderRow, selection,
   RowActionMenu).
8. List controls (search, group-by, filter popover, column toggle, page size,
   bulk bar).
9. Rewrite `ServersPage`.
10. Bulk-action fan-out + partial-failure UI.
11. Detail nested-route tabs (Summary/Network/Storage/PCI).
12. Shared `serverSummary` card set + Summary tab.
13. Verify: tsc + lint + coding-style gate + no `@mantine` imports + browser
    check.

## 11. Non-goals

- Backend changes. Everything runs on the current API.
- MAAS features with no swallow data (owner/tenancy, network fabric/VLAN/spaces,
  workload annotations).
- Bulk deploy (needs an image form) — deferred to a follow-up.

## 12. Open Questions

- None currently blocking. Conflicts across sources: none (single source).

## 13. Future Work

- Server-side grouping and multi-dimension filtering if fleets outgrow the
  client-side working-set loading.
- Bulk deploy via a shared side-panel form.
