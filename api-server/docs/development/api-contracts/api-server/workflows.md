# Workflows

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)

## Purpose

Accept durable user intent and expose swallow's owned multi-Task Workflow state:
normalized timeline, runner events, logs, and artifact metadata. A Workflow is a DAG of
Tasks executed by Runners (`internal`, `ansible`, `provisioner`) and orchestrated by
Temporal ([decision 072](../../../../../docs/decisions/072-store-driven-workflow-execution.md) (store driven workflow execution),
decision 072).

## Related Glossary Terms

- [Workflow](../../../../../docs/development/glossaries/terms/workflow.md)
- [Job](../../../../../docs/development/glossaries/terms/job.md)
- [Task](../../../../../docs/development/glossaries/terms/task.md)
- [Task Diagnostic](../../../../../docs/development/glossaries/terms/task-diagnostic.md)
- [Failure Class](../../../../../docs/development/glossaries/terms/failure-class.md)
- [Runner](../../../../../docs/development/glossaries/terms/runner.md)
- [Automation Configuration](../../../../../docs/development/glossaries/terms/automation-configuration.md)
- [Server Lock](../../../../../docs/development/glossaries/terms/server-lock.md)

## Endpoints

```text
POST /api/v1/workflows/
GET  /api/v1/workflows/
GET  /api/v1/workflows/{workflowId}
POST /api/v1/workflows/{workflowId}/cancel
POST /api/v1/workflows/{workflowId}/rerun
GET  /api/v1/workflows/{workflowId}/timeline
POST /api/v1/workflows/{workflowId}/tasks/{taskId}/retry
GET  /api/v1/workflows/{workflowId}/tasks/{taskId}/logs
GET  /api/v1/workflows/{workflowId}/tasks/{taskId}/stderr
GET  /api/v1/workflows/{workflowId}/tasks/{taskId}/events
GET  /api/v1/workflows/{workflowId}/tasks/{taskId}/artifacts
```

