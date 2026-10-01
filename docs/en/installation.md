# Installation choices

[繁體中文](../zh-TW/installation.md) · [Documentation home](README.md)

swallow is in active development and currently has no stable SemVer release.
Choose an installation path based on what you need to validate.

| Goal | Path | Builds source | Persistent data | Status |
| --- | --- | ---: | ---: | --- |
| Evaluate or develop | [Development Compose](../../deploy/dev/README.md) | Yes | Local volumes | Recommended for evaluation |
| Verify a candidate | [Testing Compose](../../deploy/testing/README.md) | No | Isolated volumes | CI/release validation |
| Exercise installation lifecycle | [Production Compose assets](../../deploy/production/README.md) | No | Backups and retained state | Active-development assets |
| Install without containers | [Native Ubuntu preview](../../deploy/production/native/README.md) | No | Native paths | Incomplete preview |

## Development Compose

This is the shortest runnable path. Source is bind-mounted, the toolchains run
inside containers, and API/Dashboard changes reload automatically. Its default
credentials and encryption material are intentionally unsafe for shared use.

## Testing Compose

Testing consumes exact candidate image digests, never source mounts or locally
built tags. It is for CI and candidate acceptance, not for long-lived operator
data.

## Compose installation assets

The production directory describes digest-pinned images, TLS, secrets,
preflight, backup, restore, upgrade, doctor, and uninstall behavior. Because no
stable release is currently published, treat these assets as the current
installation contract to evaluate rather than a general-availability promise.

Third-party prerequisites such as Docker CE, MAAS, MongoDB media, and per-Site
Prometheus are delivered separately; see the
[third-party guide](../../deploy/third-party/README.md).

## Native preview

The native bundle targets Ubuntu 24.04 amd64, but native Temporal/PostgreSQL
systemd packaging is not complete. Without the complete Temporal topology,
Workflows cannot run. Use Compose when validating the full product.

## Production-safety baseline

Never reuse development defaults. A real installation needs:

- CA-issued TLS material and restricted secret files;
- unique JWT, machine-token, bootstrap-admin, MongoDB, and credential-encryption
  secrets;
- an exact release/candidate manifest with immutable image digests;
- backup and restore drills covering MongoDB, the credential key, and Workflow
  artifacts (the deployment key's private key is stored in MongoDB, encrypted
  with the credential key, so both must be restored together; `swallowctl
  install` and `upgrade` create the key, and OS and Platform deployments are
  refused while it is missing);
- dedicated MAAS and per-Site monitoring installations with documented network
  and retention ownership.
