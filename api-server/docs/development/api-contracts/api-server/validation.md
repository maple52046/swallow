# Validation

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli`

## Purpose

Validate deployed Servers and Platforms: list the Validation Suites swallow offers (declared by the
release's modules) and the user-defined ones (a script any User writes, judged by its exit code and
run as the User who starts it) with their revisions, start a Validation
Run of a suite against its targets and read every check's verdict on every target, cancel a run, and
manage Validation Schedules that start runs on a cron expression or after a deployment
([decision 081](../../../../../docs/decisions/081-validation-suites-and-runs.md) (validation suites and
runs)). Every run is executed by a Workflow, readable on the [workflows](workflows.md) surface; the
run is the durable record of what was asked and what each check found.

The wire shape (paths, parameters, schemas, status codes) is described by
[`../openapi/areas/validation.yaml`](../openapi/areas/validation.yaml); this document holds the
behavior semantics. The behavior specification is
[`docs/development/validation.md`](../../../../../docs/development/validation.md).

## Related Glossary Terms

- [Validation Suite](../../../../../docs/development/glossaries/terms/validation-suite.md)
- [Validation Check](../../../../../docs/development/glossaries/terms/validation-check.md)
- [Validation Verdict](../../../../../docs/development/glossaries/terms/validation-verdict.md)
- [Validation Impact](../../../../../docs/development/glossaries/terms/validation-impact.md)
- [Validation Run](../../../../../docs/development/glossaries/terms/validation-run.md)
- [Validation Schedule](../../../../../docs/development/glossaries/terms/validation-schedule.md)
- [Workflow](../../../../../docs/development/glossaries/terms/workflow.md)
- [Platform Claim](../../../../../docs/development/glossaries/terms/platform-claim.md)
- [Condition](../../../../../docs/development/glossaries/terms/condition.md)
- [Pool Role](../../../../../docs/development/glossaries/terms/pool-role.md)
- [Host Access](../../../../../docs/development/glossaries/terms/host-access.md)

## Endpoints

```text
GET    /api/v1/validation/suites
POST   /api/v1/validation/suites
GET    /api/v1/validation/suites/{suiteId}
PUT    /api/v1/validation/suites/{suiteId}
DELETE /api/v1/validation/suites/{suiteId}
GET    /api/v1/validation/suites/{suiteId}/revisions/{revision}
GET    /api/v1/validation/runs
POST   /api/v1/validation/runs
GET    /api/v1/validation/runs/{runId}
POST   /api/v1/validation/runs/{runId}/cancel
GET    /api/v1/validation/schedules
POST   /api/v1/validation/schedules
GET    /api/v1/validation/schedules/{scheduleId}
PUT    /api/v1/validation/schedules/{scheduleId}
DELETE /api/v1/validation/schedules/{scheduleId}
```

| Operation | operationId |
| --- | --- |
| `GET /api/v1/validation/suites` | `listValidationSuites` |
| `POST /api/v1/validation/suites` | `createValidationSuite` |
| `GET /api/v1/validation/suites/{suiteId}` | `getValidationSuite` |
| `PUT /api/v1/validation/suites/{suiteId}` | `replaceValidationSuite` |
| `DELETE /api/v1/validation/suites/{suiteId}` | `deleteValidationSuite` |
| `GET /api/v1/validation/suites/{suiteId}/revisions/{revision}` | `getValidationSuiteRevision` |
| `GET /api/v1/validation/runs` | `listValidationRuns` |
| `POST /api/v1/validation/runs` | `startValidationRun` |
| `GET /api/v1/validation/runs/{runId}` | `getValidationRun` |
| `POST /api/v1/validation/runs/{runId}/cancel` | `cancelValidationRun` |
| `GET /api/v1/validation/schedules` | `listValidationSchedules` |
| `POST /api/v1/validation/schedules` | `createValidationSchedule` |
| `GET /api/v1/validation/schedules/{scheduleId}` | `getValidationSchedule` |
| `PUT /api/v1/validation/schedules/{scheduleId}` | `replaceValidationSchedule` |
| `DELETE /api/v1/validation/schedules/{scheduleId}` | `deleteValidationSchedule` |

## Authentication and Authorization

Authorization ([conventions](conventions.md#authorization)):

| Route | Class |
| --- | --- |
| `GET` suites, a suite, and a suite revision | Reference |
| `POST /validation/suites` | Author: any User; the caller becomes the suite's author |
| `PUT /validation/suites/{suiteId}` | Author: only the suite's author (by user id), not an admin who is not the author (`403`) |
| `DELETE /validation/suites/{suiteId}` | Author or admin (`403` otherwise) |
| `GET /validation/runs` | Pool read, filtered to runs whose every target the caller may read |
| `GET /validation/runs/{runId}` | Pool read on the run's targets; whoever reads a run reads its evidence |
| `POST /validation/runs` | Pool read on every target (`404` otherwise), then by suite: Pool operate on every target for a module suite or a user suite with `starterLevel: owner`, Pool read for `starterLevel: member` (`403`); for a user suite also the starter's Host Access and `sudo` on every target (Start) |
| `POST /validation/runs/{runId}/cancel` | Pool read on the run's targets (`404` otherwise), then the run's starter (`requestedById` is the caller), Pool operate on every target, or admin (`403` otherwise) |
| Every Validation Schedule route | Admin |

The targets are every Server of a `servers` suite run, or the Platform of a `platform` suite run.
A user suite runs on each target as the Linux account of the User who starts the run, so what it may
do there is what that User may do there.

## Suites

`GET /api/v1/validation/suites` returns module suites first (ordered by `id`), then user suites
(ordered by `name`). A module suite (`source: module`) is declared by the release's modules: `module`
names the declaring module, `platformType` is set for a `platform` suite, `parameters` is the schema
its run parameters are checked against (the [modules](modules.md) spec schema subset) or `null`, and
every user-suite field is `null`. A module suite cannot be replaced or retired (`409`).
`experimental: true` marks a module suite its module offers as a preview whose checks may still
change; the list includes it, and it is run, scheduled, and read like any other suite (a consumer
may hide it). A user suite is never experimental.

A user suite (`source: user`) has a UUID `id`, target `servers`, the single check `script` (not
gating), `budgetSeconds` equal to its `timeoutSeconds` plus five minutes, and its `script`, `sudo`,
`starterLevel`, `timeoutSeconds`, `revision`, and `scriptSha256`. Its exit code is the verdict: `0` is
`passed`, any other code `failed`, and a run past `timeoutSeconds`, or on a host where the starter's
account cannot run it (Runs), `error`.

- `createdBy` is the author's username as it was when the suite was created, for display; it does not
  change when that User is deleted. `createdById` is the author's user id, which decides who may
  replace and retire the suite; it is `null` for a suite whose author could not be identified when
  this field was introduced (treated as a deleted author). Both are `null` for a module suite.
- `sudo` (default `false`): the starter runs the script through `sudo -n`, so it runs only on targets
  where the starter has passwordless `sudo` (Start). Without it the script runs with the starter's
  own rights and may still call `sudo -n` itself, under the same Sudo Policy.
- `starterLevel` (default `owner`): the least Pool Role a User needs on every target to start the
  suite: `owner` (Pool operate) or `member` (Pool read). Admins pass both.
- `sudo` and `starterLevel` are `null` for a module suite, which behaves as `starterLevel: owner` and
  runs as root.
- `POST` creates it at revision 1 with the caller as author. Defaults: `sudo` `false`, `starterLevel`
  `owner`, `timeoutSeconds` 600, `impact` `workload`. The script is at most 64 KiB and runs with its
  own interpreter line (for example `#!/bin/bash`), or under `/bin/sh` without one; it must not
  contain secrets, because every User reads it as written. The tail of its output (stdout, then
  stderr) becomes the result's evidence, redacted, and its exit code a measurement.
