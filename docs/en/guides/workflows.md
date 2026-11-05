# Workflows

[繁體中文](../../zh-TW/guides/workflows.md) · [Documentation home](../README.md)

A Workflow is the durable record of user intent and progress. Creating an
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

swallow's store holds each Workflow's plan and state; Temporal is the Workflow executor that
runs it durably. A functional installation requires:

- Temporal Server and PostgreSQL;
- `swallow-api worker`;
- `swallow-api ansible-executor` for Ansible Tasks;
- the API and its MongoDB state.

There is no in-process dispatcher fallback.

## Observe work

The Workflow detail shows:

- overall state, intent, target resources, requester, and timestamps;
- Jobs and Tasks grouped by execution structure;
- the selected Task's failure summary or waiting notice, then its task events,
  stdout/stderr logs, and artifacts;
- a timeline of Workflow and Task state changes;
- cancellation, rerun, and task-retry controls when state permits.

The page selects the first Task that is running, failed, or in
`requires_attention`, and a Task with an error opens on its **Stderr** tab.
While the Workflow is active, the page reads it again every few seconds.

A running Task that waits for something outside swallow can report what it
observes. The Task then shows a notice titled **Waiting:** followed by its
latest observation, with how long it has waited against its budget, how much of
the budget is left, how many observations it has made, and when it last
observed; a wait without a budget shows only the time waited. The Task's
**Reason** column in the Steps table shows the same observation. The numbers
are those of the last observation, not a running clock. A Task whose Runner
does not report its waits shows no notice, and the notice disappears when the
attempt ends.

Live Ansible events and the waiting notice may arrive before the final Task
state; both are advisory. Treat the terminal Workflow result as authoritative.

## Failure handling

A Task that failed, or stopped in `requires_attention`, shows a failure summary
above its tabs, in the order you need it:

- what happened: the first line of the error message, as the summary's title
  (the **Stderr** tab has the full failure output);
- **Affected**: the Server or Platform the failure affects, linked to its page,
  when the Runner knows which of the Task's targets it is;
- **Next step**: what to do next, when the Runner recorded it;
- the error code and, when there is one, the stage.

**Show diagnostic details** opens the evidence behind the summary: the failure
class and what it means, the operation and endpoint that failed, the attempts
of the wait that gave up and the time it ran against its budget, its last
observation, the cause with its line breaks kept, and the events the provider
recorded about the target around the failure. Only what the Runner
recorded is listed. When a wait gives up, its last observation and numbers move
here from the waiting notice. Before swallow stores a failure, it masks secrets
in these texts as `[REDACTED]` and keeps the rest of the cause; still treat the
details as administrator information. An error recorded before swallow kept
these details, or by a Runner that does not record them, shows only its message
and code, without **Show diagnostic details**.

The failure class tells you whether a retry is worth trying before you change
anything:

- **Transient**: the condition is expected to clear by itself, such as a host
  still booting or an API that was briefly unreachable. Retrying the Task can
  succeed without a change.
- **Permanent**: the condition repeats until something changes, such as
  rejected credentials, a missing image, or invalid input. Fix the cause first;
  a retry without a change fails again.
- **Unknown**: swallow could not classify the failure, including an attempt
  whose outcome cannot be proven. Check the target before you retry.

The class does not decide whether you can retry: the Task's retry button
appears only when its error is retryable, whatever the class.

The Task keeps only its latest error, so after a retry the timeline is where
earlier attempts stay readable. Each event that records an attempt's failure
shows the attempt number, the error code, the failure class, and the endpoint
when there was one, followed by the first line of that attempt's message. Events recorded before
swallow kept failure classes carry none.

To handle a failure:

1. Read the failed Task's failure summary; open its diagnostic details, its
   **Stderr** tab, and its events when you need the evidence.
2. Use the failure class to decide whether to retry first or to fix the cause
   first.
3. Inspect logs and request IDs without exposing credentials.
4. Correct the external cause: provider availability, SSH known host, package
   source, runtime API, or resource eligibility.
5. Retry only the Task when supported; rerun the Workflow when the whole intent
   must be reevaluated.

A rerun converges the intent to what it describes now. Every ensure Task runs
again and first checks the current state: a Server already deployed with the
requested image and reachable over SSH completes in seconds, and its deployment
result is updated; a failed Server is returned to Ready and deployed again. The
Platform's configuration playbook also runs again, which takes its usual time.

If the Ansible executor is lost while a playbook runs, the Task stops in
`requires_attention` with reason `executor_outcome_unknown`. swallow cannot prove
whether the playbook's changes happened, so it never reruns it automatically;
retry the Task once you have checked the hosts.

## Cancellation

Cancellation records intent and asks the running orchestration to stop at a safe
boundary. It cannot undo an external side effect that already completed. Read
the resulting task states before starting another conflicting Workflow.

Canceling never changes an OS deployment that already finished: a Server that
deployed keeps its result, and so does one that failed. When the cancel
interrupts an OS deployment in progress, swallow stops it and returns that
Server to Ready, keeping its static IP bindings and without erasing disks,
before the Workflow ends. The Task's timeline says whether the Server was
returned or why it was left as it is; a Server that could not be returned can be
recovered with Recover.

## Change Plans

A destructive change, such as uninstalling a Platform, goes through a Change Plan:
every action swallow will take, which actions are destructive, the expected effects,
and anything blocking it. The dialog you confirm in the dashboard shows the same
consequences, and confirming it approves the plan, so the change starts at once; the
CLI does the same when you add `--approve`. The plan stays on the **Change Plans**
page as the record of what was decided, by whom, and which Workflow carried it out.

A plan waits on **Change Plans** for an admin to approve or reject it as a whole
(there, or with `swallow plans approve` / `swallow plans reject`) when it was sent
without approval (for example by the CLI without `--approve`), when it has blockers,
when swallow proposed it on its own, or while the safety switch is on. Each waiting
plan says why it waits. After approval, swallow checks the plan again and starts its
Workflow; if the change was edited again in the meantime the plan is superseded, and
if what the plan would do changed, a new plan waits for approval. A plan nobody
decides expires after 24 hours.

The **Approval of confirmed changes** card at the top of the page is the safety
switch. Turning on **Require a separate approval for every Change Plan** makes every
destructive change wait there, even one you confirmed; turning it off again affects
only the changes submitted afterwards. The CLI reads and sets it with
`swallow plans policy`.

The page names each Platform and Server a plan refers to, with its ID beside it,
and shows the plan's ID with a copy button. While a plan is approved, the page
refreshes on its own until the plan is applied or superseded. See the
[Change Plans contract](../../../api-server/docs/development/api-contracts/api-server/change-plans.md).

See the active
[Workflows contract](../../../api-server/docs/development/api-contracts/api-server/workflows.md)
for state transitions, supported controls, and every field of a Task's failure
details and waiting notice.
