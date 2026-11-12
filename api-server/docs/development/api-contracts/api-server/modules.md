# Modules

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Lists the Extension Modules compiled into the running build, with the spec schema each module's
manifest declares. A client renders a generic settings form and details from a schema for a module
it has no dedicated screens for ([`core-and-extensions.md`](../../../../../docs/development/core-and-extensions.md)
§7). The list describes the build, not any Site: it changes only with a new release.

## Related Glossary Terms

- [Extension Module](../../../../../docs/development/glossaries/terms/extension-module.md)
- [Module Manifest](../../../../../docs/development/glossaries/terms/module-manifest.md)
- [Software Module](../../../../../docs/development/glossaries/terms/software-module.md)

## Endpoint / RPC

```text
GET /api/v1/modules
```

## Wire Shape

The field-level shape is defined in `openapi/areas/modules.yaml`.

| Operation | operationId |
| --------- | ----------- |
| `GET /api/v1/modules` | `listModules` |

## Authentication

Bearer credential required: a Session access token or an API Key. See
[`conventions.md`](conventions.md).

## Authorization

The `admin` role, like the rest of the API.

## Request

No path parameters, query parameters, or body.

## Response

### Success Response

`200 OK`. Modules are ordered by kind and then id; `kind` is `accelerator`, `platform`, `software`, or
`validation` (a Validation Module, which defines no spec; the suites modules declare are listed by the
[validation](validation.md) contract, not here). `spec` is `null` for a module that defines no
resource spec; otherwise it is the manifest's schema as written, so its properties keep the
manifest's order, which is the order a generic form shows them in.

```json
{
  "items": [
    {
      "kind": "software",
      "id": "nfs",
      "version": "0.1.0",
      "spec": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "exportPath": {
            "type": "string",
            "title": "Export path (server)",
            "description": "Required when a Server takes the server role.",
            "examples": ["/export/data"]
          }
        }
      }
    }
  ]
}
```

### The spec schema

A schema is a JSON Schema subset that the server and a client read the same way:

- The root is `"type": "object"` with `"additionalProperties": false`, a `properties` map, and an
  optional `required` list.
- A property's `type` is `string`, `integer`, `boolean`, or `array` (whose `items` are strings),
  and it has a `title`. It may have a `description` and `examples` (sample values; a form shows the
  first as a placeholder). A string may have `enum` or `pattern`; an integer may have `minimum` and
  `maximum`. A `pattern` uses the syntax Go (RE2) and ECMAScript share.
- No other keyword exists; the server refuses to start with a manifest that uses one.

For a Software module, the schema's properties are exactly the catalog entry's `specFields`
([`software.md`](software.md)), and the install request's `spec` is checked against it before the
module's own rules: an unknown key, a value of the wrong type, a value outside `enum`, `pattern`, or
the range, or a missing `required` setting is `400 validation_error` naming the field (for example
`spec.exportPath must be a string`). A null value, a blank string, or an empty list counts as
unset. Rules that depend on more than one field or on the targets' roles (for example NFS requiring
`exportPath` when a Server takes the server role) stay with the module.

### Error Response

The shared error envelope of [`conventions.md`](conventions.md).

## Error Codes

| Code | HTTP Status | Description |
| ---- | ----------: | ----------- |
| `unauthorized` | 401 | Missing or invalid credential. |
| `forbidden` | 403 | The caller is not an `admin`. |

## Compatibility Notes

New in 2026-11. Adding a keyword to the schema subset is a contract change: a client that does not
know it would render a form the server then refuses.

## Implementation Notes

The manifest parser (`internal/core/module/domain`, `ParseSpecSchema`) defines the subset; the
composition root attaches each Software module's schema to the Software catalog, which checks an
install spec with it.
