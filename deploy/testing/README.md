# Testing environment

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

Testing never builds or bind-mounts source. Copy the exact image references (API,
Dashboard, MongoDB, Temporal PostgreSQL, and Temporal Server; Temporal UI only for the
`diagnostics` profile) from a `release-manifest.json` written by
[`publish.sh`](../release/publish.sh) into `.env`, then run:

```bash
./prepare.sh
docker compose --env-file .env up -d
./seed.sh
```

The topology is the production Compose file with isolated secrets, served over plain HTTP
on `SWALLOW_HTTP_PORT` (default 18080). PostgreSQL is published on loopback
`SWALLOW_POSTGRES_HOST_PORT` (15432 in `.env.example`) so it never collides with a production
installation on the same host. Testing does not install MAAS.

Set `COMPOSE_PROJECT_NAME` to run several isolated stacks on one host, and remove a stack with
`docker compose down --volumes` when you are done.

Generated testing secrets stay under `deploy/testing/secrets/`; they never reuse
production paths. `prepare.sh` is idempotent, and `seed.sh` can be run repeatedly. `seed.sh`
first creates the deployment key (`swallow-api deployment-key ensure`, the step `swallowctl
install` runs in production) in a one-shot API container, honors `COMPOSE_PROJECT_NAME`, and
checks the write-only automation credential contract.
