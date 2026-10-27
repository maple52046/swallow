# 064. Swallow prefers choosing upstream versions to owning patches

- Status: Accepted
- Date: 2026-10-27

## Context

Swallow deploys software it does not develop (Slurm, k0s, the AMD GPU Operator, Prometheus,
Grafana, Omnistat, Docker CE, Podman). When that software misbehaves on the hardware or topology
Swallow targets, there are two ways forward: deploy a different upstream version or commit, or
patch the software at deploy time.

Swallow already owned one patch: Omnistat v1.15.0 aborts its exporter on SR-IOV virtual functions
(for example an MI308X VF, which rejects `amdsmi_get_gpu_metrics_info`), so `roles/omnistat`
applies a patch that degrades that path instead (decision 063). On 2026-10-27 a second defect
surfaced: Omnistat v1.15.0's `omnistat-query` builds the step `"60.0s"` for jobs shorter than
five minutes, which Prometheus rejects.

The user weighed both sides. Deploying where other platforms cannot is part of Swallow's value,
and a small patch is sometimes what makes that possible. But every patch is a combination of
upstream version and fix that Swallow must keep working, and many such combinations would breed
problems of their own.

## Decision

- **Prefer upstream.** To get software working, Swallow first chooses an upstream release or
  commit that works, and maintains the combinations it deploys rather than fixes inside them.
- **Patches are allowed, as the exception.** Swallow may own a patch when it is what makes a
  deployment work, or fixes a defect that breaks a feature Swallow deploys, and no upstream
  version or commit does. An owned patch:
  - fixes one problem, in its own file pinned to one upstream version
    (`<software>-<version>-<topic>.patch`);
  - starts with a header naming the problem, the symptom without it, and when to re-check;
  - is checked and applied on its own and idempotently, so a patch added later reaches nodes that
    already carry the earlier ones;
  - never fabricates data (a missing metric stays missing);
  - is listed in the component's development documentation with how to tell, on a newer upstream
    version, whether it is still needed. A version bump re-checks every patch and drops the ones
    upstream has fixed.
- **Upstream problems Swallow does not patch are reported, not hidden.** The deployment completes
  when Swallow's own part works, and the limitation is stated to the user (contract, guides).

## Alternatives considered

- **Never own patches.** Rejected by the user: it gives up deployments that only a small fix makes
  possible, which is part of what Swallow offers.
- **Patch whenever something misbehaves.** Rejected: each patch multiplies the version
  combinations Swallow maintains, and patches drift silently as upstream changes.

## Consequences

- Omnistat v1.15.0 carries two patches, `omnistat-1.15.0-sriov-vf.patch` and
  `omnistat-1.15.0-query-step.patch`; `docs/development/platform-deployment.md` lists both with
  their re-check method, and a test fails when a patch the role applies is not listed there.
- `omnistat-query` still cannot build a report on SR-IOV virtual functions, because its report is
  built on GPU utilization those functions do not provide; that is reported, not patched.
- Choosing a different upstream version stays the first answer, so a later Omnistat version or
  commit that works on virtual functions should replace the patches rather than carry them.

## Current status

Implemented in `api-server` (`roles/omnistat`) and the development documentation.
