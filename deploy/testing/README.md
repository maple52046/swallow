# Testing environment

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

Testing never builds or bind-mounts source. Copy all six exact image references
(API, Dashboard, MongoDB, Temporal PostgreSQL, Temporal Server, and Temporal UI)
from a candidate release manifest into `.env`, then run:

```bash
./prepare.sh
docker compose --env-file .env up -d
./seed.sh
```

The same file is used for the shared environment and CI. CI supplies a unique project
name with `docker compose -p "swallow-ci-${GITHUB_RUN_ID}"` and always removes its
volumes afterward. Self-signed TLS is limited to testing; production requires CA
material supplied for the installation.

Generated testing secrets stay under `deploy/testing/secrets/`; they never reuse
production paths. `prepare.sh` is idempotent, and `seed.sh` can be run repeatedly.
The candidate workflow starts this topology from the API and Dashboard digests it just
built, waits for readiness, seeds it, verifies the reported candidate version, and
removes the project and volumes.
