# Troubleshooting

[繁體中文](../zh-TW/troubleshooting.md) · [Documentation home](README.md)

## The local stack does not start or Workflows stay pending

From a source checkout, check and converge the development stack with one command each:

```bash
./deploy/production/swallowctl doctor --profile dev
./deploy/production/swallowctl ensure --profile dev
```

`ensure --profile dev` starts the services that are not running and restarts the ones whose
check fails, such as a worker that no longer polls Temporal after a host reboot. A check it
cannot fix is printed with the service's recent logs.

- A Docker socket permission error usually means the current login session does
  not have the `docker` group.
- API restart loops usually indicate a build or configuration validation error.
- A healthy API with stuck Workflows usually means Temporal, the worker, or the
  Ansible executor is unavailable; `ensure` covers all three.

## After a power loss, a reboot, or a `swallowctl doctor` failure

In `/opt/swallow/production/` (the installation directory), run:

```bash
sudo ./swallowctl ensure
```

`ensure` converges the installation back to the state `doctor` checks: it starts the declared
services, restarts those whose check fails, reruns the idempotent MAAS and bootstrap steps when
their checks fail, and ends with `doctor`. It is safe to rerun. Each `FAIL` line that remains
names one connection, with the owning services' recent logs. Rerunning the release's
`install.sh` is safe and resumes an installation that stopped part way.

- **Image pull denied:** for a `ghcr.io` image, the GHCR package is private; make it public
  or run `docker login ghcr.io` before installing. For a `docker.io` image, Docker Hub limits
  anonymous pulls; wait and rerun, or `docker login` to Docker Hub first.
- **Worker still does not poll the task queue after `ensure`:** the printed worker logs show
  why; it needs MongoDB, the database schema, and Temporal.
- **MAAS has no complete `ubuntu/noble`:** the VM cannot reach `images.maas.io`, or the
  import is still running. Rerun `sudo ./local-maas.sh ensure-image`.
- **MAAS Integration not synced:** the line shows the Integration's last error. The API
  reaches MAAS at `SWALLOW_MAAS_URL` in `.env` (installations made before `install.sh`
  existed use `http://host.docker.internal:5240/MAAS`); confirm MAAS answers there on port
  5240 and that `secrets/maas-api-key` is current, then rerun `sudo ./bootstrap.sh apply`.

## Login fails

- Confirm the endpoint points to the API root, not `/api/v1` twice.
- Development defaults are `admin` / `admin`. The production installation generates the
  admin password in `/opt/swallow/production/secrets/bootstrap-admin-password`.
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
5. For **Releasing**, open the Server detail page. Its banner links to the
   Workflow following the release and says when that Workflow needs attention
   or has ended, or that no Workflow covers the release.

```bash
swallow servers refresh server1
swallow servers events server1 --limit 50
swallow -o json servers get server1
swallow workflows list --server server1
```

If the state changes to `failed`, `broken`, or `rescue`, it needs operator
attention rather than more waiting. See
[Servers and infrastructure](guides/servers-and-infrastructure.md#read-the-deployment-column)
for the state and recovery cues.

## A Server still shows Deploying after its Workflow was canceled

Two separate states can read as Deploying:

- **swallow's deployment result.** Canceling an OS deployment Workflow records
  the result as Canceled. If the Workflow already shows `canceled` but the
  result still reads Deploying, wait one reconcile interval: swallow settles a
  result left behind by an ended Workflow on its own.
- **The provider's provisioning state.** Cancel asks MAAS to abort the install,
  best-effort. If MAAS itself still reports Deploying, swallow keeps showing
  `deploying`, because that is the provider's fact. Refresh the Server and read
  its provider events to see whether the abort took effect. Once MAAS leaves
  Deploying, follow the recovery cues in
  [Servers and infrastructure](guides/servers-and-infrastructure.md#read-the-deployment-column).

```bash
swallow workflows list --server server1
swallow servers refresh server1
swallow servers events server1 --limit 50
```

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
- If the Workflow's reason says its execution was lost, it can no longer be
  retried, and it keeps its Servers busy until you act. Cancel it (swallow
  records the cancellation itself and frees the Servers), then start the
  Release, Recover, or deployment again.

## Monitoring is empty

- "No metrics backend for this Site" on the usage charts means no metrics
  Integration is registered (or it cannot be reached); register one for the Site.
- Verify Prometheus can call service discovery with the machine token (and trusts the
  CA of any TLS terminator in front of swallow).
- Confirm exporters are installed and reachable at the discovered targets: the
  `install-exporters` Workflow runs when a Server reaches `deployed`, and a locked
  Server gets none.
- A chart that reads "No data" for one metric only — for example the GPU charts —
  usually means that exporter is not running on the Server (a GPU Server without the
  `amd-gpu` tag gets no RDC exporter).
- Missing metric data is unknown, not proof of failure.

## Keep the request ID

API errors contain an opaque request ID matching the `X-Request-ID` header.
Include it with the exact time, Site, resource ID, and Workflow ID when
correlating a problem with logs.

Environment-specific help is also available in the
[development](../../deploy/dev/README.md) and
[Compose installation](../../deploy/production/README.md) references.
