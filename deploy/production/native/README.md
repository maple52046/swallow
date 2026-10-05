# Native Ubuntu 24.04 amd64 installation

[繁體中文](README.zh-TW.md) · [Installation choices](../../../docs/en/installation.md)

This directory is the template for a self-contained native bundle with `bin/swallow-api`,
dashboard assets, automation, an offline Python wheelhouse, systemd and Nginx files, and
separately checksummed MongoDB/Nginx packages. Releases do not publish that bundle until native
packaging is complete ([ADR 050](../../../docs/decisions/050-compose-only-release-artifacts.md));
the steps below describe the intended bundle.

Merge the checksummed MongoDB and native runtime media as
`third-party/mongodb/` and `third-party/native-runtime/`. Each must contain
`SHA256SUMS` plus its complete `packages/` dependency closure; installation uses
`apt-get --no-download`. Prepare root-only secrets, then install the CA certificate and
key supplied for this installation as `/etc/swallow/tls/tls.crt` and `tls.key`:

```bash
sudo ./prepare-secrets.sh
sudo install -m 0600 <customer.crt> /etc/swallow/tls/tls.crt
sudo install -m 0600 <customer.key> /etc/swallow/tls/tls.key
sudo ./swallowctl preflight
sudo ./swallowctl install
sudo ./swallowctl doctor
```

`install` (and `upgrade`, which reinstalls) runs the database migration and then
creates the deployment key with `swallow-api deployment-key ensure`; an existing key
is kept. OS and Platform deployments are refused until it exists.

## Durable orchestration requires Temporal

swallow's sole execution engine is Temporal ([ADR 016](../../../docs/decisions/016-temporal-operation-orchestration.md),
[ADR 017](../../../docs/decisions/017-workflow-job-task-runner-model.md)). A Workflow cannot
execute without Temporal Server, its PostgreSQL datastore, the Workflow worker
(`swallow-api worker`), and the Ansible executor (`swallow-api ansible-executor`).

This native installer does not yet provision Temporal Server and its PostgreSQL as native
systemd services. Until that packaging lands, use the [Compose topology](../compose.yaml),
which runs the complete orchestration topology. A native install without those services
cannot execute Workflows.

`upgrade` refuses active Workflows unless `--force` is explicit and always takes a
backup first. `uninstall` retains data and backups; only `uninstall --purge-data`
removes them. The installer initializes authenticated localhost-only MongoDB, runs the
explicit schema migration before starting the API, and enables
`swallow-backup.timer`. The timer keeps seven daily and four weekly backups under
`/var/backups/swallow`.
