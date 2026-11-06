# Testing environment

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

Testing never builds or bind-mounts source. Copy the exact image references (API,
Dashboard, MongoDB, PostgreSQL, Temporal Server, Prometheus, prom-label-proxy, Pulp, and Pulp web)
from a `release-manifest.json` written by [`publish.sh`](../release/publish.sh) into `.env`, then run:

```bash
./prepare.sh
docker compose --env-file .env up -d
./seed.sh
```

The topology is the production Compose file with isolated secrets, served over plain HTTP
on `SWALLOW_HTTP_PORT` (default 18080). PostgreSQL is published on loopback
`SWALLOW_POSTGRES_HOST_PORT` (15432 in `.env.example`) so it never collides with a production
installation on the same host. Testing does not install MAAS, and its Prometheus scrapes only
swallow's own metrics: the exporter discovery job is rendered by `swallowctl` in a production
installation.

Pulp, the Site's Local Repository, runs as in production and is published on every interface at
`SWALLOW_PULP_PORT` (18081 in `.env.example`, so it never collides with a production installation
or the development stack, which both default to 8080). In production `swallowctl` creates Pulp's
role and database in the shared PostgreSQL; here the one-shot `pulp-db` service does it on every
`up`, before `pulp-init` migrates the database. `seed.sh` registers Pulp as the testing Site's
`repository` Integration and writes the Local Repository's default `ubuntu` source, as
`bootstrap.sh` does in production. The Integration's `contentOrigin` is Pulp's own origin,
`http://<SWALLOW_ADDRESS>:<SWALLOW_PULP_PORT>`, with `localhost` while `SWALLOW_ADDRESS` is unset:
set `SWALLOW_ADDRESS` in `.env` before `up` when Servers must fetch from this stack. Pulp then
synchronizes the sources from the Internet; `seed.sh` does not wait for it.

Set `COMPOSE_PROJECT_NAME` to run several isolated stacks on one host, and remove a stack with
`docker compose down --volumes` when you are done. Each stack publishes its own Pulp port, so give
every stack its own `SWALLOW_HTTP_PORT`, `SWALLOW_PULP_PORT`, and `SWALLOW_POSTGRES_HOST_PORT`.

Generated testing secrets stay under `deploy/testing/secrets/`; they never reuse
production paths. They include Pulp's database password, admin password (the `repository`
Integration's credential), and secret key, generated like production's `prepare-secrets.sh`.
`prepare.sh` is idempotent, and `seed.sh` can be run repeatedly. `seed.sh`
first creates the deployment key (`swallow-api deployment-key ensure`, the step `swallowctl
install` runs in production) in a one-shot API container, honors `COMPOSE_PROJECT_NAME`, and
checks the write-only automation credential contract.
