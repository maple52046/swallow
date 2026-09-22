# Native Ubuntu 24.04 amd64 installation

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

## Durable orchestration requires Temporal (no embedded fallback)

swallow's sole execution engine is Temporal ([ADR 016](../../../docs/decisions/016-temporal-operation-orchestration.md),
[ADR 017](../../../docs/decisions/017-workflow-job-task-runner-model.md)). The former
embedded automation dispatcher was removed, so `swallow-api api` has **no** in-process fallback:
a Workflow cannot execute without the orchestration topology.

A native installation must therefore run the full topology alongside `swallow-api api`: Temporal
Server, its PostgreSQL datastore, the workflow worker (`swallow-api worker`), and the Ansible
executor (`swallow-api ansible-executor`). For air-gapped installs the release bundle already
carries the Temporal Server, UI, and PostgreSQL images with checksums (see
[`../../release/`](../../release) and `release-manifest`); the native installer provisions
them as additional systemd-managed services from that offline media.

Until the native Temporal systemd packaging lands, use the Compose topology in
[`../`](../compose.yaml), which runs Temporal, the worker, and the ansible-executor. A native
install without Temporal cannot execute any Workflow. Packaging Temporal + PostgreSQL as
native systemd units from the release media is the remaining deploy work for air-gapped sites.

`upgrade` refuses active operations unless `--force` is explicit and always takes a
backup first. `uninstall` retains data and backups; only `uninstall --purge-data`
removes them. The installer initializes authenticated localhost-only MongoDB, runs the
explicit schema migration before starting the API, and enables
`swallow-backup.timer`. The timer keeps seven daily and four weekly backups under
`/var/backups/swallow`.
