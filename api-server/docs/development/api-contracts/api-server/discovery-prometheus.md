# Prometheus Service Discovery

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- Prometheus (per-site), via `http_sd_config`
- `cli` (the `swallow` operator command-line client)
- Swallow installation tooling
- External Ansible diagnostics and tools (the Ansible inventory route)

## Purpose

Serve the Prometheus HTTP service-discovery target list for host-layer exporters, with
the metrics label contract already attached, so a metric can be joined back to a swallow
server without a lookup. The same projection also targets one server type at a time — the
RDC exporter job asks for AMD GPU servers only — so a single endpoint feeds several scrape
jobs.

The same server projection is also served as an Ansible dynamic inventory for diagnostics and
external tools (see [Ansible Inventory](#ansible-inventory)); the embedded runner builds the
identical inventory through the same use case without calling HTTP.

## Related Glossary Terms

- [Server](../../../../../docs/development/glossaries/terms/server.md)
- [Server Type](../../../../../docs/development/glossaries/terms/server-type.md)
- [Server Status](../../../../../docs/development/glossaries/terms/server-status.md)
- [Server Lock](../../../../../docs/development/glossaries/terms/server-lock.md)
- [Exporter Ownership](../../../../../docs/development/glossaries/terms/exporter-ownership.md)

See also [decision 003](../../../../../docs/decisions/003-metrics-label-contract.md) for the
label contract and monitoring topology.

## Endpoints

```text
GET /api/v1/discovery/prometheus
GET /api/v1/discovery/ansible
```

The wire shape is published in
[`openapi/areas/monitoring.yaml`](../openapi/areas/monitoring.yaml) (`listPrometheusTargets`,
`getAnsibleInventory`).

## Authentication

Both endpoints use **machine authentication**, not the interactive login flow: the caller
presents a static machine bearer token, or, for operator inspection, an admin access token.

```text
Authorization: Bearer <machineToken | adminAccessToken>
```

A missing or invalid credential yields `401` with code `unauthorized`. A non-admin access token
yields `403` with code `forbidden`. API Keys are not accepted (`401`). When the installation
configures no machine token, only an admin access token works.

## Prometheus Targets

### Query Parameters

| Param | Type | Required | Default | Meaning |
| --- | --- | ---: | --- | --- |
| `port` | integer (1–65535) | No | `9100` | Exporter port to place in each target. `9100` is node-exporter; `5000` is the RDC exporter. |
| `tag` | string | No | — | Restrict targets to servers carrying this provisioner tag. The RDC exporter job passes `tag=amd-gpu` so it only targets AMD GPU servers. |
| `siteId` | string | No | — | Restrict to one site. |
| `provisioningState` | string | No | `deployed` | Provisioning state filter. The literal `all` removes the filter. |

`port` that is not an integer, or is out of range, yields `400` with code `validation_error`.

### Response

`200 OK` with a JSON array in Prometheus `http_sd` shape (`[]` when nothing matches). Each
entry is one target with its labels already attached:

```json
[
  {
    "targets": ["10.0.1.10:9100"],
    "labels": {
      "server_id": "srv-abc123",
      "site": "site-id",
      "platform_id": "platform-id",
      "cluster": "platform-id"
    }
  }
]
```

- `server_id` and `site` are always present. `platform_id` is present only when the server
  is a known platform member; `cluster` is then present too, as a deprecated one-release
  alias that mirrors `platform_id`
  ([ADR-014](../../../../../docs/decisions/014-platform-resource-language.md)). No other label
  is emitted.
- `targets` holds exactly one `address:port`: the server's primary address and the requested
  port.
- A server with no known address is omitted rather than emitted with an empty target,
  because a target with no address would create a permanently failing series attributed
  to that server.
- A locked machine remains in discovery when it otherwise matches the query. Lock blocks
  exporter installation, removal, and reconfiguration; it does not disable an exporter
  already running or turn observability into a mutation. Its effective Exporter Ownership
  is still `unmanaged`.
- The result is never paginated: a scrape target list must be complete.
- Label names are the canonical join keys from
  [decision 003](../../../../../docs/decisions/003-metrics-label-contract.md); do not add
  identity labels beyond this set.

## Ansible Inventory

`GET /api/v1/discovery/ansible` returns the same server projection as an Ansible dynamic
inventory, for diagnostics and external tools. Its host-variable and group vocabulary is the
playbook-facing contract defined in
[platform-deployment.md](../../../../../docs/development/platform-deployment.md) (§4.1
Dynamic inventory); this section states only the HTTP surface.

### Query Parameters

| Param | Type | Required | Default | Meaning |
| --- | --- | ---: | --- | --- |
| `siteId` | string | No | — | Restrict to one site. |
| `provisioningState` | string | No | `deployed` | Provisioning state filter. The literal `all` removes the filter. |

`port` and `tag` are ignored here: an inventory names hosts, not scrape targets, and
playbooks select a server type by its `tag_<tag>` group instead.

### Response

`200 OK` with a JSON object in Ansible's dynamic-inventory shape:

```json
{
  "_meta": {
    "hostvars": {
      "srv-abc123": {
        "ansible_host": "10.0.1.10",
        "server_id": "srv-abc123",
        "site_id": "site-id",
        "integration_id": "integration-id",
        "provider_machine_id": "abc123",
        "hostname": "gpu-node-01",
        "fqdn": "gpu-node-01.maas",
        "architecture": "amd64/generic",
        "cpu_cores": 64,
        "memory_mib": 524288,
        "provisioning_state": "deployed",
        "os_system": "ubuntu",
        "distro_series": "noble"
      }
    }
  },
  "site_site_id": { "hosts": ["srv-abc123"] },
  "provisioning_deployed": { "hosts": ["srv-abc123"] },
  "all": { "children": ["provisioning_deployed", "site_site_id"] }
}
```

- Hosts are keyed by Server id; `ansible_host` carries the address. A server with no known
  address is omitted.
- `_meta.hostvars.<serverId>` always carries `ansible_host`, `server_id`, `site_id`,
  `integration_id`, `provider_machine_id`, `hostname`, `fqdn`, `architecture`, `cpu_cores`,
  and `memory_mib`. `provisioning_state`, `os_system`, and `distro_series` appear when the
  provisioning axis is observed; `platform_id`, `platform_role`, and `platform_node_name` when
  the membership axis is observed; `default_user`, `image_default_user`, `gpu_vendors`
  (compute GPUs only), and `server_tags` only when known or non-empty.
- Every other top-level key except `all` is a group, `{"hosts": [serverId, ...]}`:
  `site_<id>`, `provisioning_<state>`, `platform_<id>`, `role_<role>`, `gpu_<vendor>`, and
  `tag_<tag>`, with every character outside `[a-z0-9_]` replaced by `_`.
- `all.children` lists every group name, sorted, and is `[]` (never absent) when there are no
  hosts.
- The result is never paginated: an automation inventory must be complete.

## Errors

The common error envelope applies. `400 validation_error` for a `port` that is not an integer
between 1 and 65535 (Prometheus targets only); `401 unauthorized` / `403 forbidden` per the
authentication section; `500 internal_error` for an unexpected failure.

## Compatibility Notes

- `tag` is additive and optional; a caller that omits it gets every discoverable server as
  before.
- The label set is a contract: adding a label used to identify a server is a decision
  document ([decision 003](../../../../../docs/decisions/003-metrics-label-contract.md)),
  not a compatible response change.
- The `cluster` label is a deprecated one-release alias of `platform_id`; scrape configs and
  queries read `platform_id`.
- The Ansible inventory route predates this section, which documents it as implemented.
