# SSH Keys

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)
- Swallow installation tooling (checks that the Deployment Key exists)

## Purpose

Manage swallow-owned SSH Keys: the single system-owned **Deployment Key** that swallow
uses for SSH readiness and Ansible execution, and the caller's public-key-only **Access
Keys**. swallow realizes every key's public key into each provisioner Integration that can
hold SSH keys (MAAS), so Servers deployed afterwards authorize it. See
[decision 039](../../../../../docs/decisions/039-ssh-key-management-and-default-user.md).

Base path, bearer authentication, error envelope, and timestamps follow
[conventions.md](conventions.md); only endpoint-specific behavior is described below.

## Related Glossary Terms

- SSH Key
- Deployment Key
- Integration
- Automation Configuration

## Authentication and Authorization

Bearer token; every endpoint requires `admin`. Access Keys are scoped to the caller: list
returns only the caller's Access Keys, and delete refuses another user's key with
`404 not_found`.

## Endpoints

```text
GET    /api/v1/ssh-keys
POST   /api/v1/ssh-keys
POST   /api/v1/ssh-keys/generate
GET    /api/v1/ssh-keys/{keyId}
DELETE /api/v1/ssh-keys/{keyId}
PUT    /api/v1/ssh-keys/deployment
POST   /api/v1/ssh-keys/deployment/regenerate
POST   /api/v1/ssh-keys/sync
```

## SSH Key resource

```json
{
  "id": "5f0c…",
  "name": "work-laptop",
  "purpose": "access",
  "keyType": "ssh-ed25519",
  "fingerprint": "SHA256:0mW2…",
  "publicKey": "ssh-ed25519 AAAAC3Nza… work-laptop",
  "ownerUserId": "a91e…",
  "createdAt": "2026-10-01T00:00:00Z",
  "updatedAt": "2026-10-01T00:00:00Z",
  "providerSync": [
    {
      "integrationId": "c3d4…",
      "siteId": "b0a2…",
      "state": "synced",
      "syncedAt": "2026-10-01T00:00:05Z",
      "error": ""
    }
  ]
}
```

- `purpose` is `deployment` or `access` (glossary SSH Key).
- `keyType` is the SSH algorithm name from the public key (`ssh-ed25519`, `ssh-rsa`,
  `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp384`, `ecdsa-sha2-nistp521`).
- `fingerprint` is the OpenSSH SHA256 fingerprint, unique across all keys.
- `publicKey` is one authorized_keys line (`<type> <base64> <comment>`); public key
  material is not secret and is always returned. No endpoint ever returns a stored private
  key; only `generate` returns a freshly generated Access Key private key, once.
- `ownerUserId` is the owning User for an Access Key and omitted for the Deployment Key.
- `providerSync` has one entry per enabled provisioner Integration (across all Sites),
  always an array; a paused Integration is not synced and not listed. `state` is:
  - `synced` — the provisioner holds this public key.
  - `pending` — not yet realized (a sync has not run since the key or Integration changed).
  - `failed` — the last attempt failed; `error` carries a human-readable reason.
  - `unsupported` — the provisioner cannot hold SSH keys; Servers it deploys will not
    authorize this key through the provisioner.
  `syncedAt` is the last successful realization and omitted when never synced. `error` is
  omitted unless `state` is `failed`.

## List

`GET /api/v1/ssh-keys` returns a plain array (the set is small and bounded): the
Deployment Key first (when it exists), then the caller's Access Keys ordered by
`createdAt`. The Deployment Key is created by the installation step
(`swallow-api deployment-key ensure`, which `swallowctl install` and `upgrade` run after
`migrate`), not by the API process; until it runs the list has no Deployment Key and OS and
Platform deployment are refused with `409 conflict`.

## Read one key

`GET /api/v1/ssh-keys/{keyId}` returns `200` with one SSH Key resource, in the same shape
as a list item: the Deployment Key, or one of the caller's Access Keys. An unknown key and
another user's Access Key are both `404 not_found`. It lets a client follow one key's
`providerSync` (for example while an entry is `pending`) without re-reading the list.

## Import an Access Key

`POST /api/v1/ssh-keys`

```json
{ "name": "work-laptop", "publicKey": "ssh-ed25519 AAAAC3Nza… alice@laptop" }
```

