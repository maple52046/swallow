# Commit Spec

This project uses **Conventional Commits 1.0.0**. This produces an explicit,
machine-readable history and maps cleanly onto SemVer.

> Source: <https://www.conventionalcommits.org/en/v1.0.0/>

## Structure

```
<type>[optional scope][!]: <description>

[optional body]

[optional footer(s)]
```

- **type**: a noun describing the change class (see below), followed by an
  optional scope and an optional `!`, then `:` and a space.
- **description**: a short summary in the imperative mood, on the same line.
- **body**: free-form, starting one blank line after the description; explains
  the *why* and context.
- **footers**: `Token: value` lines (e.g. `Refs: #123`), one per line.

## Types

- `feat`: a new feature (corresponds to SemVer MINOR).
- `fix`: a bug fix (corresponds to SemVer PATCH).
- Others (no SemVer bump by themselves): `build`, `chore`, `ci`, `docs`,
  `style`, `refactor`, `perf`, `test`.

## Scope

An optional noun in parentheses describing the affected area. In this repository
the natural scopes are the platform component or the shared document category:

- Platform component: `api-server`, `dashboard`.
- Shared documents / areas: `glossary`, `contracts`, `docs`, `standards`, `deploy`.

Examples: `docs(glossary): define allocation state`, `chore(deploy): update the
dev compose stack`.

## Breaking changes (SemVer MAJOR)

Indicate either way (both may be used together):

- Append `!` after the type/scope: `feat(api-server)!: change the response schema`.
- Add a footer: `BREAKING CHANGE: <what broke and the migration>`.

## Examples

```
feat(api-server): add pagination to the server list endpoint

refactor(dashboard): replace boolean flags with an explicit status type

fix(api-server): propagate detection errors instead of swallowing them

docs(glossary): split server terminology into per-term documents

chore(deploy): rename the environment prefix to SWALLOW_
```

## Rules

- Commit messages MUST be written in English (type, description, body, and
  footers), regardless of the language used in chat or code review.
- Type and description are required; everything else is optional.
- Keep the description concise and in the imperative ("add", not "added").
- One logical change per commit where practical.
- This is a single git repository (a monorepo). A change that spans components —
  for example an API contract plus its provider and consumer — is committed
  together here; there is no submodule-pointer step.
