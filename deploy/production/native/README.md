# Native Ubuntu 24.04 amd64 installation

[繁體中文](README.zh-TW.md) · [Installation choices](../../../docs/en/installation.md)

The release workflow assembles this template into a self-contained native bundle with
`bin/swallow-api`, dashboard assets, automation, an offline Python wheelhouse, systemd and
Nginx files, and separately checksummed MongoDB/Nginx packages.

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

## Durable orchestration requires Temporal

swallow's sole execution engine is Temporal ([ADR 016](../../../docs/decisions/016-temporal-operation-orchestration.md),
[ADR 017](../../../docs/decisions/017-workflow-job-task-runner-model.md)). A Workflow cannot
execute without Temporal Server, its PostgreSQL datastore, the Workflow worker
(`swallow-api worker`), and the Ansible executor (`swallow-api ansible-executor`).

The release archive carries checksummed Temporal Server, UI, and PostgreSQL OCI images,
but this native installer does not yet provision them as native systemd services. Until
that packaging lands, use the [Compose topology](../compose.yaml), which runs the complete
orchestration topology. A native install without those services cannot execute Workflows.

`upgrade` refuses active Workflows unless `--force` is explicit and always takes a
backup first. `uninstall` retains data and backups; only `uninstall --purge-data`
removes them. The installer initializes authenticated localhost-only MongoDB, runs the
explicit schema migration before starting the API, and enables
`swallow-backup.timer`. The timer keeps seven daily and four weekly backups under
`/var/backups/swallow`.
