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
release's modules) and the user-defined ones (a script judged by its exit code), start a Validation
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

## Endpoints

```text
GET    /api/v1/validation/suites
POST   /api/v1/validation/suites
GET    /api/v1/validation/suites/{suiteId}
PUT    /api/v1/validation/suites/{suiteId}
DELETE /api/v1/validation/suites/{suiteId}
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

Every endpoint requires the `admin` role (a Session access token or an API Key), according to
[conventions](conventions.md). A user suite runs arbitrary commands on its targets with the
automation user's or root's rights, so creating and running one is admin-only like every route here.

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
gating), `budgetSeconds` equal to its `timeoutSeconds` plus five minutes, and its `script`, `runAs`
(`automation-user` or `root`), `timeoutSeconds`, `revision`, and `scriptSha256`. Its exit code is the
verdict: `0` is `passed`, any other code `failed`, and a run past `timeoutSeconds` `error`.

- `POST` creates it at revision 1. Defaults: `runAs` `automation-user`, `timeoutSeconds` 600, `impact`
  `workload`. The script is at most 64 KiB and runs with its own interpreter line (for example
  `#!/bin/bash`), or under `/bin/sh` without one; it must not contain secrets, because it is stored
  and returned as written. The tail of its output (stdout, then stderr) becomes the result's
  evidence, redacted, and its exit code a measurement.
- `PUT` replaces `name`, `description`, `script`, `runAs`, `timeoutSeconds`, and `impact`;
  `revision` increments when any of the last four changes. Runs already accepted keep the revision,
  sha256, and script they ran.
- `DELETE` retires the suite: it leaves the catalog (`includeRetired=true` lists it), stays readable
  by id, and cannot be run or replaced (`409`). It is refused (`409`) while a Validation Schedule
  names it.

## Runs

### Start

`POST /api/v1/validation/runs` accepts a report-mode run. A `servers` suite takes `serverIds` (one
or more, all at one Site); a `platform` suite takes `platformId`. The request is refused, writing
nothing, when:

- the suite does not exist or is retired (`404`), or the parameters do not match its schema (`400`);
- a target Server does not exist (`404`) or is not `deployed`, is locked, or — for a `workload` or
  `disruptive` Servers suite — has a Platform Claim (`409`; run that Platform's suites instead);
- the Platform does not exist (`404`), is not `active`, is not of the suite's `platformType`, or its
  module names no access Server for its spec (`409`);
- the suite is `disruptive` and `confirm` is not `true` (`400`);
- a target already has unfinished durable work, another run's included (`409`, naming the Workflow);
- the Site cannot run Ansible automation, or a Server Lock cannot be confirmed (`503`).

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

A run is kept for 90 days after it was requested (the installation's `validationRunRetention`); after
that it is `404`.

### Cancel

`POST /api/v1/validation/runs/{runId}/cancel` writes the cancel of a report run's Workflow and
returns `202` with the run, which ends `canceled` once the cancel path finished; results recorded
before stay. A run that already ended, and a gate run (cancel its deployment instead), are `409`.

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
- The targets follow the suite's kind as for a run; they are checked when the schedule is written and
  again at each occurrence.
- `status.nextRunAt` is the next cron time (`null` for `after-deployment` and while disabled);
  `lastRunId` and `lastRunAt` the latest run it started; `lastSkipReason` why the latest occurrence
  started none (a busy, locked, or not deployed target). A time missed while the worker was down runs
  once. A schedule never has two unfinished runs; an occurrence while one runs is skipped.
- Conditions: `realized` is `true` while the schedule can start runs, `false` with reason
  `suite_missing` or `target_missing` when its suite was retired or its target is gone, and `false`
  with reason `disabled` while `enabled` is `false`.
- `PUT` replaces the spec and increments `generation`; with `generation` in the body it applies only
  at that generation (`409` otherwise). `DELETE` removes the schedule; its runs stay.

## Errors

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | Malformed body or parameter, as each operation lists |
| 404 | `not_found` | The suite, run, schedule, target Server, or Platform does not exist |
| 409 | `conflict` | A module suite written, a retired suite run, a target not runnable or busy, a run already ended, a gate run canceled, or a stale `generation` |
| 503 | `provider_unavailable` | The Site cannot run Ansible automation, or a Server Lock cannot be confirmed |

## Compatibility Notes

New in 2026-11-12. The Workflow kind `run-validation` joins the workflows contract's kind enum, and
the modules contract's `kind` gains `validation`. Later the same day a suite gained `experimental`; Runs and Schedules gained optional exact `siteId` list filters.