- `PUT` (the author only) replaces `name`, `description`, `script`, `sudo`, `starterLevel`,
  `timeoutSeconds`, and `impact`; `revision` increments when any field but `name` and `description`
  changes. The write applies only at the revision it read: of two concurrent replaces one succeeds
  and the other is `409`, so one revision number never names two scripts. With `revision` in the
  body it applies only when that is the current revision (`409` otherwise), so a client never
  overwrites a revision it did not load. Runs already accepted keep the revision, sha256, and script
  they ran.
- `DELETE` (the author or an admin) retires the suite: it leaves the catalog (`includeRetired=true`
  lists it), stays readable by id with its revisions, and cannot be run or replaced (`409`). It is
  refused (`409`) while a Validation Schedule names it.
- A module suite written is `409`; a user suite written by a caller its class does not allow is
  `403`.

### Revisions

`GET /api/v1/validation/suites/{suiteId}/revisions/{revision}` returns one immutable revision of a
user suite, retired or not: `suiteId`, `revision`, `script`, `scriptSha256`, `sudo`, `starterLevel`,
`timeoutSeconds`, `impact`, and `createdAt` (when that revision was saved). Every revision is kept as
long as the suite. A suite created before revisions were kept has only the revision it had at that
upgrade and later ones; an earlier revision, an unknown one, and any revision of a module suite are
`404`. A consumer compares the script of the revision its caller last ran with the current one to
show what changed before the caller starts the new revision.

