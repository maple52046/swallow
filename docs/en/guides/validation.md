# Validation

[繁體中文](../../zh-TW/guides/validation.md) · [Documentation home](../README.md)

Validation checks that deployed Servers and Platforms work, not merely that they
were deployed: clocks, CPUs, memory, and disks of a Server; the network between
Servers; AMD GPUs through the HIP runtime and their RAS error reports; whether a Kubernetes cluster runs pods
and routes Services; whether a Slurm cluster hands out working GPUs, runs
containers, and launches MPI. You pick a **Validation Suite**, its targets, and its
parameters; swallow runs it as a Workflow and records every check's verdict on
every target in a **Validation Run**.

## Suites

A suite is a set of checks with an impact:

| Impact | Meaning |
| --- | --- |
| Observe | Only reads the targets, or probes them too lightly for running workloads to notice (a few pings, a DNS lookup, a port check). |
| Workload | Holds or saturates what workloads use (GPU compute or memory, bandwidth, scheduler nodes) for a while. |
| Disruptive | May interrupt service; each run must be confirmed, and no schedule can start one. |

swallow's modules provide suites:

| Suite | Validates | Impact | Checks |
| --- | --- | --- | --- |
| `slurm-acceptance` | A Slurm Platform | Workload | A step on every node, jobs share a node, rootless OCI containers, MPI with PMIx, GPU GRES |
| `host-baseline` (experimental) | Servers | Observe | Time synchronization with the installation's NTP server, CPU count, memory, root filesystem free and writable |
| `network-mesh` (experimental) | Servers | Observe | Every target reaches every other one, carries a full-sized unfragmented packet, and accepts SSH connections |
| `amd-gpu-basic` (experimental) | Servers | Workload | The amdgpu driver brings up every AMD GPU on PCI, and the HIP probe works on each |
| `amd-gpu-ras` (experimental) | Servers | Observe | RAS is on for every AMD GPU, no uncorrectable error is counted, and the driver's CPER error records export through AMD SMI with none fatal or uncorrected; `maxCorrectable` also limits correctable errors per GPU |
| `kubernetes-smoke` (experimental) | A Kubernetes Platform | Observe | Nodes Ready, a pod runs on every node that takes workloads, cluster DNS, Service routing |

In the Dashboard, **Validation → Suites** is a capability catalog grouped into Platform, Server, and custom script suites. Open a card for its checks, parameters, impact, budget, recent Runs, and Schedules. Deep links to an experimental suite remain readable.

An experimental suite is a preview: it runs like any other, but its checks may
change. The Dashboard hides experimental suites until you turn on **Show
experimental suites** on the Validation → Suites page; the setting is kept in
your browser and is separate from the experimental features setting. Runs and
schedules of an experimental suite are always shown. Likewise, **Hide other
users' suites** (off by default, kept in your browser) leaves out script suites
other users wrote; it never hides module suites or your own.

A Server without an AMD GPU, a cluster without containers or MPI turned on, and
similar cases skip the checks that do not apply; a skipped check does not fail a
run. `amd-gpu-ras` skips its two CPER checks on GPUs whose driver keeps no CPER
error records (GPUs before MI300, such as MI250X). A host without debugfs mounted
or without `amd-smi`, or whose AMD SMI cannot decode AFIDs, makes those checks
**Error**, with a summary that starts with `[debugfs_unavailable]`,
`[amd_smi_missing]`, or `[amd_smi_afid_unsupported]`. What the suite cannot read
is an Error too, never a guess: a RAS feature mask (`[ras_features_unreadable]`),
a GPU's error counts (`[ras_counts_unreadable]`), or the severity of a CPER record
(`[cper_severity_unknown]`). A failed export is **Failed**, and its summary starts
with `[cper_export_failed]`.

### Script suites

Every user can write a suite of their own and share it: a script that runs on
each target Server, judged only by its exit code — 0 is passed, any other code is
failed, and running past its timeout is an error. The last 8 KiB of its output is
kept as evidence. Module suites cannot be changed.

- **Who writes it.** Whoever creates a script suite is its author, shown as
  **Created by** on its card and page. Only the author edits it — not even an
  admin who is not the author. The author or an admin retires it; a retired suite
  leaves the catalog, cannot run, and its past runs and revisions stay readable. A
  suite whose author was deleted shows the original username marked
  *author deleted*; nobody can edit it, and an admin can retire it.
- **Who starts it.** The author chooses the starter level: **Pool owners** (the
  default) lets only an owner of every target's Pool start it; **Pool members
  too** lets any member of every target's Pool start it. Admins can start any
  suite. Module suites are started by Pool owners only.
- **What it runs as.** A script suite runs on each target as the Linux account of
  the user who starts the run, with that user's rights there — never as root or
  swallow's automation user. With **Run with sudo** on, the script runs through the
  starter's own `sudo -n`, so it runs only where the Pool's Sudo Policy gives the
  starter passwordless sudo; without it the script may still call `sudo -n` itself
  where the policy allows. HOME is the starter's home when it exists and belongs to
  them, otherwise a private directory removed after the run.
- **Revisions.** Changing the script, sudo, the starter level, the timeout (up to
  an hour), or the impact makes a new revision; a new name or description does
  not. Every revision is kept and readable, and past runs keep the revision they
  ran. Saving applies only to the revision you opened: if someone saved a newer
  one (for example you, in another tab), the save is refused with a conflict and
  nothing is overwritten — reload and edit the new revision.

Every user reads the script as written, and everyone who can read a run reads
its output, so never put a password, key, or other secret in either.

