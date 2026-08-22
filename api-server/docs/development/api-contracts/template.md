# {API Name}

## Status

Draft / Active / Deprecated

## Owner Component

`{component-name}`

## Consumer Components

- `{consumer-component}`

## Purpose

Describe the purpose of this API and the boundary it represents.

## Related Glossary Terms

- `{term}`

## Endpoint / RPC

```text
METHOD /path
```

or

```text
service.method
```

## Authentication

Describe authentication requirements.

## Authorization

Describe authorization requirements.

## Request

### Headers

| Name            | Required | Description   |
| --------------- | -------: | ------------- |
| `Authorization` |      Yes | Bearer token. |

### Path Parameters

| Name | Type | Required | Description |
| ---- | ---- | -------: | ----------- |

### Query Parameters

| Name | Type | Required | Description |
| ---- | ---- | -------: | ----------- |

### Body

```json
{
}
```

## Response

### Success Response

```json
{
}
```

### Error Response

```json
{
  "error": {
    "code": "string",
    "message": "string"
  }
}
```

## Error Codes

| Code | HTTP Status | Description |
| ---- | ----------: | ----------- |

## Compatibility Notes

Describe backward compatibility rules, breaking changes, and migration notes.

## Implementation Notes

Optional notes for implementation details that should not leak into the public contract.
