# Servers List

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Lists the complete, secret-free Server projections with optional filtering and
pagination. This inventory surface includes the independently observed
provisioning, platform-membership, and health axes.

## Related Glossary Terms

- `Server`
- `Server Status`

## Endpoint / RPC

```text
GET /api/v1/servers/
```

## Authentication

Bearer token required. See [`conventions.md`](conventions.md).

## Authorization

`admin` only. A non-admin authenticated caller receives `403 forbidden`.

## Request

### Headers

| Name | Required | Description |
| --- | -------: | --- |
| `Authorization` | Yes | Bearer token. |

### Query Parameters

| Name | Type | Required | Description |
| --- | --- | -------: | --- |
| `page` | integer | No | Page number; default `1`. See [`conventions.md`](conventions.md). |
| `pageSize` | integer | No | Items per page; default `20`, max `100`. |
| `siteId` | string | No | Return Servers observed through integrations at this Site. |
| `integrationId` | string | No | Return Servers observed through this integration. |
| `provisioningState` | string | No | Filter on the provisioning axis state. |
| `platformId` | string | No | Return Servers whose membership axis names this Platform. |
| `clusterId` | string | No | Deprecated one-release alias for `platformId`; ignored when `platformId` is also present. |
| `keyword` | string | No | Case-insensitive match on hostname, FQDN, address, serial number, or system UUID. |
| `includeAbsent` | boolean | No | Include projections absent from the latest provider inventory; default `false`. |

Filters are conjunctive. `keyword` is a search convenience, not a structured
query language. `includeAbsent=true` changes visibility only.

## Response

### Success Response

`200 OK`, using the shared pagination envelope:

```json
{
  "items": [
    {
      "id": "server-id",
      "source": {
        "siteId": "site-id",
        "integrationId": "integration-id",
        "providerMachineId": "machine-42"
      },
      "hostname": "gpu-42",
      "addresses": ["192.0.2.42"],
      "gpus": [{"vendor": "AMD", "model": "MI300X", "count": 8}],
      "hardware": {"systemUuid": "uuid", "serialNumber": "serial", "macAddresses": []},
      "provisioning": {
        "state": "deployed",
        "powerState": "on",
        "osSystem": "ubuntu",
        "observedAt": "2026-05-02T15:00:00Z"
      },
      "membership": {
        "platformId": "platform-id",
        "clusterId": "platform-id",
        "role": "worker",
        "state": "ready",
        "observedAt": "2026-05-02T15:00:00Z"
      },
      "health": {"state": "up", "observedAt": "2026-05-02T15:00:00Z"},
      "absent": false,
      "lastSeenAt": "2026-05-02T15:00:00Z",
      "createdAt": "2026-05-02T15:00:00Z",
      "updatedAt": "2026-05-02T15:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "pageSize": 20
}
```

This is the same complete Server projection returned by the detail endpoint.
Optional observations use `null`; an unavailable axis is not assigned a default
state. No BMC, SSH, integration, or automation credential is returned.

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `unauthorized` | 401 | Token is missing, malformed, or expired. |
| `forbidden` | 403 | Caller's role is not `admin`. |

An unknown filter value or an out-of-range `pageSize` is a client error; the
contract does not currently define a distinct code for it, so consumers must send
only documented values.

## Compatibility Notes

Adding an optional projection field is backward compatible. Adding a credential
field is forbidden. The three state axes and `absent` remain independent.

Following the Cluster to Platform rename, the `clusterId` query parameter and the
membership `clusterId` response field are deprecated one-release aliases for
`platformId`. When both are sent, `platformId` wins; the aliases are removed after the
deprecation window (see
[ADR-014](../../../../../docs/decisions/014-platform-resource-language.md)).

## Implementation Notes

Filtering and pagination are pushed to the repository port; the handler only
parses and shapes.
