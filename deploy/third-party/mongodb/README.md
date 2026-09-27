# MongoDB 8 media

[繁體中文](README.zh-TW.md) · [Third-party overview](../README.md)

The Compose path consumes the exact MongoDB OCI digest recorded in
`release-manifest.json`. Load the core OCI archive with `docker load`; production
must not pull implicitly.

The native path consumes Ubuntu 24.04 amd64 packages from the separately checksummed
third-party media under `third-party/packages/`. The native installer starts a
localhost-only MongoDB, creates the configured administrative user, enables
authorization, and verifies the authenticated URI before migration.

Daily backups contain MongoDB, the credential encryption key, and runner artifacts.
Seven daily and four weekly recovery points are retained. Restore drills remain a
release acceptance gate.

Official installation and backup references:

- <https://www.mongodb.com/docs/v8.0/tutorial/install-mongodb-on-ubuntu/>
- <https://www.mongodb.com/docs/manual/core/backups/>
