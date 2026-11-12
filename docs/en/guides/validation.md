# Validation

[繁體中文](../../zh-TW/guides/validation.md) · [Documentation home](../README.md)

Validation checks that deployed Servers and Platforms work, not merely that they
were deployed: clocks, CPUs, memory, and disks of a Server; the network between
Servers; AMD GPUs through the HIP runtime; whether a Kubernetes cluster runs pods
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
| `kubernetes-smoke` (experimental) | A Kubernetes Platform | Observe | Nodes Ready, a pod runs on every node that takes workloads, cluster DNS, Service routing |

In the Dashboard, **Validation → Suites** is a capability catalog grouped into Platform, Server, and custom script suites. Open a card for its checks, parameters, impact, budget, recent Runs, and Schedules. Deep links to an experimental suite remain readable.

An experimental suite is a preview: it runs like any other, but its checks may
change. The Dashboard hides experimental suites until you turn on **Show
experimental suites** on the Validation → Suites page; the setting is kept in
your browser and is separate from the experimental features setting. Runs and
schedules of an experimental suite are always shown.

A Server without an AMD GPU, a cluster without containers or MPI turned on, and
similar cases skip the checks that do not apply; a skipped check does not fail a
run.

### Script suites

You can write your own suite: a script that runs on each target Server, judged
only by its exit code — 0 is passed, any other code is failed, and running past
its timeout is an error. The last 8 KiB of its output is kept as evidence. Choose
the account it runs as (the automation user, or root), its timeout (up to an
hour), and its impact. Changing the script or these settings makes a new
revision; past runs keep the revision they ran.

The script is stored and shown as written and runs with the chosen account's
privileges on every target, so never put a password or key in it.

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
canceled. Cancel one yourself from its page or with
`swallow validation runs cancel <runId>`; the results recorded so far stay.

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

## Related

- [CLI usage: validation](../../../cli/docs/usage.md#validation)
- [Validation API contract](../../../api-server/docs/development/api-contracts/api-server/validation.md)
- [Workflows](workflows.md)
