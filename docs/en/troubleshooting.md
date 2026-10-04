# Troubleshooting

[繁體中文](../zh-TW/troubleshooting.md) · [Documentation home](README.md)

## The local stack does not start

```bash
cd deploy/dev
docker compose ps
docker compose logs api-server worker ansible-executor temporal
```

- A Docker socket permission error usually means the current login session does
  not have the `docker` group.
- API restart loops usually indicate a build or configuration validation error.
- A healthy API with stuck Workflows usually means Temporal, the worker, or the
  Ansible executor is unavailable.

## `swallowctl doctor` reports a failure

Run `sudo ./swallowctl doctor` in `production/`; each `FAIL` line names one connection.

- **Image pull denied:** the GHCR package is private. Make it public or run
  `docker login ghcr.io` before installing.
- **Worker does not poll the task queue:** read `docker compose logs worker`; it needs
  MongoDB, the database schema, and Temporal.
- **MAAS has no complete `ubuntu/noble`:** the VM cannot reach `images.maas.io`, or the
  import is still running. Rerun `sudo ./local-maas.sh ensure-image`.
- **MAAS Integration not synced:** the line shows the Integration's last error. The API
  reaches MAAS at `http://host.docker.internal:5240/MAAS`; confirm MAAS answers on port
  5240 and that `secrets/maas-api-key` is current, then rerun `sudo ./bootstrap.sh apply`.

## Login fails

- Confirm the endpoint points to the API root, not `/api/v1` twice.
- Development defaults are `admin` / `admin`. The production installation generates the
  admin password in `production/secrets/bootstrap-admin-password`.
- A CLI profile can be overridden by environment variables or flags. Run with
  an explicit `--endpoint` while diagnosing.
- An expired token requires login again; consumers must not decode JWT expiry.

## Integration has no Servers

1. Check the Site and Integration are enabled and correctly paired.
2. Read `sync.lastError` and `sync.lastSucceededAt`.
3. Verify MAAS network reachability and the write-only credential.
4. Trigger reconcile or wait for the configured interval.
5. Remember that there is no manual create-Server operation.

## A Server stays Releasing or Inspecting

`releasing`, `inspecting`, `testing`, and `deploying` are in-progress OS
provisioning states. They are expected to change without another operator
action. The spinner in the Deployment column identifies this condition but does
not prove that the provider is making progress.

When shown, the running time for one of these provider states starts when
swallow first observed it; a swallow OS deployment counts from the deployment's
start instead. It is not the provider's exact transition time or proof of
progress. No running time means the start is unknown, not that the work
has not started.

1. Hover the Deployment state for its current details.
2. Check the provisioning observation time and the Integration's sync error and
   last-success time.
3. Refresh the Server to perform a targeted provider read.
4. Read the Server's provider events to see whether the external lifecycle
   action is still advancing or has failed.

```bash
swallow servers refresh server1
swallow servers events server1 --limit 50
swallow -o json servers get server1
```

If the state changes to `failed`, `broken`, or `rescue`, it needs operator
attention rather than more waiting. See
[Servers and infrastructure](guides/servers-and-infrastructure.md#read-the-deployment-column)
for the state and recovery cues.

## Credentials stopped working after a key change

Stored integration and automation credentials are encrypted with
`api.credentialKey`. Replacing that key makes existing ciphertext
undecryptable. Restore the matching key from backup or enter every affected
credential again.

## A Workflow is pending or stuck

- Confirm API, Temporal Server/PostgreSQL, worker, and Ansible executor health.
- Inspect the Workflow's current Task, events, and logs.
- Check Site lease conflicts, Server locks, SSH known hosts, resolved SSH user,
  and external provider availability.
- Do not start a conflicting Workflow until the first reaches a terminal state
  or is safely cancelled.

## Monitoring is empty

- Confirm the metrics Integration is configured for the selected Site.
- Verify Prometheus can call service discovery with the machine token (and trusts the
  CA of any TLS terminator in front of swallow).
- Confirm exporters are installed and reachable at the discovered targets.
- Missing metric data is unknown, not proof of failure.

## Keep the request ID

API errors contain an opaque request ID matching the `X-Request-ID` header.
Include it with the exact time, Site, resource ID, and Workflow ID when
correlating a problem with logs.

Environment-specific help is also available in the
[development](../../deploy/dev/README.md) and
[Compose installation](../../deploy/production/README.md) references.
