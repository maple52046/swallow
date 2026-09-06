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

An optional noun in parentheses describing the affected area. Prefer the
component or the feature slice:

- Component: `api-server`.
- Feature slice or package: `server`, `auth`, `config`, `shared`, `proto`, `app`.
- Documents: `docs`, `contracts`.

Examples: `feat(server): add inventory update endpoint`,
`docs(contracts): promote server list contract to active`.

## Breaking changes (SemVer MAJOR)

Indicate either way (both may be used together):

- Append `!` after the type/scope: `feat(api-server)!: change the error envelope`.
- Add a footer: `BREAKING CHANGE: <what broke and the migration>`.

A change to a published API contract is a breaking change whenever an existing
consumer must be updated, even if the Go code compiles.

## Examples

```
feat(server): support keyword filtering on the server list

fix(server): stop leaking the reconcile goroutine on shutdown

refactor(auth): move token issuing behind a port

docs(contracts): add the auth login contract

test(server): cover duplicate hostname rejection
```

## Rules

- Commit messages MUST be written in English (type, description, body, and
  footers), regardless of the language used in chat or code review.
- Type and description are required; everything else is optional.
- Keep the description concise and in the imperative ("add", not "added").
- One logical change per commit where practical.
- A commit that changes API behavior must include the corresponding contract
  update under `docs/development/api-contracts/`.
