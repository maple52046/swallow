# API integration

[繁體中文](../../zh-TW/reference/api-integration.md) · [Documentation home](../README.md)

The `api-server` component owns swallow's HTTP API. External clients must use
the active provider-owned contracts and must not infer fields from private Go
types or Dashboard traffic.

## Base URL and authentication

Routes are served under `/api/v1`. Login exchanges a username and password for
an opaque bearer token:

```bash
API=https://swallow.example/api/v1
TOKEN=$(curl --fail --silent \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"REDACTED"}' \
  "$API/auth/login" | jq -r .accessToken)

curl --fail --silent \
  -H "Authorization: Bearer $TOKEN" \
  "$API/auth/me"
```

Treat the JWT as opaque. Read identity and role from `/auth/me`, and
reauthenticate after `401`.

Machine endpoints such as Prometheus discovery may accept the configured
machine bearer token. That token is not an alternative login for operator
endpoints.

## Errors

Errors use one envelope:

```json
{
  "error": {
    "code": "validation_error",
    "message": "human-readable detail",
    "requestId": "opaque-correlation-id"
  }
}
```

Branch on `code`, never parse `message`, and preserve `requestId` for logs.
The shared contract maps codes to HTTP statuses.

## Pagination and timestamps

Large list endpoints accept `page` and `pageSize` and return `items`,
`total`, `page`, and `pageSize`. Timestamps are ISO 8601 UTC. Individual
contracts state when a bounded collection returns a plain array instead.

## Identity and staleness

- Use opaque resource IDs. Do not key integrations on hostname or address.
- Treat unknown enum values conservatively.
- Preserve `null` as unknown.
- Read sync and observation timestamps before making a decision from cached
  provider data.
- Credentials are write-only; use `hasCredential` rather than expecting a
  redacted value.

## Contract directory

Start with:

1. [HTTP conventions](../../../api-server/docs/development/api-contracts/api-server/conventions.md)
2. [Active contract outline](../../../api-server/docs/development/api-contracts/api-server/outline.md)
3. The specific active contract for the resource being integrated.

Do not implement against entries marked Planned. Deprecated `/operations` and
`/clusters` aliases are migration surfaces; new clients use `/workflows` and
`/platforms`.

For shell automation, the [swallow CLI](cli.md) implements broad operator
coverage and emits lossless JSON or YAML. Managed Software currently requires
the Dashboard or HTTP API.
