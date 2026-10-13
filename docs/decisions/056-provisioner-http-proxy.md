# 056. Provisioner HTTP Proxy written through from the Integration

- Status: Accepted
- Date: 2026-10-13

## Context

Machines MAAS deploys fetch packages during installation. At sites where they cannot reach
package repositories directly, MAAS has to hand them a proxy: its own built-in proxy, an
external proxy such as a site Squid, or its built-in proxy forwarding to an upstream peer. MAAS
holds this as three region settings (`enable_http_proxy`, `use_peer_proxy`, `http_proxy`), so an
operator had to leave swallow and use the MAAS UI or CLI before a deployment could succeed.

The Integration already carries `settings`, a string map. Those settings describe how swallow
reaches the provisioner (`timeout`, `insecureSkipVerify`) and swallow's own policy for it
(`autoInspect`); MAAS never sees them.

## Decision

- **The proxy is provisioner-owned and written through.** A provisioner Integration's
  Provisioner HTTP Proxy is read live from the provider and written through to it from the API
  and the Dashboard; swallow stores nothing. MAAS calls are `GET /maas/?op=get_config` and
  `POST /maas/?op=set_config`, one per setting, inside the MAAS adapter.
- **A closed mode set, not three flags.** Operators choose `none`, `builtin`, `external`, or
  `peer`, matching the MAAS UI; the adapter maps a mode onto the three settings and derives the
  mode back with MAAS's own rule for which proxy a deployed machine gets. A write always replaces
  all three settings, so a read after a write reports what was written.
- **Its own Integration-scoped route.** `GET` and `PUT /provisioning/integrations/{id}/http-proxy`,
  behind a provider capability, so a provisioner without one refuses rather than ignores it.
- **No credentials in the URL.** A proxy URL with a user or password is refused: the setting is
  readable back, and swallow does not return secrets.

## Alternatives considered

- **Store the proxy in Integration `settings` and push it on save:** rejected. `settings` would
  hold a second copy that drifts from MAAS when someone changes MAAS directly, and the PATCH of a
  swallow record would gain a provider side effect it cannot roll back.
- **Expose the three MAAS settings as-is:** rejected. Several combinations mean the same thing
  (`use_peer_proxy` without a URL is the built-in proxy), and MAAS vocabulary would enter the API.
- **Edit the generated `maas-proxy` (Squid) configuration:** rejected. MAAS regenerates and
  overwrites it.

## Consequences

- New contract `provisioner-http-proxy.md`, glossary term Provisioner HTTP Proxy, provider
  capability `HTTPProxy`, and an HTTP Proxy section in the Dashboard's MAAS Integration dialog.
- The MAAS account behind the Integration must be an administrator to write the setting.
- The three `set_config` calls are not atomic. A failure part-way leaves a mixed state that the
  next read reports; repeating the write converges.
- A change affects only deployments MAAS starts afterwards.

## Current status

Implemented in `api-server` and `dashboard`. The CLI has no command for it yet.
