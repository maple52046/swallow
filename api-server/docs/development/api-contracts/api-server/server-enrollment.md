# Server Enrollment

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard` (Add servers guidance)
- `cli` (`swallow integrations enroll-bundle`; `swallow servers enroll` runs on the host being
  enrolled, downloaded by the enrollment script, and talks only to the provisioner)
- The enrollment script (`/downloads/swallow-enroll.sh`) on the host being enrolled

## Purpose

Bring machines into a provisioner's inventory and inspect them to a usable state
([decision 053](../../../../../docs/decisions/053-server-enrollment-and-automatic-inspection.md)).
A Server still comes into being only through reconciliation; nothing here creates one.

- **Network boot**: the operator power-cycles the machine into the provisioner's enrollment
  (PXE, or Boot Media on a network the provisioner does not serve). swallow then inspects it
  with an `inspect-hardware` Workflow.
- **Existing OS**: one command on a host that keeps its OS downloads a script and the swallow
  CLI from the installation and registers the host with the provisioner. This contract hands
  that command the provisioner endpoint and credential.

## Related Glossary Terms

- Server Enrollment
- Server
- OS Provisioning State
- Boot Media
- Workflow, Job, Task

## Endpoints

```text
POST /api/v1/provisioning/integrations/{id}/enroll-bundle
POST /api/v1/servers/{id}/inspect            (server-detail-actions.md)
GET  /downloads/swallow-enroll.sh            (no authentication)
GET  /downloads/swallow                      (no authentication)
```

The `/api/v1` routes require an admin bearer token; see [Downloads](#downloads) for the others.

## Automatic hardware inspection

A background sweep runs at the reconcile interval. It starts an `inspect-hardware` Workflow
for a Server when all of these hold:

- its provisioner Integration is enabled and `settings.autoInspect` is not `"false"`
  ([sites-integrations.md](sites-integrations.md));
- the Server is present (not absent), not locked, and in provisioning state `new`;
- swallow first observed the Server within the last 24 hours;
- the Server never had an `inspect-hardware` Workflow, in any status;
- no other Workflow holds the Server.

A Server enrolled with its OS kept arrives as `deployed` and is never inspected automatically.
A Server that stays `new` longer than 24 hours, or whose automatic Workflow failed or was
canceled, is inspected only on request (`POST /servers/{id}/inspect`). Automatic Workflows are
requested by `system`.

## inspect-hardware Workflow

Kind `inspect-hardware`, definition `hardware-inspection` version 1, one target Server, one Job
`ensure-inspected` with three Tasks in order (Task IDs carry the Server ID):

| Task ID | Kind | Runner | Ensures |
| --- | --- | --- | --- |
| `wait-enrollment-<serverId>` | `wait-enrollment-settled` | `provisioner` | The provider's own enrollment has finished. |
| `ensure-boot-media-<serverId>` | `ensure-boot-media` | `internal` | The Server's Boot Media, if enabled, is applied. |
| `inspect-<serverId>` | `inspect` | `provisioner` | The provider inspected the hardware; the Server is `ready`. |

`wait-enrollment-settled` polls every 15 seconds and succeeds after two consecutive settled
readings. For MAAS a Machine is settled once it has left New, or is New and powered off (the
power-off ends enlistment); the power state is read live from the BMC, falling back to the
state MAAS recorded. A provisioner without an enrollment check succeeds at once. A Workflow
started by `POST /servers/{id}/inspect` and any retried run succeed at once. Twenty minutes
without a settled reading is `requires_attention` with code `enrollment_not_settled`; nothing
was commissioned.

`ensure-boot-media` here reads the Server's Boot Media when it runs (it carries no frozen ISO
URL). Disabled Boot Media succeeds without touching the BMC. Enabled Boot Media whose Boot ISO
is not served fails retryably with `boot_media_not_configured`; a BMC failure fails retryably
with `boot_media_ensure_failed`.

`inspect` makes at most three attempts. Each issues Inspect (MAAS commission), after a live
Server Lock check, and observes the Server. The first attempt follows an inspection that is
already running instead of issuing another, and an automatic Workflow whose Server the provider
already brought to `ready` succeeds without inspecting. Observation:

- `ready` is success. `testing` during an attempt is progress (MAAS runs its commissioning
  tests).
- `failed` or `broken` ends the attempt as failed.
- An attempt that records no provider event for 15 minutes while `inspecting` (45 minutes when
  the provider keeps no event stream) is stalled: swallow aborts it, which returns a MAAS
  Machine that was never inspected to New.
- Leaving `inspecting` for `new` or another state without swallow's abort is
  `requires_attention` with `inspect_interrupted`; swallow does not fight an operator.

After the last attempt the Task is `requires_attention` and retryable, with code
`inspect_pxe_unreached` (the last attempt stalled) or `inspect_failed` (the provider reported
failure). The message names Boot Media for a Server whose network the provisioner does not
serve. swallow never sets the Server's provisioning state itself; a stalled Server is back in
`new`, and one MAAS marked Failed commissioning stays `failed` (Inspect is accepted from it).

Retrying any Task of the Workflow (`POST /workflows/{id}/tasks/{taskId}/retry`, or
`POST /servers/{id}/inspect`) re-runs the whole Job: the enrollment wait is skipped, Boot Media
is read and applied again, and Inspect gets three new attempts. Canceling the Workflow aborts a
running inspection.

## Enrollment bundle

`POST /api/v1/provisioning/integrations/{id}/enroll-bundle` returns what a host needs to enroll
itself into this provisioner with its OS kept. The body is optional:

```json
{ "swallowUrl": "http://10.0.0.5" }
```

`swallowUrl` is the address the host reaches this installation at; the Dashboard sends its own
origin and the CLI its profile endpoint. It must be an absolute http(s) URL without path, query,
or credentials (a trailing `/` is dropped). Without it the address of the request is used.

```json
{
  "integrationId": "integration-id",
  "providerKind": "maas",
  "endpoint": "http://10.0.0.5:5240/MAAS",
  "token": "consumer:token:secret",
  "command": "curl -fsSL 'http://10.0.0.5/downloads/swallow-enroll.sh' | sudo sh -s -- --provisioner=maas --endpoint 'http://10.0.0.5:5240/MAAS' --token 'consumer:token:secret'"
}
```

| Field | Meaning |
| --- | --- |
| `endpoint` | The provider address the host must reach (for MAAS its region URL, without `/api/2.0`). |
| `token` | The Integration's stored provider credential. For MAAS this is the API key `register-machine` needs. |
| `command` | The one line to run on the host, values shell-quoted: fetch the enrollment script from `swallowUrl` and run it as root with the provider, endpoint, and token. |

The response is `Cache-Control: no-store`. The credential is the one exception to
"credentials are write-only" ([sites-integrations.md](sites-integrations.md)): MAAS offers no
narrower credential for registering a host. api-server never logs it. A host that runs the
command can use the key; operators rotate it when a host is not trusted.

## Downloads

```text
GET /downloads/swallow-enroll.sh
GET /downloads/swallow
```

Both are served **without authentication**, because the host being enrolled has no swallow
credential, and neither carries a secret. The production reverse proxy publishes `/downloads/`
on the Dashboard port and passes the request's full `Host`.

`/downloads/swallow-enroll.sh` returns a POSIX shell script (`text/x-shellscript`). Its CLI
download address is the scheme and host of the request that fetched it, so it works at whatever
address the host used. Run as root, it:

1. refuses a host that is not x86_64 or not root;
2. downloads `/downloads/swallow` into a private temporary directory;
3. runs `swallow servers enroll` with every argument it was given;
4. removes the CLI again.

`/downloads/swallow` is the linux-amd64 `swallow` CLI from `api.cliBinary` (the production
bundle's `bin/swallow`). Without one it answers `404` with a plain-text reason.

For MAAS, `swallow servers enroll --provisioner=maas --endpoint … --token …` downloads
`maas-run-scripts` from the endpoint, runs `register-machine` (the Machine is created as
Deployed) and `report-results` (the hardware is recorded from the running OS), and deletes
everything it downloaded, including the Machine's own credentials. The host needs `python3` and
HTTP access to swallow and to the endpoint (MAAS supports Ubuntu). After the next reconcile the
Server appears as `deployed`.

## Errors

| Case | Status | Code |
| --- | --- | --- |
| Unknown Integration | 404 | `not_found` |
| `swallowUrl` is not an absolute http(s) URL without path, query, or credentials | 400 | `validation_error` |
| Integration is not a provisioner, or its provider has no existing-host enrollment | 400 | `validation_error` |
| Integration has no stored credential | 503 | `provider_unavailable` |

## Compatibility Notes

Added 2026-10-06. Workflow kind `inspect-hardware` and Task kinds `wait-enrollment-settled` and
`inspect` are new published vocabulary; renaming them breaks persisted Workflows.
