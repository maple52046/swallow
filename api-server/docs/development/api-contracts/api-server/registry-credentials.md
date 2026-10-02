# Software — Docker CE Registry Credentials

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Manage the swallow-owned Registry Credentials the Docker Host Explorer uses to pull private images
([servers-docker.md](servers-docker.md), [decision 044](../../../../../docs/decisions/044-docker-registry-credentials.md)).
A credential belongs to the installation, is keyed by one registry host, and is attached to a pull
whose image reference resolves to that registry. Passwords are sealed at rest and are never
returned. Managing credentials never contacts a registry or a host.

The routes sit under `/software/docker-ce/` because the credentials serve Docker CE's Docker Host
Explorer only. Settings of other software kinds get their own `/software/{kind}/...` paths; nothing
under `/software/` other than the catalog and assignments is shared by every kind.

## Related Glossary Terms

- [Registry Credential](../../../../../docs/development/glossaries/terms/registry-credential.md)
- [Docker Host Explorer](../../../../../docs/development/glossaries/terms/docker-host-explorer.md)

## Endpoints

```text
GET    /api/v1/software/docker-ce/registry-credentials
POST   /api/v1/software/docker-ce/registry-credentials
PUT    /api/v1/software/docker-ce/registry-credentials/{credentialId}
DELETE /api/v1/software/docker-ce/registry-credentials/{credentialId}
```

All endpoints require an admin JWT per [conventions](conventions.md).

## Registry Credential

```json
{
  "id": "4b1c7a9e-0f3d-4e7b-9a51-2c6f0d1e8a77",
  "registry": "harbor.lab.local",
  "username": "robot$ci",
  "createdAt": "2026-10-02T04:00:00Z",
  "updatedAt": "2026-10-02T04:00:00Z",
  "updatedBy": "admin"
}
```

There is no password field in any response.

## Registry Names

`registry` is a host with an optional port. On input it is normalized:

- surrounding whitespace, a leading `https://` or `http://`, and a trailing `/` are removed;
- the host is lower-cased;
- every Docker Hub name becomes `docker.io`: the names Docker itself uses (`docker.io`,
  `index.docker.io`, `registry-1.docker.io`), the index URL `https://index.docker.io/v1/`, and the
  names operators commonly type for it (`hub.docker.com`, `registry.hub.docker.com`,
  `hub.docker.io`). No image reference names Docker Hub by the latter, so a credential stored under
  one of them would never match a Docker Hub pull.

The result must be `host` or `host:port` (letters, digits, `.`, `-`; port 1–65535) with no path.

An image reference resolves to a registry with Docker's rule: the part before the first `/` when it
contains `.` or `:` or is `localhost`; otherwise `docker.io`. `nginx:1.27` and `team/app` resolve
to `docker.io`; `harbor.lab.local/team/app:1.4` and `localhost:5000/app` resolve to their host.

## List Credentials

`GET /api/v1/software/docker-ce/registry-credentials` returns every credential ordered by registry (a small,
bounded set):

```json
{ "items": [ /* Registry Credential objects */ ] }
```

## Create Credential

`POST /api/v1/software/docker-ce/registry-credentials`

```json
{ "registry": "harbor.lab.local", "username": "robot$ci", "password": "s3cret" }
```

- `registry`, `username`, and `password` are required; `password` may be a registry access token.
- Response `201 Created` with the Registry Credential.

## Replace Credential

`PUT /api/v1/software/docker-ce/registry-credentials/{credentialId}`

```json
{ "username": "robot$ci", "password": "rotated" }
```

- Both fields are required; the stored password is replaced (it is never merged or echoed).
- The registry of a credential cannot change; delete and create instead.
- Response `200 OK` with the Registry Credential.

## Delete Credential

`DELETE /api/v1/software/docker-ce/registry-credentials/{credentialId}` — response `204 No Content`. Later
pulls from that registry are anonymous.

## Errors

In addition to the shared codes in [conventions](conventions.md):

| `error.code` | HTTP Status | Meaning |
| --- | ---: | --- |
| `validation_error` | 400 | A required field is missing or `registry` is not a valid host. |
| `not_found` | 404 | The credential does not exist. |
| `conflict` | 409 | A credential for the normalized registry already exists. |

## Compatibility Notes

- Credentials are sealed with the installation credential key; rotating that key makes them
  unreadable and they must be re-entered, as with other sealed credentials.
- A pull sends the credential to the host's Docker Engine over the plain-HTTP API listener of
  decision 043; it is never written to the host.