Authorization ([conventions](conventions.md#authorization)): a Workflow belongs to its targets. Listing is Pool read and filtered: a
Workflow is listed only when the caller may read every Server and Platform it targets, and one
without targets only for admins. Reading a Workflow, its timeline, and a Task's logs, stderr, events,
and artifacts is Pool read; `cancel`, `rerun`, and Task retry are Pool operate on every target.
`POST /api/v1/workflows` is admin. A caller that may not read a Workflow gets `404 not_found`.

The wire shape (paths, parameters, request and response schemas, status codes) is authoritative
in the OpenAPI document [`../openapi/openapi.yaml`](../openapi/openapi.yaml), tag `workflows`
(authored in `../openapi/areas/workflows.yaml`); the canonical Workflow object is its `Workflow`
schema with `WorkflowTask` and `WorkflowTimelineEvent`. This document keeps the behavior
semantics. OpenAPI names the path parameters `{id}` and `{taskId}` and writes the collection path
without its trailing slash; Fiber serves both spellings.

### Wire field rename (planned)

The route segments are canonical (`workflows`, `tasks`). The JSON body and Mongo still use
the field names `steps[]`, `executor`, `operationId`, and the `provisioner` Runner still
serializes as `maas`. Renaming these to `tasks[]`, `runner`, `workflowId`, and
`provisioner` (decision 072)
has not started. When it is done, it is one switch: api-server, dashboard, and cli change in
the same commit, with a schema migration for the stored names, and no period in which
consumers read both.

## Create Request

```json
{
  "kind": "custom",
  "intent": "verify SSH connectivity",
  "targetServerIds": ["server-id"],
  "platformId": null,
  "playbookName": "diagnostic-ping",
  "extraVars": {}
}
```

`kind` and a non-empty `targetServerIds` are required. `extraVars` keys starting with `swallow_` are
dropped, so a client cannot forge the variables swallow sets.

`playbookName` is required for `custom`; built-in kinds use the Site mapping unless an
explicit release-manifest playbook is supplied by a trusted use case. Some built-in kinds
are launched only through their owning feature endpoint rather than this generic create —
for example `verify-os-image` is created by `POST /provisioning/image-verifications` and
appears here in listings and timelines. A `verify-os-image` Workflow runs
`provision-os` (the real proving deploy) → one of two mutually exclusive internal finalize
Tasks (`record-image-verification` on success, `record-image-verification-failure` on
failure, recording the swallow-owned per-Deploy-Target outcome) → `recover-server` (return
the borrowed Server to `ready`, run whether the proving deploy succeeded or failed), per
[decision 035](../../../../../docs/decisions/035-os-image-verification-and-deploy-target.md) (os image verification and deploy target)
and [decision 036](../../../../../docs/decisions/036-provisioning-lifecycle-integrity.md) (provisioning lifecycle integrity).
`inspect-hardware` is created by `POST /servers/{id}/inspect` and by the worker's automatic
inspection (the `auto-inspect` controller); its `ensure-inspected` Job (`wait-enrollment-settled` → `ensure-boot-media` → `inspect`)
and retry semantics are defined in [server-enrollment.md](server-enrollment.md). Because it runs
as a Job, a Task retry re-runs the whole Job rather than only the failed Task.
`enroll-virtual-machines` is created by `POST /provisioning/virtual-machine-enrollments`; it targets
the hypervisor Server and has one independent `enroll-virtual-machine` Task per libvirt domain, so a
Task retry re-runs only that domain ([server-enrollment.md](server-enrollment.md#virtual-machines-libvirt)). Targets are frozen,
must belong to one Site, and must pass current provisioning, Platform policy, active-work,
and live Server Lock checks. Success is `202 Accepted` after intent is persisted; the worker's
dispatcher starts the Workflow's first run after the response
([workflow-execution.md](../../../../../docs/development/workflow-execution.md) §3).
`configure-omnistat` names the Omnistat Job of a `configure-slurm` deployment, and is also a
Workflow of its own: the platform controller creates one when a deployed Slurm Platform's spec turns
Omnistat's hardware counters on or off ([platforms.md](platforms.md#omnistat)). It targets the
Platform and its members, and has the `prepare-hosts` and `configure-omnistat` Jobs.

`install-exporters` and `uninstall-exporters` are retired kinds: create refuses them with
`400 validation_error`. They appear only on Workflows an earlier release created, which is why the
kind enum still lists them. The Node Exporter is Managed Software, installed and removed by
`configure-node-exporter` and `uninstall-node-exporter` Workflows ([software.md](software.md#node-exporter)).
A Task accepted under the old playbook names can still be retried: the release manifest keeps
`install-exporters` and `uninstall-exporters` as names of the Node Exporter's playbooks.

`deploy-k8s-exporters` and `remove-k8s-exporters` are retired kinds too: create refuses them with
`400 validation_error`, and they appear only on Workflows an earlier release created. The
Kubernetes exporter DaemonSets they applied and deleted were retired, because OS provisioning
installs the Node Exporter on every Server it deploys. Their playbooks are no longer in the release
manifest, so such a Workflow cannot run again.

The `202` body of create is the accepted Workflow, the projection below with `status` `pending`,
`startState` `pending`, and `leases` `null` (it owns nothing until it starts).

## Response

`GET /workflows/{workflowId}`, the list, create, and rerun return the Workflow projection: its intent,
status, Tasks (currently serialized under `steps[]` with an `executor` field, see the
wire-rename note above), resource leases, and a compatibility `execution` projection (the
example shows one of the Workflow's Tasks):

```json
{
  "id": "workflow-id",
  "schemaVersion": 3,
  "kind": "deploy-kubernetes",
  "intent": "Deploy k0s",
  "intentSnapshot": { "summary": "Deploy k0s" },
  "definition": "deploy-kubernetes",
  "definitionVersion": 1,
  "status": "waiting_external",
  "statusReason": null,
  "startState": "started",
  "temporal": { "workflowId": "swallow-operation/workflow-id", "runId": "temporal-run-id" },
  "siteId": "site-id",
  "platformId": "platform-id",
  "targetResources": [{ "kind": "platform", "id": "platform-id" }, { "kind": "server", "id": "server-id" }],
  "targetServerIds": ["server-id"],
  "steps": [
    {
      "id": "install-platform",
      "kind": "ansible-playbook",
      "name": "Install k0s Platform",
      "executor": "ansible",
      "dependsOn": ["wait-for-ssh"],
      "targets": [{ "kind": "server", "id": "server-id" }],
      "status": "running",
      "attempt": 1,
      "progress": 0,
      "error": null,
      "externalExecution": { "provider": "ansible-runner", "id": "run-id", "generation": 1 },
      "live": {
        "currentPlay": "Install k0s",
        "currentTask": "k0s_binary : Download k0s",
        "total": 12,
        "ok": 10,
        "changed": 4,
        "failed": 0,
        "unreachable": 0,
        "skipped": 2,
        "updatedAt": "2026-09-03T00:01:20Z"
      },
      "waiting": null,
      "artifacts": [],
      "startedAt": "2026-09-03T00:01:00Z",
      "finishedAt": null
    }
  ],
  "leases": [
    {
      "resourceKey": "server:server-id",
      "owner": "workflow-id",
      "fencingToken": 2,
      "updatedAt": "2026-09-03T00:01:00Z"
    }
  ],
  "retryOfOperationId": null,
  "requestedBy": "admin",
  "requestCorrelation": "request-id",
  "requestedAt": "2026-09-03T00:00:00Z",
  "startedAt": "2026-09-03T00:00:05Z",
  "finishedAt": null,
  "updatedAt": "2026-09-03T00:01:20Z",
  "execution": {
    "runId": "temporal-run-id",
    "playbook": "",
    "status": "waiting_external",
    "statusReason": null,
    "startedAt": "2026-09-03T00:00:05Z",
    "finishedAt": null
  }
}
```

Canonical Workflow status is exactly `pending | running | waiting_external |
waiting_dependency | canceling | succeeded | failed | partially_succeeded | canceled |
requires_attention`. Task status is `pending | running | waiting_external |
waiting_dependency | succeeded | failed | canceled | skipped | requires_attention`.
`execution` is a compatibility projection and must not be used to infer Task state: its `status`
repeats the Workflow status; for a Workflow with exactly one Task, `playbook` is that Task's
`kind` (not a playbook name) and `runId` is that Task's external execution ID once it started;
otherwise `playbook` is empty and `runId` is the Temporal run ID.
Sensitive keys (`extraVars`, `userData`, `password`, `token`, `credential`, `privateKey`, and
similar) are removed from `intentSnapshot` at every depth, and Task `parameters` and opaque
secret references are never returned at all, so no credential, cloud-init, or Kubernetes secret
value crosses the API.

`leases` lists the resources the Workflow owns. It is populated only by
`GET /workflows/{workflowId}` (an empty array when the Workflow owns none, which is the case before
it starts and after it ends); it is `null` in list items and in the create and rerun responses. A Workflow owns
its resources from its first run until it ends, with no expiry: `owner` is the Workflow id,
`fencingToken` is the Workflow's current dispatch generation (it increases each time the Workflow is
dispatched or resumed), and `updatedAt` is when ownership was taken. `startState` is `pending` until
the Workflow is first dispatched and `started` after; `temporal.runId` is the current run. `targetResources`,
`targetServerIds`, and `steps` are `null`, not `[]`, when the Workflow has none. A Task's `job`,
`continueOn`, `waitingReason`, and `live` are omitted when unset, while its `error`,
`externalExecution`, `waiting`, `startedAt`, and `finishedAt` are present as `null`.

`jobs` lists every Job the Tasks name in `steps[].job`, in plan order, with the user-facing
`label` a client titles that group of Tasks with and its `retry` granularity (`task` or `job`, see
[Retry Task](#retry-task)). The launcher that plans a Workflow names its Jobs, and the label is
recorded with the plan. A Workflow accepted before Jobs had labels reads the label the running build
gives the name, or the name with each hyphen-separated word capitalized when the build does not
know it (`prepare-hosts` reads `Prepare Hosts`). A Task without a `job` belongs to no Job; a client
lists such Tasks without a group. `jobs` is `[]`, never `null`, when no Task belongs to a Job.

An Ansible Task's `externalExecution` is published as soon as the run starts (not only when it
finishes), so its live diagnostics are available during the run. While it runs, `live` carries a
coarse, output-free progress summary: the current play and task the runner started, and cumulative
host-result counts (`total`, `ok`, `changed`, `failed`, `unreachable`, `skipped`). `changed` is a
subset of `ok`. `live` is advisory and present only while a run is in flight — it is absent before
the run starts and after the Task reaches a terminal state (the authoritative outcome is `status`
and `error`). There is no percentage: Ansible's total task count is not known up front. Only the
Ansible Runner populates `live`.

### Task errors and diagnostics

A Task's `error` is its latest normalized error: `code` (machine-readable; the only field a client
branches on, with `retryable`), `message` (what happened, for the user), `retryable`, an optional
`stage`, and `diagnostic`, the structured explanation of the failure
([Task Diagnostic](../../../../../docs/development/glossaries/terms/task-diagnostic.md)). `diagnostic` is
always present and is `null` when the Runner recorded none — every error stored before 2026-11-05,
and errors of Runners that do not explain their failures yet. It has two layers:

- **User layer** — with the error's `message`, `target` (the Server or Platform the failure affects,
  by `kind` and `id`, when the Runner knows which of the Task's targets it is) and `remediation` (the
  next step, in the user's terms). A client shows this layer first; it must be actionable alone.
- **Admin layer** — `failureClass`, always present; `endpoint` (the address the failing call went
  to), `operation` (the provider or platform call that failed), `cause` (the causal chain as text, at
  most 2000 characters), `attempts`, `elapsedSeconds` and `budgetSeconds` of the wait that gave up,
  its last `observation` and `observedAt`, and `providerEvents` (what the provider recorded about the
  target around the failure, oldest first, at most 20, each with `type`, `description`, and an
  optional `at`). Each of these is omitted when the Runner did not record it; a zero count or
  duration is omitted too.

`failureClass` is `transient` (the condition is expected to clear by itself; waiting or retrying
can succeed without a change), `permanent` (it repeats until something changes; a wait that sees
it fails at once), or `unknown` (not classified, including an outcome that cannot be proven, such
as `executor_outcome_unknown`)
([Failure Class](../../../../../docs/development/glossaries/terms/failure-class.md)). It informs the
user; whether a Task retry may be requested is still only `retryable`.

Redaction removes secrets without erasing the cause. Before an error is stored, swallow masks, in
`message` and every diagnostic text, URL userinfo, bearer tokens and JSON Web Tokens, Authorization
header values, credential key-value pairs (`password=`, `token:`, `"secret": ...` and similar), and
PEM private key blocks, each replaced by `[REDACTED]` (a private key by `[REDACTED PRIVATE KEY]`);
the surrounding text is kept. `endpoint` additionally never carries userinfo, query values (each
replaced by `[REDACTED]`, keys kept), or a fragment. Redaction is by shape: a client must still treat
diagnostic text as detail for the people who operate the targets, who are the only callers besides
admins that may read a Workflow.

The diagnostic is durable: it is stored with the Task and served on every read of the Workflow. The
Task keeps only its latest error; the timeline keeps each earlier attempt's (see below).

`waiting` is what a running Task waits for, as its Runner last published it during a long wait:
`observation` (the latest observed state of what it waits for, redacted), `observedAt`, `attempts`
(observations so far in this wait), `elapsedSeconds`, and `budgetSeconds` (how long the wait may run
before the Task fails; omitted for a wait without a budget). It is advisory, like `live`: it is
`null` before a Runner publishes, for Runners that do not publish, and once the attempt ends, is
retried, or is canceled; the authoritative outcome is `status` and `error`. When a wait gives up, its
last observation, attempts, elapsed time, and budget move into the error's `diagnostic`.

## List Query

Optional filters are `siteId`, `platformId`, `serverId`, `kind`,
`status`, `active=true`, and common pagination. `active` constrains only when it is exactly
`true`. Results are Workflow projections sorted by `requestedAt` descending.

## Timeline And Task Diagnostics

`GET /timeline` returns normalized immutable events ordered by `createdAt` and ID, naming
Workflow and Task state transitions with no secret material. A Task status event
(`type` `step_status`) is recorded once per status per attempt; its `details` carry `status`
and `attempt`, and, when the Task has a normalized error, that error's `code`, `message`
(redacted, as stored), `stage`, and `retryable`, and, when the error has a diagnostic, its
`failureClass` and (when it has one) `endpoint`. The Task itself keeps only its latest error, so
these events are where an earlier attempt's failure remains readable after a retry. Events
recorded before 2026-11-05 carry no `failureClass`. A cancel that interrupted a
`provision-os` records, on that Task's `stepId`, what happened to its Server (see Cancel):
`canceled_deployment_returned_to_ready`, `canceled_deployment_left_as_is`, or
`canceled_deployment_return_failed`, with a user-facing `message` and `details.attempt`. Task logs are UTF-8
`text/plain`; a non-Ansible Task or one without output returns an empty body. `GET
/tasks/{taskId}/stderr` returns an error-only UTF-8 `text/plain` report for an Ansible Task —
one block per failed or unreachable task (task, host, message, return code, and its captured
stderr/stdout) — and an empty body when the Task recorded no failure or is not an Ansible
Task; it is the focused counterpart to `logs` (the full runner output). Secrets in the report are
masked as in Task errors, including Ansible's `*_pass` variables; the rest of each block is kept.

`GET /tasks/{taskId}/events` returns an Ansible Task's per-task results, and an empty list for
other Runners. The events stream **during** the run — new results appear as the runner completes
each task, so a client polling this endpoint sees progress instead of waiting for the whole run —
and each carries `play`, `task`, `host` (a serverId), `status` (`ok | failed | unreachable |
skipped`), `changed` (a separate boolean: a changed result has `status` `ok`, never a `changed`
status), and `startedAt`/`endedAt` (`null` when the runner recorded none); task output is never
included (secret safety). The response also carries the Ansible `runId` (empty for a Task without
an Ansible run), the Task's `status`, and derived counts (`okCount`, `changedCount` — results with
`changed` true —, `failedCount`, `unreachableCount`, `skippedCount`). The current running
play/task is on the Task's `live` field above, not in this list. Raw stdout stays on disk and is
served by `logs`; it is not part of this projection. Task artifacts return metadata only
(`id`, `name`, `mediaType`, `sizeBytes`, `createdAt`; never a server-side path). `targets`,
`artifacts`, and timeline results are JSON arrays and never `null`; `dependsOn` is `null` or
empty for a Task that waits for none.

## Cancel

`POST /{id}/cancel` returns `202` with `{ "operationId": "<workflowId>" }` once the cancel request is
stored. The worker delivers it to the Workflow's run, or resumes the Workflow when its execution is
gone or it has not started yet, so a cancel always completes. The Workflow writes `canceling`, then
`canceled` with `statusReason` `Canceled by user.`; a Workflow superseded by a rerun or by
`cancelActiveWorkflows` carries that reason instead. Every non-terminal Task, including
`requires_attention`, becomes `canceled`; Tasks that already reached an outcome keep it. Active MAAS
or Ansible execution receives a best-effort abort. Cancel on a finished Workflow is `conflict`.

A Task that was running when the cancel arrived also ends `canceled`. A canceled
`provision-os` Task sets its Server's `deployment` result to `canceled`; a `provision-os` that
already `succeeded` or `failed` keeps that outcome, including in a platform deployment whose Jobs
retried it. The provider-owned `provisioning.state` is not rewritten by the cancel itself and keeps
following the provider.

A cancel also returns the Servers whose `provision-os` it interrupted to Ready
([decision 061](../../../../../docs/decisions/061-recovery-supersedes-blocking-work.md) (recovery supersedes blocking work)). Before the
Workflow becomes `canceled`, and while it still owns its resources, swallow aborts a deployment the
provider still reports as Deploying, waits up to 10 minutes for the provider to leave it, and
returns an Allocated Machine (where MAAS parks an aborted deployment) to Ready with the Recover
plan: a plain Release that keeps static IP bindings and erases no disk. A Machine the provider
reports as deployed, failed, broken, or in rescue holds the deployment's outcome and is left as
it is; so is one already Ready. The return is best-effort; the Servers are returned in parallel,
each bounded to 30 minutes. Each outcome is a timeline event on the interrupted Task (see
Timeline), and a Server it could not return stays in a state Recover accepts. The Server's `deployment` result stays
`canceled` until reconcile observes it Ready and clears it. A `verify-os-image` Workflow returns
its borrowed Server through its own recover Task instead.

## Retry Task

`POST /{id}/tasks/{taskId}/retry` returns `202` only when the Task is `failed` or
`requires_attention` and its normalized error has `retryable=true`; any other Task is
`conflict`. The body names the Workflow and the Task: `{ "workflowId", "taskId" }`. The retry is
stored and the Workflow
applies it in the same Workflow, also when its execution was lost (the worker resumes it). A Task
without a Job, or in a Job that retries by Task, runs again at the next attempt and its successful
dependencies are not repeated; a Task in a Job that retries as a whole (for example a platform
deployment's `ensure-os` and `configure-k0s`, or `inspect-hardware`'s `ensure-inspected`) runs
every Task of that Job again at the next attempt. A provider or Ansible idempotency identity
includes Workflow, Task, target, and attempt.

## Rerun (recovery)

`POST /{id}/rerun` converges a Workflow's intent again as a new Workflow, for a Workflow that
finished `failed`, `partially_succeeded`, or `canceled`, or one parked in `requires_attention`
that the user wants to converge from the start rather than retry by Task. It launches a new
Workflow for the same intent, on the same Platform, and returns `202` with the new Workflow
projection (its `leases` is `null`: it owns none yet). A `requires_attention` original is canceled
(`statusReason` `Superseded by a rerun.`) in the same request, and the new Workflow waits in
`waiting_dependency` until the original's cancel completes and frees its Servers. The new Workflow's
`retryOfOperationId` points at the original, it carries a later
`requestedAt` so the platform lifecycle follows it, and the original stays in history.

A rerun converges the intent to its desired state
(decision 061). Every ensure
Task runs again, whatever its earlier outcome, at the attempt after its last one: `provision-os`,
`ensure-boot-media`, `wait-for-ssh`, `ansible-playbook`, `validate-platform-health`,
`release-os`, and `recover-server`. Like an explicit Task retry, each observes before acting:
`provision-os` succeeds without provider work when the Server is already deployed with the frozen
image and reachable over SSH, and records that `succeeded` result on the Server under the new
Workflow; it recovers a `failed`, `allocated`, `broken`, or `rescue` Machine before redeploying,
and ends with `deployment_image_mismatch` rather than replace a different deployed image.
`release-os` and `recover-server` succeed on a Server that is already Ready. A Server Default User
is cleared only when a deployment is actually submitted. Any other Task keeps the earlier rule: an
already `succeeded` or `skipped` one is preserved so its one-time side effect is not repeated, and
the rest run again. A succeeded `ansible-playbook` is also preserved once a `release-os` in the
same Workflow succeeded, because a released host no longer runs the OS the playbook needs. A
Workflow that already `succeeded` or is still actively advancing
(`pending`, `running`, `waiting_external`, `waiting_dependency`, `canceling`) is rejected with
`conflict`; those cases use Task retry or cancel instead.

## Durability And Protection

A Workflow's state lives only in swallow's store; Temporal executes its runs. The stable Temporal
Workflow ID is `swallow-operation/<workflowId>` (the Temporal name is history-tied and retained);
each dispatch is a new run under it, and at most one run exists at a time. The worker dispatches a
new Workflow, delivers cancel and retry requests, and resumes a Workflow whose execution is gone:
Tasks whose Runner can observe continue at the same attempt and observe before acting, and an
Ansible Task whose outcome cannot be proven becomes `requires_attention` with
`executor_outcome_unknown`. A run superseded by a later dispatch cannot write state or cause side
effects (dispatch-generation fencing).

Resource mutations take sorted `server:<id>` and `platform:<id>` ownership in one step when the
Workflow starts; a resource another Workflow owns keeps it in `waiting_dependency`. Ownership has
no expiry and is released when the Workflow ends, so a Workflow parked in `requires_attention`
keeps its Servers for as long as it waits, with no renewal that a transient outage could break.

## Errors

Target overlap, Server Lock, a target not in the required provisioning state, Platform policy,
invalid control state, an ineligible Task retry, and a playbook that fetches artifacts while the
Site's Local Repository serves deployments and is not ready
([local-repository.md](local-repository.md#deployments)) return `conflict`. A malformed request,
targets of more than one Site, or an unregistered or unmapped playbook returns
`validation_error`. Disabled Site automation, a missing Deployment Key or Site
credential, and an unknown live Server Lock state return `provider_unavailable`. An unknown
Workflow or Task, an unknown target Server, and a Site with no Automation Configuration return
`not_found`.

## Compatibility Notes

On 2026-11-03 the `statusReason` of a Workflow a user canceled became `Canceled by user.` and that of
a Workflow stopped at a failed Step `A failed Step requires user attention.` (both said operator).
Workflows recorded before keep their old text; clients must not match on `statusReason`.

On 2026-10-19 (decision 061) a
cancel began returning the Servers of interrupted `provision-os` Tasks to Ready and recording
three new timeline event types, and a rerun began running every ensure Task again instead of
carrying succeeded ones over. Routes, statuses, and error codes are unchanged. A rerun of a
platform deployment now takes the time of its idempotent playbook again.

On 2026-10-30 the OpenAPI document became the authoritative wire shape. This contract was
corrected to the implemented behavior, with no behavior change: create answers in the
compatibility shape; `leases` is populated only on get by ID; an ineligible Task retry and a
Site with no Automation Configuration are `conflict` and `not_found` (not `validation_error` and
`provider_unavailable`); Task events never carry a `changed` status; the legacy logs and events
of a schema-v3 Workflow are `not_found`; and the cancel and Task-retry bodies are documented.

On 2026-10-31 (decision 072)
execution state moved to the store alone. Breaking: a lease is ownership with no `expiresAt`, its
`owner` is the Workflow id instead of the Temporal Workflow ID, and `fencingToken` is the dispatch
generation. Task retry and cancel no longer return `conflict` when the execution is gone; the
Workflow is resumed instead, so no Workflow is marked with a lost-execution `statusReason` any
more. Cancel now also cancels a parked Workflow's `requires_attention` Tasks, and a rerun of a
`requires_attention` Workflow cancels the original through its own cancel path. Every existing Job
keeps retrying as a whole, and Tasks now start in a sliding window of `OperationMaxParallelism`
instead of batches; neither changes a route or a status.

On 2026-10-31 error codes were aligned with conventions.md. Breaking: a target not in the
required provisioning state is `conflict` (it was `validation_error`), and the legacy logs and
events of a schema-v3 Workflow are `conflict` (they were `not_found`, because the conflict was
overwritten). Targets of more than one Site stay `validation_error` here and are now that in
every context.

On 2026-10-31 ([decision 074](../../../../../docs/decisions/074-retire-schema-v2-and-deprecated-aliases.md) (retire schema v2 and deprecated aliases))
the schema-v2 Operation records were retired. Breaking: create answers with the Workflow projection
(it was the compatibility `LegacyOperation` shape); the list and get serve Workflows only, so a
schema-v2 ID is `not_found`; and `GET /operations/{id}/logs`, `GET /operations/{id}/events`, and
`POST /operations/{id}/retry` were removed (use the per-Task endpoints, Task retry, or rerun).
Schema v10 drops the records.

Also on 2026-10-31 (decision 074) the one-release aliases ended. Breaking: the deprecated
`/api/v1/operations` alias (including its `steps/{stepId}` paths) was removed; `clusterId` is no
longer a Workflow field, a create field, or a list filter; and the Task-retry body no longer
repeats the Workflow and Task as `operationId` and `stepId`.

On 2026-11-02 each Workflow gained `jobs`, the Job names with their label and retry granularity, so
a client no longer derives Job titles or groups from Task ids. Additive: no field changed.

On 2026-11-03 the kinds `configure-node-exporter` and `uninstall-node-exporter` were added, and
create began refusing `install-exporters` and `uninstall-exporters` with `400 validation_error`
(breaking); Workflows of those kinds that an earlier release created are still listed and read.

Also on 2026-11-03 create began refusing `deploy-k8s-exporters` and `remove-k8s-exporters` with
`400 validation_error` (breaking), and their playbooks left the release manifest; Workflows of
those kinds that an earlier release created are still listed and read.

On 2026-11-05 ([decision 077](../../../../../docs/decisions/077-task-diagnostics-and-wait-policy.md) (Task
diagnostics and wait policy)) a Task's `error` gained `diagnostic`, the two-layer structured
explanation of the failure (`null` when there is none, including every error stored before), and each
Task gained `waiting`, what a running Task waits for (`null` when it reports nothing), and
`step_status` timeline details gained `failureClass` and `endpoint`. Additive: no field, status, or
error code changed meaning, and clients that ignore the new fields see the old shape. Error
`message`s are now stored redacted, so a message that used to echo a credential shows `[REDACTED]`
in its place. The same day the `stderr` report and the `ansible_failed` message began masking
secrets in task output (`[REDACTED]`), and an `ansible_failed` error carries a `diagnostic`
(`failureClass` `unknown`; `operation` and `target` when one task or one Server failed).

On 2026-11-09 a `configure-omnistat` Workflow of its own began to appear: the platform controller
creates one when a deployed Slurm Platform turns Omnistat's hardware counters on or off. The kind
was already in the enum (the Omnistat Job of a deployment); no field, status, or error changed.
