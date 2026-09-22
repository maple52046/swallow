# CLI Coding Style

The `cli` component follows the swallow Go coding style defined at
`../../../api-server/docs/development/coding-style.md`. That document is
authoritative for formatting, naming, comments, error handling, concurrency, and
the documentation completion gate. This file records only the additions specific
to the CLI.

## Baseline

- Go 1.25, module `github.com/maple52046/swallow/cli`.
- Dependencies are kept minimal: `spf13/cobra` for the command tree and
  `gopkg.in/yaml.v3` for config and YAML input. Prefer the standard library
  (`net/http`, `text/tabwriter`, `encoding/json`) over new dependencies.
- Before claiming done: `gofmt -l .` (no output), `go vet ./...`,
  `go build ./...`, `go test ./...`.

## Command conventions

- One file per resource group under `internal/command`, named after the group
  (`servers.go`, `provisioning.go`, ...). Each exposes a single
  `newXCommand() *cobra.Command` constructor registered in `root.go`.
- Commands map one-to-one onto Active api-server endpoints. A command's `Short`
  help states what it does; where behavior is subtle (deprecated routes omitted,
  machine auth, no `create` for reconciled resources) a `Long` or a code comment
  cites the reason and the contract.
- Read commands use flags for path and query parameters and print through the
  shared `getJSON` helper. Write/action commands use `sendJSON`/`sendNoContent`
  and accept a `--file` body for structured payloads, with convenience flags only
  where the contract fields are simple and explicit.
- Do not duplicate request/response plumbing in a command. Add or extend a shared
  helper in `helpers.go` instead, so every command builds requests, applies auth,
  and renders output the same way.

## Comments

- The package comment on each `internal/*` package states its responsibility and
  the component boundary (consumer of the api-server contract; no api-server
  internals).
- Exported types and functions in `client`, `config`, and `output` carry doc
  comments covering contract, caller obligations, error semantics, and lifecycle
  — especially credential handling, the error envelope, TLS behavior, and stream
  ownership. Comments that only restate an identifier are treated as missing.
