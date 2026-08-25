# 005. Consolidate the platform into a single swallow monorepo

- Status: Accepted
- Date: 2026-08-22

## Context

The platform began as a git submodule *superproject* named `gdcm` (GPU Datacenter
Management). Business code lived in two submodules under `src/` — `src/swallow`
(Go backend) and `src/dashboard` (TypeScript frontend) — and the repository root
exposed each platform component through a symlink (`api-server`, `agent`,
`dashboard`). The root owned only the shared model: glossary, API contracts,
ADRs, and the structure contract.

Two things changed the calculus:

1. The product is installed on a single login / management node, not as a
   distributed fleet agent. The node-side `agent` was already retired in
   [ADR-001](001-system-ownership-boundaries.md); node control is delegated to
   AWX / Ansible ([ADR-004](004-automation-via-awx.md)). The original
   distributed-systems motivation for keeping separate repositories no longer
   applies.
2. The platform has no external consumers yet. The submodules had no independent
   release cadence, no third-party consumers, and no separate access-control
   boundary — one team ships one product on one release train.

The submodule layout therefore only imposed cost: two-step commits (submodule
first, then a pointer bump), cross-repository pull requests for a single logical
change, and the constant `--recurse-submodules` footgun — all of which work
*against* the platform's own goals of a single source of truth, reuse-first
development, and atomic cross-context evolution.

The brand name `gdcm` was also confusingly close to NVIDIA DCGM (Data Center GPU
Manager), a common tool in exactly this GPU-datacenter domain, and to the
unrelated Grassroots DICOM (`gdcm`) library. The decision was taken to promote
the existing backend codename `swallow` to the platform brand.

## Decision

Consolidate everything into a single repository named **swallow**, and rebrand
the platform from `gdcm` to `swallow`.

- The two submodules become ordinary top-level directories, with git history
  preserved via `git subtree`:
  - `src/swallow` -> `api-server/`
  - `src/dashboard` -> `dashboard/`
- The `agent`, `api-server`, and `dashboard` root symlinks, `.gitmodules`, and
  the `src/` tree are removed.
- Component boundaries remain, but as **logical** boundaries enforced by
  top-level directories, `docs/` contracts, and the glossary — not by separate
  git repositories.
- The rebrand is comprehensive: prose, the Go module path
  (`github.com/AFDEAPAC/swallow` -> `github.com/maple52046/swallow`), the
  environment-variable prefix (`GDCM_` -> `SWALLOW_`), the in-container config
  path (`/etc/gdcm` -> `/etc/swallow`), and AWX extra-var keys
  (`gdcm_operation_id` -> `swallow_operation_id`, etc.). The lowercase `swallow`
  denotes the running service; `Swallow` denotes the platform brand.
- The backend binary keeps the name `swallow`: it is now unambiguously "the
  Swallow platform server", so no rename is warranted.
- The top-level checkout folder is intentionally left as-is for now to keep the
  editor workspace stable; renaming it is out of scope for this decision.

## Alternatives considered

- **Keep the submodule superproject.** Rejected: it preserves independence the
  platform does not use, while charging the daily submodule tax. The independence
  it buys (separate release, access control, external consumers) has no current
  driver; when a concrete driver appears, a single component can be extracted
  back out of a monorepo — history-preserving — far more cheaply than the reverse.
- **Promote the backend repo (`AFDEAPAC/swallow`) to be the platform repo.** A
  reasonable option (the name and org were already correct), but it makes a single
  component's history the trunk. Rejected in favour of keeping the superproject's
  platform-governance history (`docs/`, ADRs, structure contract) as the trunk;
  both component histories are still preserved under their subdirectories via
  subtree.
- **Rebrand only the marketing name, leaving `gdcm` in code and env.** Rejected:
  the confusing token would remain throughout the codebase, and there are no users
  yet — this is the cheapest moment to rename comprehensively.

## Consequences

- A cross-component change (for example an API contract plus its provider and
  consumer) is now one commit and one review, matching the platform's
  single-source-of-truth intent.
- No more submodule lifecycle: cloning, syncing, and CI are simpler.
- The risk shifts from cross-repo drift to in-repo boundary erosion. This is
  mitigated by the structure contract, the provider-owned API contracts, and the
  architecture spec's prohibition on `common` / `util` / `shared` dumping grounds.
- `go build` / `go test` verification of the module rename was not run at
  migration time because the Go toolchain was absent on the migration host; the
  change is a mechanical 1:1 rewrite of the module path and imports.

## Current status

Implemented on the `chore/swallow-monorepo` branch: submodules removed, code
imported with history under `api-server/` and `dashboard/`, module and env
prefixes rebranded, and the structure-contract docs and Cursor harness updated to
the monorepo model.
