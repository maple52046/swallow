# 044. Docker registry credentials: swallow-owned, sealed, resolved per pull

- Status: Accepted
- Date: 2026-10-02
- 現行規格：[software-deployment.md](../development/software-deployment.md) §4.7 Docker CE 的 Registry Credential

## Context

The Docker Host Explorer ([ADR 043](043-docker-host-management.md)) pulls images through each
host's Docker Engine API. Its first version supported only anonymously readable registries, so
private images — an internal Harbor, a private Docker Hub repository — could not be pulled.

The Docker Engine API does not use credentials stored on the host: `docker login` writes the
*CLI's* `config.json`, while an Engine API pull authenticates only with the `X-Registry-Auth`
header the caller sends. Logging in on each host therefore does not help; the credential has to
travel with swallow's pull request. The user chose to store registry credentials in swallow rather
than type them for every pull.

## Decision

- **A Registry Credential is swallow-owned, installation-wide data.** It records one registry (a
  normalized host with optional port), a username, and a password or token; there is at most one
  credential per registry. The password is sealed with the installation credential key, like
  integration credentials and the Deployment Key
  ([ADR 039](039-ssh-key-management-and-default-user.md)), is write-only through the API, and is
  never logged.
- **Credentials are resolved per pull from the image reference**, by Docker's own registry rule
  (a reference without a registry host is Docker Hub). When a credential exists for that registry,
  api-server sends it to the Engine as `X-Registry-Auth` for that one request; otherwise the pull
  is anonymous. Nothing is written to the host.
- **Scope is the installation, not the Site or the Server.** Registries are organization-wide and
  named by host, so different registries for different Sites already coexist. Per-Site or
  per-Server credentials for the same host are deferred until there is a need.
- **ADR 043's "nothing Docker-owned is persisted" still holds.** A Registry Credential is swallow's
  own secret used to act on the Engine, not a mirror of an Engine object.

## Alternatives considered

- **Type the credential in the pull dialog every time, never stored:** rejected by the user in
  favour of stored credentials; it would also not serve future automated pulls.
- **`docker login` on each host through Ansible:** rejected — the Engine API ignores the CLI's
  stored credentials, so it would not affect swallow's pulls, and it would spread secrets to disk
  on every host.
- **Site-scoped or Server-scoped credentials:** deferred — host-keyed credentials already separate
  registries; finer scoping adds a precedence model without a current use.
- **Verify the credential against the registry when it is saved:** deferred — verification needs a
  host to dial from (the Engine's `/auth`); a wrong credential surfaces on the first pull with the
  registry's own reason.

## Consequences

- Credentials are a Docker CE setting, not a software-wide one: they live with Docker CE in the API
  and the dashboard, and later kinds add their own settings.
- The credential crosses the network to the host in the clear, because ADR 043's Engine API
  listener is plain HTTP. On the internal networks that listener is limited to, anyone able to read
  that traffic can already control the host; the hardening slice (TLS) covers both.
- Rotating the installation credential key makes stored registry passwords unreadable, as with
  every other sealed credential; they must be re-entered.
