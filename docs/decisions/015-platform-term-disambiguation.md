# 015. swallow names the system; Platform names the managed runtime aggregate

- Status: Accepted
- Date: 2026-09-05

## Context

The word "platform" was overloaded. swallow itself was routinely called "the platform" /
"the swallow platform" in `AGENTS.md`, `codebase-structure.md`, and `architecture-spec.md`,
while the domain also has a capital-P `Platform` aggregate — a registered or Swallow-deployed
Kubernetes or Slurm runtime, renamed from Cluster in [ADR 014](014-platform-resource-language.md)
on 2026-09-03. A third, generic usage ("platform-wide", "platform component") layered on top.
The same word therefore denoted the whole system, one managed runtime, and a generic
adjective, which made "reorganize the platform definition" itself ambiguous.

## Decision

`swallow` is the only name for the system this repository builds. Refer to the system,
product, and codebase simply as `swallow`.

Capital-P `Platform` is reserved for the domain aggregate defined in ADR 014: a registered
or Swallow-deployed Kubernetes or Slurm runtime.

The common noun "platform" (and Chinese「平台」) must not denote swallow itself. Where a
concept applies across the whole system, say "swallow-wide" rather than "platform-wide";
where it refers to one of swallow's build/test units, say "component" rather than "platform
component". "monorepo" and "component" remain structural descriptions of how swallow is
organised, not alternative names for swallow.

## Alternatives considered

- Rename the aggregate away from `Platform`: rejected because ADR 014 had just renamed it
  from Cluster; a second rename two days later is pure churn and re-opens a settled decision.
- Keep calling swallow "the platform" and disambiguate by context: rejected because that
  reliance on context is exactly what made the definitions feel disordered.
- Coin a distinct product name such as "swallow control plane" or "swallow product":
  rejected because it adds a synonym for something that already has a perfectly good name.

## Consequences

- The glossary gains a `swallow` term and keeps the `Platform` term, each listing the other
  under Disallowed meaning so the boundary is explicit and enforceable.
- `AGENTS.md`, `codebase-structure.md`, `architecture-spec.md`, and API/contract docs stop
  calling swallow "the platform" and stop using "platform component"; existing occurrences
  are corrected as part of the platform-definition-reorg work.
- Bounded-context labels in glossary terms that meant "system-wide" become "swallow-wide".

## Current status

Accepted. Documentation alignment is tracked by
`docs/plans/manuscripts/20260905-platform-definition-reorg.md`.
