# CLI reference

[繁體中文](../../zh-TW/reference/cli.md) · [Documentation home](../README.md)

The `swallow` binary is the operator command-line client. It is a separate Go
module from `swallow-api` and communicates only through the published HTTP API.

## Install from source

No stable packaged release is currently published. Build the active-development
binary from the repository:

```bash
cd cli
go build -o bin/swallow ./cmd/swallow
bin/swallow --help
```

## Configure and authenticate

```bash
printf '%s\n' "$PASSWORD" |
  swallow --endpoint https://swallow.example \
  login -u admin --password-stdin
swallow auth me
```

The profile is stored in the user configuration directory with owner-only
permissions. Environment variables override the profile and flags override
environment variables. Use `logout` to clear the stored access token.

## Output and request bodies

- `table` is the interactive default.
- `-o json` and `-o yaml` are lossless scripting formats.
- `servers watch` emits SSE frames as JSON lines.
- Structured mutations accept `--file` with JSON, YAML, or `-` for stdin.

Prefer explicit `--site-id` in scripts. A configured global Site is convenient
for interactive work but can make automation ambiguous.

## Command groups

`auth`, `login`, `logout`, `overview`, `sites`, `integrations`,
`servers`, `provisioning`, `infrastructure`, `ssh-keys`, `platforms`, `workflows`,
`monitoring`, and `discovery` expose the CLI's implemented operator surface.
Managed Software currently uses the Dashboard or HTTP API. Deprecated aliases
and Planned endpoints are intentionally absent.

Read the complete [CLI usage manual](../../../cli/docs/usage.md) for flags,
examples, exit codes, and recipes.