`name` is required, trimmed, 1–100 characters, and unique (case-insensitive) among the
caller's Access Keys. `publicKey` is one authorized_keys line; options before the key type
are rejected, and the comment is preserved. RSA keys shorter than 2048 bits and
unsupported types are rejected. Returns `201` with the SSH Key resource. A malformed or
unsupported key, or a missing/invalid name, is `400 validation_error`; a key whose
fingerprint already exists (as any SSH Key, including the Deployment Key) or a duplicate
name is `409 conflict`. Provider realization starts in the background; the response's
`providerSync` entries are `pending`.

## Generate an Access Key pair

`POST /api/v1/ssh-keys/generate`

```json
{ "name": "ops-jumpbox" }
```

Generates an ed25519 key pair, stores only the public key as an Access Key, and returns
`201`:

```json
{
  "key": { "id": "…", "name": "ops-jumpbox", "purpose": "access", "…": "…" },
  "privateKey": "-----BEGIN OPENSSH PRIVATE KEY-----\n…\n-----END OPENSSH PRIVATE KEY-----\n"
}
```

`privateKey` is an unencrypted OpenSSH private key returned exactly once; swallow keeps no
copy and it cannot be retrieved again. Clients must present it for saving immediately.
Name validation matches import.

## Delete an Access Key

`DELETE /api/v1/ssh-keys/{keyId}` deletes one of the caller's Access Keys and removes it
from every provisioner where swallow registered it. Returns `204`. An unknown key (or
another user's key) is `404 not_found`; the Deployment Key is `409 conflict` (replace or
regenerate it instead). Provider removal is best effort after the record is deleted: a
provider that cannot be reached is retried by the periodic sync. Already-deployed Servers
keep the key in their `authorized_keys`.

## Replace the Deployment Key

`PUT /api/v1/ssh-keys/deployment`

```json
{ "privateKey": "-----BEGIN OPENSSH PRIVATE KEY-----\n…", "name": "optional" }
```

Replaces the Deployment Key with an existing private key (OpenSSH, PKCS#1, PKCS#8, or SEC1
PEM). The public key is derived from it. A passphrase-protected, malformed, or unsupported
key is `400 validation_error`; a key whose fingerprint equals an Access Key is
`409 conflict`. `name` defaults to the current name. Returns `200` with the new SSH Key
resource (the `id` is preserved). The previous public key is removed from provisioners
where swallow registered it, and the new one is registered.

`PUT /deployment` and `POST /deployment/regenerate` replace an existing Deployment Key; with
none yet they return `404 not_found` (create it with the installation step).

## Regenerate the Deployment Key

`POST /api/v1/ssh-keys/deployment/regenerate` (no body) replaces the Deployment Key with a
freshly generated ed25519 key pair. Returns `200` with the SSH Key resource. The private key
is never returned.

Replacing or regenerating the Deployment Key does **not** re-authorize Servers deployed
earlier: they authorize only the public key present when they were deployed, so swallow can
no longer log in to them with the new key unless a Site Automation Configuration overrides
the credential or the Server is redeployed.

## Trigger a sync

`POST /api/v1/ssh-keys/sync` (no body) requests an immediate realization pass over every
provisioner Integration and returns `202` with no body. Results are read from
`providerSync` on the next list.

## Provider realization

- swallow lists the provisioner's keys first, adopts a key whose material already matches,
  adds missing keys, and removes only keys swallow itself registered or adopted. Keys an
  operator added directly in the provisioner are never removed.
- Realization runs after each key change, periodically, and before every OS deployment
  through a key-capable provisioner. A deployment is not started when the Deployment Key
  cannot be realized on a key-capable provisioner (the Workflow Task fails retryably with
  `ssh_key_registration_failed`).
- MAAS registers keys on the MAAS user that owns the Integration's API key and injects them
  into the deployed image's default user.

## Errors

The common envelope applies: `400 validation_error`, `401 unauthorized`, `403 forbidden`,
`404 not_found`, `409 conflict`, `500 internal_error`.

## Compatibility Notes

New surface. It supersedes the planned `/ssh-keys` design in
[planned-surface.md](planned-surface.md) §7.11: `publicKey` is returned (public key material
is not secret) and there is no `vaultRef`.