#### What a run needs on each target

Because the script runs as your own account, swallow checks before it accepts the
run that every target can run it as you, and refuses the run otherwise:

| Refused when | Status |
| --- | --- |
| You may not start the suite on a target (its starter level) | `403 forbidden` |
| You may not log in to a target (your Pool and the Platform's settings decide; see [Zones and Pools](servers-and-infrastructure.md#zones-and-pools)) | `403 forbidden` |
| The suite uses sudo and the Sudo Policy gives you no sudo on a target | `403 forbidden` |
| The suite uses sudo and your sudo on a target needs a password | `409 conflict` |
| No Directory is configured, your account has no uid, or a target has no [directory client](managed-software.md#directory-client-sssd) (every non-Ubuntu Server) | `409 conflict` |

The error's message names the target. A host can still disagree with what swallow
saw, for example when a sudo rule has not reached it yet (rules take about a
minute) or a local account has your username. That target's result is then
**Error**, and its summary starts with a code:

| Code | Meaning |
| --- | --- |
| `[validation_account_missing]` | The host has no account with your username. |
| `[validation_account_mismatch]` | The account with your username on the host has another uid than yours. |
| `[validation_become_failed]` | The automation user may not run commands as your account on the host. |
| `[validation_sudo_unavailable]` | The suite uses sudo and your `sudo -n` failed on the host; sudo's message follows. |
| `[validation_script_unplaced]` | The script could not be put on the host. |

## Run a suite

In the Dashboard, open **Validation → Runs → Run a suite**, **Run suite** on a Suite detail page, or **Run a suite** in the Validation section of a Server or Platform. The shared flow first chooses the Suite and targets, then shows parameters, impact, budget, and the final review. In the all-Sites scope, a multi-Server run is restricted to the Site of its first selected Server. With the
CLI:

```bash
swallow validation runs start --suite host-baseline --server srv1,srv2 --wait
swallow validation runs start --suite slurm-acceptance --platform plat1 --wait
```

swallow refuses a run whose targets are not deployed, are locked, or are busy
with another Workflow, and a Workload or Disruptive suite on a Server a Platform
claims (validate the Platform instead). A run never blocks your work: a
deployment, release, or software change that needs a Server under validation
cancels the run and proceeds. A run that exceeds its suite's time budget is
canceled. The user who started a run, an owner of every target's Pool, or an admin
can cancel it from its page or with `swallow validation runs cancel <runId>`; the
results recorded so far stay.

### Running a changed script suite

Before you run a script suite, the Dashboard shows its author, revision, script,
and that it **runs as your account**; for a sudo suite it says the run needs your
passwordless sudo on every target, under the Pool's Sudo Policy. If the suite changed
since your own last run of it, the run dialog first shows **Changed since your
last run: rN → rM**: every changed setting and a line-by-line diff of the
scripts (or the whole new script, when the earlier revision predates kept
revisions). **Use the new version** is the only way on; the run is then pinned to
that revision. If the author saves another revision while the dialog is open, the
start is refused with a conflict and the dialog shows the new change to review.
A run never executes an older revision than the current one.

With the CLI, read a revision and pin the one you reviewed:

```bash
swallow validation runs list --suite <suiteId> --mine --page-size 1   # the revision you last ran
swallow validation suites revision <suiteId> <revision>                # its script and settings
swallow validation runs start --suite <suiteId> --server srv1 --suite-revision <revision> --wait
```

A start whose `--suite-revision` is no longer the current revision is refused
with `409 conflict`.

## Read the results

A run page lists **Failed** and **Error** issues first with their target, summary, measurements, and expandable evidence. Failed means the check completed and found a mismatch; Error means it could not complete a judgment. The complete check-by-target matrix remains below for audit, including Passed and Skipped results; selecting a verdict expands its inspector beside that check. The run links to its Workflow, which has the
Task logs.

A completed run's verdict is **Failed** when any check failed, **Error** when
none failed but some could not be judged (a host was unreachable, a probe was
missing), and **Passed** otherwise. Runs are kept for 90 days by default.

`swallow validation runs watch <runId>` follows a run and exits 0 only when its
verdict is passed, so scripts can gate on it.

## Deployments run their acceptance suite

Every Slurm deployment runs `slurm-acceptance` as its gate, right after Slurm is
installed: if GPUs, containers, or MPI do not work, the deployment stops and asks
for attention, and GPU nodes that fail are drained. The gate's run is listed with
the others (started by **Deployment gate**). Retrying the deployment's validation
Job reruns the whole suite after you fix the cause.

## Schedules

A schedule starts runs on its own: **After each deployment**, **Daily**, **Weekly**, or a five-field **Custom cron**, all in an IANA time zone. Existing expressions that do not match Daily or Weekly remain unchanged in Custom.
A run starts within a minute of its time or of the deployment. A time missed while swallow was down runs once when it is back. A schedule never
starts a second run while its previous one is unfinished, and skips an occurrence
whose targets are busy, locked, or not deployed; it shows the reason. In the
Dashboard use **Validation → Schedules**; with the CLI,
`swallow validation schedules create -f schedule.yaml`.

Schedules are an admin's, and they start module suites only: a script suite runs
as the user who starts it, and a scheduled run has no such user. A schedule
written before this rule that names a script suite starts no run and says why
(**Script suite not schedulable**); edit it to use a module suite, or delete it.

## Related

- [CLI usage: validation](../../../cli/docs/usage.md#validation)
- [Validation API contract](../../../api-server/docs/development/api-contracts/api-server/validation.md)
- [Workflows](workflows.md)