## Runs

### Start

`POST /api/v1/validation/runs` accepts a report-mode run. A `servers` suite takes `serverIds` (one
or more, all at one Site); a `platform` suite takes `platformId`. For a user suite, `suiteRevision`
(optional) names the revision the caller reviewed; without it the current revision runs. The request
is refused, writing nothing, when:

- the suite does not exist or is retired (`404`), or the parameters do not match its schema (`400`);
- `suiteRevision` is given for a module suite (`400`), or for a user suite whose current revision
  differs (`409`: the suite changed after the caller reviewed it; read the new revision and start
  again with it);
- the caller may not read a target (`404`), or is below the suite's starter level on a target
  (`403`; Authentication and Authorization);
- a target Server does not exist (`404`) or is not `deployed`, is locked, or — for a `workload` or
  `disruptive` Servers suite — has a Platform Claim (`409`; run that Platform's suites instead);
- the Platform does not exist (`404`), is not `active`, is not of the suite's `platformType`, or its
  module names no access Server for its spec (`409`);
- the suite is `disruptive` and `confirm` is not `true` (`400`);
- for a user suite, the caller's account cannot run it on every target
  ([identity-and-access §8.3](../../../../../docs/development/identity-and-access.md)):
  - the caller has no Host Access to a target (`403`);
  - the suite has `sudo: true` and the caller has no `sudo` on a target (`403`), or only `sudo` that
    needs a password (`409`);
  - no Directory is configured, the caller has no `uidNumber`, or a target has no installed
    directory client, which every non-Ubuntu Server lacks (`409`: the target has no account for the
    caller);
- a target already has unfinished durable work, another run's included (`409`, naming the Workflow);
- the Site cannot run Ansible automation, or a Server Lock cannot be confirmed (`503`).

The error's `message` names the reason and the target. A user suite then runs on each target as
the caller's account, through `sudo -n` when `sudo` is `true`. A host can still disagree with what
the API saw (a `sudo` rule takes about a minute to reach the hosts, or a local account shares the
caller's name); that host's `script` result is then `error`, and its `summary` starts with a code in
brackets:

| Code | When |
| --- | --- |
| `validation_account_missing` | The host has no account with the caller's username |
| `validation_account_mismatch` | The account with that username has another uid than the caller's `uidNumber` |
| `validation_become_failed` | The automation user may not run commands as the caller on the host |
| `validation_sudo_unavailable` | `sudo: true` and the caller's `sudo -n` fails on the host; the summary carries sudo's message |
| `validation_script_unplaced` | The script could not be put on the host |

The run's verdict follows from its results as usual; its Workflow still succeeds.

The `202` response is the `pending` run. Its `workflowId` is a `run-validation` Workflow that takes
the targets (for a Platform: `platform:<id>` and its access Servers). It is unattended, bounded by
the suite's `budgetSeconds`, and preemptible unless the suite is `disruptive`: a later Workflow that
needs one of its Servers (a deployment, a Release, a software install) cancels it, and the run ends
`canceled` with `statusReason` naming that Workflow. It cannot be rerun on the workflows surface
(`409`); start a new run instead.

### Read and list

`GET /api/v1/validation/runs/{runId}` returns the run with its `results`; the list returns runs
without `results`, newest first. `serverId` matches a run whose target Servers include the Server or
whose Platform has it as a member at acceptance. `siteId` exact-matches the Site recorded by the run; when it is
omitted the list spans all Sites, and an unknown Site returns an empty page with `total: 0`.
`requestedBy=me` keeps only runs the caller started (`requestedById` is the caller's id); `me` is
its only value (`400` otherwise). With `suiteId`, its first item is the caller's latest run of that
suite, whose `suite.revision` is the revision the caller last ran.

- `status`: `pending` until its first check Task starts, `running`, then `completed` or `canceled`.
- `verdict`: `null` until `completed` and for a `canceled` run; then `failed` when any result is
  `failed`, else `error` when any is `error`, else `passed`. `counts` counts the results by verdict.
- `results`: one per check and target. A `servers` suite has a result for every check on every
  target Server once completed; a check no Task judged gets `error` with `taskId: null`. A `platform`
  suite's results target the Platform or individual member Servers (for example one per GPU node).
  `evidence` is redacted and at most 8 KiB; `summary` at most 1 KiB.
- `checks` is the suite's checks as they were when the run was accepted, and `suite` records the
  module version or the user suite's revision and sha256 it ran.
- `trigger.kind` is `manual`, `schedule` (with `scheduleId`, and `workflowId` when the schedule's
  trigger was a deployment), or `deployment` (a gate run, `mode: gate`, whose `workflowId` is the
  deployment's Workflow).
- `requestedBy` is who asked, as before: the starter's username for a `manual` run, the deployment's
  requester for a gate run, `validation-schedule` for a scheduled run. `requestedById` is the
  starter's user id for a `manual` run; it is `null` for other triggers and for runs accepted before
  this field existed.
- `runAs` is the account a user suite run uses on its targets: `username`, `uidNumber`, and `sudo`
  (whether the script runs through `sudo -n`); `null` for a module suite run, which runs as root.

A run is kept for 90 days after it was requested (the installation's `validationRunRetention`); after
that it is `404`.

### Cancel

`POST /api/v1/validation/runs/{runId}/cancel` writes the cancel of a report run's Workflow and
returns `202` with the run, which ends `canceled` once the cancel path finished; results recorded
before stay. A run that already ended, and a gate run (cancel its deployment instead), are `409`.
The run's starter, an owner of every target's Pool, and an admin may cancel it; another caller who
may read the run is `403`.

## Schedules

`GET /api/v1/validation/schedules` accepts an optional exact `siteId`. When omitted it returns
schedules across all Sites; an unknown Site returns an empty list.

A Validation Schedule is desired state ([desired-state-model](../../../../../docs/development/desired-state-model.md)):
`spec` is written only by this API, `status` only by the worker's `validation-schedule` controller.

- `spec.trigger.kind` `cron` needs `cron`, five fields (minute, hour, day of month, month, day of
  week; `*`, lists, ranges, and `/step`), read in `timezone` (an IANA name, `UTC` when not set);
  `after-deployment` takes neither and runs after each deployment of the target: for a Platform, each
  Workflow that succeeds in realizing its spec (a deployment, its rerun, or a change such as the GPU
  Operator's; not a run or an uninstall); for Servers, each `deploy-os` Workflow that succeeds on one
  of them. The deployment that exists when the schedule (or a change of its spec) is first seen does
  not start a run.
- `spec.suiteId` names a module suite. A user suite is `400` on `POST` and `PUT`: it runs as the User
  who starts it, and a scheduled run has none.
- The targets follow the suite's kind as for a run; they are checked when the schedule is written and
  again at each occurrence.
- `status.nextRunAt` is the next cron time (`null` for `after-deployment` and while disabled);
  `lastRunId` and `lastRunAt` the latest run it started; `lastSkipReason` why the latest occurrence
  started none (a busy, locked, or not deployed target). A time missed while the worker was down runs
  once. A schedule never has two unfinished runs; an occurrence while one runs is skipped.
- Conditions: `realized` is `true` while the schedule can start runs, `false` with reason
  `suite_missing` or `target_missing` when its suite was retired or its target is gone, `false` with
  reason `user_suite_unsupported` when it names a user suite (written before schedules refused them),
  and `false` with reason `disabled` while `enabled` is `false`. A schedule that is not realized
  starts no run.
- `PUT` replaces the spec and increments `generation`; with `generation` in the body it applies only
  at that generation (`409` otherwise). `DELETE` removes the schedule; its runs stay.

## Errors

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | Malformed body or parameter, as each operation lists: among them `suiteRevision` for a module suite, a `requestedBy` other than `me`, and a schedule naming a user suite |
| 403 | `forbidden` | A user suite replaced by other than its author, or retired by other than its author or an admin; a run started below the suite's starter level, or by a caller without Host Access, or without `sudo` for a `sudo` suite, on a target; a run canceled by a caller who is not its starter, an owner of every target's Pool, or an admin |
| 404 | `not_found` | The suite, suite revision, run, schedule, target Server, or Platform does not exist, or the caller may not read it |
| 409 | `conflict` | A module suite written, a retired suite run, a stale `suiteRevision` or `revision`, a concurrent replace, a target not runnable (including no account for the caller there, or `sudo` that needs a password) or busy, a run already ended, a gate run canceled, or a stale `generation` |
| 503 | `provider_unavailable` | The Site cannot run Ansible automation, or a Server Lock cannot be confirmed |

## Compatibility Notes

New in 2026-11-12. The Workflow kind `run-validation` joins the workflows contract's kind enum, and
the modules contract's `kind` gains `validation`. Later the same day a suite gained `experimental`; Runs and Schedules gained optional exact `siteId` list filters.

Since 2026-11-17 every User writes and shares user suites, which run as the User who starts them:

- `POST /validation/suites` moved from Admin to Author; `PUT` to Author (an admin who is not the
  author is now `403`); `DELETE` to Author or admin. Suites gained `createdById`, `starterLevel`, and
  `sudo`; the new route `GET /validation/suites/{suiteId}/revisions/{revision}` reads kept revisions,
  and `PUT` accepts an optional `revision` precondition.
- `BREAKING CHANGE`: a suite's `runAs` (`automation-user` or `root`) is removed from requests and
  responses; `sudo` replaces it. A stored `runAs: root` reads as `sudo: true`, anything else as
  `false`.
- Existing user suites change behavior: they run as the User who starts each run, no longer as the
  automation user or root. A former `root` suite (now `sudo: true`) runs only where its starter has
  passwordless `sudo`, which no Pool grants by default. Every user suite needs a Directory, the
  directory client on its targets, and the starter's Host Access there.
- `POST /validation/runs` accepts `suiteRevision`, refuses a user suite started below its
  `starterLevel` (`403`), and checks the starter's Host Access, `sudo`, and account (`403`, `409`).
  `starterLevel: owner`, the default and the value every existing suite reads as, keeps the earlier
  rule that only Pool owners start it.
- Runs gained `requestedById` and `runAs`; the list gained `requestedBy=me`. The run's starter may now
  cancel it without being an owner.
- Schedules refuse user suites (`400`); an existing schedule that names one is `realized=false` with
  reason `user_suite_unsupported`.
