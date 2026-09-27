# Workflows

[繁體中文](../../zh-TW/guides/workflows.md) · [Documentation home](../README.md)

A Workflow is the durable record of operator intent and progress. Creating an
OS deployment, Platform lifecycle action, Managed Software change, or diagnostic
operation returns before all work is complete; the Workflow is where completion
and failure are determined.

## Structure

- **Workflow:** end-to-end desired outcome and resource ownership.
- **Job:** reusable convergent sub-goal.
- **Task:** atomic idempotent unit assigned to one Runner.
- **Runner:** provisioner, Ansible, or internal execution mechanism.

The wire model retains some historical operation/step field names. The
Dashboard and current `/api/v1/workflows` routes use canonical Workflow/Task
language.

## Runtime topology

Temporal is the sole durable orchestration engine. A functional installation
requires:

- Temporal Server and PostgreSQL;
- `swallow-api worker`;
- `swallow-api ansible-executor` for Ansible Tasks;
- the API and its MongoDB state.

There is no in-process dispatcher fallback.

## Observe work

The Workflow detail shows:

- overall state, intent, target resources, requester, and timestamps;
- Jobs and Tasks grouped by execution structure;
- task events, stdout/stderr logs, and artifacts;
- cancellation, rerun, and task-retry controls when state permits.

Live Ansible events may arrive before the final Task state. Treat the terminal
Workflow result as authoritative.

## Failure handling

1. Read the failed Task's status reason and events.
2. Inspect logs and request IDs without exposing credentials.
3. Correct the external cause: provider availability, SSH known host, package
   source, runtime API, or resource eligibility.
4. Retry only the Task when supported; rerun the Workflow when the whole intent
   must be reevaluated.

An expired or lost execution can become `indeterminate`; it is never retried
automatically because swallow cannot prove whether the external side effect
occurred.

## Cancellation

Cancellation records intent and asks the running orchestration to stop at a safe
boundary. It cannot undo an external side effect that already completed. Read
the resulting task states before starting another conflicting Workflow.

See the active
[Workflows contract](../../../api-server/docs/development/api-contracts/api-server/workflows.md)
for state transitions and supported controls.
